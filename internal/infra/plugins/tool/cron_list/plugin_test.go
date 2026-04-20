package cron_list

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

type mockStore struct {
	port.StorePlugin
	entries []entity.CronEntry
	runs    map[entity.CronEntryID][]entity.CronRun
}

func (m *mockStore) ListCronEntries(_ context.Context, _ entity.TenantID, _ entity.UserID) ([]entity.CronEntry, error) {
	return m.entries, nil
}

func (m *mockStore) ListCronRuns(_ context.Context, _ entity.TenantID, id entity.CronEntryID, limit int) ([]entity.CronRun, error) {
	runs := m.runs[id]
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func newTestPlugin(tz string) *Plugin {
	p := NewPlugin().(*Plugin)
	if tz != "" {
		cfg, _ := json.Marshal(map[string]string{"timezone": tz})
		_ = p.Init(context.Background(), "tool.cron_list", cfg)
	}
	return p
}

func tc() port.ToolContext {
	return port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
	}
}

func TestListEntries(t *testing.T) {
	p := newTestPlugin("UTC")
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	ms := &mockStore{
		entries: []entity.CronEntry{
			{
				ID:        "ce-1",
				Name:      "standup",
				Type:      "recurring",
				Status:    "active",
				CronExpr:  "0 9 * * *",
				NextRunAt: &next,
				CreatedAt: now,
			},
		},
		runs: map[entity.CronEntryID][]entity.CronRun{},
	}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"ce-1", "standup", "recurring", "active", "0 9 * * *"} {
		if !strings.Contains(result, want) {
			t.Errorf("result should contain %q, got:\n%s", want, result)
		}
	}
}

func TestListWithLastRun(t *testing.T) {
	p := newTestPlugin("UTC")
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	finished := now.Add(-10 * time.Minute)
	ms := &mockStore{
		entries: []entity.CronEntry{
			{
				ID:        "ce-2",
				Name:      "report",
				Type:      "recurring",
				Status:    "active",
				CronExpr:  "0 12 * * 1",
				NextRunAt: &next,
				CreatedAt: now,
			},
		},
		runs: map[entity.CronEntryID][]entity.CronRun{
			"ce-2": {
				{
					ID:         "cr-1",
					Status:     "success",
					StartedAt:  now.Add(-15 * time.Minute),
					FinishedAt: &finished,
				},
			},
		},
	}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "success") {
		t.Errorf("result should show last run status, got:\n%s", result)
	}
}

func TestListEmpty(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{entries: nil, runs: map[entity.CronEntryID][]entity.CronRun{}}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "No scheduled tasks found." {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestListTimezone(t *testing.T) {
	p := newTestPlugin("UTC")
	ms := &mockStore{
		entries: []entity.CronEntry{
			{
				ID:        "ce-3",
				Name:      "tz-test",
				Type:      "once",
				Status:    "active",
				NextRunAt: timePtr(time.Date(2026, 3, 15, 13, 0, 0, 0, time.UTC)),
				CreatedAt: time.Now().UTC(),
			},
		},
		runs: map[entity.CronEntryID][]entity.CronRun{},
	}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{"timezone": "America/New_York"})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 13:00 UTC = 09:00 EDT
	if !strings.Contains(result, "09:00") {
		t.Errorf("expected time converted to EDT, got:\n%s", result)
	}
}

func TestMissingStore(t *testing.T) {
	p := newTestPlugin("UTC")

	args, _ := json.Marshal(map[string]string{})
	_, err := p.Execute(context.Background(), tc(), args)
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestListFiltersDeleted(t *testing.T) {
	p := newTestPlugin("UTC")
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	ms := &mockStore{
		entries: []entity.CronEntry{
			{
				ID:        "ce-a",
				Name:      "keep-active",
				Type:      "recurring",
				Status:    "active",
				CronExpr:  "0 9 * * *",
				NextRunAt: &next,
				CreatedAt: now,
			},
			{
				ID:        "ce-p",
				Name:      "keep-paused",
				Type:      "recurring",
				Status:    "paused",
				CronExpr:  "0 12 * * *",
				NextRunAt: &next,
				CreatedAt: now,
			},
			{
				ID:        "ce-d",
				Name:      "gone-deleted",
				Type:      "recurring",
				Status:    "deleted",
				CronExpr:  "0 18 * * *",
				NextRunAt: &next,
				CreatedAt: now,
			},
		},
		runs: map[entity.CronEntryID][]entity.CronRun{},
	}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "keep-active") {
		t.Errorf("result should contain active entry, got:\n%s", result)
	}
	if !strings.Contains(result, "keep-paused") {
		t.Errorf("result should contain paused entry, got:\n%s", result)
	}
	if strings.Contains(result, "gone-deleted") {
		t.Errorf("result should NOT contain deleted entry name, got:\n%s", result)
	}
	if strings.Contains(result, "ce-d") {
		t.Errorf("result should NOT contain deleted entry ID, got:\n%s", result)
	}
}

func TestListAllDeleted(t *testing.T) {
	p := newTestPlugin("UTC")
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	ms := &mockStore{
		entries: []entity.CronEntry{
			{
				ID:        "ce-del",
				Name:      "removed-task",
				Type:      "once",
				Status:    "deleted",
				NextRunAt: &next,
				CreatedAt: now,
			},
		},
		runs: map[entity.CronEntryID][]entity.CronRun{},
	}
	p.SetStore(ms)

	args, _ := json.Marshal(map[string]string{})
	result, err := p.Execute(context.Background(), tc(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "No scheduled tasks found." {
		t.Errorf("expected empty message when all deleted, got: %s", result)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
