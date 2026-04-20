package telegram

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
)

// ---------------------------------------------------------------------------
// bestPhoto tests
// ---------------------------------------------------------------------------

func TestBestPhotoUnsortedMultiVariant(t *testing.T) {
	photos := []tgPhotoSize{
		{FileID: "small", Width: 100, Height: 100, FileSize: 1000},
		{FileID: "large", Width: 800, Height: 600, FileSize: 50000},
		{FileID: "medium", Width: 320, Height: 240, FileSize: 10000},
	}
	got := bestPhoto(photos)
	if got.FileID != "large" {
		t.Errorf("bestPhoto selected %q, want %q (highest Width*Height)", got.FileID, "large")
	}
}

func TestBestPhotoSingleElement(t *testing.T) {
	photos := []tgPhotoSize{
		{FileID: "only", Width: 640, Height: 480, FileSize: 20000},
	}
	got := bestPhoto(photos)
	if got.FileID != "only" {
		t.Errorf("bestPhoto selected %q, want %q", got.FileID, "only")
	}
}

// TestSendPrefersDeliveryMap verifies that when msg.Delivery["chat_id"] is set,
// Send routes the message to that chat_id instead of msg.ChatExternalID.
// This is the cron-path requirement: cron-fired messages carry Delivery populated
// by the scheduler, not the runtime identity resolver, so the channel must
// read from Delivery rather than ChatExternalID.
func TestSendPrefersDeliveryMap(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		// ChatExternalID is the wrong/fallback chat. If Send ignores Delivery,
		// the test will capture "fallback-chat" instead of "delivery-chat".
		ChatID:  "fallback-chat",
		Content: "hello from cron",
		Delivery: map[string]string{
			"chat_id": "delivery-chat",
		},
	})
	if err != nil {
		t.Fatalf("Send returned unexpected error: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.calls) == 0 {
		t.Fatal("expected at least one API call (sendMessage), got none")
	}

	// Find the sendMessage call.
	var found bool
	for _, c := range rec.calls {
		if c.method != "sendMessage" {
			continue
		}
		found = true
		got, _ := c.params["chat_id"].(string)
		if got != "delivery-chat" {
			t.Errorf("sendMessage chat_id: got %q, want %q (from Delivery map, not ChatExternalID)", got, "delivery-chat")
		}
	}
	if !found {
		t.Error("no sendMessage call recorded; Send did not call the Telegram API")
	}
}

// TestSendErrorsWhenDeliveryEmpty verifies that when msg.Delivery is nil or
// empty, Send returns an error (ChatID fallback removed, Delivery is required).
func TestSendErrorsWhenDeliveryEmpty(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	_, err := p.Send(context.Background(), dto.OutgoingMessage{
		ChatID:  "direct-chat",
		Content: "hello direct",
		// Delivery is nil — should error.
	})
	if err == nil {
		t.Fatal("expected error when Delivery is empty, got nil")
	}
	if !strings.Contains(err.Error(), "missing chat_id in Delivery") {
		t.Errorf("error = %q, want message about missing chat_id in Delivery", err)
	}
}

// TestHandleUpdateProcessesEditedMessage verifies that an update with only
// EditedMessage (no Message) is processed through the handler.
func TestHandleUpdateProcessesEditedMessage(t *testing.T) {
	var got dto.IncomingMessage
	var called bool
	p := &Plugin{
		botID: "123",
		log:   slog.Default(),
		handler: func(_ context.Context, msg dto.IncomingMessage) error {
			called = true
			got = msg
			return nil
		},
	}

	p.handleUpdate(context.Background(), tgUpdate{
		UpdateID: 1,
		EditedMessage: &tgMessage{
			MessageID: 42,
			Chat:      tgChat{ID: 100, Type: "supergroup"},
			From:      &tgUser{ID: 7},
			Text:      "edited to mention @bot",
			Entities: []tgMessageEntity{
				{Type: "mention", Offset: 17, Length: 4},
			},
		},
	})

	if !called {
		t.Fatal("handler should be called for edited messages")
	}
	if got.Content != "edited to mention @bot" {
		t.Errorf("content = %q, want %q", got.Content, "edited to mention @bot")
	}
	if !got.IsMention {
		t.Error("IsMention should be true when edited message contains a mention")
	}
	if !got.IsGroup {
		t.Error("IsGroup should be true for supergroup chat")
	}
}
