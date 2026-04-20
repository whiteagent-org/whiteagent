// Package config loads, resolves, and validates the whiteagent runtime config file.
// All types, Load(), env resolution, validation, and loader bridge methods live here.
// This file uses stdlib only — no external dependencies.
package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/infra/loader"
)

// ---------------------------------------------------------------------------
// Config struct hierarchy
// ---------------------------------------------------------------------------

// Config is the top-level config structure parsed from the JSON config file.
// Tenant and agent definitions are managed via the store, not this file.
type Config struct {
	Runtime    RuntimeConfig   `json:"runtime"`
	Gateway    GatewayConfig   `json:"gateway"`
	Agent      AgentConfig     `json:"agent"`
	Transport  PluginSingleton `json:"transport"`
	Store      PluginSingleton `json:"store"`
	Sandbox    PluginSingleton `json:"sandbox"`
	LLM        LLMConfig       `json:"llm"`
	Channels   []PluginDef     `json:"channels"`
	Tools      []PluginDef     `json:"tools"`
	Middleware []PluginDef     `json:"middleware"`
}

// GatewayConfig configures the HTTP gateway server.
type GatewayConfig struct {
	Address   string `json:"address"`    // Listen address, e.g. ":8080"
	PublicURL string `json:"public_url"` // Public-facing URL for external callbacks (e.g. secret entry links)
}

// PluginSingleton is for singleton plugin kinds (store, transport).
// Singletons have no enabled flag — they are always loaded when present.
type PluginSingleton struct {
	PluginID string          `json:"plugin_id"`
	Path     string          `json:"path"`
	Config   json.RawMessage `json:"config"`
}

// PluginDef is for multi-instance plugin kinds (channels, tools, middleware).
type PluginDef struct {
	PluginID string          `json:"plugin_id"`
	Enabled  bool            `json:"enabled"`
	Path     string          `json:"path"`
	Config   json.RawMessage `json:"config"`
}

// RuntimeConfig holds runtime operational settings: logging level, shutdown timeout, and timezone.
type RuntimeConfig struct {
	LoggingLevel          string `json:"logging_level"`           // "debug", "info", "warn", "error" — case-insensitive
	ShutdownTimeout       string `json:"shutdown_timeout"`        // Duration string (e.g. "30s"); consumed by main.go for graceful shutdown deadline
	SchedulerPollInterval string `json:"scheduler_poll_interval"` // Duration string (e.g. "60s"); how often the cron scheduler checks for due entries
	Timezone              string `json:"timezone"`                // IANA timezone (e.g. "America/New_York"); empty means UTC
	EncryptionKey         string `json:"encryption_key"`          // Master key for secret encryption; supports env: resolution; 64 hex chars = 32 bytes
	RedactSecrets         *bool  `json:"redact_secrets"`          // Replace decrypted secret values in output with [REDACTED]; default true
	SkillsDir             string `json:"skills_dir"`              // Global skills source directory; default "./skills/"
	DataDir               string `json:"data_dir"`                // Data directory for tenant/user home directories; default "./data/"
}

// AgentConfig holds agent loop settings.
type AgentConfig struct {
	MaxIterations int    `json:"max_iterations"` // default 25
	TurnTimeout   string `json:"turn_timeout"`   // default "60s", parsed to time.Duration by consumer
	MaxWorkers    int    `json:"max_workers"`    // default 10
	TokenBudget   int    `json:"token_budget"`   // default 32000, max tokens for conversation context window

	legacySummariesLimitSet bool `json:"-"`
}

// LLMConfig configures LLM drivers, endpoints, and routing.
type LLMConfig struct {
	Drivers    []LLMDriver       `json:"drivers"`
	Routing    LLMRouting        `json:"routing"`
	Compaction *CompactionConfig `json:"compaction"`
}

// CompactionConfig configures the dedicated model and threshold used for
// conversation summary generation.
type CompactionConfig struct {
	Model                  string  `json:"model"`
	Threshold              float64 `json:"threshold"`
	PreserveRecentMessages int     `json:"preserve_recent_messages"`

	thresholdSet              bool `json:"-"`
	preserveRecentMessagesSet bool `json:"-"`
}

// LLMDriver groups one plugin .so with its API endpoints.
type LLMDriver struct {
	Plugin    PluginDef     `json:"plugin"`
	Endpoints []LLMEndpoint `json:"endpoints"`
}

// LLMEndpoint is a specific API destination served by a driver.
type LLMEndpoint struct {
	ID      string          `json:"id"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"` // api_base, api_key, etc.
}

// LLMRouting selects which endpoint+model to use.
type LLMRouting struct {
	Primary         string   `json:"primary"`          // "endpoint_id:model_name"
	Fallback        []string `json:"fallback"`         // failover chain of "endpoint_id:model_name" entries
	CooldownSeconds int      `json:"cooldown_seconds"` // seconds to cool down an endpoint after error; default 30
}

// ---------------------------------------------------------------------------
// Load pipeline: parse → resolve → applyDefaults → validate
// ---------------------------------------------------------------------------

// Load reads and parses the JSON config file at path, resolves env: and env_path:
// prefixes in all string values, applies defaults, then validates the result.
// All validation errors are collected before returning — startup does not fail
// on missing env vars.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Step 1: parse raw JSON into map for env resolution.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Step 2: resolve env: and env_path: prefixes in all string values at any depth.
	resolveTree(raw)

	// Re-encode resolved map to JSON, then unmarshal into typed struct.
	resolved, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-encode resolved config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(resolved, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// Step 3: apply defaults (after env resolution, before validation).
	applyDefaults(&cfg)

	// Step 4: validate — collect all errors before returning.
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// applyDefaults fills in zero-valued config fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Runtime.ShutdownTimeout == "" {
		cfg.Runtime.ShutdownTimeout = "30s"
	}
	if cfg.Runtime.SchedulerPollInterval == "" {
		cfg.Runtime.SchedulerPollInterval = "60s"
	}
	if cfg.Agent.MaxIterations <= 0 {
		cfg.Agent.MaxIterations = 25
	}
	if cfg.Agent.TurnTimeout == "" {
		cfg.Agent.TurnTimeout = "60s"
	}
	if cfg.Agent.MaxWorkers <= 0 {
		cfg.Agent.MaxWorkers = 10
	}
	if cfg.Agent.TokenBudget <= 0 {
		cfg.Agent.TokenBudget = 32000
	}
	if cfg.LLM.Routing.CooldownSeconds <= 0 {
		cfg.LLM.Routing.CooldownSeconds = 30
	}
	if cfg.LLM.Compaction != nil && !cfg.LLM.Compaction.thresholdSet {
		cfg.LLM.Compaction.Threshold = 0.9
	}
	if cfg.LLM.Compaction != nil && !cfg.LLM.Compaction.preserveRecentMessagesSet {
		cfg.LLM.Compaction.PreserveRecentMessages = 6
	}
	if cfg.Runtime.RedactSecrets == nil {
		t := false
		cfg.Runtime.RedactSecrets = &t
	}
	if cfg.Runtime.SkillsDir == "" {
		cfg.Runtime.SkillsDir = "./skills/"
	}
	if cfg.Runtime.DataDir == "" {
		cfg.Runtime.DataDir = "./data/"
	}
}

// ---------------------------------------------------------------------------
// Env resolution helpers
// ---------------------------------------------------------------------------

// resolveTree recursively walks the parsed JSON tree and resolves env: / env_path:
// prefixes in all string values. Operates in-place on maps and slices.
func resolveTree(v any) any {
	switch val := v.(type) {
	case string:
		return resolveString(val)
	case map[string]any:
		for k, child := range val {
			val[k] = resolveTree(child)
		}
		return val
	case []any:
		for i, child := range val {
			val[i] = resolveTree(child)
		}
		return val
	default:
		return v // bool, float64, nil — not strings, pass through
	}
}

// resolveString resolves a single string value.
//   - "env:VAR" → value of $VAR; missing var → empty string + slog.Warn
//   - "env_path:VAR" → trimmed file contents at path given by $VAR; missing var or
//     unreadable file → empty string + slog.Warn
//   - anything else → returned unchanged
//
// slog.Warn is used directly because the global logger is set up before Load is called
// (two-phase init in main.go). Missing env vars do NOT fail startup.
func resolveString(s string) string {
	switch {
	case strings.HasPrefix(s, "env:"):
		varName := strings.TrimPrefix(s, "env:")
		val, ok := os.LookupEnv(varName)
		if !ok {
			slog.Warn("env var not set, resolving to empty string", "var", varName)
			return ""
		}
		return val

	case strings.HasPrefix(s, "env_path:"):
		varName := strings.TrimPrefix(s, "env_path:")
		filePath, ok := os.LookupEnv(varName)
		if !ok {
			slog.Warn("env_path var not set, resolving to empty string", "var", varName)
			return ""
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("env_path file read failed, resolving to empty string",
				"var", varName, "path", filePath, "err", err)
			return ""
		}
		// Trim leading/trailing whitespace and newlines per CONTEXT.md.
		return strings.TrimSpace(string(content))

	default:
		return s
	}
}

func (c *AgentConfig) UnmarshalJSON(data []byte) error {
	type alias AgentConfig
	var base alias
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*c = AgentConfig(base)

	var aux struct {
		SummariesLimit *int `json:"summaries_limit"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	c.legacySummariesLimitSet = aux.SummariesLimit != nil
	return nil
}

func (c *CompactionConfig) UnmarshalJSON(data []byte) error {
	type alias CompactionConfig
	var base alias
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*c = CompactionConfig(base)

	var aux struct {
		Threshold              *float64 `json:"threshold"`
		PreserveRecentMessages *int     `json:"preserve_recent_messages"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	c.thresholdSet = aux.Threshold != nil
	c.preserveRecentMessagesSet = aux.PreserveRecentMessages != nil
	return nil
}

// ---------------------------------------------------------------------------
// ID qualification helper
// ---------------------------------------------------------------------------

// QualifyID returns a kind-prefixed plugin ID. If rawID already starts with
// "kind.", it is returned unchanged to prevent double-prefixing (e.g.
// "channel.channel.telegram"). Otherwise "kind." is prepended.
func QualifyID(kind, rawID string) string {
	prefix := kind + "."
	if strings.HasPrefix(rawID, prefix) {
		return rawID
	}
	return prefix + rawID
}

// ---------------------------------------------------------------------------
// Validation — collect all errors before returning
// ---------------------------------------------------------------------------

// validate checks all required fields and detects duplicate plugin IDs across sections.
// Uses errors.Join so all problems are visible in one startup failure message.
func validate(cfg *Config) error {
	var errs []error

	// Encryption key is required and must decode to exactly 32 bytes.
	if cfg.Runtime.EncryptionKey == "" {
		errs = append(errs, fmt.Errorf("runtime.encryption_key: required (set via env: or provide 64 hex chars)"))
	} else {
		keyBytes, err := hex.DecodeString(cfg.Runtime.EncryptionKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("runtime.encryption_key: invalid hex encoding: %w", err))
		} else if len(keyBytes) != 32 {
			errs = append(errs, fmt.Errorf("runtime.encryption_key: must be 32 bytes (64 hex chars), got %d bytes", len(keyBytes)))
		}
	}

	// Gateway address is required.
	if cfg.Gateway.Address == "" {
		errs = append(errs, fmt.Errorf("gateway.address: required"))
	}
	if cfg.Agent.legacySummariesLimitSet {
		errs = append(errs, fmt.Errorf("agent.summaries_limit: removed; use llm.compaction.preserve_recent_messages"))
	}

	// Singleton: transport — both plugin_id and path are required.
	if cfg.Transport.Path == "" {
		errs = append(errs, fmt.Errorf("transport.path: required"))
	}
	if cfg.Transport.PluginID == "" {
		errs = append(errs, fmt.Errorf("transport.plugin_id: required"))
	}

	// Singleton: store — both plugin_id and path are required.
	if cfg.Store.Path == "" {
		errs = append(errs, fmt.Errorf("store.path: required"))
	}
	if cfg.Store.PluginID == "" {
		errs = append(errs, fmt.Errorf("store.plugin_id: required"))
	}

	// Singleton: sandbox — both plugin_id and path are required.
	if cfg.Sandbox.Path == "" {
		errs = append(errs, fmt.Errorf("sandbox.path: required"))
	}
	if cfg.Sandbox.PluginID == "" {
		errs = append(errs, fmt.Errorf("sandbox.plugin_id: required"))
	}

	// Track all fully-qualified (kind-prefixed) plugin IDs to detect duplicates.
	// Value is a human-readable location string for collision reporting.
	seenIDs := make(map[string]string)

	// Singletons go into seenIDs first (kind-prefixed).
	if cfg.Transport.PluginID != "" {
		seenIDs[QualifyID("transport", cfg.Transport.PluginID)] = "transport"
	}
	if cfg.Store.PluginID != "" {
		seenIDs[QualifyID("store", cfg.Store.PluginID)] = "store"
	}
	if cfg.Sandbox.PluginID != "" {
		seenIDs[QualifyID("sandbox", cfg.Sandbox.PluginID)] = "sandbox"
	}

	// checkDef validates a single PluginDef entry and tracks its kind-prefixed ID.
	checkDef := func(section string, kind string, idx int, id, path string) {
		if path == "" {
			errs = append(errs, fmt.Errorf("%s[%d].path: required", section, idx))
		}
		if id == "" {
			errs = append(errs, fmt.Errorf("%s[%d].plugin_id: required", section, idx))
		} else {
			qualifiedID := QualifyID(kind, id)
			if prev, dup := seenIDs[qualifiedID]; dup {
				slog.Warn("duplicate plugin ID, later entry overrides", "id", qualifiedID, "previous", prev, "current", fmt.Sprintf("%s[%d]", section, idx))
			}
			seenIDs[qualifiedID] = fmt.Sprintf("%s[%d]", section, idx)
		}
	}

	// LLM drivers and endpoints validation.
	if len(cfg.LLM.Drivers) == 0 {
		errs = append(errs, fmt.Errorf("llm.drivers: at least one driver required"))
	}

	seenEndpointIDs := make(map[string]string)
	for i, drv := range cfg.LLM.Drivers {
		if drv.Plugin.Path == "" {
			errs = append(errs, fmt.Errorf("llm.drivers[%d].plugin.path: required", i))
		}
		if drv.Plugin.PluginID == "" {
			errs = append(errs, fmt.Errorf("llm.drivers[%d].plugin.plugin_id: required", i))
		} else {
			qualifiedID := QualifyID("llm", drv.Plugin.PluginID)
			if prev, dup := seenIDs[qualifiedID]; dup {
				slog.Warn("duplicate plugin ID, later entry overrides", "id", qualifiedID, "previous", prev, "current", fmt.Sprintf("llm.drivers[%d]", i))
			}
			seenIDs[qualifiedID] = fmt.Sprintf("llm.drivers[%d]", i)
		}
		for j, ep := range drv.Endpoints {
			if ep.ID == "" {
				errs = append(errs, fmt.Errorf("llm.drivers[%d].endpoints[%d].id: required", i, j))
			} else if prev, dup := seenEndpointIDs[ep.ID]; dup {
				errs = append(errs, fmt.Errorf("llm.drivers[%d].endpoints[%d].id %q: duplicate (also in %s)", i, j, ep.ID, prev))
			} else {
				seenEndpointIDs[ep.ID] = fmt.Sprintf("llm.drivers[%d].endpoints[%d]", i, j)
			}
		}
	}

	// Routing validation.
	if cfg.LLM.Routing.Primary == "" {
		errs = append(errs, fmt.Errorf("llm.routing.primary: required"))
	} else if !strings.Contains(cfg.LLM.Routing.Primary, ":") {
		errs = append(errs, fmt.Errorf("llm.routing.primary: must be \"endpoint_id:model_name\" format"))
	}
	if cfg.LLM.Compaction != nil {
		if strings.TrimSpace(cfg.LLM.Compaction.Model) == "" {
			errs = append(errs, fmt.Errorf("llm.compaction.model: required"))
		} else if !strings.Contains(cfg.LLM.Compaction.Model, ":") {
			errs = append(errs, fmt.Errorf("llm.compaction.model: must be \"endpoint_id:model_name\" format"))
		}
		if cfg.LLM.Compaction.thresholdSet {
			if cfg.LLM.Compaction.Threshold <= 0 || cfg.LLM.Compaction.Threshold > 1.0 {
				errs = append(errs, fmt.Errorf("llm.compaction.threshold: must be within (0, 1.0]"))
			}
		}
		if cfg.LLM.Compaction.PreserveRecentMessages <= 0 {
			errs = append(errs, fmt.Errorf("llm.compaction.preserve_recent_messages: must be greater than 0"))
		}
	}

	// Build set of enabled endpoint IDs for fallback/primary validation.
	enabledEndpointIDs := make(map[string]bool)
	for _, drv := range cfg.LLM.Drivers {
		for _, ep := range drv.Endpoints {
			if ep.Enabled && ep.ID != "" {
				enabledEndpointIDs[ep.ID] = true
			}
		}
	}

	// Validate primary endpoint ID exists in enabled endpoints.
	if cfg.LLM.Routing.Primary != "" && strings.Contains(cfg.LLM.Routing.Primary, ":") {
		epID := strings.SplitN(cfg.LLM.Routing.Primary, ":", 2)[0]
		if !enabledEndpointIDs[epID] {
			errs = append(errs, fmt.Errorf("llm.routing.primary: endpoint %q not found in enabled endpoints", epID))
		}
	}

	// Validate fallback endpoint IDs.
	for i, fb := range cfg.LLM.Routing.Fallback {
		if !strings.Contains(fb, ":") {
			errs = append(errs, fmt.Errorf("llm.routing.fallback[%d]: must be \"endpoint_id:model_name\" format", i))
			continue
		}
		epID := strings.SplitN(fb, ":", 2)[0]
		if !enabledEndpointIDs[epID] {
			errs = append(errs, fmt.Errorf("llm.routing.fallback[%d]: endpoint %q not found in enabled endpoints", i, epID))
		}
	}
	if cfg.LLM.Compaction != nil && strings.Contains(cfg.LLM.Compaction.Model, ":") {
		epID := strings.SplitN(cfg.LLM.Compaction.Model, ":", 2)[0]
		if !enabledEndpointIDs[epID] {
			errs = append(errs, fmt.Errorf("llm.compaction.model: endpoint %q not found in enabled endpoints", epID))
		}
	}

	for i, e := range cfg.Channels {
		checkDef("channels", "channel", i, e.PluginID, e.Path)
	}
	for i, e := range cfg.Tools {
		checkDef("tools", "tool", i, e.PluginID, e.Path)
	}
	for i, e := range cfg.Middleware {
		checkDef("middleware", "middleware", i, e.PluginID, e.Path)
	}

	return errors.Join(errs...)
}

// ---------------------------------------------------------------------------
// Loader bridge methods
// ---------------------------------------------------------------------------

// LoaderEntries converts the parsed config into the []loader.PluginEntry slice
// that Loader.Load() consumes. Only enabled entries are included.
// Singletons (transport, store) are always included — they have no enabled flag.
// LLM drivers produce one loader entry per enabled endpoint, all sharing the
// driver's .so path but carrying per-endpoint config.
func (cfg *Config) LoaderEntries() []loader.PluginEntry {
	var entries []loader.PluginEntry

	entries = append(entries, loader.PluginEntry{
		Path:         cfg.Transport.Path,
		ExpectedKind: entity.PluginKindTransport,
		Config:       cfg.Transport.Config,
		ID:           QualifyID("transport", cfg.Transport.PluginID),
	})
	entries = append(entries, loader.PluginEntry{
		Path:         cfg.Store.Path,
		ExpectedKind: entity.PluginKindStore,
		Config:       cfg.Store.Config,
		ID:           QualifyID("store", cfg.Store.PluginID),
	})
	entries = append(entries, loader.PluginEntry{
		Path:         cfg.Sandbox.Path,
		ExpectedKind: entity.PluginKindSandbox,
		Config:       cfg.Sandbox.Config,
		ID:           QualifyID("sandbox", cfg.Sandbox.PluginID),
	})

	// LLM: iterate drivers, for each driver iterate enabled endpoints.
	for _, drv := range cfg.LLM.Drivers {
		for _, ep := range drv.Endpoints {
			if ep.Enabled {
				entries = append(entries, loader.PluginEntry{
					Path:         drv.Plugin.Path,
					ExpectedKind: entity.PluginKindLLM,
					Config:       ep.Config,
					ID:           QualifyID("llm", ep.ID),
				})
			}
		}
	}

	for _, e := range cfg.Channels {
		if e.Enabled {
			entries = append(entries, loader.PluginEntry{
				Path:         e.Path,
				ExpectedKind: entity.PluginKindChannel,
				Config:       e.Config,
				ID:           QualifyID("channel", e.PluginID),
			})
		}
	}
	for _, e := range cfg.Tools {
		if e.Enabled {
			entries = append(entries, loader.PluginEntry{
				Path:         e.Path,
				ExpectedKind: entity.PluginKindTool,
				Config:       e.Config,
				ID:           QualifyID("tool", e.PluginID),
			})
		}
	}
	for _, e := range cfg.Middleware {
		if e.Enabled {
			entries = append(entries, loader.PluginEntry{
				Path:         e.Path,
				ExpectedKind: entity.PluginKindMiddleware,
				Config:       e.Config,
				ID:           QualifyID("middleware", e.PluginID),
			})
		}
	}
	return entries
}

// ConfigsByID produces the map[plugin_id]json.RawMessage consumed by Manager.Init.
// Includes all plugins (enabled and disabled) — the manager only calls Init on
// registered plugins, so disabled ones never appear in the registry anyway.
// For LLM: maps endpoint.ID -> endpoint.Config, plus driver plugin_id -> driver config (if set).
func (cfg *Config) ConfigsByID() map[string]json.RawMessage {
	m := make(map[string]json.RawMessage)
	m[QualifyID("transport", cfg.Transport.PluginID)] = cfg.Transport.Config
	m[QualifyID("store", cfg.Store.PluginID)] = cfg.Store.Config
	m[QualifyID("sandbox", cfg.Sandbox.PluginID)] = cfg.Sandbox.Config

	for _, drv := range cfg.LLM.Drivers {
		if drv.Plugin.PluginID != "" && drv.Plugin.Config != nil {
			m[QualifyID("llm", drv.Plugin.PluginID)] = drv.Plugin.Config
		}
		for _, ep := range drv.Endpoints {
			m[QualifyID("llm", ep.ID)] = ep.Config
		}
	}

	for _, e := range cfg.Channels {
		m[QualifyID("channel", e.PluginID)] = e.Config
	}
	for _, e := range cfg.Tools {
		m[QualifyID("tool", e.PluginID)] = e.Config
	}
	for _, e := range cfg.Middleware {
		m[QualifyID("middleware", e.PluginID)] = e.Config
	}
	return m
}
