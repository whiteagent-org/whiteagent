package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// newTestStore creates an in-memory SQLite store with migrations applied.
func newTestStore(t *testing.T) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "store.sqlite", json.RawMessage(`{"path":":memory:"}`)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { p.Stop(context.Background()) })
	return p
}

// seedTestData creates a tenant, user, and agent so FK constraints are satisfied.
func seedTestData(t *testing.T, p *Plugin, tid entity.TenantID, uid entity.UserID, aid entity.AgentID) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := p.SaveTenant(ctx, tid, entity.Tenant{Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("seedTestData SaveTenant: %v", err)
	}
	if err := p.SaveUser(ctx, tid, entity.User{ID: uid, Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("seedTestData SaveUser: %v", err)
	}
	if err := p.SaveAgent(ctx, tid, entity.Agent{ID: aid, Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("seedTestData SaveAgent: %v", err)
	}
}

func TestSaveCronEntryPreservesID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	knownID := entity.CronEntryID("test-known-id-123")
	entry := entity.CronEntry{
		ID:           knownID,
		TenantID:     tid,
		AgentID:      aid,
		UserID:       uid,
		ChatID:       "chat-1",
		IsGroup:      false,
		Name:         "id-test",
		Instructions: "check id persistence",
		Type:         "once",
		Status:       "active",
		Metadata:     map[string]string{"sender_name": "Alice"},
	}

	if err := p.SaveCronEntry(ctx, tid, entry); err != nil {
		t.Fatalf("SaveCronEntry: %v", err)
	}

	got, err := p.GetCronEntry(ctx, tid, knownID)
	if err != nil {
		t.Fatalf("GetCronEntry: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.ID != knownID {
		t.Errorf("ID = %q, want %q", got.ID, knownID)
	}

	// Metadata round-trip
	if got.Metadata == nil {
		t.Fatal("expected Metadata, got nil")
	}
	if got.Metadata["sender_name"] != "Alice" {
		t.Errorf("Metadata[sender_name] = %q, want %q", got.Metadata["sender_name"], "Alice")
	}
}

func TestCronEntryCRUD(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, entity.AgentID("a1"))

	now := time.Now().UTC().Truncate(time.Second)
	id := entity.CronEntryID("crud-test-1")
	entry := entity.CronEntry{
		ID:             id,
		TenantID:       tid,
		AgentID:        entity.AgentID("a1"),
		UserID:         uid,
		ChatID:         entity.ChatID("chat-test-1"),
		IsGroup:        true,
		Name:           "daily report",
		Instructions:   "send a summary",
		Type:           "recurring",
		CronExpr:       "0 9 * * *",
		NextRunAt:      &now,
		Status:         "active",
		ConversationID: entity.ConversationID("conv-abc-123"),
		MessageID:      entity.MessageID("msg-abc-123"),
	}

	// Save
	if err := p.SaveCronEntry(ctx, tid, entry); err != nil {
		t.Fatalf("SaveCronEntry: %v", err)
	}

	// Get and verify fields roundtrip
	got, err := p.GetCronEntry(ctx, tid, id)
	if err != nil {
		t.Fatalf("GetCronEntry: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Name != "daily report" {
		t.Errorf("Name = %q, want %q", got.Name, "daily report")
	}
	if got.Instructions != "send a summary" {
		t.Errorf("Instructions = %q, want %q", got.Instructions, "send a summary")
	}
	if got.Type != "recurring" {
		t.Errorf("Type = %q, want %q", got.Type, "recurring")
	}
	if got.CronExpr != "0 9 * * *" {
		t.Errorf("CronExpr = %q, want %q", got.CronExpr, "0 9 * * *")
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if got.NextRunAt == nil {
		t.Fatal("expected NextRunAt, got nil")
	}
	if !got.NextRunAt.Equal(now) {
		t.Errorf("NextRunAt = %v, want %v", got.NextRunAt, now)
	}
	if string(got.AgentID) != "a1" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "a1")
	}
	if got.ChatID != "chat-test-1" {
		t.Errorf("ChatID = %q, want %q", got.ChatID, "chat-test-1")
	}
	if !got.IsGroup {
		t.Error("IsGroup should be true")
	}
	if got.ConversationID != "conv-abc-123" {
		t.Errorf("ConversationID = %q, want %q", got.ConversationID, "conv-abc-123")
	}
	if got.MessageID != "msg-abc-123" {
		t.Errorf("MessageID = %q, want %q", got.MessageID, "msg-abc-123")
	}

	// Delete
	if err := p.DeleteCronEntry(ctx, tid, id); err != nil {
		t.Fatalf("DeleteCronEntry: %v", err)
	}

	// Get after delete returns nil
	got, err = p.GetCronEntry(ctx, tid, id)
	if err != nil {
		t.Fatalf("GetCronEntry after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestListActiveCronEntries(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	seedTestData(t, p, "t1", "u1", "a1")
	seedTestData(t, p, "t2", "u1", "a1")

	// Save entries across two tenants, one paused
	entries := []struct {
		id     entity.CronEntryID
		tid    entity.TenantID
		status string
	}{
		{"active-t1", "t1", "active"},
		{"active-t2", "t2", "active"},
		{"paused-t1", "t1", "paused"},
	}
	for _, e := range entries {
		ce := entity.CronEntry{
			ID:       e.id,
			TenantID: e.tid,
			AgentID:  entity.AgentID("a1"),
			UserID:   entity.UserID("u1"),
			Name:     "task",
			Status:   e.status,
			Type:     "recurring",
		}
		if err := p.SaveCronEntry(ctx, e.tid, ce); err != nil {
			t.Fatalf("SaveCronEntry: %v", err)
		}
	}

	active, err := p.ListActiveCronEntries(ctx)
	if err != nil {
		t.Fatalf("ListActiveCronEntries: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active entries, got %d", len(active))
	}
	// Verify cross-tenant
	tenants := map[entity.TenantID]bool{}
	for _, e := range active {
		tenants[e.TenantID] = true
	}
	if !tenants["t1"] || !tenants["t2"] {
		t.Errorf("expected entries from t1 and t2, got %v", tenants)
	}
}

func TestUpdateCronStatus(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, "u1", "a1")

	id := entity.CronEntryID("status-test-1")
	entry := entity.CronEntry{
		ID:       id,
		TenantID: tid,
		AgentID:  entity.AgentID("a1"),
		UserID:   entity.UserID("u1"),
		Name:     "task",
		Status:   "active",
		Type:     "once",
	}
	if err := p.SaveCronEntry(ctx, tid, entry); err != nil {
		t.Fatalf("SaveCronEntry: %v", err)
	}

	if err := p.UpdateCronStatus(ctx, tid, id, "paused"); err != nil {
		t.Fatalf("UpdateCronStatus: %v", err)
	}

	got, _ := p.GetCronEntry(ctx, tid, id)
	if got.Status != "paused" {
		t.Errorf("Status = %q, want %q", got.Status, "paused")
	}
}

func TestUpdateCronNextRun(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, "u1", "a1")

	now := time.Now().UTC().Truncate(time.Second)
	id := entity.CronEntryID("nextrun-test-1")
	entry := entity.CronEntry{
		ID:        id,
		TenantID:  tid,
		AgentID:   entity.AgentID("a1"),
		UserID:    entity.UserID("u1"),
		Name:      "task",
		Status:    "active",
		Type:      "recurring",
		NextRunAt: &now,
	}
	if err := p.SaveCronEntry(ctx, tid, entry); err != nil {
		t.Fatalf("SaveCronEntry: %v", err)
	}

	// Update to new time
	later := now.Add(24 * time.Hour)
	if err := p.UpdateCronNextRun(ctx, tid, id, &later); err != nil {
		t.Fatalf("UpdateCronNextRun: %v", err)
	}
	got, _ := p.GetCronEntry(ctx, tid, id)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(later) {
		t.Errorf("NextRunAt = %v, want %v", got.NextRunAt, later)
	}

	// Update to nil
	if err := p.UpdateCronNextRun(ctx, tid, id, nil); err != nil {
		t.Fatalf("UpdateCronNextRun to nil: %v", err)
	}
	got, _ = p.GetCronEntry(ctx, tid, id)
	if got.NextRunAt != nil {
		t.Errorf("NextRunAt = %v, want nil", got.NextRunAt)
	}
}

func TestCronRunLifecycle(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, "u1", "a1")

	// Create a cron entry first
	entryID := entity.CronEntryID("run-lifecycle-1")
	entry := entity.CronEntry{
		ID:       entryID,
		TenantID: tid,
		AgentID:  entity.AgentID("a1"),
		UserID:   entity.UserID("u1"),
		Name:     "task",
		Status:   "active",
		Type:     "recurring",
	}
	if err := p.SaveCronEntry(ctx, tid, entry); err != nil {
		t.Fatalf("SaveCronEntry: %v", err)
	}

	// Insert runs
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		run := entity.CronRun{
			ID:          entity.CronRunID(fmt.Sprintf("run-%d", i)),
			CronEntryID: entryID,
			TenantID:    tid,
			StartedAt:   now.Add(time.Duration(i) * time.Hour),
		}
		if err := p.InsertCronRun(ctx, tid, run); err != nil {
			t.Fatalf("InsertCronRun[%d]: %v", i, err)
		}
	}

	// Update first run
	runs, err := p.ListCronRuns(ctx, tid, entryID, 10)
	if err != nil {
		t.Fatalf("ListCronRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
	// Verify DESC order (newest first)
	if runs[0].StartedAt.Before(runs[1].StartedAt) {
		t.Error("expected runs ordered by started_at DESC")
	}

	fin := now.Add(10 * time.Minute)
	if err := p.UpdateCronRun(ctx, tid, runs[0].ID, "success", "", &fin); err != nil {
		t.Fatalf("UpdateCronRun: %v", err)
	}

	// Verify limit
	limited, _ := p.ListCronRuns(ctx, tid, entryID, 2)
	if len(limited) != 2 {
		t.Fatalf("expected 2 runs with limit, got %d", len(limited))
	}

	// Verify updated run
	allRuns, _ := p.ListCronRuns(ctx, tid, entryID, 10)
	updated := allRuns[0]
	if updated.Status != "success" {
		t.Errorf("Status = %q, want %q", updated.Status, "success")
	}
	if updated.FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}
}
