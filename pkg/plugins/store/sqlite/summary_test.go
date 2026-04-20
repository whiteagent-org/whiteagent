package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestSummarySaveStoresMessageBoundary(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("tenant-1")
	seedTestData(t, p, tid, entity.UserID("user-1"), entity.AgentID("agent-1"))

	summary := makeSummary("summary-1", tid, "conv-1", "msg-050", "first summary", time.Now().UTC().Truncate(time.Second))
	if err := p.SaveSummary(ctx, tid, summary); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	var messageID string
	err := p.db.QueryRowContext(ctx, `SELECT message_id FROM summaries WHERE tenant_id = ? AND conversation_id = ? AND id = ?`,
		string(tid), "conv-1", "summary-1").Scan(&messageID)
	if err != nil {
		t.Fatalf("query saved summary: %v", err)
	}
	if messageID != "msg-050" {
		t.Fatalf("message_id = %q, want %q", messageID, "msg-050")
	}
}

func TestSummaryGetLatestReturnsNilWhenMissing(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	got, err := p.GetLatestSummary(ctx, entity.TenantID("tenant-1"), entity.ConversationID("conv-1"))
	if err != nil {
		t.Fatalf("GetLatestSummary: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil summary, got %#v", got)
	}
}

func TestSummaryListReturnsChronologicalOrder(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("tenant-1")
	seedTestData(t, p, tid, entity.UserID("user-1"), entity.AgentID("agent-1"))

	base := time.Now().UTC().Truncate(time.Second)
	summaries := []entity.Summary{
		makeSummary("summary-1", tid, "conv-1", "msg-010", "first", base.Add(1*time.Minute)),
		makeSummary("summary-2", tid, "conv-1", "msg-020", "second", base.Add(2*time.Minute)),
		makeSummary("summary-3", tid, "conv-1", "msg-030", "third", base.Add(3*time.Minute)),
	}
	for _, summary := range summaries {
		if err := p.SaveSummary(ctx, tid, summary); err != nil {
			t.Fatalf("SaveSummary(%s): %v", summary.ID, err)
		}
	}

	got, err := p.ListSummaries(ctx, tid, "conv-1", 0, 10)
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(got))
	}
	if got[0].ID != "summary-1" || got[1].ID != "summary-2" || got[2].ID != "summary-3" {
		t.Fatalf("unexpected summary order: %#v", got)
	}
}

func TestSummaryListAppliesOffsetAndLimit(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("tenant-1")
	seedTestData(t, p, tid, entity.UserID("user-1"), entity.AgentID("agent-1"))

	base := time.Now().UTC().Truncate(time.Second)
	for i := 1; i <= 4; i++ {
		summary := makeSummary(
			"summary-"+string(rune('0'+i)),
			tid,
			"conv-1",
			entity.MessageID("msg-0"+string(rune('0'+i))),
			"summary",
			base.Add(time.Duration(i)*time.Minute),
		)
		if err := p.SaveSummary(ctx, tid, summary); err != nil {
			t.Fatalf("SaveSummary(%d): %v", i, err)
		}
	}

	got, err := p.ListSummaries(ctx, tid, "conv-1", 1, 2)
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(got))
	}
	if got[0].ID != "summary-2" || got[1].ID != "summary-3" {
		t.Fatalf("unexpected paginated summaries: %#v", got)
	}
}

func TestSummaryQueriesAreTenantScoped(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tidA := entity.TenantID("tenant-a")
	tidB := entity.TenantID("tenant-b")
	seedTestData(t, p, tidA, entity.UserID("user-a"), entity.AgentID("agent-a"))
	seedTestData(t, p, tidB, entity.UserID("user-b"), entity.AgentID("agent-b"))

	if err := p.SaveSummary(ctx, tidA, makeSummary("summary-a", tidA, "conv-1", "msg-100", "tenant a", time.Now().UTC().Add(1*time.Minute))); err != nil {
		t.Fatalf("SaveSummary tenant a: %v", err)
	}
	if err := p.SaveSummary(ctx, tidB, makeSummary("summary-b", tidB, "conv-1", "msg-200", "tenant b", time.Now().UTC().Add(2*time.Minute))); err != nil {
		t.Fatalf("SaveSummary tenant b: %v", err)
	}

	gotA, err := p.GetLatestSummary(ctx, tidA, "conv-1")
	if err != nil {
		t.Fatalf("GetLatestSummary tenant a: %v", err)
	}
	if gotA == nil || gotA.ID != "summary-a" {
		t.Fatalf("tenant a latest = %#v, want summary-a", gotA)
	}

	listA, err := p.ListSummaries(ctx, tidA, "conv-1", 0, 10)
	if err != nil {
		t.Fatalf("ListSummaries tenant a: %v", err)
	}
	if len(listA) != 1 || listA[0].ID != "summary-a" {
		t.Fatalf("tenant a list = %#v, want only summary-a", listA)
	}
}

func TestSummaryMigrationCreatesExpectedColumns(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	rows, err := p.db.QueryContext(ctx, `PRAGMA table_info(summaries)`)
	if err != nil {
		t.Fatalf("table_info(summaries): %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}

	want := []string{"id", "tenant_id", "conversation_id", "content", "message_id", "created_at"}
	if len(columns) != len(want) {
		t.Fatalf("columns = %v, want %v", columns, want)
	}
	for i, name := range want {
		if columns[i] != name {
			t.Fatalf("columns[%d] = %q, want %q (all columns: %v)", i, columns[i], name, columns)
		}
	}
}

func makeSummary(id string, tenantID entity.TenantID, convID entity.ConversationID, messageID entity.MessageID, content string, createdAt time.Time) entity.Summary {
	return entity.Summary{
		ID:             id,
		TenantID:       tenantID,
		ConversationID: convID,
		Content:        content,
		MessageID:      messageID,
		CreatedAt:      createdAt,
	}
}
