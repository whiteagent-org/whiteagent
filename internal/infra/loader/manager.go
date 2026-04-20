package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// kindInitOrder is the fixed Init/Start order across plugin kinds.
// Store goes first so other plugins can use persistence during their own Init.
// Channel goes last so all consumers (LLM, Sandbox, Tools, Middleware) are
// fully started before any inbound messages arrive.
// Stop is always called in reverse of this order.
var kindInitOrder = []entity.PluginKind{
	entity.PluginKindStore,
	entity.PluginKindTransport,
	entity.PluginKindLLM,
	entity.PluginKindSandbox,
	entity.PluginKindTool,
	entity.PluginKindMiddleware,
	entity.PluginKindChannel,
}

// Manager handles the Init/Start/Stop lifecycle for all loaded plugins
// in the fixed kind-based order. Phase 3 (runtime wiring) creates the Manager
// after loading plugins and calls Init then Start at startup.
type Manager struct {
	registry *Registry
	// started tracks plugins that have been successfully started, in start order.
	// Used to call Stop in strict reverse order on failure or graceful shutdown.
	started []port.Plugin
}

// NewManager creates a Manager wrapping the given Registry.
func NewManager(reg *Registry) *Manager {
	return &Manager{registry: reg}
}

// Init calls Init on all plugins in kindInitOrder.
// Per-plugin config is looked up from cfgs by plugin ID.
// If any Init fails, returns an error immediately (fail fast — no partial init).
func (m *Manager) Init(ctx context.Context, cfgs map[string]json.RawMessage) error {
	for _, kind := range kindInitOrder {
		for _, plug := range m.registry.byKind[kind] {
			configID := m.registry.ConfigID(plug)
			cfg := cfgs[configID] // nil json.RawMessage is valid — plugin's Init handles absent config
			if err := plug.Init(ctx, configID, cfg); err != nil {
				return fmt.Errorf("init plugin %s (kind=%s): %w", configID, plug.Kind(), err)
			}
			slog.Debug("plugin.init", "id", configID, "kind", plug.Kind())
		}
	}
	return nil
}

// Start calls Start on all plugins in kindInitOrder.
// If any Start fails, Stop is called on already-started plugins in reverse order
// before returning the error. This ensures no plugins are left in a started state
// after a failure.
func (m *Manager) Start(ctx context.Context) error {
	for _, kind := range kindInitOrder {
		for _, plug := range m.registry.byKind[kind] {
			if err := plug.Start(ctx); err != nil {
				// Stop already-started plugins in reverse order before returning.
				_ = m.stopStarted(ctx)
				return fmt.Errorf("start plugin %s (kind=%s): %w", plug.ID(), plug.Kind(), err)
			}
			slog.Debug("plugin.start", "id", plug.ID(), "kind", plug.Kind())
			m.started = append(m.started, plug)
		}
	}
	return nil
}

// Stop calls Stop on all started plugins in reverse-start order.
// Errors from individual Stop calls are collected and joined — shutdown continues
// even if a plugin's Stop returns an error.
func (m *Manager) Stop(ctx context.Context) error {
	return m.stopStarted(ctx)
}

// stopStarted stops all started plugins in reverse order and resets the started slice.
func (m *Manager) stopStarted(ctx context.Context) error {
	var errs []error
	for i := len(m.started) - 1; i >= 0; i-- {
		plug := m.started[i]
		if err := plug.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop plugin %s: %w", plug.ID(), err))
		}
	}
	m.started = nil
	return errors.Join(errs...)
}
