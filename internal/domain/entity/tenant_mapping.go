package entity

// TenantMapping maps a channel's external workspace identifier to an internal tenant.
// The combination of ChannelID + ExternalTenantID is unique (1:1 mapping).
type TenantMapping struct {
	ChannelID        string   // Plugin ID of the channel (e.g. "channel.telegram")
	ExternalTenantID string   // Platform workspace ID (e.g. bot_id for Telegram)
	TenantID         TenantID // Internal tenant this workspace maps to
}
