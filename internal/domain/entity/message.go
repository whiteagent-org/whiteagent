package entity

import "time"

// ---------------------------------------------------------------------------
// Message -- unified type for LLM history and session storage
// ---------------------------------------------------------------------------

// MessageKind distinguishes regular text messages from reactions.
type MessageKind string

const (
	// MessageKindMessage is a regular text message.
	MessageKindMessage MessageKind = "message"

	// MessageKindReaction is an emoji reaction targeting another message.
	MessageKindReaction MessageKind = "reaction"

	// MessageKindCron is a scheduled task execution (synthetic, not user-initiated).
	MessageKindCron MessageKind = "cron"
)

// Role represents the speaker role in an LLM conversation turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
	ID        string // Opaque tool call ID for correlation with tool results
	Name      string // Tool function name
	Arguments string // JSON-encoded arguments as a string
}

// Message is the unified domain type used for both channel routing and
// LLM conversation history storage. It is a tenant-owned entity with a
// stable MessageID. TenantID and UserID live at the root level for
// structural query scoping; MessageContext holds routing-only fields.
type Message struct {
	ID             MessageID
	TenantID       TenantID
	UserID         UserID
	AgentID        AgentID
	ConversationID ConversationID
	ChatID         ChatID
	IsGroup        bool // Eager: set at creation time from chat's is_group
	MessageContext MessageContext
	Kind           MessageKind // "message" or "reaction"
	RepliedToID    MessageID   // For replies: the internal message being replied to; empty otherwise
	TargetID       MessageID   // For replies/reactions: the message being replied/reacted to; empty otherwise
	CausedByID     MessageID   // For responses: the user message that triggered this; empty otherwise
	Role           Role
	Content        string     // Text content; empty for pure tool-call assistant turns
	ToolCalls      []ToolCall // Populated for RoleAssistant turns requesting tool calls
	ToolCallID     string     // Populated for RoleTool turns (correlates with ToolCall.ID)
	ToolName       string     // Tool name; populated for RoleTool turns
	Attachments    []Attachment
	IsMention      bool
	Evicted        bool // true when message has been evicted from active context
	Metadata       map[string]string
	CreatedAt      time.Time
}

// NewReply creates a reply skeleton from this message, copying shared routing
// fields (tenant, user, agent, chat) and setting CausedByID to this message's ID.
func (m Message) NewReply(id MessageID, role Role) Message {
	return Message{
		ID:             id,
		TenantID:       m.TenantID,
		UserID:         m.UserID,
		AgentID:        m.AgentID,
		ChatID:         m.ChatID,
		IsGroup:        m.IsGroup,
		ConversationID: m.ConversationID,
		CausedByID:     m.ID,
		RepliedToID:    m.ID,
		TargetID:       m.ID,
		Role:           role,
		CreatedAt:      time.Now().UTC(),
	}
}
