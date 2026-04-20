package cron_set_status

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore implements the store methods needed by cron_set_status.
type mockStore struct {
	port.StorePlugin // embed to satisfy interface

	entries map[entity.CronEntryID]*entity.CronEntry

	lastStatusUpdate struct {
		id     entity.CronEntryID
		status string
	}
	lastNextRunUpdate struct {
		id        entity.CronEntryID
		nextRunAt *time.Time
	}
}

func newMockStore() *mockStore {
	return &mockStore{entries: make(map[entity.CronEntryID]*entity.CronEntry)}
}

func (m *mockStore) GetCronEntry(_ context.Context, _ entity.TenantID, id entity.CronEntryID) (*entity.CronEntry, error) {
	e, ok := m.entries[id]
	if !ok {
		return nil, nil
	}
	cp := *e
	return &cp, nil
}

func (m *mockStore) UpdateCronStatus(_ context.Context, _ entity.TenantID, id entity.CronEntryID, status string) error {
	m.lastStatusUpdate.id = id
	m.lastStatusUpdate.status = status
	if e, ok := m.entries[id]; ok {
		e.Status = status
	}
	return nil
}

func (m *mockStore) UpdateCronNextRun(_ context.Context, _ entity.TenantID, id entity.CronEntryID, nextRunAt *time.Time) error {
	m.lastNextRunUpdate.id = id
	m.lastNextRunUpdate.nextRunAt = nextRunAt
	if e, ok := m.entries[id]; ok {
		e.NextRunAt = nextRunAt
	}
	return nil
}

func tc(tenantID, userID string) port.ToolContext {
	return port.ToolContext{
		TenantID: entity.TenantID(tenantID),
		UserID:   entity.UserID(userID),
	}
}

func args(id, status string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"id": id, "status": status})
	return b
}

func TestPauseSuccess(t *testing.T) {
	ms := newMockStore()
	ms.entries["e1"] = &entity.CronEntry{
		ID: "e1", TenantID: "t1", UserID: "u1",
		Name: "Daily report", Status: "active", Type: "recurring",
	}

	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("e1", "paused"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms.lastStatusUpdate.status != "paused" {
		t.Errorf("expected status 'paused', got %q", ms.lastStatusUpdate.status)
	}
	if !strings.Contains(result, "paused") {
		t.Errorf("result should mention paused: %s", result)
	}
	if !strings.Contains(result, "Daily report") {
		t.Errorf("result should mention task name: %s", result)
	}
}

func TestResumeSuccess(t *testing.T) {
	ms := newMockStore()
	ms.entries["e2"] = &entity.CronEntry{
		ID: "e2", TenantID: "t1", UserID: "u1",
		Name: "Hourly check", Status: "paused", Type: "recurring",
		CronExpr: "0 * * * *",
	}

	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("e2", "active"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms.lastStatusUpdate.status != "active" {
		t.Errorf("expected status 'active', got %q", ms.lastStatusUpdate.status)
	}
	if ms.lastNextRunUpdate.nextRunAt == nil {
		t.Fatal("expected nextRunAt to be set for resumed recurring entry")
	}
	if !strings.Contains(result, "active") {
		t.Errorf("result should mention active: %s", result)
	}
	if !strings.Contains(result, "Next run") {
		t.Errorf("result should mention next run time: %s", result)
	}
}

func TestDeleteSuccess(t *testing.T) {
	ms := newMockStore()
	ms.entries["e3"] = &entity.CronEntry{
		ID: "e3", TenantID: "t1", UserID: "u1",
		Name: "Old task", Status: "active", Type: "once",
	}

	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("e3", "deleted"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms.lastStatusUpdate.status != "deleted" {
		t.Errorf("expected status 'deleted', got %q", ms.lastStatusUpdate.status)
	}
	if !strings.Contains(result, "Removed") {
		t.Errorf("result should say Removed: %s", result)
	}
	if !strings.Contains(result, "Old task") {
		t.Errorf("result should mention task name: %s", result)
	}
}

func TestNotFound(t *testing.T) {
	ms := newMockStore()
	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("nonexistent", "paused"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "not found") {
		t.Errorf("result should say not found: %s", result)
	}
}

func TestForbidden(t *testing.T) {
	ms := newMockStore()
	ms.entries["e4"] = &entity.CronEntry{
		ID: "e4", TenantID: "t1", UserID: "other_user",
		Name: "Someone else's task", Status: "active",
	}

	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("e4", "paused"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "not authorized") {
		t.Errorf("result should say not authorized: %s", result)
	}
}

func TestInvalidStatus(t *testing.T) {
	ms := newMockStore()
	ms.entries["e5"] = &entity.CronEntry{
		ID: "e5", TenantID: "t1", UserID: "u1",
		Name: "Some task", Status: "active",
	}

	p := &Plugin{}
	p.SetStore(ms)

	result, err := p.Execute(context.Background(), tc("t1", "u1"), args("e5", "running"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "active") || !strings.Contains(result, "paused") || !strings.Contains(result, "deleted") {
		t.Errorf("result should mention valid statuses: %s", result)
	}
}

func TestMissingStore(t *testing.T) {
	p := &Plugin{}

	_, err := p.Execute(context.Background(), tc("t1", "u1"), args("e1", "paused"))
	if err == nil {
		t.Fatal("expected error when store is nil")
	}
}
