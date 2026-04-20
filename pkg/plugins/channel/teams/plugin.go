package teams

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

// Compile-time interface checks.
var (
	_ port.ChannelPlugin  = (*Plugin)(nil)
	_ port.IndicatorAware = (*Plugin)(nil)
)

// config is the JSON config shape for the Teams channel plugin.
type config struct {
	AppID               string `json:"app_id"`
	AppPassword         string `json:"app_password"`
	WebhookPath         string `json:"webhook_path"`
	TenantID            string `json:"tenant_id"`             // optional, for single-tenant apps
	MaxDownloadFilesize int64  `json:"max_download_filesize"` // max attachment download size in bytes
}

// Plugin implements port.ChannelPlugin for Microsoft Teams via Bot Framework.
type Plugin struct {
	id         string
	log        *slog.Logger
	config     config
	handler    port.IncomingMessageHandler
	refs       sync.Map // key: ChatExternalID (conversation ID), value: conversationRef
	jwks       *jwksCache
	tokenCache struct {
		mu        sync.Mutex
		token     string
		expiresAt time.Time
	}
	httpClient *http.Client
	degraded   atomic.Bool
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindChannel}
}

// NewPlugin returns a new Teams channel plugin instance.
func NewPlugin() port.Plugin {
	return &Plugin{}
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

// Init parses config and initializes HTTP client and JWKS cache.
func (p *Plugin) Init(ctx context.Context, id string, cfg json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	p.log = logger.FromCtx(ctx).With("component", "teams", "id", p.id)

	if err := json.Unmarshal(cfg, &p.config); err != nil {
		return fmt.Errorf("teams: parse config: %w", err)
	}
	if p.config.AppID == "" {
		return fmt.Errorf("teams: app_id is required")
	}
	if p.config.AppPassword == "" {
		return fmt.Errorf("teams: app_password is required")
	}
	if p.config.WebhookPath == "" {
		p.config.WebhookPath = "/channels/teams/webhook"
	}
	p.httpClient = &http.Client{Timeout: 10 * time.Second}
	p.jwks = &jwksCache{
		client: p.httpClient,
		keys:   make(map[string][]string),
	}

	p.log.Info("teams plugin initialized")
	return nil
}

// Start logs plugin startup.
func (p *Plugin) Start(_ context.Context) error {
	p.log.Info("teams: started")
	return nil
}

// Stop logs plugin shutdown.
func (p *Plugin) Stop(_ context.Context) error {
	p.log.Info("teams: stopped")
	return nil
}

// RegisterRoutes registers the Teams webhook endpoint.
func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(p.config.WebhookPath, p.handleWebhook)
}

// refFromMap reconstructs a conversationRef from a map[string]string.
func refFromMap(m map[string]string) conversationRef {
	return conversationRef{
		ServiceURL:     strings.TrimRight(m["service_url"], "/"),
		ConversationID: m["conversation_id"],
		BotID:          m["bot_id"],
		RecipientID:    m["recipient_id"],
	}
}

// Indicate starts a periodic typing indicator for the given conversation.
// The indication value must be a map[string]string with Teams routing keys;
// otherwise a no-op stop is returned.
// An immediate typing activity is sent, then refreshed every 4 seconds.
func (p *Plugin) Indicate(ctx context.Context, indication map[string]string) (stop func()) {
	if len(indication) == 0 {
		return func() {}
	}
	ref := refFromMap(indication)

	// Send immediate typing activity (best-effort).
	if err := p.sendTypingActivity(ctx, ref); err != nil {
		p.log.Debug("teams: initial typing indicator failed", "err", err)
	}

	done := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = p.sendTypingActivity(ctx, ref)
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
	}
}

// sendTypingActivity posts a typing activity to the Bot Connector REST API.
func (p *Plugin) sendTypingActivity(ctx context.Context, ref conversationRef) error {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("teams: typing: %w", err)
	}

	activity := map[string]any{
		"type":         "typing",
		"from":         map[string]any{"id": ref.BotID},
		"recipient":    map[string]any{"id": ref.RecipientID},
		"conversation": map[string]any{"id": ref.ConversationID},
	}

	body, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("teams: marshal typing activity: %w", err)
	}

	url := fmt.Sprintf("%s/v3/conversations/%s/activities", ref.ServiceURL, ref.ConversationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("teams: create typing request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	slog.Debug("teams: http.post", "url", url, "token_len", len(token))
	req = traceRequest(req)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: send typing activity: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("teams: typing activity returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Webhook handler
// ---------------------------------------------------------------------------

func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
		// fall through to webhook logic
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err := p.validateJWT(authHeader); err != nil {
		p.log.Warn("teams: JWT validation failed", "err", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var activity Activity
	if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Store/update conversation reference for outbound routing.
	p.refs.Store(activity.Conversation.ID, conversationRef{
		ServiceURL:     strings.TrimRight(activity.ServiceURL, "/"),
		ConversationID: activity.Conversation.ID,
		BotID:          activity.Recipient.ID,
		RecipientID:    activity.From.ID,
	})

	switch activity.Type {
	case "message", "messageUpdate":
		p.handleMessage(r.Context(), &activity)
	case "conversationUpdate":
		p.log.Info("teams: conversationUpdate", "from", activity.From.Name)
	}

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

func (p *Plugin) handleMessage(ctx context.Context, activity *Activity) {
	isGroup := activity.Conversation.ConversationType != "personal"

	// Determine user external ID: prefer AAD object ID, fall back to From.ID.
	userExternalID := activity.From.AadObjectID
	if userExternalID == "" {
		userExternalID = activity.From.ID
	}

	// Generate message ID: use activity ID if present, otherwise generate one.
	msgID := entity.MessageID(activity.ID)
	if activity.ID == "" {
		msgID = entity.MessageID(uuid.New().String())
	}

	deliveryMap := map[string]string{
		"service_url":     strings.TrimRight(activity.ServiceURL, "/"),
		"conversation_id": activity.Conversation.ID,
		"bot_id":          activity.Recipient.ID,
		"recipient_id":    activity.From.ID,
	}

	isMention := !isGroup || p.isBotMentioned(activity)

	incoming := dto.IncomingMessage{
		ID:         string(msgID),
		TenantID:   activity.Conversation.TenantID,
		ChatID:     activity.Conversation.ID,
		UserID:     userExternalID,
		Content:    activity.Text,
		ReplyToID:  activity.ReplyToID,
		IsGroup:    isGroup,
		IsMention:  isMention,
		Metadata:   buildMetadata(activity, isGroup, isMention),
		Delivery:   deliveryMap,
		Indication: deliveryMap,
		AgentName:  activity.Recipient.Name,
		UserName:   activity.From.Name,
		GroupName:  activity.Conversation.Name,
		ReceivedAt: time.Now(),
	}

	// Extract attachments (downloaded to temp dirs, relocated by runtime).
	if len(activity.Attachments) > 0 {
		token, tokenErr := p.getAccessToken(ctx)
		if tokenErr == nil {
			incoming.Attachments = p.extractAttachments(ctx, activity.Attachments, token)
		} else {
			p.log.Warn("teams: skipping attachments, no token", "err", tokenErr)
		}
	}

	if p.handler != nil {
		if err := p.handler(ctx, incoming); err != nil {
			p.log.Error("teams: handler error", "err", err, "conversation", activity.Conversation.ID)
		}
	}
}

// isBotMentioned checks if the bot is mentioned in the activity entities.
func (p *Plugin) isBotMentioned(activity *Activity) bool {
	for _, e := range activity.Entities {
		if e.Type == "mention" && e.Mentioned != nil {
			if normalizeBotID(e.Mentioned.ID) == p.config.AppID {
				return true
			}
		}
	}
	return false
}

// normalizeBotID strips the "28:" prefix used by Teams for bot IDs.
func normalizeBotID(id string) string {
	return strings.TrimPrefix(id, "28:")
}

// buildMetadata extracts LLM-relevant metadata from a Teams activity.
func buildMetadata(activity *Activity, isGroup bool, isMention bool) map[string]string {
	meta := map[string]string{
		"response_format": "Microsoft Teams Markdown. Supported: **bold**, _italic_, ~~strikethrough~~, [links](url), `inline code`, ```code blocks```, numbered and bulleted lists, > blockquotes. No HTML tags. No headings.",
	}

	if activity.From.Name != "" {
		meta["sender_name"] = activity.From.Name
	}
	if activity.From.ID != "" {
		meta["sender_id"] = activity.From.ID
	}
	if activity.From.AadObjectID != "" {
		meta["sender_aad_id"] = activity.From.AadObjectID
	}
	if activity.Conversation.ConversationType != "" {
		meta["chat_type"] = activity.Conversation.ConversationType
	}
	if activity.Conversation.Name != "" {
		meta["chat_name"] = activity.Conversation.Name
	}
	if activity.Conversation.TenantID != "" {
		meta["teams_tenant_id"] = activity.Conversation.TenantID
	}
	if activity.ChannelID != "" {
		meta["teams_channel_id"] = activity.ChannelID
	}

	// Group mention context.
	if isGroup {
		meta["is_mentioned"] = strconv.FormatBool(isMention)
	}

	return meta
}

// ---------------------------------------------------------------------------
// Send (outbound delivery)
// ---------------------------------------------------------------------------

// Send delivers an outgoing message via the Bot Connector REST API.
// Prefers msg.Delivery (map[string]string) when present; falls back to the in-memory refs map.
// Returns a SendResult with the message ID from the Bot Framework response.
func (p *Plugin) Send(ctx context.Context, msg dto.OutgoingMessage) (port.SendResult, error) {
	var ref conversationRef
	if dm := msg.Delivery; len(dm) > 0 {
		ref = refFromMap(dm)
	} else if val, ok := p.refs.Load(msg.ChatID); ok {
		ref = val.(conversationRef)
	} else {
		return port.SendResult{}, fmt.Errorf("teams: no conversation reference for %q", msg.ChatID)
	}

	token, err := p.getAccessToken(ctx)
	if err != nil {
		p.degraded.Store(true)
		return port.SendResult{}, fmt.Errorf("teams: send: %w", err)
	}

	// Split long messages.
	chunks := splitMessage(msg.Content, 4000)
	var firstResult port.SendResult
	for i, chunk := range chunks {
		activity := map[string]any{
			"type":         "message",
			"text":         chunk,
			"from":         map[string]any{"id": ref.BotID},
			"recipient":    map[string]any{"id": ref.RecipientID},
			"conversation": map[string]any{"id": ref.ConversationID},
		}
		// Thread only the first chunk under the original message.
		if i == 0 && msg.TargetID != "" {
			activity["replyToId"] = msg.TargetID
		}
		result, sendErr := p.sendActivity(ctx, ref, token, activity)
		if sendErr != nil {
			return port.SendResult{}, sendErr
		}
		if i == 0 {
			firstResult = result
		}
	}

	// Send outbound attachments.
	for _, att := range msg.Attachments {
		if !strings.HasPrefix(att.MimeType, "image/") {
			p.log.Warn("teams: skipping non-image attachment (Teams requires hosted files)", "mime", att.MimeType, "name", att.Filename)
			continue
		}
		attachmentActivity := map[string]any{
			"type":         "message",
			"from":         map[string]any{"id": ref.BotID},
			"recipient":    map[string]any{"id": ref.RecipientID},
			"conversation": map[string]any{"id": ref.ConversationID},
			"attachments": []map[string]any{
				{
					"contentType": att.MimeType,
					"contentUrl":  att.Path,
					"name":        att.Filename,
				},
			},
		}
		if msg.TargetID != "" {
			attachmentActivity["replyToId"] = msg.TargetID
		}
		if _, sendErr := p.sendActivity(ctx, ref, token, attachmentActivity); sendErr != nil {
			p.log.Warn("teams: failed to send attachment", "name", att.Filename, "err", sendErr)
		}
	}

	p.degraded.Store(false)
	return firstResult, nil
}

// traceRequest attaches httptrace hooks that log DNS, TCP, TLS, and response
// milestones at DEBUG level. Useful for diagnosing connectivity issues.
func traceRequest(req *http.Request) *http.Request {
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			slog.Debug("teams: http.trace dns_start", "host", info.Host)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			var addrs []string
			for _, a := range info.Addrs {
				addrs = append(addrs, a.String())
			}
			slog.Debug("teams: http.trace dns_done", "addrs", addrs, "err", info.Err)
		},
		ConnectStart: func(network, addr string) {
			slog.Debug("teams: http.trace connect_start", "network", network, "addr", addr)
		},
		ConnectDone: func(network, addr string, err error) {
			slog.Debug("teams: http.trace connect_done", "network", network, "addr", addr, "err", err)
		},
		TLSHandshakeStart: func() {
			slog.Debug("teams: http.trace tls_start")
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			slog.Debug("teams: http.trace tls_done", "version", state.Version, "server", state.ServerName, "err", err)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			slog.Debug("teams: http.trace got_conn", "reused", info.Reused, "idle", info.WasIdle, "addr", info.Conn.RemoteAddr())
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			slog.Debug("teams: http.trace wrote_request", "err", info.Err)
		},
		GotFirstResponseByte: func() {
			slog.Debug("teams: http.trace first_byte")
		},
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

// sendActivity posts a single activity to the Bot Connector and returns the response ID.
func (p *Plugin) sendActivity(ctx context.Context, ref conversationRef, token string, activity map[string]any) (port.SendResult, error) {
	body, err := json.Marshal(activity)
	if err != nil {
		return port.SendResult{}, fmt.Errorf("teams: marshal activity: %w", err)
	}

	url := fmt.Sprintf("%s/v3/conversations/%s/activities", ref.ServiceURL, ref.ConversationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return port.SendResult{}, fmt.Errorf("teams: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	slog.Debug("teams: http.post", "url", url, "token_len", len(token))
	req = traceRequest(req)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return port.SendResult{}, fmt.Errorf("teams: send activity: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return port.SendResult{}, fmt.Errorf("teams: Bot Connector returned %d: %s", resp.StatusCode, respBody)
	}

	var resource struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		slog.Debug("teams: could not parse send response", "err", err)
		return port.SendResult{}, nil
	}

	return port.SendResult{
		MessageID: resource.ID,
		Timestamp: time.Now(),
	}, nil
}

// ---------------------------------------------------------------------------
// Message splitting
// ---------------------------------------------------------------------------

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
// Attachment extraction (inbound)
// ---------------------------------------------------------------------------

// extractAttachments downloads file attachments from a Teams activity to temp directories
// and returns a slice of dto.Attachment. Adaptive Cards are skipped.
// ContentURL provides the original file content (not thumbnails); Bearer token auth is required.
// The runtime handler relocates files to the final messages/{internal_uuid}/ path.
func (p *Plugin) extractAttachments(ctx context.Context, attachments []ActivityAttachment, token string) []dto.Attachment {
	var result []dto.Attachment
	for _, att := range attachments {
		// Skip Adaptive Cards and other card types.
		if strings.Contains(att.ContentType, "card") {
			continue
		}
		if att.ContentURL == "" {
			continue
		}

		// Download the file.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.ContentURL, nil)
		if err != nil {
			p.log.Warn("teams: create download request", "err", err, "name", att.Name)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			p.log.Warn("teams: download attachment", "err", err, "name", att.Name)
			continue
		}

		// Check file size via Content-Length if configured.
		if p.config.MaxDownloadFilesize > 0 && resp.ContentLength > p.config.MaxDownloadFilesize {
			resp.Body.Close()
			p.log.Warn("teams: attachment too large", "name", att.Name, "size", resp.ContentLength, "max", p.config.MaxDownloadFilesize)
			continue
		}

		// Create temp directory for download; runtime handler will relocate
		// to the final messages/{internal_uuid}/ path after ID assignment.
		dir, err := os.MkdirTemp("", "wa-attach-*")
		if err != nil {
			resp.Body.Close()
			p.log.Warn("teams: create temp attachment dir", "err", err)
			continue
		}

		filename := att.Name
		if filename == "" {
			filename = uuid.New().String()
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			p.log.Warn("teams: read attachment body", "err", err, "name", att.Name)
			continue
		}

		// Check actual size after download.
		if p.config.MaxDownloadFilesize > 0 && int64(len(data)) > p.config.MaxDownloadFilesize {
			p.log.Warn("teams: attachment too large after download", "name", att.Name, "size", len(data), "max", p.config.MaxDownloadFilesize)
			continue
		}

		filePath := dir + "/" + filename
		if err := writeFile(filePath, data); err != nil {
			p.log.Warn("teams: write attachment file", "err", err, "path", filePath)
			continue
		}

		result = append(result, dto.Attachment{
			Kind:     mimeToKind(att.ContentType),
			Filename: filename,
			MimeType: att.ContentType,
			Size:     int64(len(data)),
			Path:     filePath,
		})
	}

	return result
}

// mimeToKind maps a MIME type to an attachment kind.
func mimeToKind(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "photo"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	default:
		return "document"
	}
}

// writeFile writes data to a file path.
func writeFile(path string, data []byte) error {
	return writeFileFunc(path, data)
}

// writeFileFunc is the file-writing function (swappable for tests).
var writeFileFunc = defaultWriteFile

func defaultWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
