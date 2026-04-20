// Package teams implements a Microsoft Teams Bot Framework channel plugin.
package teams

import "encoding/json"

// ---------------------------------------------------------------------------
// Bot Framework Activity types
// ---------------------------------------------------------------------------

// Activity represents a Bot Framework v4 Activity object received via webhook.
type Activity struct {
	Type         string               `json:"type"`
	ID           string               `json:"id"`
	Timestamp    string               `json:"timestamp"`
	ServiceURL   string               `json:"serviceUrl"`
	ChannelID    string               `json:"channelId"`
	From         ChannelAccount       `json:"from"`
	Conversation ConversationAccount  `json:"conversation"`
	Recipient    ChannelAccount       `json:"recipient"`
	ReplyToID    string               `json:"replyToId"`
	Text         string               `json:"text"`
	Entities     []Entity             `json:"entities"`
	Attachments  []ActivityAttachment `json:"attachments"`
	ChannelData  json.RawMessage      `json:"channelData"`
}

// ActivityAttachment represents an attachment in a Bot Framework Activity.
type ActivityAttachment struct {
	ContentType string          `json:"contentType"`
	ContentURL  string          `json:"contentUrl"`
	Name        string          `json:"name"`
	Content     json.RawMessage `json:"content"`
}

// ChannelAccount represents a user or bot in a Teams conversation.
type ChannelAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AadObjectID string `json:"aadObjectId"`
}

// ConversationAccount identifies a Teams conversation.
type ConversationAccount struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ConversationType string `json:"conversationType"`
	TenantID         string `json:"tenantId"`
	IsGroup          bool   `json:"isGroup"`
}

// Entity represents an entity within an Activity (e.g., @mentions).
type Entity struct {
	Type      string          `json:"type"`
	Mentioned *ChannelAccount `json:"mentioned"`
}

// ---------------------------------------------------------------------------
// Internal types (unexported)
// ---------------------------------------------------------------------------

// conversationRef stores routing info for outbound messages.
// Also used as the opaque indication value passed through the domain layer.
type conversationRef struct {
	ServiceURL     string
	ConversationID string
	BotID          string
	RecipientID    string
}

// tokenResponse is the OAuth2 token endpoint JSON response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// openIDConfig is the OpenID metadata response.
type openIDConfig struct {
	JwksURI string `json:"jwks_uri"`
}

// jwksResponse is the JWKS endpoint JSON response.
type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

// jwksKey represents a single key from the JWKS endpoint.
type jwksKey struct {
	Kid          string   `json:"kid"`
	X5c          []string `json:"x5c"`
	Endorsements []string `json:"endorsements"`
}
