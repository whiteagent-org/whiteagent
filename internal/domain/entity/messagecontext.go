package entity

// MessageContext holds routing-only fields for a conversational message.
// Identity fields (TenantID, UserID) live on Message root.
// Chat-level routing data (channel, external chat) lives on entity.Chat.
type MessageContext struct {
	ExternalUserID    string // Carried for outbound routing convenience
	ExternalMessageID string // Original platform message ID
	ExternalReplyToID string // Raw platform reply ID from DTO; only set on inbound user replies
}
