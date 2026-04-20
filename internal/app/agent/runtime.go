// Package agent provides the Runtime struct that owns the full plugin lifecycle,
// dependency injection, and agent loop wiring. main.go creates a Runtime, starts
// it, and shuts it down on signal.
package agent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/compaction"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/conversation"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/llm"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/loop"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/mapper"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/outbound"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/prompt"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/scheduler"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/skill"
	"github.com/whiteagent-org/whiteagent/internal/infra/loader"
	"github.com/whiteagent-org/whiteagent/internal/infra/scopedfs"
	"github.com/whiteagent-org/whiteagent/pkg/runtime"
)

// Runtime owns the full plugin lifecycle, dependency injection, service
// creation, and agent loop wiring. It does NOT own the HTTP gateway.
type Runtime struct {
	cfg       *config.Config
	state     *runtime.RuntimeState
	mgr       *loader.Manager
	registry  *loader.Registry
	transport port.TransportPlugin
	store     port.StorePlugin
	sandbox   port.SandboxPlugin
	secretSvc secret.SecretService
	channels  map[string]port.ChannelEntry
	loop      *loop.Loop
	scheduler *scheduler.Scheduler
	skillSvc  *skill.Service
}

// NewRuntime creates a Runtime that will use the given config and state.
// Call Start() to load plugins and wire everything up.
func NewRuntime(cfg *config.Config, state *runtime.RuntimeState) *Runtime {
	return &Runtime{
		cfg:   cfg,
		state: state,
	}
}

// Channels returns the channel entry map for gateway route registration.
func (rt *Runtime) Channels() map[string]port.ChannelEntry {
	return rt.channels
}

// AllPlugins returns every loaded plugin for gateway health reporting.
func (rt *Runtime) AllPlugins() []port.Plugin {
	if rt.registry == nil {
		return nil
	}
	return rt.registry.All()
}

// Loop returns the agent loop for external callback wiring (e.g. secret form notifications).
func (rt *Runtime) Loop() *loop.Loop { return rt.loop }

// SecretService returns the secret service for gateway route registration.
func (rt *Runtime) SecretService() secret.SecretService { return rt.secretSvc }

// SkillService returns the skill service for downstream phases (prompt/tools).
func (rt *Runtime) SkillService() *skill.Service { return rt.skillSvc }

// Start loads all plugins, wires dependencies, creates domain services, and
// starts the plugin lifecycle. On any failure, already-started components are
// cleaned up before the error is returned.
func (rt *Runtime) Start(ctx context.Context) error {
	// -----------------------------------------------------------------------
	// Plugin loading
	// -----------------------------------------------------------------------

	// Create a registry and register built-in tools first. .so plugins loaded
	// afterward will replace any built-in with a matching config ID.
	registry := loader.NewRegistry()
	builtins := builtinTools(rt.cfg.Agent, rt.cfg.Runtime)
	for _, bt := range builtins {
		if err := registry.Register(bt.plugin, bt.pluginID); err != nil {
			return fmt.Errorf("register built-in tool %s: %w", bt.pluginID, err)
		}
	}

	builtinMWs := builtinMiddlewares()
	for _, bm := range builtinMWs {
		if err := registry.Register(bm.plugin, bm.pluginID); err != nil {
			return fmt.Errorf("register built-in middleware %s: %w", bm.pluginID, err)
		}
	}

	ldr := loader.NewLoader()
	if err := ldr.LoadInto(registry, rt.cfg.LoaderEntries()); err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}
	rt.registry = registry

	mgr := loader.NewManager(registry)
	rt.mgr = mgr

	// Merge built-in tool configs into the config map so Init receives them.
	cfgs := rt.cfg.ConfigsByID()
	for _, bt := range builtins {
		if bt.config != nil {
			cfgs[bt.pluginID] = bt.config
		}
	}

	// Init all plugins (store first, then transport, channels, LLM, tools, middleware).
	if err := mgr.Init(ctx, cfgs); err != nil {
		return fmt.Errorf("init plugins: %w", err)
	}

	// -----------------------------------------------------------------------
	// Extract typed plugin instances from registry
	// -----------------------------------------------------------------------

	transportID := config.QualifyID("transport", rt.cfg.Transport.PluginID)
	transportPlugin, ok := registry.Get(transportID).(port.TransportPlugin)
	if !ok || transportPlugin == nil {
		return fmt.Errorf("transport plugin %q not found or wrong type", transportID)
	}
	rt.transport = transportPlugin

	storeID := config.QualifyID("store", rt.cfg.Store.PluginID)
	storePlugin, ok := registry.Get(storeID).(port.StorePlugin)
	if !ok || storePlugin == nil {
		return fmt.Errorf("store plugin %q not found or wrong type", storeID)
	}
	rt.store = storePlugin

	// -----------------------------------------------------------------------
	// Create SecretService
	// -----------------------------------------------------------------------

	encKey, err := hex.DecodeString(rt.cfg.Runtime.EncryptionKey)
	if err != nil {
		return fmt.Errorf("decode encryption key: %w", err)
	}
	publicURL := rt.cfg.Gateway.PublicURL
	if publicURL == "" {
		publicURL = "http://" + rt.cfg.Gateway.Address
	}
	secretService, err := secret.NewService(storePlugin, encKey, publicURL, *rt.cfg.Runtime.RedactSecrets)
	if err != nil {
		return fmt.Errorf("create secret service: %w", err)
	}
	rt.secretSvc = secretService

	// Build LLM plugin map with bare endpoint IDs (not "llm." prefix).
	llmPlugins := make(map[string]port.LLMPlugin)
	for _, drv := range rt.cfg.LLM.Drivers {
		for _, ep := range drv.Endpoints {
			if ep.Enabled {
				pluginID := config.QualifyID("llm", ep.ID)
				p := registry.Get(pluginID)
				if lp, ok := p.(port.LLMPlugin); ok {
					llmPlugins[ep.ID] = lp
				}
			}
		}
	}
	if len(llmPlugins) == 0 {
		return fmt.Errorf("no LLM plugins loaded")
	}

	// Build tool map from registry (contains both built-in and .so tools;
	// duplicates already resolved by warn-and-replace during registration).
	toolMap := make(map[string]port.ToolPlugin)
	for _, p := range registry.ByKind(entity.PluginKindTool) {
		if t, ok := p.(port.ToolPlugin); ok {
			toolMap[t.Name()] = t
		}
	}

	// -----------------------------------------------------------------------
	// Extract sandbox plugin from registry
	// -----------------------------------------------------------------------

	sandboxID := config.QualifyID("sandbox", rt.cfg.Sandbox.PluginID)
	sandboxRaw := registry.Get(sandboxID)
	sandboxPlugin, ok := sandboxRaw.(port.SandboxPlugin)
	if !ok || sandboxPlugin == nil {
		return fmt.Errorf("sandbox plugin %q not found or wrong type", sandboxID)
	}
	rt.sandbox = sandboxPlugin

	// -----------------------------------------------------------------------
	// Create ConversationService
	// -----------------------------------------------------------------------

	convService := conversation.NewService(storePlugin)

	// -----------------------------------------------------------------------
	// Normalize paths to absolute
	// -----------------------------------------------------------------------

	absDataDir, err := filepath.Abs(rt.cfg.Runtime.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data_dir: %w", err)
	}
	rt.cfg.Runtime.DataDir = absDataDir

	absSkillsDir, err := filepath.Abs(rt.cfg.Runtime.SkillsDir)
	if err != nil {
		return fmt.Errorf("resolve skills_dir: %w", err)
	}
	rt.cfg.Runtime.SkillsDir = absSkillsDir

	// -----------------------------------------------------------------------
	// Create SkillService
	// -----------------------------------------------------------------------

	rt.skillSvc = skill.New(rt.cfg.Runtime.SkillsDir, rt.cfg.Runtime.DataDir, storePlugin)
	slog.Info("skill.service.created", "global_dir", rt.cfg.Runtime.SkillsDir, "data_dir", rt.cfg.Runtime.DataDir)

	// -----------------------------------------------------------------------
	// Inject dependencies into all plugins (unified loop)
	// -----------------------------------------------------------------------

	for _, p := range registry.All() {
		if sa, ok := p.(port.StoreAware); ok {
			sa.SetStore(storePlugin)
		}
		if ta, ok := p.(port.TransportAware); ok {
			ta.SetTransport(transportPlugin)
		}
		if ca, ok := p.(port.ConversationAware); ok {
			ca.SetConversationResetter(convService)
		}
		if sa, ok := p.(port.SandboxAware); ok {
			sa.SetSandbox(sandboxPlugin)
		}
		if sa, ok := p.(port.SecretAware); ok {
			sa.SetSecretService(secretService)
		}
	}

	// -----------------------------------------------------------------------
	// Inject middleware into transport (if MiddlewareAware)
	// -----------------------------------------------------------------------

	if ma, ok := transportPlugin.(port.MiddlewareAware); ok {
		mwIDs := ma.MiddlewareIDs()
		seen := make(map[string]bool, len(mwIDs))
		mws := make([]port.MiddlewarePlugin, 0, len(mwIDs))
		// Config-driven middleware first (preserves user-specified order).
		for _, id := range mwIDs {
			qualifiedID := "middleware." + id
			seen[qualifiedID] = true
			slog.Debug("runtime.middleware.config", "id", qualifiedID)
			p := registry.Get(qualifiedID)
			if mw, ok := p.(port.MiddlewarePlugin); ok {
				mws = append(mws, mw)
			} else {
				slog.Warn("middleware plugin not found in registry", "id", qualifiedID)
			}
		}
		// Append built-in middleware not already referenced by config.
		for _, p := range registry.ByKind(entity.PluginKindMiddleware) {
			id := registry.ConfigID(p)
			if !seen[id] {
				slog.Debug("runtime.middleware.builtin", "id", id)
				if mw, ok := p.(port.MiddlewarePlugin); ok {
					mws = append(mws, mw)
				}
			}
		}
		ma.SetMiddleware(mws)
	}

	// -----------------------------------------------------------------------
	// Extract channel plugins
	// -----------------------------------------------------------------------

	channelMap := make(map[string]port.ChannelEntry)
	for _, p := range registry.ByKind(entity.PluginKindChannel) {
		if ch, ok := p.(port.ChannelPlugin); ok {
			var caps port.ChannelCapabilities
			if _, ok := p.(port.IndicatorAware); ok {
				caps.Indication = true
			}
			if _, ok := p.(port.ReactionAware); ok {
				caps.Reactions = true
			}
			channelMap[ch.ID()] = port.ChannelEntry{Plugin: ch, Capabilities: caps}
		}
	}
	rt.channels = channelMap

	// -----------------------------------------------------------------------
	// Create domain services
	// -----------------------------------------------------------------------

	onboardingSvc := onboarding.NewService(storePlugin)
	identityResolver := identity.NewResolver(storePlugin, onboardingSvc)
	msgMapper := mapper.NewMapper(storePlugin, identityResolver)

	// -----------------------------------------------------------------------
	// Create global ScopedFS and inject into channel plugins
	// -----------------------------------------------------------------------

	sfs := scopedfs.New(rt.cfg.Runtime.DataDir)
	if fsa, ok := sandboxPlugin.(port.ScopedFSAware); ok {
		fsa.SetScopedFS(sfs)
	}
	for _, entry := range channelMap {
		if fsa, ok := entry.Plugin.(port.ScopedFSAware); ok {
			fsa.SetScopedFS(sfs)
		}
	}

	// -----------------------------------------------------------------------
	// Set inbound handler on each channel plugin
	// -----------------------------------------------------------------------

	for id, entry := range channelMap {
		chID := id         // capture for closure
		ch := entry.Plugin // capture for closure
		ch.SetMessageHandler(func(ctx context.Context, incoming dto.IncomingMessage) error {
			msg, replyConvID, err := msgMapper.ToMessage(ctx, incoming, chID)
			if err != nil {
				if errors.Is(err, identity.ErrUnknownGroup) {
					if !incoming.IsMention {
						return nil // not addressed to bot, silent discard
					}
					// Treat as unknown user for onboarding purposes
					err = identity.ErrUnknownUser
				}
				if errors.Is(err, identity.ErrUnknownUser) {
					// For groups, mapper returns partial identity with TenantID resolved.
					// For DMs, tenantID is empty (TryJoin resolves via workspace).
					var tenantID entity.TenantID
					if incoming.IsGroup {
						tenantID = msg.TenantID
					}
					result, rejectionMsg, joinErr := onboardingSvc.TryJoin(ctx, chID, incoming, tenantID)
					if joinErr != nil {
						if errors.Is(joinErr, onboarding.ErrRejected) {
							_, _ = ch.Send(ctx, dto.OutgoingMessage{
								ChannelID: chID,
								Content:   rejectionMsg,
								Delivery:  incoming.Delivery,
								TargetID:  incoming.ID,
							})
							slog.Info("onboarding.rejected", "channel", chID, "chat", incoming.ChatID, "user", incoming.UserID)
						} else {
							slog.Error("onboarding.try_join_failed", "err", joinErr, "channel", chID)
						}
						return nil
					}
					// Send feedback message after successful join.
					if feedback := onboarding.FeedbackMessage(result.Action); feedback != "" {
						_, _ = ch.Send(ctx, dto.OutgoingMessage{
							ChannelID: chID,
							Content:   feedback,
							Delivery:  incoming.Delivery,
							TargetID:  incoming.ID,
						})
					}
					slog.Info("onboarding."+result.Action, "channel", chID, "chat", incoming.ChatID, "user", incoming.UserID)
					if result.Action != "auto_joined" {
						return nil
					}
					// Re-resolve identity so the first message reaches the agent.
					msg, replyConvID, err = msgMapper.ToMessage(ctx, incoming, chID)
					if err != nil {
						slog.Warn("identity.re_resolve_failed", "err", err, "channel", chID)
						return nil
					}
					goto processMessage
				}
				slog.Warn("identity.resolve_failed", "err", err, "channel", chID)
				return nil
			}
		processMessage:
			// If reply resolved to a different conversation, switch the cache
			// so subsequent non-reply messages continue in the switched conversation.
			if !replyConvID.IsEmpty() {
				convService.SwitchConversation(msg, replyConvID)
			}

			// Resolve conversation for all messages (enables GroupMode filtering
			// and pre-sets ConversationID so agent loop skips re-resolution).
			convID, convErr := convService.ResolveConversation(ctx, msg)
			if convErr != nil {
				slog.Warn("conversation.resolve_failed", "err", convErr, "channel", chID)
				return nil
			}

			// Relocate attachment files to 3-level path and store relative paths.
			if len(msg.Attachments) > 0 {
				relocateAttachments(sfs, msg.TenantID, convID, msg.ID, msg.Attachments)
			}

			// GroupMode filtering: save non-mentioned group messages to history
			// without invoking the agent loop when tenant policy is mention_only.
			if msg.IsGroup && !msg.IsMention {
				tenant, tErr := storePlugin.GetTenant(ctx, msg.TenantID)
				if tErr != nil {
					slog.Warn("tenant.load_failed", "err", tErr, "tenant_id", msg.TenantID)
					return nil
				}
				if tenant != nil && tenant.GroupMode == entity.GroupModeMentionOnly {
					if appendErr := convService.Append(ctx, convID, msg); appendErr != nil {
						slog.Warn("conversation.append_failed", "err", appendErr)
					}
					return nil // silent drop -- saved to history but no agent invocation
				}
			}

			msg.ConversationID = convID
			rt.skillSvc.EnsureSync(msg.TenantID)
			return transportPlugin.Publish(ctx, entity.TopicInbound, msg)
		})
	}

	// -----------------------------------------------------------------------
	// Create and subscribe outbound router
	// -----------------------------------------------------------------------

	outboundHandler := outbound.NewHandler(channelMap, msgMapper, storePlugin)
	if err := transportPlugin.Subscribe(entity.TopicOutbound, outboundHandler); err != nil {
		return fmt.Errorf("subscribe outbound router: %w", err)
	}

	// -----------------------------------------------------------------------
	// Create and wire agent loop
	// -----------------------------------------------------------------------

	router, err := llm.NewRouter(rt.cfg.LLM.Routing.Primary, rt.cfg.LLM.Routing.Fallback)
	if err != nil {
		return fmt.Errorf("create model router: %w", err)
	}

	cooldownDuration := time.Duration(rt.cfg.LLM.Routing.CooldownSeconds) * time.Second
	completionSvc := llm.NewCompletionService(router, llmPlugins, cooldownDuration)
	var compactionSvc *compaction.Service
	if rt.cfg.LLM.Compaction != nil {
		compactionSvc, err = compaction.New(completionSvc, convService, storePlugin, rt.cfg.LLM.Compaction)
		if err != nil {
			return fmt.Errorf("create compaction service: %w", err)
		}
	}

	userResolver := identity.NewUserResolver(storePlugin)

	promptBuilder, err := prompt.NewPromptBuilder(toolMap, convService, storePlugin, userResolver, rt.cfg.Agent.TokenBudget, rt.cfg.LLM.Compaction, rt.skillSvc, sandboxPlugin)
	if err != nil {
		return fmt.Errorf("create prompt builder: %w", err)
	}

	loopCfg, err := loop.ParseLoopConfig(rt.cfg.Agent)
	if err != nil {
		return fmt.Errorf("parse loop config: %w", err)
	}

	agentLoop := loop.NewLoop(loopCfg, completionSvc, storePlugin, convService, transportPlugin, toolMap, promptBuilder, compactionSvc, channelMap, secretService, sandboxPlugin, sfs)
	rt.loop = agentLoop

	if err := transportPlugin.Subscribe(entity.TopicInbound, agentLoop.Handle); err != nil {
		return fmt.Errorf("subscribe loop to inbound: %w", err)
	}

	// -----------------------------------------------------------------------
	// Create scheduler
	// -----------------------------------------------------------------------

	pollInterval, err := time.ParseDuration(rt.cfg.Runtime.SchedulerPollInterval)
	if err != nil {
		return fmt.Errorf("parse scheduler_poll_interval %q: %w", rt.cfg.Runtime.SchedulerPollInterval, err)
	}
	rt.scheduler = scheduler.New(storePlugin, transportPlugin, sfs, pollInterval, rt.cfg.Runtime.Timezone)

	// -----------------------------------------------------------------------
	// Start all plugins (transport starts dispatch goroutines)
	// -----------------------------------------------------------------------

	if err := mgr.Start(ctx); err != nil {
		rt.cleanup(ctx)
		return fmt.Errorf("start plugins: %w", err)
	}

	// Start scheduler after plugins so transport is ready for Publish.
	rt.scheduler.Start(ctx)

	slog.Info("runtime.started")
	return nil
}

// Shutdown gracefully drains the runtime in the correct order:
// 1. Stop channel plugins (no new inbound messages)
// 2. Drain agent loop (finish in-flight turns)
// 3. Stop remaining plugins in reverse init order via manager
func (rt *Runtime) Shutdown(ctx context.Context) error {
	rt.state.Set(runtime.StateDraining)
	slog.Info("runtime.state", "state", "draining")

	var errs []error

	// Step 1: Stop channel plugins explicitly.
	for id, entry := range rt.channels {
		if err := entry.Plugin.Stop(ctx); err != nil {
			slog.Error("runtime.shutdown.channel_stop", "channel", id, "err", err)
			errs = append(errs, fmt.Errorf("stop channel %s: %w", id, err))
		} else {
			slog.Info("runtime.shutdown.channel_stopped", "channel", id)
		}
	}

	// Step 1.5: Stop scheduler (no new cron messages).
	if rt.scheduler != nil {
		rt.scheduler.Stop()
		slog.Info("runtime.shutdown.scheduler_stopped")
	}

	// Step 2: Drain agent loop.
	if rt.loop != nil {
		if err := rt.loop.Shutdown(ctx); err != nil {
			slog.Error("runtime.shutdown.loop_drain", "err", err)
			errs = append(errs, fmt.Errorf("drain loop: %w", err))
		} else {
			slog.Info("runtime.shutdown.loop_drained")
		}
	}

	// Step 3: Stop remaining plugins in reverse init order.
	if rt.mgr != nil {
		if err := rt.mgr.Stop(ctx); err != nil {
			slog.Error("runtime.shutdown.plugins_stop", "err", err)
			errs = append(errs, fmt.Errorf("stop plugins: %w", err))
		} else {
			slog.Info("runtime.shutdown.plugins_stopped")
		}
	}

	rt.state.Set(runtime.StateStopped)
	slog.Info("runtime.state", "state", "stopped")

	return errors.Join(errs...)
}

// relocateAttachments moves attachment files from their temp download directories
// to the canonical 3-level messages/{tenantID}/{convID}/{msgID}/ directory.
// After relocation, stores relative paths in the entity so the DB persists
// relative paths (e.g. {tenantID}/{convID}/{msgID}/filename).
func relocateAttachments(sfs *scopedfs.FS, tenantID entity.TenantID, convID entity.ConversationID, msgID entity.MessageID, attachments []entity.Attachment) {
	targetDir, err := sfs.EnsureMessageDir(string(tenantID), string(convID), string(msgID))
	if err != nil {
		slog.Warn("relocate_attachments: ensure dir", "err", err, "msg_id", msgID)
		return
	}
	for i := range attachments {
		oldPath := attachments[i].Path
		if oldPath == "" {
			continue
		}
		newPath := filepath.Join(targetDir, filepath.Base(oldPath))
		if err := os.Rename(oldPath, newPath); err != nil {
			slog.Warn("relocate_attachments: rename", "err", err, "from", oldPath, "to", newPath)
			continue
		}
		// Store relative path in entity -- DB will persist this relative path.
		// Format: {tenantID}/{convID}/{msgID}/{filename}
		relPath := filepath.Join(string(tenantID), string(convID), string(msgID), filepath.Base(oldPath))
		attachments[i].Path = relPath
		// Remove empty temp parent directory (best-effort).
		_ = os.Remove(filepath.Dir(oldPath))
	}
}

// cleanup stops already-started components on partial Start failure.
func (rt *Runtime) cleanup(ctx context.Context) {
	if rt.mgr != nil {
		_ = rt.mgr.Stop(ctx)
	}
}
