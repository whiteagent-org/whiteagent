package entity

// ---------------------------------------------------------------------------
// PluginKind enum
// ---------------------------------------------------------------------------

// PluginKind identifies the role a plugin plays in the runtime.
type PluginKind string

const (
	// PluginKindTransport is a pub/sub message delivery plugin (PLUG-02).
	PluginKindTransport PluginKind = "transport"

	// PluginKindChannel is an external platform adapter plugin (PLUG-02).
	PluginKindChannel PluginKind = "channel"

	// PluginKindLLM is an LLM API completions plugin (PLUG-02).
	PluginKindLLM PluginKind = "llm"

	// PluginKindTool is an LLM tool execution plugin (PLUG-02).
	PluginKindTool PluginKind = "tool"

	// PluginKindStore is a persistence plugin (PLUG-02).
	PluginKindStore PluginKind = "store"

	// PluginKindMiddleware wraps MessageHandler and is applied to both transport
	// and channel; order is determined by config (PLUG-02).
	PluginKindMiddleware PluginKind = "middleware"

	// PluginKindSandbox is a sandboxed execution environment plugin.
	PluginKindSandbox PluginKind = "sandbox"
)

// Topic constants for the internal message bus.
const (
	TopicInbound  = "inbound"  // Channel plugins publish here; agent loop subscribes
	TopicOutbound = "outbound" // Agent loop publishes here; channel plugins subscribe
)

// ---------------------------------------------------------------------------
// PluginState enum
// ---------------------------------------------------------------------------

// PluginState represents the health status of a plugin.
type PluginState string

const (
	PluginStateHealthy   PluginState = "healthy"
	PluginStateDegraded  PluginState = "degraded"
	PluginStateUnhealthy PluginState = "unhealthy"
)
