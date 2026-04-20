// Package telegram implements a Telegram Bot API channel plugin using long polling.
package telegram

import "encoding/json"

// ---------------------------------------------------------------------------
// Telegram Bot API response types (unexported)
// ---------------------------------------------------------------------------

// tgApiResponse is the generic wrapper returned by all Telegram Bot API methods.
type tgApiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

// tgUpdate represents a single update from the getUpdates response.
type tgUpdate struct {
	UpdateID      int        `json:"update_id"`
	Message       *tgMessage `json:"message"`
	EditedMessage *tgMessage `json:"edited_message"`
}

// tgMessage represents a Telegram message.
type tgMessage struct {
	MessageID      int               `json:"message_id"`
	From           *tgUser           `json:"from"`
	Chat           tgChat            `json:"chat"`
	Date           int64             `json:"date"`
	Text           string            `json:"text"`
	Photo          []tgPhotoSize     `json:"photo"`
	Voice          *tgVoice          `json:"voice"`
	VideoNote      *tgVideoNote      `json:"video_note"`
	Document       *tgDocument       `json:"document"`
	Video          *tgVideo          `json:"video"`
	Audio          *tgAudio          `json:"audio"`
	Caption        string            `json:"caption"`
	ReplyToMessage *tgMessage        `json:"reply_to_message"`
	Entities       []tgMessageEntity `json:"entities"`
	ForwardOrigin  *tgForwardOrigin  `json:"forward_origin"`
}

// tgForwardOrigin represents the origin of a forwarded message (Bot API 7.0+).
type tgForwardOrigin struct {
	Type            string  `json:"type"`              // "user", "hidden_user", "chat", "channel"
	Date            int64   `json:"date"`
	SenderUser      *tgUser `json:"sender_user"`       // type "user"
	SenderUserName  string  `json:"sender_user_name"`  // type "hidden_user"
	SenderChat      *tgChat `json:"sender_chat"`       // type "chat" or "channel"
	AuthorSignature string  `json:"author_signature"`  // type "channel" (optional)
	MessageID       int     `json:"message_id"`        // type "channel"
}

// tgUser represents a Telegram user.
type tgUser struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

// tgChat represents a Telegram chat.
type tgChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"` // "private", "group", "supergroup", "channel"
	Title    string `json:"title"`
	Username string `json:"username"`
}

// tgPhotoSize represents one resolution of a photo.
type tgPhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size"`
}

// tgVoice represents a voice message.
type tgVoice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	FileSize     int64  `json:"file_size"`
	MimeType     string `json:"mime_type"`
}

// tgVideoNote represents a video note (round video).
type tgVideoNote struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	Length       int    `json:"length"`
	FileSize     int64  `json:"file_size"`
}

// tgDocument represents a general file.
type tgDocument struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
}

// tgVideo represents a video file.
type tgVideo struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	Duration     int    `json:"duration"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

// tgAudio represents an audio file.
type tgAudio struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	Duration     int    `json:"duration"`
	Performer    string `json:"performer"`
	Title        string `json:"title"`
}

// tgMessageEntity represents an entity in a message (bot command, mention, etc.).
type tgMessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// tgFile is the response from the getFile API call.
type tgFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	FilePath     string `json:"file_path"`
}
