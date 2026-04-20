package outbound

import (
	"context"
	"errors"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/mapper"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stubMapperStore satisfies port.StorePlugin for the mapper dependency.
// GetMessages returns empty by default (no TargetID lookups needed in these tests).
type stubMapperStore struct {
	port.StorePlugin
}

func (s *stubMapperStore) GetMessages(_ context.Context, _ entity.TenantID, _ port.MessageFilter) ([]entity.Message, error) {
	return nil, nil
}

func (s *stubMapperStore) GetChat(_ context.Context, _ entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	// Return the chat with a ChannelID matching the test channel registration.
	channelID := string(chatID) // test convention: chatID == channelID
	return &entity.Chat{
		ID:             chatID,
		ChannelID:      channelID,
		ExternalChatID: "ext-chat-1",
	}, nil
}

func (s *stubMapperStore) SaveChat(_ context.Context, _ entity.TenantID, _ entity.Chat) error {
	return nil
}

// stubHandlerStore satisfies port.StorePlugin for the outbound handler's UpdateExternalMessageID call.
type stubHandlerStore struct {
	port.StorePlugin

	updateCalled       bool
	updatedMsgID       entity.MessageID
	updatedExternalID  string
	updateErr          error
}

func (s *stubHandlerStore) UpdateExternalMessageID(_ context.Context, msgID entity.MessageID, externalMsgID string) error {
	s.updateCalled = true
	s.updatedMsgID = msgID
	s.updatedExternalID = externalMsgID
	return s.updateErr
}

// stubChannel satisfies port.ChannelPlugin for Send testing.
type stubChannel struct {
	port.ChannelPlugin

	sendResult port.SendResult
	sendErr    error
	sendPanic  bool
	sendCalled bool
}

func (c *stubChannel) Send(_ context.Context, _ dto.OutgoingMessage) (port.SendResult, error) {
	c.sendCalled = true
	if c.sendPanic {
		panic("channel panic")
	}
	return c.sendResult, c.sendErr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildHandler creates a NewHandler with the provided channel and handler store.
// The mapper uses a no-op store (no TargetID lookups needed).
func buildHandler(
	channelID string,
	ch *stubChannel,
	handlerStore *stubHandlerStore,
) port.MessageHandler {
	entry := port.ChannelEntry{Plugin: ch}
	channels := map[string]port.ChannelEntry{channelID: entry}
	m := mapper.NewMapper(&stubMapperStore{}, nil) // resolver nil: ToOutgoing does not use it
	return NewHandler(channels, m, handlerStore)
}

// baseMsg returns a minimal entity.Message routed to the given channelID.
func baseMsg(channelID string) entity.Message {
	return entity.Message{
		ID:       "msg-1",
		TenantID: "tenant-1",
		AgentID:  "agent-1",
		ChatID:   entity.ChatID(channelID), // test convention: chatID matches channelID
		Kind:     entity.MessageKindMessage,
		Role:     entity.RoleAssistant,
		Content:  "hello",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestOutboundRoutesToCorrectChannel verifies that the handler routes to the
// channel whose ID matches outgoing.ChannelID.
func TestOutboundRoutesToCorrectChannel(t *testing.T) {
	ch := &stubChannel{sendResult: port.SendResult{MessageID: "ext-1"}}
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	err := handler(context.Background(), baseMsg("chan-A"))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !ch.sendCalled {
		t.Fatal("expected Send to be called on the matching channel, but it was not")
	}
}

// TestOutboundReturnsNilOnChannelNotFound verifies that when the outgoing
// message references a channel not in the map, the handler returns nil
// (tenant isolation — unknown channel must not block the bus).
func TestOutboundReturnsNilOnChannelNotFound(t *testing.T) {
	ch := &stubChannel{}
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	msg := baseMsg("chan-UNKNOWN")
	err := handler(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected nil on channel-not-found, got: %v", err)
	}
	if ch.sendCalled {
		t.Fatal("Send must not be called when channel is not found")
	}
}

// TestOutboundReturnsNilOnSendError verifies that when channel.Send returns
// an error, the handler returns nil (tenant isolation).
func TestOutboundReturnsNilOnSendError(t *testing.T) {
	ch := &stubChannel{sendErr: errors.New("channel unavailable")}
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	err := handler(context.Background(), baseMsg("chan-A"))
	if err != nil {
		t.Fatalf("expected nil on send error (tenant isolation), got: %v", err)
	}
}

// TestOutboundCallsUpdateExternalMessageIDWhenSendResultHasMessageID verifies
// that after a successful Send returning a non-empty MessageID, the handler
// calls store.UpdateExternalMessageID with the correct arguments.
func TestOutboundCallsUpdateExternalMessageIDWhenSendResultHasMessageID(t *testing.T) {
	ch := &stubChannel{sendResult: port.SendResult{MessageID: "ext-42"}}
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	msg := baseMsg("chan-A")
	msg.ID = "msg-internal-99"
	msg.TenantID = "tenant-abc"

	err := handler(context.Background(), msg)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if !store.updateCalled {
		t.Fatal("expected UpdateExternalMessageID to be called, but it was not")
	}
	if store.updatedMsgID != "msg-internal-99" {
		t.Errorf("UpdateExternalMessageID msgID: got %q, want %q", store.updatedMsgID, "msg-internal-99")
	}
	if store.updatedExternalID != "ext-42" {
		t.Errorf("UpdateExternalMessageID externalMsgID: got %q, want %q", store.updatedExternalID, "ext-42")
	}
}

// TestOutboundDoesNotCallUpdateExternalMessageIDWhenSendResultMessageIDIsEmpty
// verifies that when Send returns an empty MessageID, UpdateExternalMessageID
// is NOT called (no external ID to persist).
func TestOutboundDoesNotCallUpdateExternalMessageIDWhenSendResultMessageIDIsEmpty(t *testing.T) {
	ch := &stubChannel{sendResult: port.SendResult{MessageID: ""}} // empty MessageID
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	err := handler(context.Background(), baseMsg("chan-A"))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if store.updateCalled {
		t.Fatal("UpdateExternalMessageID must NOT be called when SendResult.MessageID is empty")
	}
}

// TestOutboundUpdateExternalMessageIDErrorDoesNotPropagateToHandler verifies
// that if UpdateExternalMessageID fails, the handler still returns nil
// (fire-and-forget — store errors must not block the bus).
func TestOutboundUpdateExternalMessageIDErrorDoesNotPropagateToHandler(t *testing.T) {
	ch := &stubChannel{sendResult: port.SendResult{MessageID: "ext-1"}}
	store := &stubHandlerStore{updateErr: errors.New("db unavailable")}
	handler := buildHandler("chan-A", ch, store)

	err := handler(context.Background(), baseMsg("chan-A"))
	if err != nil {
		t.Fatalf("expected nil even when UpdateExternalMessageID fails, got: %v", err)
	}
}

// TestOutboundSafeSendRecoversPanic verifies that a panic in channel.Send is
// caught by safeSend and results in the handler returning nil rather than
// crashing (tenant isolation — panicking channels must not kill the bus).
func TestOutboundSafeSendRecoversPanic(t *testing.T) {
	ch := &stubChannel{sendPanic: true}
	store := &stubHandlerStore{}
	handler := buildHandler("chan-A", ch, store)

	var recovered bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered = true
			}
		}()
		err := handler(context.Background(), baseMsg("chan-A"))
		if err != nil {
			t.Errorf("expected nil after panic recovery, got: %v", err)
		}
	}()

	if recovered {
		t.Fatal("panic escaped safeSend — should have been caught internally")
	}
}

// TestSafeSendReturnsPanicAsError verifies that safeSend itself converts a
// panic to a non-nil error with descriptive message.
func TestSafeSendReturnsPanicAsError(t *testing.T) {
	ch := &stubChannel{sendPanic: true}
	_, err := safeSend(context.Background(), ch, dto.OutgoingMessage{})
	if err == nil {
		t.Fatal("expected error from safeSend on panic, got nil")
	}
	want := "panic in channel.Send"
	if !contains(err.Error(), want) {
		t.Errorf("error message: got %q, want substring %q", err.Error(), want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestOutboundHandlerAlwaysReturnsNil is a table-driven regression test
// verifying the tenant-isolation contract: the handler never returns a
// non-nil error regardless of what goes wrong.
func TestOutboundHandlerAlwaysReturnsNil(t *testing.T) {
	cases := []struct {
		name      string
		channelID string // channel ID on the message
		sendErr   error
		sendPanic bool
	}{
		{"channel not found", "unknown-chan", nil, false},
		{"send error", "chan-A", errors.New("network error"), false},
		{"send panic", "chan-A", nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &stubChannel{sendErr: tc.sendErr, sendPanic: tc.sendPanic}
			store := &stubHandlerStore{}
			handler := buildHandler("chan-A", ch, store)

			msg := baseMsg(tc.channelID)
			err := handler(context.Background(), msg)
			if err != nil {
				t.Errorf("case %q: expected nil, got %v", tc.name, err)
			}
		})
	}
}

