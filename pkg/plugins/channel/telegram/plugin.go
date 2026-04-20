package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

const defaultPluginID = "channel.telegram"

// Compile-time interface checks.
var (
	_ port.ChannelPlugin  = (*Plugin)(nil)
	_ port.IndicatorAware = (*Plugin)(nil)
	_ port.ReactionAware  = (*Plugin)(nil)
)

// pluginConfig is the JSON config shape for the Telegram channel plugin.
type pluginConfig struct {
	BotToken            string `json:"bot_token"`
	MaxDownloadFilesize int64  `json:"max_download_filesize"` // bytes; 0 = unlimited
}

// Plugin implements port.ChannelPlugin for Telegram via long polling.
type Plugin struct {
	id       string
	botID    string // extracted from bot token (digits before first colon)
	botName  string // display name fetched via getMe at startup
	log      *slog.Logger
	api      *apiClient
	handler  port.IncomingMessageHandler
	config   pluginConfig
	cancel   context.CancelFunc
	done     chan struct{}
	degraded atomic.Bool
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindChannel}
}

// NewPlugin returns a new Telegram channel plugin instance.
func NewPlugin() port.Plugin {
	return &Plugin{id: defaultPluginID}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return p.id }

// Kind returns the plugin kind.
func (p *Plugin) Kind() entity.PluginKind { return entity.PluginKindChannel }

// Status returns the plugin health state.
func (p *Plugin) Status() entity.PluginState {
	if p.degraded.Load() {
		return entity.PluginStateDegraded
	}
	return entity.PluginStateHealthy
}

// SetMessageHandler stores the inbound message handler provided by the runtime.
func (p *Plugin) SetMessageHandler(handler port.IncomingMessageHandler) {
	p.handler = handler
}

// Indicate sends an immediate typing indicator and starts a background ticker
// that refreshes it every 4 seconds. The returned stop function terminates
// the ticker goroutine and is safe to call multiple times.
func (p *Plugin) Indicate(ctx context.Context, indication map[string]string) (stop func()) {
	chatExternalID := indication["chat_id"]
	if chatExternalID == "" {
		return func() {}
	}

	// Send immediate typing action (no gap before first tick).
	_, _ = p.api.call(ctx, "sendChatAction", map[string]any{
		"chat_id": chatExternalID,
		"action":  "typing",
	})

	done := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Use context.Background() -- turn ctx may be cancelled on timeout
				// but we want typing to continue until stop() is called.
				_, _ = p.api.call(context.Background(), "sendChatAction", map[string]any{
					"chat_id": chatExternalID,
					"action":  "typing",
				})
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
			// Send cancel action to clear typing indicator immediately.
			// Without this, typing lingers ~5s after reactions or no_reply
			// because no message is sent to clear it.
			_, _ = p.api.call(context.Background(), "sendChatAction", map[string]any{
				"chat_id": chatExternalID,
				"action":  "cancel",
			})
		})
	}
}

// SupportsReactions implements port.ReactionAware.
func (*Plugin) SupportsReactions() {}

// RegisterRoutes is a no-op for long-polling mode.
func (p *Plugin) RegisterRoutes(_ *http.ServeMux) {}

// Init parses config and creates the API client.
func (p *Plugin) Init(ctx context.Context, id string, cfg json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	p.log = logger.FromCtx(ctx).With("component", "telegram", "id", p.id)

	if err := json.Unmarshal(cfg, &p.config); err != nil {
		return fmt.Errorf("telegram: parse config: %w", err)
	}
	if p.config.BotToken == "" {
		return fmt.Errorf("telegram: bot_token is required")
	}
	// Extract bot_id from token (format: "botID:secret").
	if idx := strings.Index(p.config.BotToken, ":"); idx > 0 {
		p.botID = p.config.BotToken[:idx]
	}

	p.api = newAPIClient(p.config.BotToken, p.log)
	p.log.Info("telegram plugin initialized", "bot_id", p.botID)
	return nil
}

// Start begins the long-polling loop in a background goroutine.
func (p *Plugin) Start(ctx context.Context) error {
	// Fetch bot info to populate AgentName on incoming messages.
	botInfo, err := p.api.getMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram: getMe: %w", err)
	}
	p.botName = strings.TrimSpace(botInfo.FirstName + " " + botInfo.LastName)

	pollCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})

	p.log.Info("telegram plugin starting long poll", "bot_name", p.botName)
	go p.pollLoop(pollCtx)
	return nil
}

// Stop cancels the poll loop and waits for it to finish.
func (p *Plugin) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.log.Info("telegram plugin stopped")
	return nil
}

// ---------------------------------------------------------------------------
// Long polling
// ---------------------------------------------------------------------------

func (p *Plugin) pollLoop(ctx context.Context) {
	defer close(p.done)

	var offset int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		result, err := p.api.call(ctx, "getUpdates", map[string]any{
			"offset":  offset,
			"limit":   100,
			"timeout": 30,
		})
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled — clean shutdown
			}
			p.log.Warn("telegram: poll error", "err", err)
			p.degraded.Store(true)
			time.Sleep(time.Second)
			continue
		}
		p.degraded.Store(false)

		var updates []tgUpdate
		if err := json.Unmarshal(result, &updates); err != nil {
			p.log.Warn("telegram: unmarshal updates", "err", err)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			p.handleUpdate(ctx, u)
		}
	}
}

func (p *Plugin) handleUpdate(ctx context.Context, update tgUpdate) {
	msg := update.Message
	if msg == nil {
		msg = update.EditedMessage
	}
	if msg == nil {
		return
	}
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	isGroup := msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"

	// Extract text content.
	content := msg.Text
	if content == "" {
		content = msg.Caption
	}

	// Extract sender info.
	var userExternalID string
	if msg.From != nil {
		userExternalID = strconv.FormatInt(msg.From.ID, 10)
	}

	msgIDStr := strconv.Itoa(msg.MessageID)

	// Extract attachments.
	attachments := p.extractAttachments(ctx, chatID, msg)

	// Extract reply-to message ID if the user quoted a message.
	var replyToID string
	if msg.ReplyToMessage != nil {
		replyToID = strconv.Itoa(msg.ReplyToMessage.MessageID)
	}

	isMention := !isGroup || p.isBotMentioned(msg)

	// Build sender display name.
	var userName string
	if msg.From != nil {
		userName = strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
	}

	incoming := dto.IncomingMessage{
		ID:          msgIDStr,
		TenantID:    p.botID,
		ChatID:      chatID,
		UserID:      userExternalID,
		Content:     content,
		IsGroup:     isGroup,
		IsMention:   isMention,
		Attachments: attachments,
		ReplyToID:   replyToID,
		Metadata:    buildMetadata(msg, isGroup, isMention),
		Delivery:    map[string]string{"chat_id": chatID},
		Indication:  map[string]string{"chat_id": chatID},
		AgentName:   p.botName,
		UserName:    userName,
		GroupName:   msg.Chat.Title,
		ReceivedAt:  time.Now(),
	}

	if p.handler != nil {
		if err := p.handler(ctx, incoming); err != nil {
			p.log.Error("telegram: handler error", "err", err, "chat_id", chatID)
		}
	}
}

// isBotMentioned checks if the bot is mentioned in entities or the message is a reply to the bot.
func (p *Plugin) isBotMentioned(msg *tgMessage) bool {
	// Check if reply to a bot message.
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot {
		return true
	}
	// Check for bot_command entities.
	for _, e := range msg.Entities {
		if e.Type == "bot_command" || e.Type == "mention" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Metadata building
// ---------------------------------------------------------------------------

// buildMetadata extracts rich metadata from a Telegram message into a
// channel-agnostic string map. Empty values are omitted.
func buildMetadata(msg *tgMessage, isGroup bool, isMention bool) map[string]string {
	meta := make(map[string]string)

	// Formatting instruction for the LLM — Telegram supports a subset of HTML.
	meta["response_format"] = "Telegram HTML. Only these tags are allowed: <b>, <i>, <u>, <s>, <code>, <pre>, <a href=\"...\">. No other HTML tags exist — no <p>, <br>, <ul>, <li>, <ol>, <h1>–<h6>, <div>, <span>, <img>, <table>. Use plain text with newlines for structure. Do not use Markdown syntax."

	// Chat metadata (META-01).
	if msg.Chat.Type != "" {
		meta["chat_type"] = msg.Chat.Type
	}
	if msg.Chat.Title != "" {
		meta["chat_title"] = msg.Chat.Title
	}
	if msg.Chat.Username != "" {
		meta["chat_username"] = msg.Chat.Username
	}

	// Sender metadata (META-02).
	if msg.From != nil {
		name := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		if name != "" {
			meta["sender_name"] = name
		}
		if msg.From.Username != "" {
			meta["sender_username"] = msg.From.Username
		}
		meta["sender_is_bot"] = strconv.FormatBool(msg.From.IsBot)
		if msg.From.LanguageCode != "" {
			meta["sender_language"] = msg.From.LanguageCode
		}
	}

	// Message context (META-03).
	meta["message_date"] = time.Unix(msg.Date, 0).UTC().Format(time.RFC3339)

	if msg.ForwardOrigin != nil {
		meta["is_forwarded"] = "true"
		meta["forward_date"] = time.Unix(msg.ForwardOrigin.Date, 0).UTC().Format(time.RFC3339)

		var forwardName string
		switch msg.ForwardOrigin.Type {
		case "user":
			if msg.ForwardOrigin.SenderUser != nil {
				forwardName = strings.TrimSpace(
					msg.ForwardOrigin.SenderUser.FirstName + " " + msg.ForwardOrigin.SenderUser.LastName,
				)
			}
		case "hidden_user":
			forwardName = msg.ForwardOrigin.SenderUserName
		case "chat":
			if msg.ForwardOrigin.SenderChat != nil {
				forwardName = msg.ForwardOrigin.SenderChat.Title
			}
		case "channel":
			if msg.ForwardOrigin.SenderChat != nil {
				forwardName = msg.ForwardOrigin.SenderChat.Title
				if msg.ForwardOrigin.AuthorSignature != "" {
					forwardName += " (" + msg.ForwardOrigin.AuthorSignature + ")"
				}
			}
		}
		if forwardName != "" {
			meta["forward_from_name"] = forwardName
		}
	}

	// Group mention context.
	if isGroup {
		meta["is_mentioned"] = strconv.FormatBool(isMention)
	}

	// Allowed reaction emoji for this channel.
	meta["allowed_reactions"] = "👍 👎 ❤ 🔥 🥰 👏 😁 🤔 🤯 😱 🤬 😢 🎉 🤩 🤮 💩 🙏 👌 🕊 🤡 🥱 🥴 😍 🐳 ❤\u200d🔥 🌚 🌭 💯 🤣 ⚡ 🍌 🏆 💔 🤨 😐 🍓 🍾 💋 🖕 😈 😴 😭 🤓 👻 👨\u200d💻 👀 🎃 🙈 😇 😨 🤝 ✍ 🤗 🫡 🎅 🎄 ☃ 💅 🤪 🗿 🆒 💘 🙉 🦄 😘 💊 🙊 😎 👾 🤷\u200d♂ 🤷 🤷\u200d♀ 😡"

	return meta
}

// ---------------------------------------------------------------------------
// Attachment helpers
// ---------------------------------------------------------------------------

// bestPhoto returns the photo variant with the largest pixel area (Width*Height).
// Caller must ensure len(photos) > 0.
func bestPhoto(photos []tgPhotoSize) tgPhotoSize {
	best := photos[0]
	bestArea := best.Width * best.Height
	for _, ph := range photos[1:] {
		area := ph.Width * ph.Height
		if area > bestArea {
			best = ph
			bestArea = area
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Attachment extraction
// ---------------------------------------------------------------------------

func (p *Plugin) extractAttachments(ctx context.Context, chatID string, msg *tgMessage) []dto.Attachment {
	type fileInfo struct {
		kind     string
		fileID   string
		fileSize int64
		filename string
		mimeType string
	}

	var files []fileInfo

	// Photo: pick variant with largest pixel area (Width*Height).
	if len(msg.Photo) > 0 {
		photo := bestPhoto(msg.Photo)
		p.log.Debug("telegram: selected photo variant",
			"width", photo.Width, "height", photo.Height,
			"file_size", photo.FileSize, "variants", len(msg.Photo))
		files = append(files, fileInfo{kind: "photo", fileID: photo.FileID, fileSize: photo.FileSize})
	}
	if msg.Voice != nil {
		files = append(files, fileInfo{kind: "voice", fileID: msg.Voice.FileID, fileSize: msg.Voice.FileSize, mimeType: msg.Voice.MimeType})
	}
	if msg.VideoNote != nil {
		files = append(files, fileInfo{kind: "video_note", fileID: msg.VideoNote.FileID, fileSize: msg.VideoNote.FileSize})
	}
	if msg.Document != nil {
		files = append(files, fileInfo{kind: "document", fileID: msg.Document.FileID, fileSize: msg.Document.FileSize, filename: msg.Document.FileName, mimeType: msg.Document.MimeType})
	}
	if msg.Video != nil {
		files = append(files, fileInfo{kind: "video", fileID: msg.Video.FileID, fileSize: msg.Video.FileSize, filename: msg.Video.FileName, mimeType: msg.Video.MimeType})
	}
	if msg.Audio != nil {
		files = append(files, fileInfo{kind: "audio", fileID: msg.Audio.FileID, fileSize: msg.Audio.FileSize, filename: msg.Audio.FileName, mimeType: msg.Audio.MimeType})
	}

	if len(files) == 0 {
		return nil
	}

	var attachments []dto.Attachment
	for i, f := range files {
		// Check max download filesize.
		if p.config.MaxDownloadFilesize > 0 && f.fileSize > p.config.MaxDownloadFilesize {
			maxMB := p.config.MaxDownloadFilesize / (1024 * 1024)
			errMsg := fmt.Sprintf("File too large (max %d MB)", maxMB)
			_, _ = p.api.call(ctx, "sendMessage", map[string]any{
				"chat_id": chatID,
				"text":    errMsg,
			})
			p.log.Warn("telegram: file too large, skipped", "kind", f.kind, "size", f.fileSize)
			continue
		}

		// Get remote file path.
		remotePath, fileSize, err := p.api.downloadFile(ctx, f.fileID)
		if err != nil {
			p.log.Error("telegram: getFile failed", "err", err, "file_id", f.fileID)
			continue
		}
		if f.fileSize == 0 {
			f.fileSize = fileSize
		}

		// Create temp directory for download; runtime handler will relocate
		// to the final messages/{internal_uuid}/ path after ID assignment.
		dir, err := os.MkdirTemp("", "wa-attach-*")
		if err != nil {
			p.log.Error("telegram: create temp attachment dir", "err", err)
			continue
		}

		// Determine local filename.
		localFilename := f.filename
		if localFilename == "" {
			localFilename = filepath.Base(remotePath)
		}
		localPath := filepath.Join(dir, localFilename)

		// Download to local path.
		if err := p.api.downloadToPath(ctx, remotePath, localPath); err != nil {
			p.log.Error("telegram: download file", "err", err, "remote", remotePath)
			continue
		}
		p.log.Debug("telegram: downloaded attachment", "kind", f.kind, "size", f.fileSize, "file", localFilename)

		att := dto.Attachment{
			Kind:     f.kind,
			Filename: localFilename,
			MimeType: f.mimeType,
			Size:     f.fileSize,
			Path:     localPath,
		}
		// Caption goes only on the first attachment.
		if i == 0 && msg.Caption != "" {
			att.Caption = msg.Caption
		}

		attachments = append(attachments, att)
	}

	return attachments
}

// ---------------------------------------------------------------------------
// Send (outbound delivery)
// ---------------------------------------------------------------------------

// Send delivers an outgoing message to Telegram.
// Returns a SendResult with the platform message ID from the first successful sendMessage call.
func (p *Plugin) Send(ctx context.Context, msg dto.OutgoingMessage) (port.SendResult, error) {
	chatID := msg.Delivery["chat_id"]
	if chatID == "" {
		return port.SendResult{}, fmt.Errorf("telegram: missing chat_id in Delivery")
	}

	// Handle reactions.
	if msg.Kind == string(entity.MessageKindReaction) {
		if err := p.sendReaction(ctx, chatID, msg); err != nil {
			return port.SendResult{}, err
		}
		return port.SendResult{}, nil
	}

	var result port.SendResult

	// Send text content.
	if msg.Content != "" {
		msgID, err := p.sendText(ctx, chatID, msg.Content, msg.TargetID)
		if err != nil {
			return port.SendResult{}, err
		}
		if msgID != "" {
			result.MessageID = msgID
			result.Timestamp = time.Now()
		}
	}

	// Send attachments.
	for _, att := range msg.Attachments {
		if err := p.sendAttachment(ctx, chatID, att); err != nil {
			p.log.Error("telegram: send attachment", "err", err, "kind", att.Kind)
			// Continue sending remaining attachments.
		}
	}

	return result, nil
}

func (p *Plugin) sendReaction(ctx context.Context, chatID string, msg dto.OutgoingMessage) error {
	_, err := p.api.call(ctx, "setMessageReaction", map[string]any{
		"chat_id":    chatID,
		"message_id": msg.TargetID,
		"reaction":   []map[string]any{{"type": "emoji", "emoji": msg.Content}},
	})
	if err != nil {
		return fmt.Errorf("telegram: set reaction: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTML sending and message splitting
// ---------------------------------------------------------------------------

// sendText sends text content, returning the message_id from the first successful
// sendMessage call (used by the caller to populate SendResult).
func (p *Plugin) sendText(ctx context.Context, chatID, content string, replyTo string) (string, error) {
	parts := splitMessage(content, 4096)
	var firstMsgID string
	for _, part := range parts {
		params := map[string]any{
			"chat_id":    chatID,
			"text":       part,
			"parse_mode": "HTML",
		}
		if replyTo != "" {
			params["reply_parameters"] = map[string]any{"message_id": json.Number(replyTo)}
		}
		result, err := p.api.call(ctx, "sendMessage", params)
		if err != nil {
			// Fallback to plain text on parse error.
			p.log.Debug("telegram: HTML parse failed, falling back to plain text", "err", err)
			plainParams := map[string]any{
				"chat_id": chatID,
				"text":    part,
			}
			if replyTo != "" {
				plainParams["reply_parameters"] = map[string]any{"message_id": json.Number(replyTo)}
			}
			result, err = p.api.call(ctx, "sendMessage", plainParams)
			if err != nil {
				return "", fmt.Errorf("telegram: send message: %w", err)
			}
		}
		// Extract message_id from the first chunk's response.
		if firstMsgID == "" && result != nil {
			var sent tgMessage
			if jsonErr := json.Unmarshal(result, &sent); jsonErr == nil && sent.MessageID != 0 {
				firstMsgID = strconv.Itoa(sent.MessageID)
			}
		}
		// Only reply-thread the first chunk; subsequent chunks are standalone.
		replyTo = ""
	}
	return firstMsgID, nil
}

// splitMessage splits content into chunks of at most maxLen characters.
// Splits at paragraph boundary ("\n\n"), then sentence (". "), then newline ("\n"), then hard cut.
func splitMessage(content string, maxLen int) []string {
	if len(content) <= maxLen {
		return []string{content}
	}

	var parts []string
	for len(content) > 0 {
		if len(content) <= maxLen {
			parts = append(parts, content)
			break
		}

		chunk := content[:maxLen]
		cutAt := -1

		// Try paragraph boundary.
		if idx := strings.LastIndex(chunk, "\n\n"); idx > 0 {
			cutAt = idx + 2
		}
		// Try sentence boundary.
		if cutAt < 0 {
			if idx := strings.LastIndex(chunk, ". "); idx > 0 {
				cutAt = idx + 2
			}
		}
		// Try newline.
		if cutAt < 0 {
			if idx := strings.LastIndex(chunk, "\n"); idx > 0 {
				cutAt = idx + 1
			}
		}
		// Hard cut.
		if cutAt < 0 {
			cutAt = maxLen
		}

		parts = append(parts, content[:cutAt])
		content = content[cutAt:]
	}

	return parts
}

// ---------------------------------------------------------------------------
// Attachment sending
// ---------------------------------------------------------------------------

// attachmentMethodMap maps attachment kinds to Telegram API method and field name.
var attachmentMethodMap = map[string][2]string{
	"photo":      {"sendPhoto", "photo"},
	"voice":      {"sendVoice", "voice"},
	"document":   {"sendDocument", "document"},
	"video":      {"sendVideo", "video"},
	"audio":      {"sendAudio", "audio"},
	"video_note": {"sendVideoNote", "video_note"},
}

func (p *Plugin) sendAttachment(ctx context.Context, chatID string, att dto.Attachment) error {
	mapping, ok := attachmentMethodMap[att.Kind]
	if !ok {
		// Unknown kind — fall back to document.
		mapping = attachmentMethodMap["document"]
	}
	method, fieldName := mapping[0], mapping[1]

	params := map[string]any{
		"chat_id": chatID,
	}
	if att.Caption != "" {
		params["caption"] = att.Caption
	}

	_, err := p.api.uploadFile(ctx, method, params, fieldName, att.Path)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	return nil
}
