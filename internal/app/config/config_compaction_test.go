package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactionOmittedSectionLeavesNil(t *testing.T) {
	cfg := loadTestConfig(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if cfg.LLM.Compaction != nil {
		t.Fatalf("expected nil compaction config when llm.compaction is omitted, got %#v", cfg.LLM.Compaction)
	}
}

func TestCompactionParsesExplicitValues(t *testing.T) {
	cfg := loadTestConfig(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {
				"model": "openai:gpt-4o-mini",
				"threshold": 0.85,
				"preserve_recent_messages": 4
			},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if cfg.LLM.Compaction == nil {
		t.Fatal("expected compaction config to be present")
	}
	if cfg.LLM.Compaction.Model != "openai:gpt-4o-mini" {
		t.Fatalf("Compaction.Model = %q, want openai:gpt-4o-mini", cfg.LLM.Compaction.Model)
	}
	if cfg.LLM.Compaction.Threshold != 0.85 {
		t.Fatalf("Compaction.Threshold = %v, want 0.85", cfg.LLM.Compaction.Threshold)
	}
	if cfg.LLM.Compaction.PreserveRecentMessages != 4 {
		t.Fatalf("Compaction.PreserveRecentMessages = %d, want 4", cfg.LLM.Compaction.PreserveRecentMessages)
	}
}

func TestCompactionDefaultsThresholdWhenOmitted(t *testing.T) {
	cfg := loadTestConfig(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "openai:gpt-4o-mini"},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if cfg.LLM.Compaction == nil {
		t.Fatal("expected compaction config to be present")
	}
	if cfg.LLM.Compaction.Threshold != 0.9 {
		t.Fatalf("Compaction.Threshold = %v, want 0.9 when omitted", cfg.LLM.Compaction.Threshold)
	}
}

func TestCompactionDefaultsPreserveRecentMessagesWhenOmitted(t *testing.T) {
	cfg := loadTestConfig(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "openai:gpt-4o-mini", "threshold": 0.9},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if cfg.LLM.Compaction == nil {
		t.Fatal("expected compaction config to be present")
	}
	if cfg.LLM.Compaction.PreserveRecentMessages != 6 {
		t.Fatalf("Compaction.PreserveRecentMessages = %d, want 6 when omitted", cfg.LLM.Compaction.PreserveRecentMessages)
	}
}

func TestCompactionRejectsExplicitZeroThreshold(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "openai:gpt-4o-mini", "threshold": 0.0},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "llm.compaction.threshold") {
		t.Fatalf("expected llm.compaction.threshold validation error, got %v", err)
	}
}

func TestCompactionRejectsThresholdAboveOne(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "openai:gpt-4o-mini", "threshold": 1.1},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "llm.compaction.threshold") {
		t.Fatalf("expected llm.compaction.threshold validation error, got %v", err)
	}
}

func TestCompactionRejectsNonPositivePreserveRecentMessages(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {
				"model": "openai:gpt-4o-mini",
				"threshold": 0.9,
				"preserve_recent_messages": 0
			},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "llm.compaction.preserve_recent_messages") {
		t.Fatalf("expected llm.compaction.preserve_recent_messages validation error, got %v", err)
	}
}

func TestCompactionRejectsEmptyModel(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "", "threshold": 0.9},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "llm.compaction.model") {
		t.Fatalf("expected llm.compaction.model validation error, got %v", err)
	}
}

func TestCompactionRejectsModelWithoutEndpointPrefix(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "gpt-4o-mini", "threshold": 0.9},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "llm.compaction.model: must be \"endpoint_id:model_name\" format") {
		t.Fatalf("expected llm.compaction.model format validation error, got %v", err)
	}
}

func TestCompactionRejectsUnknownEndpoint(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"compaction": {"model": "missing:gpt-4o-mini", "threshold": 0.9},
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), `llm.compaction.model: endpoint "missing" not found in enabled endpoints`) {
		t.Fatalf("expected llm.compaction.model endpoint validation error, got %v", err)
	}
}

func TestCompactionRejectsLegacySummariesLimit(t *testing.T) {
	_, err := loadConfigExpectError(t, `{
		"runtime": {"encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"gateway": {"address": ":8080"},
		"agent": {"summaries_limit": 1},
		"transport": {"plugin_id": "memory", "path": "./transport.so", "config": {}},
		"store": {"plugin_id": "sqlite", "path": "./store.so", "config": {}},
		"sandbox": {"plugin_id": "local", "path": "./sandbox.so", "config": {}},
		"llm": {
			"drivers": [{
				"plugin": {"plugin_id": "openai", "enabled": true, "path": "./llm.so", "config": null},
				"endpoints": [{"id": "openai", "enabled": true, "config": {}}]
			}],
			"routing": {"primary": "openai:gpt-4o"}
		}
	}`)

	if err == nil || !strings.Contains(err.Error(), "agent.summaries_limit: removed; use llm.compaction.preserve_recent_messages") {
		t.Fatalf("expected legacy agent.summaries_limit removal error, got %v", err)
	}
}

func loadTestConfig(t *testing.T, raw string) *Config {
	t.Helper()

	cfg, err := loadConfigExpectError(t, raw)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	return cfg
}

func loadConfigExpectError(t *testing.T, raw string) (*Config, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return Load(path)
}
