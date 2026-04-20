package loader

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockPlugin implements port.Plugin for testing registry behavior.
type mockPlugin struct {
	id     string
	kind   entity.PluginKind
	initID string // records the id passed to Init
}

func (m *mockPlugin) ID() string             { return m.id }
func (m *mockPlugin) Kind() entity.PluginKind { return m.kind }
func (m *mockPlugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	m.initID = id
	if id != "" {
		m.id = id
	}
	return nil
}
func (m *mockPlugin) Start(_ context.Context) error  { return nil }
func (m *mockPlugin) Stop(_ context.Context) error   { return nil }
func (m *mockPlugin) Status() entity.PluginState      { return entity.PluginStateHealthy }

var _ port.Plugin = (*mockPlugin)(nil)

func TestRegisterOverrideID(t *testing.T) {
	t.Run("two plugins same native ID different override IDs succeed", func(t *testing.T) {
		reg := NewRegistry()
		p1 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}
		p2 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}

		if err := reg.register(p1, "llm.openrouter"); err != nil {
			t.Fatalf("register p1: %v", err)
		}
		if err := reg.register(p2, "llm.ollama"); err != nil {
			t.Fatalf("register p2: %v", err)
		}
	})

	t.Run("empty override ID uses native plugin ID", func(t *testing.T) {
		reg := NewRegistry()
		p := &mockPlugin{id: "transport.bus", kind: entity.PluginKindTransport}

		if err := reg.register(p, ""); err != nil {
			t.Fatalf("register: %v", err)
		}
		if got := reg.Get("transport.bus"); got != p {
			t.Fatalf("Get(native ID) = %v, want %v", got, p)
		}
	})

	t.Run("Get with override ID returns correct plugin", func(t *testing.T) {
		reg := NewRegistry()
		p1 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}
		p2 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}

		_ = reg.register(p1, "llm.openrouter")
		_ = reg.register(p2, "llm.ollama")

		if got := reg.Get("llm.openrouter"); got != p1 {
			t.Errorf("Get(llm.openrouter) = %v, want %v", got, p1)
		}
		if got := reg.Get("llm.ollama"); got != p2 {
			t.Errorf("Get(llm.ollama) = %v, want %v", got, p2)
		}
		// Native ID should NOT be registered when override is provided
		if got := reg.Get("llm.driver"); got != nil {
			t.Errorf("Get(native ID) should be nil when override used, got %v", got)
		}
	})

	t.Run("duplicate override ID replaces old plugin", func(t *testing.T) {
		reg := NewRegistry()
		p1 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}
		p2 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}

		_ = reg.register(p1, "llm.openrouter")
		if err := reg.register(p2, "llm.openrouter"); err != nil {
			t.Fatalf("expected no error on replace, got %v", err)
		}
		if got := reg.Get("llm.openrouter"); got != p2 {
			t.Fatalf("Get after replace = %v, want p2", got)
		}
		llms := reg.ByKind(entity.PluginKindLLM)
		if len(llms) != 1 {
			t.Fatalf("ByKind(LLM) = %d plugins, want 1", len(llms))
		}
		if llms[0] != p2 {
			t.Fatal("ByKind(LLM)[0] should be p2")
		}
		all := reg.All()
		if len(all) != 1 {
			t.Fatalf("All() = %d plugins, want 1", len(all))
		}
	})

	t.Run("duplicate native ID without override replaces old plugin", func(t *testing.T) {
		reg := NewRegistry()
		p1 := &mockPlugin{id: "store.sqlite", kind: entity.PluginKindStore}
		p2 := &mockPlugin{id: "store.sqlite", kind: entity.PluginKindStore}

		_ = reg.register(p1, "")
		if err := reg.register(p2, ""); err != nil {
			t.Fatalf("expected no error on replace, got %v", err)
		}
		if got := reg.Get("store.sqlite"); got != p2 {
			t.Fatalf("Get after replace = %v, want p2", got)
		}
		stores := reg.ByKind(entity.PluginKindStore)
		if len(stores) != 1 {
			t.Fatalf("ByKind(Store) = %d plugins, want 1", len(stores))
		}
	})

	t.Run("ByKind returns all overridden plugins", func(t *testing.T) {
		reg := NewRegistry()
		p1 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}
		p2 := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}

		_ = reg.register(p1, "llm.openrouter")
		_ = reg.register(p2, "llm.ollama")

		llms := reg.ByKind(entity.PluginKindLLM)
		if len(llms) != 2 {
			t.Fatalf("ByKind(LLM) = %d plugins, want 2", len(llms))
		}
	})
}

func TestManagerInitPassesConfigID(t *testing.T) {
	reg := NewRegistry()
	p := &mockPlugin{id: "llm.driver", kind: entity.PluginKindLLM}
	if err := reg.register(p, "llm.custom"); err != nil {
		t.Fatalf("register: %v", err)
	}

	mgr := NewManager(reg)
	cfgs := map[string]json.RawMessage{
		"llm.custom": json.RawMessage(`{"key":"value"}`),
	}
	if err := mgr.Init(context.Background(), cfgs); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if p.initID != "llm.custom" {
		t.Errorf("initID = %q, want %q", p.initID, "llm.custom")
	}
	if p.ID() != "llm.custom" {
		t.Errorf("ID() = %q, want %q", p.ID(), "llm.custom")
	}
}

func TestRegisterReplaceRemovesFromByKind(t *testing.T) {
	reg := NewRegistry()
	pA := &mockPlugin{id: "tool.memory_get", kind: entity.PluginKindTool}
	pB := &mockPlugin{id: "tool.memory_get", kind: entity.PluginKindTool}

	_ = reg.register(pA, "tool.memory_get")
	_ = reg.register(pB, "tool.memory_get")

	if got := reg.Get("tool.memory_get"); got != pB {
		t.Fatalf("Get(tool.memory_get) = %v, want pB", got)
	}
	tools := reg.ByKind(entity.PluginKindTool)
	if len(tools) != 1 {
		t.Fatalf("ByKind(Tool) = %d plugins, want 1", len(tools))
	}
	if tools[0] != pB {
		t.Fatal("ByKind(Tool)[0] should be pB")
	}
	all := reg.All()
	if len(all) != 1 {
		t.Fatalf("All() = %d plugins, want 1", len(all))
	}
}

func TestRegisterReplaceAcrossKinds(t *testing.T) {
	reg := NewRegistry()
	toolA := &mockPlugin{id: "tool.x", kind: entity.PluginKindTool}
	toolB := &mockPlugin{id: "tool.x", kind: entity.PluginKindTool}
	chanC := &mockPlugin{id: "channel.y", kind: entity.PluginKindChannel}

	_ = reg.register(toolA, "tool.x")
	_ = reg.register(toolB, "tool.x")
	_ = reg.register(chanC, "channel.y")

	tools := reg.ByKind(entity.PluginKindTool)
	if len(tools) != 1 {
		t.Fatalf("ByKind(Tool) = %d plugins, want 1", len(tools))
	}
	if tools[0] != toolB {
		t.Fatal("ByKind(Tool)[0] should be toolB")
	}
	channels := reg.ByKind(entity.PluginKindChannel)
	if len(channels) != 1 {
		t.Fatalf("ByKind(Channel) = %d plugins, want 1", len(channels))
	}
	if channels[0] != chanC {
		t.Fatal("ByKind(Channel)[0] should be chanC")
	}
}

func TestManagerInitEmptyConfigID(t *testing.T) {
	reg := NewRegistry()
	p := &mockPlugin{id: "transport.bus", kind: entity.PluginKindTransport}
	if err := reg.register(p, ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	mgr := NewManager(reg)
	cfgs := map[string]json.RawMessage{
		"transport.bus": json.RawMessage(`{}`),
	}
	if err := mgr.Init(context.Background(), cfgs); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// With empty override, ConfigID returns the native ID (used as registry key).
	// The plugin's Init receives that ID, but since it equals the native ID,
	// the plugin keeps its native ID.
	if p.ID() != "transport.bus" {
		t.Errorf("ID() = %q, want %q", p.ID(), "transport.bus")
	}
}
