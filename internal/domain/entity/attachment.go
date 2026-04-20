package entity

// Attachment represents a file attached to a message (photo, voice, document, etc.).
type Attachment struct {
	ID       string // Generated UUID for agent reference
	Kind     string // "photo", "voice", "video_note", "document", "video", "audio"
	Filename string
	MimeType string
	Size     int64  // file size in bytes
	Path     string // local file path
	Caption  string // optional
}
