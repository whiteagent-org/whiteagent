package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestAppendAndGetErrorLog(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	now := time.Now().UTC().Truncate(time.Second)
	entries := []entity.ErrorLogEntry{
		{TenantID: tid, UserID: uid, RefType: "cron", RefID: "c1", Content: "first error", CreatedAt: now.Add(-2 * time.Minute)},
		{TenantID: tid, UserID: uid, RefType: "cron", RefID: "c2", Content: "second error", CreatedAt: now.Add(-1 * time.Minute)},
		{TenantID: tid, UserID: uid, RefType: "message", RefID: "m1", Content: "msg error", CreatedAt: now},
	}
	for i, e := range entries {
		if err := p.AppendErrorLog(ctx, tid, e); err != nil {
			t.Fatalf("AppendErrorLog[%d]: %v", i, err)
		}
	}

	// Get all for user
	filter := entity.ErrorLogFilter{UserID: uid}
	got, err := p.GetErrorLog(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetErrorLog: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	// Verify newest-first order
	if got[0].Content != "msg error" {
		t.Errorf("expected newest first, got %q", got[0].Content)
	}
	// Verify IDs generated
	for i, e := range got {
		if e.ID.IsEmpty() {
			t.Errorf("entry[%d] has empty ID", i)
		}
	}
}

func TestErrorLogRefTypeFilter(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	entries := []entity.ErrorLogEntry{
		{TenantID: tid, UserID: uid, RefType: "cron", RefID: "c1", Content: "cron err 1"},
		{TenantID: tid, UserID: uid, RefType: "cron", RefID: "c2", Content: "cron err 2"},
		{TenantID: tid, UserID: uid, RefType: "message", RefID: "m1", Content: "msg err"},
	}
	for _, e := range entries {
		p.AppendErrorLog(ctx, tid, e)
	}

	filter := entity.ErrorLogFilter{UserID: uid, RefType: "cron"}
	got, err := p.GetErrorLog(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetErrorLog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 cron entries, got %d", len(got))
	}
}

func TestErrorLogLimit(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	seedTestData(t, p, tid, uid, "a1")

	for i := 0; i < 5; i++ {
		p.AppendErrorLog(ctx, tid, entity.ErrorLogEntry{
			TenantID: tid, UserID: uid, Content: "error",
		})
	}

	filter := entity.ErrorLogFilter{UserID: uid, Limit: 3}
	got, err := p.GetErrorLog(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetErrorLog: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries with limit, got %d", len(got))
	}
}

func TestErrorLogUserScoping(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, "u1", "a1")
	seedTestData(t, p, tid, "u2", "a1") // reuses same tenant+agent, adds second user

	p.AppendErrorLog(ctx, tid, entity.ErrorLogEntry{TenantID: tid, UserID: entity.UserID("u1"), Content: "u1 err"})
	p.AppendErrorLog(ctx, tid, entity.ErrorLogEntry{TenantID: tid, UserID: entity.UserID("u2"), Content: "u2 err"})

	filter := entity.ErrorLogFilter{UserID: entity.UserID("u1")}
	got, err := p.GetErrorLog(ctx, tid, filter)
	if err != nil {
		t.Fatalf("GetErrorLog: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry for u1, got %d", len(got))
	}
	if got[0].Content != "u1 err" {
		t.Errorf("expected u1 err, got %q", got[0].Content)
	}
}
