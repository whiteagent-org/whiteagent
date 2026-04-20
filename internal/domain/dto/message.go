// Package dto defines channel-agnostic data transfer objects for the whiteagent runtime.
// DTOs are the types that flow between the channel plugins, transport bus, and agent loop.
package dto

import (
	"time"
)

// Attachment is a file attached to a message (photo, voice, document, etc.).
// Separate DTO type from entity.Attachment — a message mapper converts between them.
type Attachment struct {
	ID       string // Generated UUID for agent reference
	Kind     string // "photo", "voice", "video_note", "document", "video", "audio"
	Filename string
	MimeType string
	Size     int64  // file size in bytes
	Path     string // local file path
	Caption  string // optional
}

// IncomingMessage is the channel-agnostic inbound message DTO.
// Channel plugins produce these; the runtime identity resolver and mapper consume them.
// Contains only raw channel data — no resolved domain IDs. The identity resolver
// returns a separate ResolvedIdentity struct; the mapper combines both into entity.Message.
type IncomingMessage struct {
	ID          string
	TenantID    string // Opaque platform workspace/tenant identifier (Slack workspace ID, Teams tenant ID, etc.)
	UserID      string // External user identifier on the channel platform
	ChatID      string // External chat identifier (always string, never int64)
	Content     string
	Attachments []Attachment
	IsGroup     bool
	IsMention   bool
	Metadata    map[string]string
	Delivery    map[string]string // Channel-specific delivery data (persisted on cron entries)
	ReplyToID   string            // Platform ID of the quoted/replied-to message; set by channel plugins
	Indication  map[string]string // Ephemeral channel-specific routing data for typing indicators

	// Name fields for entity creation, populated by channel plugins.
	AgentName  string // Bot/agent display name from channel platform
	TenantName string // Workspace/org name (empty if unavailable)
	UserName   string // Sender's display name
	GroupName  string // Group chat title (empty for DMs)

	ReceivedAt time.Time
}

// OutgoingMessage is the channel-agnostic outbound message DTO.
// The mapper produces these from entity.Message; channel plugins consume and deliver them.
type OutgoingMessage struct {
	ID          string
	TenantID    string // Opaque platform workspace/tenant identifier
	ChannelID   string // ID of the channel plugin that handles delivery
	ChatID      string // Where to send (always string, never int64)
	Kind        string // "message" or "reaction"
	TargetID    string // Target message: reaction target or reply-to; empty if none
	Content     string
	Attachments []Attachment
	Metadata    map[string]string
	Delivery    map[string]string // Channel-specific delivery data
}
