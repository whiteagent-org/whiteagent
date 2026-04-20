package sqlite

import (
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestBuildJournalQuery verifies that ConversationID and BeforeTime filter fields
// produce the correct SQL WHERE clauses and positional args in buildJournalQuery.
func TestBuildJournalQuery(t *testing.T) {
	tenantID := "tenant-abc"

	t.Run("ConversationID_adds_WHERE_clause_and_arg", func(t *testing.T) {
		filter := entity.JournalFilter{
			ConversationID: entity.ConversationID("conv-xyz"),
		}
		q, args := buildJournalQuery(tenantID, filter)

		if !strings.Contains(q, "conversation_id = ?") {
			t.Errorf("expected SQL to contain 'conversation_id = ?', got:\n%s", q)
		}
		if !containsArg(args, "conv-xyz") {
			t.Errorf("expected args to contain 'conv-xyz', got: %v", args)
		}
	})

	t.Run("BeforeTime_adds_WHERE_clause_and_arg", func(t *testing.T) {
		before := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
		filter := entity.JournalFilter{
			BeforeTime: before,
		}
		q, args := buildJournalQuery(tenantID, filter)

		if !strings.Contains(q, "created_at < ?") {
			t.Errorf("expected SQL to contain 'created_at < ?', got:\n%s", q)
		}
		want := before.Format(time.RFC3339)
		if !containsArg(args, want) {
			t.Errorf("expected args to contain %q, got: %v", want, args)
		}
	})

	t.Run("ConversationID_and_BeforeTime_combined_produce_both_clauses", func(t *testing.T) {
		before := time.Date(2026, 3, 16, 15, 30, 0, 0, time.UTC)
		filter := entity.JournalFilter{
			ConversationID: entity.ConversationID("conv-combined"),
			BeforeTime:     before,
		}
		q, args := buildJournalQuery(tenantID, filter)

		if !strings.Contains(q, "conversation_id = ?") {
			t.Errorf("expected SQL to contain 'conversation_id = ?', got:\n%s", q)
		}
		if !strings.Contains(q, "created_at < ?") {
			t.Errorf("expected SQL to contain 'created_at < ?', got:\n%s", q)
		}
		if !containsArg(args, "conv-combined") {
			t.Errorf("expected args to contain 'conv-combined', got: %v", args)
		}
		if !containsArg(args, before.Format(time.RFC3339)) {
			t.Errorf("expected args to contain %q, got: %v", before.Format(time.RFC3339), args)
		}
		// tenant_id is always the first arg
		if len(args) < 1 || args[0] != tenantID {
			t.Errorf("expected first arg to be tenantID %q, got: %v", tenantID, args)
		}
	})

	t.Run("no_filters_omits_conversation_and_before_clauses", func(t *testing.T) {
		filter := entity.JournalFilter{}
		q, args := buildJournalQuery(tenantID, filter)

		if strings.Contains(q, "conversation_id = ?") {
			t.Errorf("expected no 'conversation_id = ?' filter when ConversationID is empty, got:\n%s", q)
		}
		if strings.Contains(q, "created_at <") {
			t.Errorf("expected no 'created_at <' clause when BeforeTime is zero, got:\n%s", q)
		}
		// Only the tenant_id arg should be present
		if len(args) != 1 {
			t.Errorf("expected 1 arg (tenantID only), got %d: %v", len(args), args)
		}
	})

	t.Run("result_ordered_ASC_by_created_at", func(t *testing.T) {
		filter := entity.JournalFilter{}
		q, _ := buildJournalQuery(tenantID, filter)

		if !strings.Contains(q, "ORDER BY created_at ASC") {
			t.Errorf("expected ORDER BY created_at ASC in query, got:\n%s", q)
		}
	})
}

// containsArg reports whether args contains v (as a string comparison).
func containsArg(args []any, v string) bool {
	for _, a := range args {
		if s, ok := a.(string); ok && s == v {
			return true
		}
	}
	return false
}
