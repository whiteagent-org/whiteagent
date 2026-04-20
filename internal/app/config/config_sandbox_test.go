package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestConfigSandboxFieldParsesJSON(t *testing.T) {
	raw := `{"sandbox":{"plugin_id":"local","path":"./sandbox.so","config":{"base_dir":"./ws"}}}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Sandbox.PluginID != "local" {
		t.Errorf("Sandbox.PluginID = %q, want local", cfg.Sandbox.PluginID)
	}
	if cfg.Sandbox.Path != "./sandbox.so" {
		t.Errorf("Sandbox.Path = %q", cfg.Sandbox.Path)
	}
}

func TestValidateSandboxRequired(t *testing.T) {
	cfg := validMinimalConfig()
	cfg.Sandbox.PluginID = ""
	cfg.Sandbox.Path = ""

	err := validate(&cfg)
	if err == nil {
		t.Fatal("expected validation error for missing sandbox")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sandbox.plugin_id") {
		t.Errorf("expected sandbox.plugin_id error, got: %s", msg)
	}
	if !strings.Contains(msg, "sandbox.path") {
		t.Errorf("expected sandbox.path error, got: %s", msg)
	}
}

func TestLoaderEntriesIncludesSandbox(t *testing.T) {
	cfg := validMinimalConfig()
	entries := cfg.LoaderEntries()
	found := false
	for _, e := range entries {
		if e.ExpectedKind == entity.PluginKindSandbox {
			found = true
			if e.ID != "sandbox.local-sandbox" {
				t.Errorf("sandbox entry ID = %q", e.ID)
			}
		}
	}
	if !found {
		t.Error("LoaderEntries missing sandbox entry")
	}
}

func TestConfigsByIDIncludesSandbox(t *testing.T) {
	cfg := validMinimalConfig()
	m := cfg.ConfigsByID()
	if _, ok := m["sandbox.local-sandbox"]; !ok {
		t.Error("ConfigsByID missing sandbox entry")
	}
}

func TestRuntimeConfigEncryptionKey(t *testing.T) {
	raw := `{"encryption_key":"secret123"}`
	var rc RuntimeConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.EncryptionKey != "secret123" {
		t.Errorf("EncryptionKey = %q", rc.EncryptionKey)
	}
}

func TestGatewayConfigPublicURL(t *testing.T) {
	raw := `{"address":":8080","public_url":"https://example.com"}`
	var gc GatewayConfig
	if err := json.Unmarshal([]byte(raw), &gc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gc.PublicURL != "https://example.com" {
		t.Errorf("PublicURL = %q", gc.PublicURL)
	}
}

// validMinimalConfig returns a Config with all required fields populated.
func validMinimalConfig() Config {
	return Config{
		Gateway:   GatewayConfig{Address: ":8080"},
		Transport: PluginSingleton{PluginID: "mem", Path: "./transport.so"},
		Store:     PluginSingleton{PluginID: "sqlite", Path: "./store.so"},
		Sandbox:   PluginSingleton{PluginID: "local-sandbox", Path: "./sandbox.so", Config: json.RawMessage(`{}`)},
		LLM: LLMConfig{
			Drivers: []LLMDriver{{
				Plugin: PluginDef{PluginID: "openai", Enabled: true, Path: "./llm.so"},
				Endpoints: []LLMEndpoint{{
					ID: "ep1", Enabled: true, Config: json.RawMessage(`{}`),
				}},
			}},
			Routing: LLMRouting{Primary: "ep1:model"},
		},
	}
}
