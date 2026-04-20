package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestMemoryGetReturnsNilWhenNotFound verifies that GetMemory returns (nil, nil)
// when no memory entry exists for the given owner.
func TestMemoryGetReturnsNilWhenNotFound(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-mem-1")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	mem, err := p.GetMemory(ctx, tid, "user", "u-nonexistent")
	if err != nil {
		t.Fatalf("GetMemory: unexpected error: %v", err)
	}
	if mem != nil {
		t.Errorf("GetMemory: expected nil for missing entry, got %+v", mem)
	}
}

// TestMemorySaveAndGetUserMemoryRoundTrip verifies that user memory can be saved
// and retrieved correctly with the correct owner type and ID.
func TestMemorySaveAndGetUserMemoryRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-mem-2")
	uid := entity.UserID("u-alice")
	seedTestData(t, p, tid, uid, entity.AgentID("a1"))

	now := time.Now().UTC().Truncate(time.Second)
	mem := entity.Memory{
		TenantID:  tid,
		OwnerType: "user",
		OwnerID:   uid.String(),
		Content:   "Alice prefers dark mode and Go.",
		UpdatedAt: now,
	}

	if err := p.SaveMemory(ctx, tid, mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	got, err := p.GetMemory(ctx, tid, "user", uid.String())
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got == nil {
		t.Fatal("GetMemory: expected non-nil memory, got nil")
	}
	if got.Content != mem.Content {
		t.Errorf("Content: got %q, want %q", got.Content, mem.Content)
	}
	if got.OwnerType != "user" {
		t.Errorf("OwnerType: got %q, want %q", got.OwnerType, "user")
	}
	if got.OwnerID != uid.String() {
		t.Errorf("OwnerID: got %q, want %q", got.OwnerID, uid.String())
	}
	if got.TenantID != tid {
		t.Errorf("TenantID: got %q, want %q", got.TenantID, tid)
	}
}

// TestMemorySaveAndGetGroupMemoryRoundTrip verifies that group memory can be saved
// and retrieved with "group" owner type and the GroupID as owner ID.
func TestMemorySaveAndGetGroupMemoryRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-mem-3")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	chatID := entity.ChatID("chat-eng-team")
	mem := entity.Memory{
		TenantID:  tid,
		OwnerType: "chat",
		OwnerID:   chatID.String(),
		Content:   "Team uses Go and deploys on Fridays.",
	}

	if err := p.SaveMemory(ctx, tid, mem); err != nil {
		t.Fatalf("SaveMemory (group): %v", err)
	}

	got, err := p.GetMemory(ctx, tid, "chat", chatID.String())
	if err != nil {
		t.Fatalf("GetMemory (group): %v", err)
	}
	if got == nil {
		t.Fatal("GetMemory (group): expected non-nil memory, got nil")
	}
	if got.Content != mem.Content {
		t.Errorf("Content: got %q, want %q", got.Content, mem.Content)
	}
	if got.OwnerType != "chat" {
		t.Errorf("OwnerType: got %q, want %q", got.OwnerType, "chat")
	}
}

// TestMemoryUpsertUpdatesExistingContent verifies that SaveMemory performs an
// upsert — calling it twice with the same composite key updates the content.
func TestMemoryUpsertUpdatesExistingContent(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-mem-4")
	uid := entity.UserID("u-bob")
	seedTestData(t, p, tid, uid, entity.AgentID("a1"))

	first := entity.Memory{
		TenantID:  tid,
		OwnerType: "user",
		OwnerID:   uid.String(),
		Content:   "Original memory.",
	}
	if err := p.SaveMemory(ctx, tid, first); err != nil {
		t.Fatalf("SaveMemory (first): %v", err)
	}

	updated := entity.Memory{
		TenantID:  tid,
		OwnerType: "user",
		OwnerID:   uid.String(),
		Content:   "Updated memory after upsert.",
	}
	if err := p.SaveMemory(ctx, tid, updated); err != nil {
		t.Fatalf("SaveMemory (upsert): %v", err)
	}

	got, err := p.GetMemory(ctx, tid, "user", uid.String())
	if err != nil {
		t.Fatalf("GetMemory (after upsert): %v", err)
	}
	if got == nil {
		t.Fatal("GetMemory: expected non-nil after upsert")
	}
	if got.Content != "Updated memory after upsert." {
		t.Errorf("Content after upsert: got %q, want %q", got.Content, "Updated memory after upsert.")
	}
}

// TestMemoryIsolatedByTenant verifies that memories are tenant-scoped:
// one tenant cannot see another tenant's memory.
func TestMemoryIsolatedByTenant(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	tid1 := entity.TenantID("t-mem-iso-1")
	tid2 := entity.TenantID("t-mem-iso-2")
	uid := entity.UserID("u-shared-name")
	seedTestData(t, p, tid1, uid, entity.AgentID("a1"))
	seedTestData(t, p, tid2, uid, entity.AgentID("a1"))

	mem1 := entity.Memory{TenantID: tid1, OwnerType: "user", OwnerID: uid.String(), Content: "tenant-1 secret"}
	if err := p.SaveMemory(ctx, tid1, mem1); err != nil {
		t.Fatalf("SaveMemory tenant1: %v", err)
	}

	got, err := p.GetMemory(ctx, tid2, "user", uid.String())
	if err != nil {
		t.Fatalf("GetMemory tenant2: %v", err)
	}
	if got != nil {
		t.Errorf("tenant isolation broken: tenant2 can read tenant1 memory: %+v", got)
	}
}
