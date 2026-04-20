package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestJournalMessageIDStoredAndRetrieved(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	entry := entity.JournalEntry{
		TenantID:  tid,
		UserID:    uid,
		Category:  "Key Events",
		Content:   "agent met user",
		MessageID: "msg-abc-123",
	}

	if err := p.AppendJournal(ctx, tid, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	filter := entity.JournalFilter{UserID: uid}
	got, err := p.GetJournal(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetJournal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].MessageID != "msg-abc-123" {
		t.Errorf("expected MessageID %q, got %q", "msg-abc-123", got[0].MessageID)
	}
	if got[0].Content != "agent met user" {
		t.Errorf("expected Content %q, got %q", "agent met user", got[0].Content)
	}
}

func TestJournalConversation(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	// Insert two entries with different ConversationIDs and different times.
	entryA := entity.JournalEntry{
		TenantID:       tid,
		UserID:         uid,
		ConversationID: entity.ConversationID("conv-A"),
		Category:       "Key Events",
		Content:        "entry from conv A",
	}
	entryB := entity.JournalEntry{
		TenantID:       tid,
		UserID:         uid,
		ConversationID: entity.ConversationID("conv-B"),
		Category:       "Key Events",
		Content:        "entry from conv B",
	}

	if err := p.AppendJournal(ctx, tid, entryA); err != nil {
		t.Fatalf("AppendJournal A: %v", err)
	}
	// Sleep >1 second so RFC3339 timestamps differ.
	time.Sleep(1100 * time.Millisecond)
	midTime := time.Now().UTC()
	time.Sleep(1100 * time.Millisecond)

	if err := p.AppendJournal(ctx, tid, entryB); err != nil {
		t.Fatalf("AppendJournal B: %v", err)
	}

	// Filter by ConversationID only.
	got, err := p.GetJournal(ctx, tid, entity.JournalFilter{UserID: uid, ConversationID: "conv-A"})
	if err != nil {
		t.Fatalf("GetJournal conv-A: %v", err)
	}
	if len(got) != 1 || got[0].Content != "entry from conv A" {
		t.Errorf("expected 1 conv-A entry, got %d", len(got))
	}
	if got[0].ConversationID != entity.ConversationID("conv-A") {
		t.Errorf("expected ConversationID 'conv-A', got %q", got[0].ConversationID)
	}

	// Filter by BeforeTime only (should return only entry A).
	got, err = p.GetJournal(ctx, tid, entity.JournalFilter{UserID: uid, BeforeTime: midTime})
	if err != nil {
		t.Fatalf("GetJournal BeforeTime: %v", err)
	}
	if len(got) != 1 || got[0].Content != "entry from conv A" {
		t.Errorf("expected 1 entry before midTime, got %d", len(got))
	}

	// Combined filter: ConversationID + BeforeTime (far future) returns conv-A only.
	got, err = p.GetJournal(ctx, tid, entity.JournalFilter{
		UserID:         uid,
		ConversationID: "conv-A",
		BeforeTime:     time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("GetJournal combined: %v", err)
	}
	if len(got) != 1 || got[0].Content != "entry from conv A" {
		t.Errorf("expected 1 conv-A entry with combined filter, got %d", len(got))
	}
}

func TestJournalMessageIDEmptyWhenUnlinked(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	entry := entity.JournalEntry{
		TenantID: tid,
		UserID:   uid,
		Category: "Preferences",
		Content:  "prefers short answers",
		// MessageID intentionally left empty
	}

	if err := p.AppendJournal(ctx, tid, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	filter := entity.JournalFilter{UserID: uid}
	got, err := p.GetJournal(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetJournal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].MessageID != "" {
		t.Errorf("expected empty MessageID for unlinked entry, got %q", got[0].MessageID)
	}
}
