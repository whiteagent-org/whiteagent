package cron_create

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore captures the last saved cron entry.
type mockStore struct {
	port.StorePlugin
	mu    sync.Mutex
	saved *entity.CronEntry
}

func (m *mockStore) SaveCronEntry(_ context.Context, _ entity.TenantID, entry entity.CronEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = &entry
	return nil
}

func (m *mockStore) lastSaved() *entity.CronEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

func newTestPlugin(tz string) *Plugin {
	p := NewPlugin().(*Plugin)
	if tz != "" {
		cfg, _ := json.Marshal(map[string]string{"timezone": tz})
		_ = p.Init(context.Background(), "tool.cron_create", cfg)
	}
	return p
}

func tc() port.ToolContext {
	return port.ToolContext{
		TenantID:  "t1",
		AgentID:   "agent-1",
		UserID:    "u1",
		ChatID:    "chat-1",
		IsGroup:   false,
		MessageID: "msg-1",
	}
}

func TestCreateRecurring(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{
		"schedule":     "0 9 * * *",
		"instructions": "Send daily standup",
		"name":         "standup",
		"type":         "recurring",
	})

	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "standup") {
		t.Errorf("result should contain name, got: %s", result)
	}
	if !strings.Contains(result, "recurring") {
		t.Errorf("result should contain type, got: %s", result)
	}
	if !strings.Contains(result, "active") {
		t.Errorf("result should contain status, got: %s", result)
	}

	saved := ms.lastSaved()
	if saved == nil {
		t.Fatal("expected entry to be saved")
	}
	if saved.Type != "recurring" {
		t.Errorf("type = %q, want recurring", saved.Type)
	}
	if saved.CronExpr != "0 9 * * *" {
		t.Errorf("cron expr = %q, want '0 9 * * *'", saved.CronExpr)
	}
	if saved.NextRunAt == nil {
		t.Error("NextRunAt should be set")
	}
	if saved.Status != "active" {
		t.Errorf("status = %q, want active", saved.Status)
	}
	if saved.ChatID != "chat-1" {
		t.Errorf("ChatID = %q, want chat-1", saved.ChatID)
	}
}

func TestCreateOnce(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	args, _ := json.Marshal(map[string]string{
		"schedule":     future,
		"instructions": "Remind me",
		"name":         "reminder",
		"type":         "once",
	})

	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "once") {
		t.Errorf("result should contain type, got: %s", result)
	}

	saved := ms.lastSaved()
	if saved == nil {
		t.Fatal("expected entry to be saved")
	}
	if saved.Type != "once" {
		t.Errorf("type = %q, want once", saved.Type)
	}
	if saved.NextRunAt == nil {
		t.Error("NextRunAt should be set")
	}
	if saved.CronExpr != "" {
		t.Errorf("CronExpr should be empty for once, got %q", saved.CronExpr)
	}
}

func TestInvalidCronExpr(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{
		"schedule":     "bad cron",
		"instructions": "test",
		"name":         "test",
		"type":         "recurring",
	})

	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("should return descriptive error in result, not Go error: %v", err)
	}

	if !strings.Contains(strings.ToLower(result), "5-field") {
		t.Errorf("result should hint about 5-field format, got: %s", result)
	}
}

func TestInvalidTimestamp(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{
		"schedule":     "not-a-timestamp",
		"instructions": "test",
		"name":         "test",
		"type":         "once",
	})

	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("should return descriptive error in result, not Go error: %v", err)
	}

	if !strings.Contains(result, "RFC3339") || !strings.Contains(result, "2006-01-02T15:04:05Z") {
		t.Errorf("result should contain RFC3339 example, got: %s", result)
	}
}

func TestInvalidTimezone(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{
		"schedule":     "0 9 * * *",
		"instructions": "test",
		"name":         "test",
		"type":         "recurring",
		"timezone":     "Invalid/Zone",
	})

	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("should return descriptive error in result, not Go error: %v", err)
	}

	if !strings.Contains(strings.ToLower(result), "timezone") {
		t.Errorf("result should mention timezone, got: %s", result)
	}
}

func TestTimezoneConversion(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	// User says "2026-03-15T09:00:00" in America/New_York (UTC-4 in March)
	args, _ := json.Marshal(map[string]string{
		"schedule":     "2026-03-15T09:00:00",
		"instructions": "test",
		"name":         "tz test",
		"type":         "once",
		"timezone":     "America/New_York",
	})

	_, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved := ms.lastSaved()
	if saved == nil {
		t.Fatal("expected entry to be saved")
	}
	if saved.NextRunAt == nil {
		t.Fatal("NextRunAt should be set")
	}
	// 9:00 AM EDT (UTC-4) = 13:00 UTC
	if saved.NextRunAt.UTC().Hour() != 13 {
		t.Errorf("expected hour 13 UTC, got %d", saved.NextRunAt.UTC().Hour())
	}
}

func TestMissingStore(t *testing.T) {
	p := newTestPlugin("UTC")

	args, _ := json.Marshal(map[string]string{
		"schedule":     "0 9 * * *",
		"instructions": "test",
		"name":         "test",
		"type":         "recurring",
	})

	_, err := p.Execute(context.Background(), tc(), args)
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestChatContextCapture(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{}
	p.SetStore(ms)

	ctx := port.ToolContext{
		TenantID:  "t1",
		AgentID:   "agent-99",
		UserID:    "u1",
		ChatID:    "chat-99",
		IsGroup:   true,
		MessageID: "msg-99",
	}

	args, _ := json.Marshal(map[string]string{
		"schedule":     "0 9 * * *",
		"instructions": "test",
		"name":         "ctx test",
		"type":         "recurring",
	})

	_, err := p.Execute(context.Background(), ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved := ms.lastSaved()
	if saved == nil {
		t.Fatal("expected entry to be saved")
	}
	if saved.ChatID != "chat-99" {
		t.Errorf("ChatID = %q, want chat-99", saved.ChatID)
	}
	if !saved.IsGroup {
		t.Error("expected IsGroup to be true")
	}
	if saved.AgentID != "agent-99" {
		t.Errorf("AgentID = %q, want agent-99", saved.AgentID)
	}
	if saved.MessageID != "msg-99" {
		t.Errorf("MessageID = %q, want msg-99", saved.MessageID)
	}
}
