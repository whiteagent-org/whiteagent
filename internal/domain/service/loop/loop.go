// Package loop implements the ReAct agent loop: receive -> prompt -> LLM -> tools -> response.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/compaction"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/llm"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/prompt"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// LoopConfig holds parsed agent loop settings.
type LoopConfig struct {
	MaxIterations int
	TurnTimeout   time.Duration
	MaxWorkers    int
}

// ParseLoopConfig converts config.AgentConfig into a LoopConfig with parsed
// duration values.
func ParseLoopConfig(cfg config.AgentConfig) (LoopConfig, error) {
	timeout, err := time.ParseDuration(cfg.TurnTimeout)
	if err != nil {
		return LoopConfig{}, fmt.Errorf("parse turn_timeout %q: %w", cfg.TurnTimeout, err)
	}
	return LoopConfig{
		MaxIterations: cfg.MaxIterations,
		TurnTimeout:   timeout,
		MaxWorkers:    cfg.MaxWorkers,
	}, nil
}

// Loop implements the ReAct agent loop: receive -> prompt -> LLM -> tools -> response.
// Per-session mutex serializes messages for the same session. A counting semaphore
// bounds overall concurrency to MaxWorkers.
type Loop struct {
	completion    llm.CompletionService
	tools         map[string]port.ToolPlugin
	store         port.StorePlugin
	convService   port.ConversationService
	transport     port.TransportPlugin
	promptBuilder *prompt.PromptBuilder
	compactionSvc *compaction.Service
	channels      map[string]port.ChannelEntry
	secretService secret.SecretService
	sandbox       port.SandboxPlugin
	scopedFS      port.ScopedFS
	cfg           LoopConfig
	sessions      sync.Map      // ChatID.String() -> *sync.Mutex
	compactions   sync.Map      // ConversationID.String() -> struct{}
	semaphore     chan struct{} // bounded parallelism
	draining      atomic.Bool   // set during shutdown to reject new messages
}

// NewLoop creates an agent loop with all required dependencies.
// sandbox and scopedFS may be nil (sandbox features disabled).
func NewLoop(cfg LoopConfig, completion llm.CompletionService, store port.StorePlugin, convService port.ConversationService, transport port.TransportPlugin, tools map[string]port.ToolPlugin, promptBuilder *prompt.PromptBuilder, compactionSvc *compaction.Service, channels map[string]port.ChannelEntry, secretSvc secret.SecretService, sandbox port.SandboxPlugin, scopedFS port.ScopedFS) *Loop {
	return &Loop{
		completion:    completion,
		tools:         tools,
		store:         store,
		convService:   convService,
		transport:     transport,
		promptBuilder: promptBuilder,
		compactionSvc: compactionSvc,
		channels:      channels,
		secretService: secretSvc,
		sandbox:       sandbox,
		scopedFS:      scopedFS,
		cfg:           cfg,
		semaphore:     make(chan struct{}, cfg.MaxWorkers),
	}
}

// Handle is the MessageHandler entry point subscribed to TopicInbound.
// It acquires a semaphore slot, then a per-session mutex, applies turn timeout,
// and runs the ReAct loop.
func (l *Loop) Handle(ctx context.Context, msg entity.Message) error {
	// Silently drop new messages while draining.
	if l.draining.Load() {
		return nil
	}

	slog.Info("agent.handle", "chat_id", msg.ChatID, "msg_id", msg.ID)

	// Acquire semaphore slot (bounded concurrency).
	select {
	case l.semaphore <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-l.semaphore }()

	// Acquire per-session mutex (message ordering).
	key := msg.ChatID.String()
	mu := l.getOrCreateMutex(key)
	mu.Lock()
	defer mu.Unlock()

	// Apply per-turn timeout.
	turnCtx, cancel := context.WithTimeout(ctx, l.cfg.TurnTimeout)
	defer cancel()

	return l.runTurn(turnCtx, msg)
}

// getOrCreateMutex returns a per-session mutex, creating one if needed.
func (l *Loop) getOrCreateMutex(key string) *sync.Mutex {
	v, _ := l.sessions.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Shutdown drains the agent loop by setting the draining flag and waiting for
// all in-flight workers to finish. New messages are silently dropped once
// draining is set. The method blocks until all semaphore slots are reclaimed
// (workers finished) or ctx is cancelled.
func (l *Loop) Shutdown(ctx context.Context) error {
	l.draining.Store(true)
	slog.Info("agent.loop.draining")

	// Wait for all workers to finish by filling the semaphore to capacity.
	for i := 0; i < l.cfg.MaxWorkers; i++ {
		select {
		case l.semaphore <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	slog.Info("agent.loop.drained")
	return nil
}

// runTurn executes a single ReAct turn: load context, iterate LLM calls and tool
// executions up to MaxIterations, then publish the final response.
func (l *Loop) runTurn(ctx context.Context, inbound entity.Message) error {
	slog.Info("agent.turn.start", "chat_id", inbound.ChatID)

	// Load tenant from store.
	tenant, err := l.store.GetTenant(ctx, inbound.TenantID)
	if err != nil {
		slog.Warn("agent.turn.tenant_load_error", "err", err)
		return l.publishResponse(ctx, inbound, "Internal error: unable to load tenant configuration.")
	}
	if tenant == nil {
		slog.Warn("agent.turn.tenant_not_found", "tenant_id", inbound.TenantID)
		return l.publishResponse(ctx, inbound, "Internal error: tenant not found.")
	}

	// Load agent from store. Prefer inbound.AgentID (set by cron entries targeting
	// a specific agent), fall back to tenant's default agent.
	// @TODO change it to get the agent from the resolved message
	var agent *entity.Agent
	agentID := tenant.DefaultAgentID
	if !inbound.AgentID.IsEmpty() {
		agentID = inbound.AgentID
	}
	if !agentID.IsEmpty() {
		agent, err = l.store.GetAgent(ctx, inbound.TenantID, agentID)
		if err != nil {
			slog.Warn("agent.turn.agent_load_error", "err", err)
		}
	}
	if agent != nil {
		slog.Debug("agent.turn.agent_loaded", "agent_id", agent.ID, "name", agent.Name)
	} else {
		slog.Warn("agent.turn.no_agent", "tenant_id", tenant.ID, "agent_id", agentID)
		return l.publishResponse(ctx, inbound, "Configuration error: no agent configured for this tenant.")
	}
	// Stamp the resolved agent ID on the incoming message so it persists correctly.
	inbound.AgentID = agent.ID

	// Load user from store (for DMs with known user).
	var user *entity.User
	if !inbound.IsGroup && !inbound.UserID.IsEmpty() {
		user, err = l.store.GetUser(ctx, inbound.TenantID, inbound.UserID)
		if err != nil {
			slog.Warn("agent.turn.user_load_error", "err", err)
		}
	}

	// Resolve conversation (DB-backed, survives restart).
	// If the inbound message already has a ConversationID (e.g. from cron scheduler),
	// use it directly instead of resolving from chat identity.
	var convID entity.ConversationID
	if !inbound.ConversationID.IsEmpty() {
		convID = inbound.ConversationID
		l.convService.RegisterConversation(inbound.TenantID, convID)
	} else {
		convID, err = l.convService.ResolveConversation(ctx, inbound)
		if err != nil {
			slog.Warn("agent.turn.resolve_conversation_error", "err", err)
			return l.publishResponse(ctx, inbound, "Internal error: unable to resolve conversation.")
		}
	}

	// Save user message to conversation history.
	if err := l.convService.Append(ctx, convID, inbound); err != nil {
		slog.Error("agent.turn.append_error", "err", err)
		return l.publishResponse(ctx, inbound, "Internal error: unable to save message.")
	}

	// Start typing indicator: fetch chat entity for indication data and channel routing.
	var caps port.ChannelCapabilities
	if !inbound.ChatID.IsEmpty() {
		chat, chatErr := l.store.GetChat(ctx, inbound.TenantID, inbound.ChatID)
		if chatErr != nil {
			slog.Debug("loop.indication.chat_load_error", "err", chatErr)
		} else if chat != nil {
			if entry, ok := l.channels[chat.ChannelID]; ok {
				caps = entry.Capabilities
				if caps.Indication {
					if ia, ok := entry.Plugin.(port.IndicatorAware); ok {
						stopCh := make(chan func(), 1)
						go func() { stopCh <- ia.Indicate(ctx, chat.Indication) }()
						defer func() {
							if stop := <-stopCh; stop != nil {
								stop()
							}
						}()
					}
				}
			}
		}
	}

	// Pre-allocate outgoing message ID for sandbox file output.
	outMsgID := entity.MessageID(util.NewID())

	// Create sandbox proxy if sandbox is available.
	// The proxy computes the conversation messages dir internally from scopedFS.
	var proxy *sandboxProxy
	if l.sandbox != nil {
		messagesTarget := l.sandbox.MessagesPath()
		var proxyErr error
		proxy, proxyErr = newSandboxProxy(l.sandbox, l.secretService, l.scopedFS, inbound.TenantID, inbound.UserID, outMsgID, convID, "", messagesTarget)
		if proxyErr != nil {
			slog.Warn("agent.turn.sandbox_proxy_error", "err", proxyErr)
		} else {
			if _, ensureErr := proxy.Ensure(ctx, inbound.UserID); ensureErr != nil {
				slog.Warn("agent.turn.sandbox_ensure_error", "err", ensureErr)
				proxy = nil // don't inject broken proxy into tools
			} else {
				// Create outbox directory for this turn's file output.
				if prepErr := proxy.PrepareOutbox(ctx, inbound.UserID); prepErr != nil {
					slog.Warn("agent.turn.prepare_outbox_error", "err", prepErr)
				}
				// Inject proxy into sandbox-aware tools for this turn.
				for _, t := range l.tools {
					if sa, ok := t.(port.SandboxAware); ok {
						sa.SetSandbox(proxy)
					}
				}
				// Restore original sandbox after turn completes.
				defer func() {
					for _, t := range l.tools {
						if sa, ok := t.(port.SandboxAware); ok {
							sa.SetSandbox(l.sandbox)
						}
					}
				}()
			}
		}
	}

	// Create per-message model override holder and inject into aware tools.
	override := &port.ModelOverride{}
	ctx = port.WithModelOverride(ctx, override)
	for _, t := range l.tools {
		if ma, ok := t.(port.ModelOverrideAware); ok {
			ma.SetModelOverride(override)
		}
	}

	// ReAct loop.
	var lastContent string
	resolvedAgentID := agent.ID
	inboundID := inbound.ID // UUIDv7 — lexicographic boundary for history fetch
	ephemeralResultIDs := make([]entity.MessageID, 0, 4)
	compactionTriggeredThisTurn := false
	for i := 0; i < l.cfg.MaxIterations; i++ {
		slog.Info("agent.turn.llm_call", "iteration", i+1, "chat_id", inbound.ChatID)

		// Build prompt messages (system + token-windowed history).
		// On the first iteration, bound history to the inbound message ID so that
		// messages arriving after this one are excluded from the prompt.
		// On subsequent iterations, pass nil so assistant/tool messages from prior
		// iterations (which have later IDs) are included.
		var historyUpTo *entity.MessageID
		if i == 0 {
			historyUpTo = &inboundID
		}
		var outMsgPath string
		if l.sandbox != nil {
			outMsgPath = l.sandbox.UserHomePath(inbound.UserID) + "/.outbox/" + string(outMsgID)
		}
		messages, compactionSignal, err := l.promptBuilder.Build(ctx, tenant, agent, user, inbound, convID, caps, historyUpTo, outMsgPath)
		if err != nil {
			slog.Warn("agent.turn.build_error", "err", err)
			return l.publishResponse(ctx, inbound, "Internal error.")
		}
		if compactionSignal != nil {
			slog.Debug("agent.turn.compaction_signal", "latest_message_id", compactionSignal.LatestMessageID)
		}
		if !compactionTriggeredThisTurn && compactionSignal != nil && l.compactionSvc != nil {
			compactionTriggeredThisTurn = true
			convKey := string(convID)
			if _, loaded := l.compactions.LoadOrStore(convKey, struct{}{}); !loaded {
				latestMessageID := compactionSignal.LatestMessageID
				go func() {
					defer l.compactions.Delete(convKey)
					err := l.compactionSvc.Compact(context.WithoutCancel(ctx), compaction.Request{
						TenantID:        inbound.TenantID,
						ConversationID:  convID,
						LatestMessageID: latestMessageID,
					})
					if err != nil {
						slog.Error("compaction.error", "conversation_id", convID, "err", err)
					}
				}()
			}
		}
		// (Messages mount replaces per-message CopyTo -- no SetMessages needed.)
		for j, m := range messages {
			content := m.Content
			if m.Role == entity.RoleSystem && len(content) > 200 {
				content = content[:200] + "...[truncated]"
			}
			slog.Debug("agent.turn.message", "iteration", i+1, "idx", j, "role", m.Role, "content", content)
		}

		// Get filtered tool definitions for the LLM.
		toolDefs := l.promptBuilder.FilteredToolDefs(tenant, caps)

		// Call LLM via CompletionService (model and stream set by service).
		resp, err := l.completion.Complete(ctx, port.CompletionRequest{
			TenantID: inbound.TenantID,
			Messages: messages,
			Tools:    toolDefs,
		})

		// Check timeout.
		if ctx.Err() != nil {
			slog.Warn("agent.turn.timeout", "chat_id", inbound.ChatID, "iteration", i+1)
			l.logError(ctx, inbound, "Turn timed out")
			content := lastContent
			if content == "" {
				content = "I was unable to complete your request. [Processing limit reached]"
			} else {
				content += "\n\n[Processing limit reached]"
			}
			return l.publishResponse(ctx, inbound, content)
		}

		if err != nil {
			slog.Error("agent.turn.llm_error", "err", err, "iteration", i+1)
			l.logError(ctx, inbound, "LLM error: "+err.Error())
			return l.publishResponse(ctx, inbound, "I encountered an error processing your request.")
		}

		// Strip any runtime context tags or reasoning blocks the LLM echoed back.
		resp.Content = prompt.SanitizeResponse(resp.Content)

		// Build assistant message and save to conversation history.
		// Every iteration gets a fresh ID so that message ordering by
		// UUIDv7 / created_at matches the logical sequence.  The
		// pre-allocated outMsgID is only used for the sandbox outbox path.
		iterMsgID := entity.MessageID(util.NewID())
		assistantMsg := inbound.NewReply(iterMsgID, entity.RoleAssistant)
		assistantMsg.AgentID = resolvedAgentID
		assistantMsg.Content = resp.Content
		assistantMsg.ToolCalls = resp.ToolCalls

		if err := l.convService.Append(context.WithoutCancel(ctx), convID, assistantMsg); err != nil {
			slog.Error("agent.turn.append_assistant_error", "err", err)
			return l.publishResponse(ctx, inbound, "Internal error: unable to save response.")
		}

		lastContent = resp.Content

		// If no tool calls, we have a final response -- publish the saved
		// assistantMsg directly so the outbound handler's UpdateExternalMessageID
		// targets the same ID that's already in the DB.
		if len(resp.ToolCalls) == 0 {
			slog.Info("agent.turn.complete", "chat_id", inbound.ChatID, "iterations", i+1)
			// Redact secret values from final response.
			assistantMsg.Content = l.secretService.Redact(context.WithoutCancel(ctx), assistantMsg.Content, inbound.TenantID, inbound.UserID)
			assistantMsg.Kind = entity.MessageKindMessage

			// Harvest outgoing attachments from sandbox.
			if proxy != nil {
				outDir, harvestErr := proxy.HarvestOutgoing(context.WithoutCancel(ctx), iterMsgID)
				if harvestErr != nil {
					slog.Warn("agent.turn.harvest_error", "err", harvestErr)
				} else {
					attachments, scanErr := harvestAttachments(outDir)
					if scanErr != nil {
						slog.Warn("agent.turn.scan_attachments_error", "err", scanErr)
					} else if len(attachments) > 0 {
						assistantMsg.Attachments = attachments
						slog.Debug("agent.turn.harvest.done", "attachment_count", len(attachments))
					}
				}
				// Note: message dir is NOT cleaned up here because Publish is
				// async — the channel reads attachment files after this returns.
			}

			if err := l.transport.Publish(context.WithoutCancel(ctx), entity.TopicOutbound, assistantMsg); err != nil {
				return err
			}
			if len(ephemeralResultIDs) > 0 {
				if err := l.store.EvictMessages(context.WithoutCancel(ctx), inbound.TenantID, convID, ephemeralResultIDs); err != nil {
					return err
				}
			}
			return nil
		}

		// Execute tool calls in parallel.
		results := l.executeToolsParallel(ctx, inbound, resp.ToolCalls, convID)

		// Save tool results to conversation history.
		// By default all tool results are evicted after the reply; tools implementing
		// EphemeralTool and returning false opt out to keep their results in context.
		for _, result := range results {
			result.AgentID = resolvedAgentID
			shouldEvict := true
			if tool, ok := l.tools[result.ToolName]; ok {
				if et, ok := tool.(port.EphemeralTool); ok && !et.IsEphemeral() {
					shouldEvict = false
				}
			}
			result.Ephemeral = shouldEvict
			if err := l.convService.Append(context.WithoutCancel(ctx), convID, result); err != nil {
				slog.Error("agent.turn.append_tool_result_error", "err", err)
				return l.publishResponse(ctx, inbound, "Internal error: unable to save tool result.")
			}
			if shouldEvict {
				ephemeralResultIDs = append(ephemeralResultIDs, result.ID)
			}
		}
	}

	// Max iterations reached.
	slog.Warn("agent.turn.max_iterations_reached", "chat_id", inbound.ChatID, "max", l.cfg.MaxIterations)
	l.logError(ctx, inbound, "Max iterations reached")
	content := lastContent
	if content == "" {
		content = "I was unable to complete your request. [Processing limit reached]"
	} else {
		content += "\n\n[Processing limit reached]"
	}
	return l.publishResponse(ctx, inbound, content)
}

// executeToolsParallel executes all tool calls concurrently and returns results
// in the same order as the input tool calls.
func (l *Loop) executeToolsParallel(ctx context.Context, inbound entity.Message, toolCalls []entity.ToolCall, convID entity.ConversationID) []entity.Message {
	tc := port.ToolContext{TenantID: inbound.TenantID, AgentID: inbound.AgentID, UserID: inbound.UserID, ChatID: inbound.ChatID, IsGroup: inbound.IsGroup, MessageID: inbound.ID, ConversationID: convID}

	results := make([]entity.Message, len(toolCalls))
	var wg sync.WaitGroup

	for i, call := range toolCalls {
		wg.Add(1)
		go func(idx int, call entity.ToolCall) {
			defer wg.Done()
			start := time.Now()

			tool, ok := l.tools[call.Name]
			if !ok {
				slog.Warn("agent.tool.unknown", "name", call.Name)
				result := inbound.NewReply(entity.MessageID(util.NewID()), entity.RoleTool)
				result.Content = fmt.Sprintf("Unknown tool: %s", call.Name)
				result.ToolCallID = call.ID
				result.ToolName = call.Name
				results[idx] = result
				return
			}

			// Parse arguments.
			args := json.RawMessage(call.Arguments)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}

			slog.Debug("agent.tool.call", "name", call.Name, "args", string(args))
			execResult, err := tool.Execute(ctx, tc, args)
			duration := time.Since(start)

			if err != nil {
				slog.Error("agent.tool.execute", "name", call.Name, "duration", duration, "error", err)
				execResult = fmt.Sprintf("Tool error: %s", err.Error())
			} else {
				slog.Info("agent.tool.execute", "name", call.Name, "duration", duration)
			}

			// Redact secret values from tool output.
			execResult = l.secretService.Redact(context.WithoutCancel(ctx), execResult, tc.TenantID, tc.UserID)

			result := inbound.NewReply(entity.MessageID(util.NewID()), entity.RoleTool)
			result.Content = execResult
			result.ToolCallID = call.ID
			result.ToolName = call.Name
			results[idx] = result
		}(i, call)
	}

	wg.Wait()
	return results
}

// logError writes a fire-and-forget error log entry. Failures are logged via
// slog but never returned. Uses context.WithoutCancel so timeout-cancelled
// contexts don't prevent the write.
func (l *Loop) logError(ctx context.Context, inbound entity.Message, errMsg string) {
	entry := entity.ErrorLogEntry{
		UserID:  inbound.UserID,
		RefType: "message",
		RefID:   inbound.ID.String(),
		Content: errMsg,
	}
	if err := l.store.AppendErrorLog(context.WithoutCancel(ctx), inbound.TenantID, entry); err != nil {
		slog.Error("agent.errorlog.write", "err", err)
	}
}

// harvestAttachments scans a directory and converts files to entity.Attachment.
// Returns nil, nil if the directory does not exist (no-op).
func harvestAttachments(dir string) ([]entity.Attachment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var attachments []entity.Attachment
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			slog.Debug("harvest.file_info_error", "name", entry.Name(), "err", err)
			continue
		}
		attachments = append(attachments, entity.Attachment{
			ID:       util.NewID(),
			Kind:     "document",
			Filename: entry.Name(),
			MimeType: mimeFromFilename(entry.Name()),
			Size:     info.Size(),
			Path:     filepath.Join(dir, entry.Name()),
		})
	}
	return attachments, nil
}

// mimeFromFilename returns the MIME type for a filename based on its extension.
// Defaults to "application/octet-stream" if the type cannot be determined.
func mimeFromFilename(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}

// publishResponse builds a response message and publishes it to TopicOutbound.
// Used only for error/timeout/max-iterations paths where the message is not
// persisted to conversation history.
func (l *Loop) publishResponse(ctx context.Context, inbound entity.Message, content string) error {
	resp := inbound.NewReply(entity.MessageID(util.NewID()), entity.RoleAssistant)
	resp.AgentID = inbound.AgentID
	resp.Content = content
	resp.Kind = entity.MessageKindMessage
	return l.transport.Publish(context.WithoutCancel(ctx), entity.TopicOutbound, resp)
}
