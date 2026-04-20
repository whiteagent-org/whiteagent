package port

import (
	"context"
	"encoding/json"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ToolContext carries runtime-injected context for tool execution.
// TenantID, UserID, ChatID, IsGroup, MessageID, and ConversationID
// are set by the agent loop from session state; they are NEVER derived from LLM output.
// Tools that need delivery/indication context look up the Chat entity by ChatID.
type ToolContext struct {
	TenantID       entity.TenantID
	AgentID        entity.AgentID
	UserID         entity.UserID
	ChatID         entity.ChatID
	IsGroup        bool
	MessageID      entity.MessageID
	ConversationID entity.ConversationID
}

// ToolPlugin executes tool calls requested by the LLM.
// ToolContext is injected by the runtime from session context and carries
// ChatID + IsGroup (chat scope) and MessageID (originating user message
// for reaction targeting). (PLUG-02, PLUG-03, TOOL-05)
type ToolPlugin interface {
	Plugin
	Name() string                // Tool function name used in tool schema
	Description() string         // Human-readable description for LLM
	Parameters() json.RawMessage // JSON Schema describing tool parameters
	Instructions() string        // Embedded instructions template text injected into system prompt
	Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error)
}

// EphemeralTool may be implemented by a ToolPlugin to control whether its
// result messages are auto-evicted after the reply is sent.
// By default (no interface), all tool results are evicted post-reply.
// Implement IsEphemeral() returning false to opt out and keep results in context.
type EphemeralTool interface {
	IsEphemeral() bool
}
