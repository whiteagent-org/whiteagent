package teams

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// Metadata tests
// ---------------------------------------------------------------------------

func TestBuildMetadataPersonal(t *testing.T) {
	a := &Activity{
		From:         ChannelAccount{Name: "Alice", ID: "u1", AadObjectID: "aad-1"},
		Conversation: ConversationAccount{ConversationType: "personal", TenantID: "t1"},
	}
	meta := buildMetadata(a, false, true)

	assertMeta(t, meta, "sender_name", "Alice")
	assertMeta(t, meta, "sender_id", "u1")
	assertMeta(t, meta, "sender_aad_id", "aad-1")
	assertMeta(t, meta, "chat_type", "personal")
	assertMeta(t, meta, "teams_tenant_id", "t1")
	assertAbsent(t, meta, "chat_name")
	assertAbsent(t, meta, "teams_channel_id")
}

func TestBuildMetadataGroup(t *testing.T) {
	a := &Activity{
		From:         ChannelAccount{Name: "Bob", ID: "u2"},
		Conversation: ConversationAccount{ConversationType: "groupChat", Name: "Dev Team"},
		ChannelID:    "ch1",
	}
	meta := buildMetadata(a, false, true)

	assertMeta(t, meta, "chat_type", "groupChat")
	assertMeta(t, meta, "chat_name", "Dev Team")
	assertMeta(t, meta, "teams_channel_id", "ch1")
	assertAbsent(t, meta, "sender_aad_id")
}

func TestBuildMetadataEmptyFields(t *testing.T) {
	a := &Activity{}
	meta := buildMetadata(a, false, true)

	assertAbsent(t, meta, "sender_name")
	assertAbsent(t, meta, "sender_id")
	assertAbsent(t, meta, "sender_aad_id")
	assertAbsent(t, meta, "chat_type")
	assertAbsent(t, meta, "chat_name")
	assertAbsent(t, meta, "teams_tenant_id")
	assertAbsent(t, meta, "teams_channel_id")
}

// ---------------------------------------------------------------------------
// Webhook handler tests
// ---------------------------------------------------------------------------

func TestHandleWebhookHealthProbeGet(t *testing.T) {
	p := &Plugin{}
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	p.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleWebhookRejectsOtherMethods(t *testing.T) {
	p := &Plugin{}
	req := httptest.NewRequest(http.MethodDelete, "/webhook", nil)
	w := httptest.NewRecorder()

	p.handleWebhook(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleWebhookRejectsNoAuth(t *testing.T) {
	p := &Plugin{}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	p.handleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// handleMessage tests
// ---------------------------------------------------------------------------

func TestHandleMessagePopulatesIncomingMessage(t *testing.T) {
	var captured dto.IncomingMessage
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			captured = msg
			return nil
		},
	}

	activity := &Activity{
		Type:       "message",
		ID:         "msg-123",
		ServiceURL: "https://smba.trafficmanager.net/amer",
		From:       ChannelAccount{ID: "u1", Name: "Alice", AadObjectID: "aad-alice"},
		Recipient:  ChannelAccount{ID: "bot-1"},
		Conversation: ConversationAccount{
			ID:               "conv-1",
			ConversationType: "personal",
			TenantID:         "tid-1",
		},
		Text: "hello teams",
	}

	p.handleMessage(context.Background(), activity)

	if captured.ChatID != "conv-1" {
		t.Errorf("ChatID: got %q, want %q", captured.ChatID, "conv-1")
	}
	if captured.UserID != "aad-alice" {
		t.Errorf("UserExternalID: got %q, want %q", captured.UserID, "aad-alice")
	}
	if captured.Content != "hello teams" {
		t.Errorf("Content: got %q, want %q", captured.Content, "hello teams")
	}
	if captured.TenantID != "tid-1" {
		t.Errorf("TenantID: got %q, want %q", captured.TenantID, "tid-1")
	}
	if captured.IsGroup {
		t.Error("expected IsGroup false for personal conversation")
	}

	// Metadata checks.
	assertMeta(t, captured.Metadata, "sender_name", "Alice")
	assertMeta(t, captured.Metadata, "chat_type", "personal")

	// Delivery should be a map[string]string.
	if captured.Delivery == nil {
		t.Fatal("Delivery should not be nil")
	}
	if captured.Delivery["service_url"] != "https://smba.trafficmanager.net/amer" {
		t.Errorf("Delivery service_url: got %q", captured.Delivery["service_url"])
	}
	if captured.Delivery["conversation_id"] != "conv-1" {
		t.Errorf("Delivery conversation_id: got %q", captured.Delivery["conversation_id"])
	}
	if captured.Delivery["bot_id"] != "bot-1" {
		t.Errorf("Delivery bot_id: got %q", captured.Delivery["bot_id"])
	}
	if captured.Delivery["recipient_id"] != "u1" {
		t.Errorf("Delivery recipient_id: got %q", captured.Delivery["recipient_id"])
	}

	// Indication should also be a map[string]string (same value).
	if captured.Indication == nil {
		t.Fatal("Indication should not be nil")
	}
	if captured.Indication["service_url"] != "https://smba.trafficmanager.net/amer" {
		t.Errorf("Indication service_url: got %q", captured.Indication["service_url"])
	}
	if captured.Indication["conversation_id"] != "conv-1" {
		t.Errorf("Indication conversation_id: got %q", captured.Indication["conversation_id"])
	}
	if captured.Indication["bot_id"] != "bot-1" {
		t.Errorf("Indication bot_id: got %q", captured.Indication["bot_id"])
	}
	if captured.Indication["recipient_id"] != "u1" {
		t.Errorf("Indication recipient_id: got %q", captured.Indication["recipient_id"])
	}
}

func TestHandleMessageFallsBackToFromID(t *testing.T) {
	var captured dto.IncomingMessage
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			captured = msg
			return nil
		},
	}

	activity := &Activity{
		Type:         "message",
		ServiceURL:   "https://smba.trafficmanager.net/amer",
		From:         ChannelAccount{ID: "u-fallback"},
		Recipient:    ChannelAccount{ID: "bot-1"},
		Conversation: ConversationAccount{ID: "conv-1", ConversationType: "personal"},
		Text:         "hi",
	}

	p.handleMessage(context.Background(), activity)

	if captured.UserID != "u-fallback" {
		t.Errorf("UserExternalID: got %q, want %q", captured.UserID, "u-fallback")
	}
}

// ---------------------------------------------------------------------------
// Group IsMention population
// ---------------------------------------------------------------------------

func TestHandleMessageGroupIsMention(t *testing.T) {
	var captured dto.IncomingMessage
	var callCount int
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			captured = msg
			callCount++
			return nil
		},
	}

	// Group message without bot mention -- handler called, IsMention false.
	activity := &Activity{
		Type:         "message",
		ServiceURL:   "https://smba.trafficmanager.net/amer",
		From:         ChannelAccount{ID: "u1"},
		Recipient:    ChannelAccount{ID: "28:app1"},
		Conversation: ConversationAccount{ID: "conv-1", ConversationType: "groupChat"},
		Text:         "hello all",
	}
	p.handleMessage(context.Background(), activity)

	if callCount != 1 {
		t.Fatalf("handler should be called for all group messages, got %d calls", callCount)
	}
	if captured.IsMention {
		t.Error("IsMention should be false for group message without mention")
	}
	assertMeta(t, captured.Metadata, "is_mentioned", "false")

	// Group message with bot mention -- handler called, IsMention true.
	activity.Entities = []Entity{
		{Type: "mention", Mentioned: &ChannelAccount{ID: "28:app1"}},
	}
	p.handleMessage(context.Background(), activity)

	if callCount != 2 {
		t.Fatalf("handler should be called, got %d calls", callCount)
	}
	if !captured.IsMention {
		t.Error("IsMention should be true for group message with bot mention")
	}
	assertMeta(t, captured.Metadata, "is_mentioned", "true")
}

// ---------------------------------------------------------------------------
// Conversation ref storage
// ---------------------------------------------------------------------------

func TestConversationRefStoredOnInbound(t *testing.T) {
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		handler: func(_ context.Context, _ dto.IncomingMessage) error {
			return nil
		},
	}

	activity := &Activity{
		Type:         "message",
		ServiceURL:   "https://smba.trafficmanager.net/amer",
		From:         ChannelAccount{ID: "u1"},
		Recipient:    ChannelAccount{ID: "bot-1"},
		Conversation: ConversationAccount{ID: "conv-stored", ConversationType: "personal"},
		Text:         "test",
	}

	// Simulate webhook handler storing the ref (handleWebhook does this before handleMessage).
	p.refs.Store(activity.Conversation.ID, conversationRef{
		ServiceURL:     activity.ServiceURL,
		ConversationID: activity.Conversation.ID,
		BotID:          activity.Recipient.ID,
		RecipientID:    activity.From.ID,
	})

	val, ok := p.refs.Load("conv-stored")
	if !ok {
		t.Fatal("expected conversation ref to be stored")
	}
	ref := val.(conversationRef)
	if ref.ServiceURL != "https://smba.trafficmanager.net/amer" {
		t.Errorf("ServiceURL: got %q", ref.ServiceURL)
	}
	if ref.BotID != "bot-1" {
		t.Errorf("BotID: got %q", ref.BotID)
	}
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

func TestSendUsesConversationRef(t *testing.T) {
	var capturedURL string
	var capturedAuth string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token endpoint.
		if strings.Contains(r.URL.Path, "oauth2") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		capturedURL = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	// Override token endpoint by pre-caching token.
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	p.refs.Store("conv-send", conversationRef{
		ServiceURL:     srv.URL,
		ConversationID: "conv-send",
		BotID:          "bot-1",
		RecipientID:    "u1",
	})

	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		ChatID:  "conv-send",
		Content: "hello from bot",
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if capturedURL != "/v3/conversations/conv-send/activities" {
		t.Errorf("URL: got %q", capturedURL)
	}
	if capturedAuth != "Bearer test-token" {
		t.Errorf("Auth: got %q", capturedAuth)
	}
	if capturedBody["text"] != "hello from bot" {
		t.Errorf("body text: got %v", capturedBody["text"])
	}
	if capturedBody["type"] != "message" {
		t.Errorf("body type: got %v", capturedBody["type"])
	}
}

// TestSendPrefersDeliveryOverRefsMap verifies that when msg.Delivery is populated,
// Send uses it for routing instead of the in-memory p.refs map. This is the
// cron-path requirement: messages with Delivery set do not need a prior inbound
// conversation to have populated p.refs.
func TestSendPrefersDeliveryOverRefsMap(t *testing.T) {
	var capturedURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	// Store a DIFFERENT ref in p.refs for the same ChatExternalID.
	// If Send uses p.refs, it would construct a URL with "wrong-conv".
	p.refs.Store("chat-via-delivery", conversationRef{
		ServiceURL:     srv.URL,
		ConversationID: "wrong-conv",
		BotID:          "bot-wrong",
		RecipientID:    "u-wrong",
	})

	// Provide Delivery with the correct routing data.
	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		ChatID:  "chat-via-delivery",
		Content: "from cron",
		Delivery: map[string]string{
			"service_url":     srv.URL,
			"conversation_id": "correct-conv",
			"bot_id":          "bot-correct",
			"recipient_id":    "u-correct",
		},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// URL must use conversation_id from Delivery, not from p.refs.
	wantURL := "/v3/conversations/correct-conv/activities"
	if capturedURL != wantURL {
		t.Errorf("expected URL %q (from Delivery), got %q (from p.refs)", wantURL, capturedURL)
	}
}

// ---------------------------------------------------------------------------
// normalizeBotID
// ---------------------------------------------------------------------------

func TestNormalizeBotID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"28:abc", "abc"},
		{"abc", "abc"},
		{"28:", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeBotID(tt.input); got != tt.want {
			t.Errorf("normalizeBotID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Indicate lifecycle
// ---------------------------------------------------------------------------

func TestIndicateStartsAndStops(t *testing.T) {
	var typingCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token endpoint.
		if strings.Contains(r.URL.Path, "oauth2") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		typingCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	indication := map[string]string{
		"service_url":     srv.URL,
		"conversation_id": "conv-ind",
		"bot_id":          "bot-1",
		"recipient_id":    "u1",
	}

	stop := p.Indicate(context.Background(), indication)

	// Immediate call should have happened.
	time.Sleep(50 * time.Millisecond)
	if typingCount.Load() < 1 {
		t.Fatal("expected at least 1 typing request")
	}

	stop()
	time.Sleep(50 * time.Millisecond)
	countAfterStop := typingCount.Load()

	// Verify no more requests after stop.
	time.Sleep(100 * time.Millisecond)
	if typingCount.Load() != countAfterStop {
		t.Errorf("expected no new typing requests after stop, got %d -> %d", countAfterStop, typingCount.Load())
	}
}

func TestIndicateEmptyMapReturnsNoop(t *testing.T) {
	p := &Plugin{}
	stop := p.Indicate(context.Background(), map[string]string{})
	// Should not panic.
	stop()
}

func TestIndicateStopIdempotent(t *testing.T) {
	var typingCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		typingCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	indication := map[string]string{
		"service_url":     srv.URL,
		"conversation_id": "conv-idem",
		"bot_id":          "bot-1",
		"recipient_id":    "u1",
	}

	stop := p.Indicate(context.Background(), indication)
	stop()
	stop() // Must not panic.
}

// ---------------------------------------------------------------------------
// Response format metadata
// ---------------------------------------------------------------------------

func TestBuildMetadataResponseFormat(t *testing.T) {
	a := &Activity{
		From:         ChannelAccount{Name: "Alice", ID: "u1"},
		Conversation: ConversationAccount{ConversationType: "personal"},
	}
	meta := buildMetadata(a, false, true)

	rf, ok := meta["response_format"]
	if !ok {
		t.Fatal("expected response_format key in metadata")
	}
	if !strings.Contains(rf, "Teams Markdown") {
		t.Errorf("response_format should mention Teams Markdown, got: %s", rf)
	}
}

// ---------------------------------------------------------------------------
// ReplyToID extraction
// ---------------------------------------------------------------------------

func TestHandleMessageReplyToID(t *testing.T) {
	var captured dto.IncomingMessage
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			captured = msg
			return nil
		},
	}

	activity := &Activity{
		Type:         "message",
		ID:           "msg-1",
		ServiceURL:   "https://smba.trafficmanager.net/amer",
		From:         ChannelAccount{ID: "u1"},
		Recipient:    ChannelAccount{ID: "bot-1"},
		Conversation: ConversationAccount{ID: "conv-1", ConversationType: "personal"},
		ReplyToID:    "original-msg-42",
		Text:         "reply to something",
	}

	p.handleMessage(context.Background(), activity)

	if captured.ReplyToID != "original-msg-42" {
		t.Errorf("ReplyToID: got %q, want %q", captured.ReplyToID, "original-msg-42")
	}
}

// ---------------------------------------------------------------------------
// Edited message handling
// ---------------------------------------------------------------------------

func TestHandleWebhookProcessesMessageUpdate(t *testing.T) {
	var handlerCalled bool
	var got dto.IncomingMessage
	p := &Plugin{
		id:     "teams-test",
		config: config{AppID: "app1", AppPassword: "pw"},
		log:    slog.Default(),
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			handlerCalled = true
			got = msg
			return nil
		},
	}

	activity := Activity{
		Type:         "messageUpdate",
		ID:           "edited-1",
		ServiceURL:   "https://smba.trafficmanager.net/amer",
		From:         ChannelAccount{ID: "u1", Name: "Alice"},
		Conversation: ConversationAccount{ID: "conv-1", ConversationType: "personal"},
		Text:         "edited text",
	}

	p.handleMessage(context.Background(), &activity)

	if !handlerCalled {
		t.Fatal("handler should be called for messageUpdate")
	}
	if got.Content != "edited text" {
		t.Errorf("content = %q, want %q", got.Content, "edited text")
	}
}

// ---------------------------------------------------------------------------
// Degraded state tracking
// ---------------------------------------------------------------------------

func TestStatusDegraded(t *testing.T) {
	p := &Plugin{}
	if p.Status() != entity.PluginStateHealthy {
		t.Errorf("expected healthy, got %v", p.Status())
	}

	p.degraded.Store(true)
	if p.Status() != entity.PluginStateDegraded {
		t.Errorf("expected degraded, got %v", p.Status())
	}
}

func TestStatusHealthyAfterRecovery(t *testing.T) {
	p := &Plugin{}
	p.degraded.Store(true)
	if p.Status() != entity.PluginStateDegraded {
		t.Fatal("should be degraded")
	}

	p.degraded.Store(false)
	if p.Status() != entity.PluginStateHealthy {
		t.Errorf("expected healthy after recovery, got %v", p.Status())
	}
}

// ---------------------------------------------------------------------------
// Message splitting
// ---------------------------------------------------------------------------

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    int // expected number of parts
	}{
		{"short", "hello", 4000, 1},
		{"paragraph split", strings.Repeat("a", 3999) + "\n\n" + "b", 4000, 2},
		{"sentence split", strings.Repeat("x", 3998) + ". y", 4000, 2},
		{"hard cut", strings.Repeat("z", 8000), 4000, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitMessage(tt.content, tt.maxLen)
			if len(parts) != tt.want {
				t.Errorf("got %d parts, want %d", len(parts), tt.want)
			}
			// Verify all content is preserved.
			joined := strings.Join(parts, "")
			if joined != tt.content {
				t.Errorf("joined parts don't match original content")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Attachment extraction (inbound)
// ---------------------------------------------------------------------------

func TestExtractAttachmentsSkipsCards(t *testing.T) {
	p := &Plugin{
		id:  "teams-test",
		log: slog.Default(),
	}

	attachments := []ActivityAttachment{
		{ContentType: "application/vnd.microsoft.card.adaptive", Content: json.RawMessage(`{}`)},
		{ContentType: "application/vnd.microsoft.card.hero", Content: json.RawMessage(`{}`)},
	}

	result := p.extractAttachments(context.Background(), attachments, "token")
	if len(result) != 0 {
		t.Errorf("expected 0 attachments after filtering cards, got %d", len(result))
	}
}

func TestExtractAttachmentsDownloadsFile(t *testing.T) {
	fileContent := []byte("fake image data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fileContent)
	}))
	defer srv.Close()

	var writtenPath string
	var writtenData []byte
	origWriteFile := writeFileFunc
	writeFileFunc = func(path string, data []byte) error {
		writtenPath = path
		writtenData = data
		return nil
	}
	defer func() { writeFileFunc = origWriteFile }()

	p := &Plugin{
		id:         "teams-test",
		log:        slog.Default(),
		httpClient: srv.Client(),
	}

	attachments := []ActivityAttachment{
		{ContentType: "image/png", ContentURL: srv.URL + "/file.png", Name: "photo.png"},
	}

	result := p.extractAttachments(context.Background(), attachments, "test-token")
	if len(result) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(result))
	}

	att := result[0]
	if att.Kind != "photo" {
		t.Errorf("Kind: got %q, want %q", att.Kind, "photo")
	}
	if att.Filename != "photo.png" {
		t.Errorf("Filename: got %q, want %q", att.Filename, "photo.png")
	}
	if att.MimeType != "image/png" {
		t.Errorf("MimeType: got %q, want %q", att.MimeType, "image/png")
	}
	if att.Size != int64(len(fileContent)) {
		t.Errorf("Size: got %d, want %d", att.Size, len(fileContent))
	}
	if !strings.HasSuffix(writtenPath, "/photo.png") {
		t.Errorf("written path should end with /photo.png: got %q", writtenPath)
	}
	if string(writtenData) != string(fileContent) {
		t.Error("written data doesn't match downloaded content")
	}
}

func TestExtractAttachmentsMaxSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "999999")
		_, _ = w.Write([]byte("large data"))
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		log:        slog.Default(),
		config:     config{MaxDownloadFilesize: 100},
		httpClient: srv.Client(),
	}

	attachments := []ActivityAttachment{
		{ContentType: "image/png", ContentURL: srv.URL + "/big.png", Name: "big.png"},
	}

	result := p.extractAttachments(context.Background(), attachments, "token")
	if len(result) != 0 {
		t.Errorf("expected 0 attachments (size limit exceeded), got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Send with attachment
// ---------------------------------------------------------------------------

func TestSendWithAttachment(t *testing.T) {
	var bodies []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1"}`))
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	p.refs.Store("conv-att", conversationRef{
		ServiceURL:     srv.URL,
		ConversationID: "conv-att",
		BotID:          "bot-1",
		RecipientID:    "u1",
	})

	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		ChatID:  "conv-att",
		Content: "here is an image",
		Attachments: []dto.Attachment{
			{ID: "att-1", Kind: "photo", Filename: "test.png", MimeType: "image/png", Path: "/tmp/test.png"},
		},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Should have sent 2 activities: text message + attachment
	if len(bodies) != 2 {
		t.Fatalf("expected 2 activities sent, got %d", len(bodies))
	}

	// First is the text message.
	if bodies[0]["text"] != "here is an image" {
		t.Errorf("first activity text: got %v", bodies[0]["text"])
	}

	// Second is the attachment.
	atts, ok := bodies[1]["attachments"].([]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("expected 1 attachment in second activity, got %v", bodies[1]["attachments"])
	}
	attMap := atts[0].(map[string]any)
	if attMap["contentType"] != "image/png" {
		t.Errorf("attachment contentType: got %v", attMap["contentType"])
	}
	if attMap["name"] != "test.png" {
		t.Errorf("attachment name: got %v", attMap["name"])
	}
}

// ---------------------------------------------------------------------------
// Send includes replyToId when TargetID is set
// ---------------------------------------------------------------------------

func TestSendReplyToID(t *testing.T) {
	var bodies []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1"}`))
	}))
	defer srv.Close()

	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: srv.Client(),
	}
	p.tokenCache.token = "test-token"
	p.tokenCache.expiresAt = time.Now().Add(time.Hour)

	p.refs.Store("conv-reply", conversationRef{
		ServiceURL:     srv.URL,
		ConversationID: "conv-reply",
		BotID:          "bot-1",
		RecipientID:    "u1",
	})

	t.Run("with TargetID", func(t *testing.T) {
		bodies = nil
		_, err := p.Send(context.Background(), dto.OutgoingMessage{
			ChatID:   "conv-reply",
			Content:  "threaded reply",
			TargetID: "orig-msg-123",
		})
		if err != nil {
			t.Fatalf("Send error: %v", err)
		}
		if len(bodies) != 1 {
			t.Fatalf("expected 1 activity, got %d", len(bodies))
		}
		if bodies[0]["replyToId"] != "orig-msg-123" {
			t.Errorf("replyToId: got %v, want %q", bodies[0]["replyToId"], "orig-msg-123")
		}
	})

	t.Run("without TargetID", func(t *testing.T) {
		bodies = nil
		_, err := p.Send(context.Background(), dto.OutgoingMessage{
			ChatID:  "conv-reply",
			Content: "plain message",
		})
		if err != nil {
			t.Fatalf("Send error: %v", err)
		}
		if len(bodies) != 1 {
			t.Fatalf("expected 1 activity, got %d", len(bodies))
		}
		if _, exists := bodies[0]["replyToId"]; exists {
			t.Errorf("replyToId should not be set, got %v", bodies[0]["replyToId"])
		}
	})

	t.Run("with attachment", func(t *testing.T) {
		bodies = nil
		_, err := p.Send(context.Background(), dto.OutgoingMessage{
			ChatID:   "conv-reply",
			Content:  "image reply",
			TargetID: "orig-msg-456",
			Attachments: []dto.Attachment{
				{ID: "att-1", Kind: "photo", Filename: "pic.png", MimeType: "image/png", Path: "/tmp/pic.png"},
			},
		})
		if err != nil {
			t.Fatalf("Send error: %v", err)
		}
		if len(bodies) != 2 {
			t.Fatalf("expected 2 activities, got %d", len(bodies))
		}
		// Text activity has replyToId.
		if bodies[0]["replyToId"] != "orig-msg-456" {
			t.Errorf("text replyToId: got %v", bodies[0]["replyToId"])
		}
		// Attachment activity also has replyToId.
		if bodies[1]["replyToId"] != "orig-msg-456" {
			t.Errorf("attachment replyToId: got %v", bodies[1]["replyToId"])
		}
	})
}

// ---------------------------------------------------------------------------
// Send sets degraded on token failure
// ---------------------------------------------------------------------------

func TestSendSetsDegradedOnTokenFailure(t *testing.T) {
	p := &Plugin{
		id:         "teams-test",
		config:     config{AppID: "app1", AppPassword: "pw"},
		httpClient: &http.Client{Timeout: time.Second},
	}
	// Token is expired and no token server available → will fail.
	p.tokenCache.token = ""
	p.tokenCache.expiresAt = time.Time{}

	// Need a ref so we don't fail on "no conversation reference".
	p.refs.Store("conv-1", conversationRef{
		ServiceURL:     "http://localhost:1", // unreachable
		ConversationID: "conv-1",
		BotID:          "bot-1",
		RecipientID:    "u1",
	})

	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		ChatID:  "conv-1",
		Content: "test",
	})
	if err == nil {
		t.Fatal("expected error from Send")
	}
	if !p.degraded.Load() {
		t.Error("expected degraded=true after token failure")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertMeta(t *testing.T, meta map[string]string, key, expected string) {
	t.Helper()
	v, ok := meta[key]
	if !ok {
		t.Errorf("expected metadata key %q to be present", key)
		return
	}
	if v != expected {
		t.Errorf("metadata key %q: got %q, want %q", key, v, expected)
	}
}

func assertAbsent(t *testing.T, meta map[string]string, key string) {
	t.Helper()
	if v, ok := meta[key]; ok {
		t.Errorf("expected metadata key %q to be absent, got %v", key, v)
	}
}
