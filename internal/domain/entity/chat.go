package entity

import "time"

// Chat represents a chat session (DM or group) bound to a tenant.
// Chats are auto-created on first encounter by the onboarding service.
// All chat-level routing data (delivery, indication) lives here;
// other entities reference chats via ChatID.
type Chat struct {
	ID             ChatID
	TenantID       TenantID
	ChannelID      string            // Channel plugin ID (e.g., "channel.telegram")
	ExternalChatID string            // External chat identifier on the channel platform
	UserID         UserID            // Populated for DMs, empty for groups
	IsGroup        bool
	Name           string            // Empty for DMs, populated for group chats
	AgentID        AgentID           // Optional override; zero value means use tenant default agent
	Delivery       map[string]string // Channel-specific routing data, JSON in SQLite
	Indication     map[string]string // Typing indicator routing data, JSON in SQLite
	CreatedAt      time.Time
}
