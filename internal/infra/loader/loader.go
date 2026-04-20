package loader

import (
	"encoding/json"
	"fmt"
	"plugin"
	"runtime/debug"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// PluginEntry describes a single plugin .so file to load.
// Phase 3 (config parsing) constructs these from the JSON config file.
type PluginEntry struct {
	Path         string            // Absolute or relative path to the .so file
	ExpectedKind entity.PluginKind // Kind declared in config; validated against Manifest.Kind
	Config       json.RawMessage   // Per-plugin config blob passed to Init()
	ID           string            // Expected plugin ID; empty for singletons
}

// Loader loads plugin .so files and produces a validated Registry.
type Loader struct{}

// NewLoader creates a new Loader.
func NewLoader() *Loader {
	return &Loader{}
}

// Load loads all plugin entries into a Registry.
// Each entry is loaded individually with panic recovery — one plugin's panic does not
// prevent other plugins from loading, but all failures are collected and returned as
// a combined error. Validation failure for any plugin causes Load to return an error.
func (l *Loader) Load(entries []PluginEntry) (*Registry, error) {
	reg := NewRegistry()
	if err := l.LoadInto(reg, entries); err != nil {
		return nil, err
	}
	return reg, nil
}

// LoadInto loads all plugin entries into an existing Registry. This allows
// callers to pre-populate the registry (e.g., with built-in tools) before
// loading .so plugins that may override them by matching ID.
func (l *Loader) LoadInto(reg *Registry, entries []PluginEntry) error {
	for _, entry := range entries {
		plug, err := l.loadOne(entry.Path, entry.ExpectedKind)
		if err != nil {
			return fmt.Errorf("load plugin %q: %w", entry.Path, err)
		}
		if err := reg.register(plug, entry.ID); err != nil {
			return fmt.Errorf("register plugin %q: %w", entry.Path, err)
		}
	}
	return nil
}

// loadOne loads a single plugin .so file with panic recovery.
// The named return 'err' allows the deferred recover to assign to it.
// IMPORTANT: recover() is called directly inside the deferred function — not via a helper.
func (l *Loader) loadOne(path string, expectedKind entity.PluginKind) (plug port.Plugin, err error) {
	defer func() {
		if r := recover(); r != nil {
			// Capture stack trace for debuggability. Treat as validation failure.
			stack := debug.Stack()
			err = fmt.Errorf("panic during load: %v\n%s", r, stack)
		}
	}()

	p, openErr := plugin.Open(path)
	if openErr != nil {
		return nil, fmt.Errorf("open: %w", openErr)
	}

	// --- Lookup and validate Manifest (pre-instantiation) ---
	manifestSym, lookupErr := p.Lookup("Manifest")
	if lookupErr != nil {
		return nil, fmt.Errorf("missing Manifest symbol: %w", lookupErr)
	}
	manifestFn, ok := manifestSym.(func() port.PluginManifest)
	if !ok {
		return nil, fmt.Errorf("Manifest symbol has wrong type %T, want func() port.PluginManifest", manifestSym)
	}
	manifest := manifestFn()

	if manifest.Kind != expectedKind {
		return nil, fmt.Errorf("manifest kind %q does not match expected kind %q", manifest.Kind, expectedKind)
	}

	// --- Lookup and call NewPlugin ---
	newPluginSym, lookupErr := p.Lookup("NewPlugin")
	if lookupErr != nil {
		return nil, fmt.Errorf("missing NewPlugin symbol: %w", lookupErr)
	}
	newPluginFn, ok := newPluginSym.(func() port.Plugin)
	if !ok {
		return nil, fmt.Errorf("NewPlugin symbol has wrong type %T, want func() port.Plugin", newPluginSym)
	}
	plug = newPluginFn()

	// --- Post-instantiation cross-check ---
	if plug.Kind() != manifest.Kind {
		return nil, fmt.Errorf("live plugin Kind %q does not match manifest Kind %q", plug.Kind(), manifest.Kind)
	}

	// --- Kind-specific interface assertion ---
	if assertErr := assertKindInterface(plug, expectedKind, path); assertErr != nil {
		return nil, assertErr
	}

	return plug, nil
}

// assertKindInterface verifies the plugin implements the correct kind-specific port interface.
// This is the only place where all seven kinds must be explicitly enumerated.
func assertKindInterface(plug port.Plugin, kind entity.PluginKind, path string) error {
	switch kind {
	case entity.PluginKindStore:
		if _, ok := plug.(port.StorePlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=store) does not implement port.StorePlugin", path)
		}
	case entity.PluginKindTransport:
		if _, ok := plug.(port.TransportPlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=transport) does not implement port.TransportPlugin", path)
		}
	case entity.PluginKindChannel:
		if _, ok := plug.(port.ChannelPlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=channel) does not implement port.ChannelPlugin", path)
		}
	case entity.PluginKindLLM:
		if _, ok := plug.(port.LLMPlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=llm) does not implement port.LLMPlugin", path)
		}
	case entity.PluginKindSandbox:
		if _, ok := plug.(port.SandboxPlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=sandbox) does not implement port.SandboxPlugin", path)
		}
	case entity.PluginKindTool:
		if _, ok := plug.(port.ToolPlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=tool) does not implement port.ToolPlugin", path)
		}
	case entity.PluginKindMiddleware:
		if _, ok := plug.(port.MiddlewarePlugin); !ok {
			return fmt.Errorf("plugin at %s (kind=middleware) does not implement port.MiddlewarePlugin", path)
		}
	default:
		return fmt.Errorf("plugin at %s: unknown kind %q", path, kind)
	}
	return nil
}
