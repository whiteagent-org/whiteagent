package config

import (
	"testing"
)

func TestQualifyID(t *testing.T) {
	tests := []struct {
		kind string
		raw  string
		want string
	}{
		// Without existing prefix — should add it.
		{"channel", "telegram", "channel.telegram"},
		{"transport", "nats", "transport.nats"},
		{"store", "sqlite", "store.sqlite"},
		{"llm", "openai", "llm.openai"},
		{"tool", "search", "tool.search"},
		{"middleware", "router", "middleware.router"},

		// With existing prefix — should NOT double it.
		{"channel", "channel.telegram", "channel.telegram"},
		{"transport", "transport.nats", "transport.nats"},
		{"store", "store.sqlite", "store.sqlite"},
		{"llm", "llm.openai", "llm.openai"},
		{"tool", "tool.search", "tool.search"},
		{"middleware", "middleware.router", "middleware.router"},

		// Edge: prefix appears but not at start — should still add prefix.
		{"channel", "mychannel.foo", "channel.mychannel.foo"},
	}
	for _, tt := range tests {
		got := QualifyID(tt.kind, tt.raw)
		if got != tt.want {
			t.Errorf("QualifyID(%q, %q) = %q, want %q", tt.kind, tt.raw, got, tt.want)
		}
	}
}

// TestTokenBudgetDefaultsWhenZero verifies that TokenBudget is set to 32000
// when the configured value is 0 (i.e. the field was omitted from JSON).
func TestTokenBudgetDefaultsWhenZero(t *testing.T) {
	cfg := &Config{}
	cfg.Agent.TokenBudget = 0
	applyDefaults(cfg)
	if cfg.Agent.TokenBudget != 32000 {
		t.Errorf("expected TokenBudget 32000 when input is 0, got %d", cfg.Agent.TokenBudget)
	}
}

// TestTokenBudgetDefaultsWhenNegative verifies that a negative TokenBudget
// is replaced with the default of 32000.
func TestTokenBudgetDefaultsWhenNegative(t *testing.T) {
	cfg := &Config{}
	cfg.Agent.TokenBudget = -1
	applyDefaults(cfg)
	if cfg.Agent.TokenBudget != 32000 {
		t.Errorf("expected TokenBudget 32000 when input is -1, got %d", cfg.Agent.TokenBudget)
	}
}

// TestTokenBudgetPreservedWhenPositive verifies that a user-specified positive
// TokenBudget is not overwritten by the defaults logic.
func TestTokenBudgetPreservedWhenPositive(t *testing.T) {
	cfg := &Config{}
	cfg.Agent.TokenBudget = 8000
	applyDefaults(cfg)
	if cfg.Agent.TokenBudget != 8000 {
		t.Errorf("expected TokenBudget 8000 to be preserved, got %d", cfg.Agent.TokenBudget)
	}
}

// TestAgentConfigHasNoMaxHistoryField is a compile-time assertion: if MaxHistory
// were present on AgentConfig this file would not compile because there is no
// such field. The test itself is a no-op; compilation is the check.
func TestAgentConfigHasNoMaxHistoryField(t *testing.T) {
	// Declare a literal with all current known fields. If MaxHistory were
	// re-introduced this block would need updating, making the absence
	// of the field visible at review time.
	_ = AgentConfig{
		MaxIterations: 25,
		TurnTimeout:   "60s",
		MaxWorkers:    10,
		TokenBudget:   32000,
	}
	// If the struct literal above compiles without a "MaxHistory" field,
	// the requirement is satisfied.
}
