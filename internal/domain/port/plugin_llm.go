package port

import (
	"context"
	"encoding/json"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ToolDef describes a tool available to the LLM.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema object
}

// CompletionRequest is sent to a LLMPlugin to request an LLM completion.
type CompletionRequest struct {
	TenantID entity.TenantID
	Messages []entity.Message // Full conversation history including system prompt as first message
	Tools    []ToolDef        // Tool definitions available to the LLM (may be empty)
	Model    string           // Model name selected by the model router
	Stream   bool             // Whether the provider should stream internally (accumulated before return)
}

// CompletionResponse is returned by a LLMPlugin after LLM completion.
type CompletionResponse struct {
	Content      string            // Final text content (accumulated from stream if streaming)
	ToolCalls    []entity.ToolCall // Tool calls requested by the LLM (if any)
	FinishReason string            // "stop", "tool_calls", "length", "content_filter", etc.
	TokensIn     int
	TokensOut    int
	EndpointID   string // Populated by CompletionService after successful call (not by plugin)
	Model        string // Populated by CompletionService after successful call (not by plugin)
}

// LLMPlugin sends completion requests to LLM APIs.
// Streaming is an implementation detail — providers accumulate streams
// and return a complete CompletionResponse. (PLUG-02, PLUG-03)
type LLMPlugin interface {
	Plugin
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}
