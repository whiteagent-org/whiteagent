package loader

import (
	"log/slog"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Registry holds all loaded plugins indexed for fast lookup and lifecycle iteration.
type Registry struct {
	byID      map[string]port.Plugin              // flat ID → plugin (O(1) lookup, duplicate detection)
	byKind    map[entity.PluginKind][]port.Plugin  // kind → ordered plugins (lifecycle ordering)
	configIDs map[port.Plugin]string               // plugin → config-driven ID used as registry key
}

func NewRegistry() *Registry {
	return &Registry{
		byID:      make(map[string]port.Plugin),
		byKind:    make(map[entity.PluginKind][]port.Plugin),
		configIDs: make(map[port.Plugin]string),
	}
}

// Get returns a plugin by ID. Returns nil if not found.
func (r *Registry) Get(id string) port.Plugin {
	return r.byID[id]
}

// ByKind returns all plugins of a given kind, in load order.
func (r *Registry) ByKind(kind entity.PluginKind) []port.Plugin {
	return r.byKind[kind]
}

// All returns every loaded plugin in no guaranteed order.
func (r *Registry) All() []port.Plugin {
	all := make([]port.Plugin, 0, len(r.byID))
	for _, p := range r.byID {
		all = append(all, p)
	}
	return all
}

// register adds a plugin to the registry. When overrideID is non-empty it is used
// as the registry key instead of the plugin's native ID. This allows the same .so
// to be loaded multiple times with different logical IDs (e.g. LLM endpoints).
// If a plugin with the same ID already exists, it is replaced (warn-and-replace):
// the old plugin is removed from byID, byKind, and configIDs before the new one
// is inserted.
// Register is the exported version of register for use by external packages
// (e.g., runtime registering built-in tools before .so loading).
func (r *Registry) Register(plug port.Plugin, overrideID string) error {
	return r.register(plug, overrideID)
}

func (r *Registry) register(plug port.Plugin, overrideID string) error {
	id := plug.ID()
	if overrideID != "" {
		id = overrideID
	}
	if old, exists := r.byID[id]; exists {
		slog.Warn("registry.plugin_replaced", "id", id)
		// Remove old plugin from byKind slice.
		oldKind := old.Kind()
		plugins := r.byKind[oldKind]
		filtered := make([]port.Plugin, 0, len(plugins))
		for _, p := range plugins {
			if p != old {
				filtered = append(filtered, p)
			}
		}
		r.byKind[oldKind] = filtered
		// Remove old plugin from configIDs.
		delete(r.configIDs, old)
	}
	kind := plug.Kind()
	r.byID[id] = plug
	r.byKind[kind] = append(r.byKind[kind], plug)
	r.configIDs[plug] = id
	return nil
}

// ConfigID returns the config-driven ID stored for a plugin during registration.
// This is the effective ID that Manager passes to Init.
func (r *Registry) ConfigID(plug port.Plugin) string {
	return r.configIDs[plug]
}
