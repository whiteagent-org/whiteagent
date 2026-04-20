package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func TestTenantMappingCRUD(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	mapping := entity.TenantMapping{
		ChannelID:        "channel.telegram",
		ExternalTenantID: "bot123",
		TenantID:         tid,
	}

	// SaveTenantMapping inserts.
	if err := p.SaveTenantMapping(ctx, mapping); err != nil {
		t.Fatalf("SaveTenantMapping: %v", err)
	}

	// GetTenantByMapping retrieves.
	got, err := p.GetTenantByMapping(ctx, "channel.telegram", "bot123")
	if err != nil {
		t.Fatalf("GetTenantByMapping: %v", err)
	}
	if got != tid {
		t.Fatalf("GetTenantByMapping = %q, want %q", got, tid)
	}

	// GetTenantByMapping returns empty for missing mapping (no error).
	missing, err := p.GetTenantByMapping(ctx, "channel.teams", "workspace999")
	if err != nil {
		t.Fatalf("GetTenantByMapping missing: %v", err)
	}
	if missing != "" {
		t.Fatalf("expected empty TenantID for missing mapping, got %q", missing)
	}

	// SaveTenantMapping upserts (updates tenant).
	tid2 := entity.TenantID("t2")
	seedTestData(t, p, tid2, entity.UserID("u2"), entity.AgentID("a2"))
	mapping.TenantID = tid2
	if err := p.SaveTenantMapping(ctx, mapping); err != nil {
		t.Fatalf("SaveTenantMapping upsert: %v", err)
	}
	updated, _ := p.GetTenantByMapping(ctx, "channel.telegram", "bot123")
	if updated != tid2 {
		t.Fatalf("after upsert: got %q, want %q", updated, tid2)
	}

	// DeleteTenantMapping removes the mapping.
	if err := p.DeleteTenantMapping(ctx, "channel.telegram", "bot123"); err != nil {
		t.Fatalf("DeleteTenantMapping: %v", err)
	}
	deleted, _ := p.GetTenantByMapping(ctx, "channel.telegram", "bot123")
	if deleted != "" {
		t.Fatalf("expected empty after delete, got %q", deleted)
	}
}

func TestListTenantMappings(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	tid1 := entity.TenantID("t1")
	tid2 := entity.TenantID("t2")
	seedTestData(t, p, tid1, entity.UserID("u1"), entity.AgentID("a1"))
	seedTestData(t, p, tid2, entity.UserID("u2"), entity.AgentID("a2"))

	// Create mappings for two tenants.
	m1 := entity.TenantMapping{ChannelID: "channel.telegram", ExternalTenantID: "bot1", TenantID: tid1}
	m2 := entity.TenantMapping{ChannelID: "channel.teams", ExternalTenantID: "team1", TenantID: tid1}
	m3 := entity.TenantMapping{ChannelID: "channel.telegram", ExternalTenantID: "bot2", TenantID: tid2}

	for _, m := range []entity.TenantMapping{m1, m2, m3} {
		if err := p.SaveTenantMapping(ctx, m); err != nil {
			t.Fatalf("SaveTenantMapping: %v", err)
		}
	}

	// List all (empty tenant filter).
	all, err := p.ListTenantMappings(ctx, "")
	if err != nil {
		t.Fatalf("ListTenantMappings all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListTenantMappings all: got %d, want 3", len(all))
	}

	// List filtered by tenant.
	filtered, err := p.ListTenantMappings(ctx, tid1)
	if err != nil {
		t.Fatalf("ListTenantMappings filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("ListTenantMappings filtered: got %d, want 2", len(filtered))
	}
	for _, m := range filtered {
		if m.TenantID != tid1 {
			t.Fatalf("ListTenantMappings filtered: got tenant %q, want %q", m.TenantID, tid1)
		}
	}

	// List for tenant with one mapping.
	filtered2, err := p.ListTenantMappings(ctx, tid2)
	if err != nil {
		t.Fatalf("ListTenantMappings tid2: %v", err)
	}
	if len(filtered2) != 1 {
		t.Fatalf("ListTenantMappings tid2: got %d, want 1", len(filtered2))
	}
}

func TestMergeUser(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	now := time.Now().UTC().Truncate(time.Second)

	// Seed tenant and agent.
	if err := p.SaveTenant(ctx, tid, entity.Tenant{Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("SaveTenant: %v", err)
	}
	aid := entity.AgentID("a1")
	if err := p.SaveAgent(ctx, tid, entity.Agent{ID: aid, Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	// Create source and target users with channel identities.
	sourceUID := entity.UserID("source-user")
	targetUID := entity.UserID("target-user")

	sourceUser := entity.User{
		ID:        sourceUID,
		Name:      "Source",
		CreatedAt: now,
	}
	targetUser := entity.User{
		ID:        targetUID,
		Name:      "Target",
		CreatedAt: now,
	}
	if err := p.SaveUser(ctx, tid, sourceUser); err != nil {
		t.Fatalf("SaveUser source: %v", err)
	}
	if err := p.SaveUser(ctx, tid, targetUser); err != nil {
		t.Fatalf("SaveUser target: %v", err)
	}
	if err := p.AddUserIdentity(ctx, tid, "channel.telegram", "tg-source", sourceUID); err != nil {
		t.Fatalf("AddUserIdentity source: %v", err)
	}
	if err := p.AddUserIdentity(ctx, tid, "channel.teams", "teams-target", targetUID); err != nil {
		t.Fatalf("AddUserIdentity target: %v", err)
	}

	// Create a message from the source user.
	msg := entity.Message{
		ID:             entity.MessageID("msg1"),
		TenantID:       tid,
		UserID:         sourceUID,
		AgentID:        aid,
		ConversationID: entity.ConversationID("conv1"),
		ChatID:         "chat1",
		Role:           entity.RoleUser,
		Content:        "hello from source",
		CreatedAt:      now,
	}
	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// Create a journal entry from the source user.
	journal := entity.JournalEntry{
		TenantID:       tid,
		UserID:         sourceUID,
		ConversationID: entity.ConversationID("conv1"),
		Category:       "Key Events",
		Content:        "source journal entry",
		CreatedAt:      now,
	}
	if err := p.AppendJournal(ctx, tid, journal); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	// Merge source into target.
	if err := p.MergeUser(ctx, tid, sourceUID, targetUID); err != nil {
		t.Fatalf("MergeUser: %v", err)
	}

	// Source user should be deleted.
	srcAfter, err := p.GetUser(ctx, tid, sourceUID)
	if err != nil {
		t.Fatalf("GetUser source after merge: %v", err)
	}
	if srcAfter != nil {
		t.Fatal("expected source user to be deleted after merge")
	}

	// Target user should still exist.
	tgtAfter, err := p.GetUser(ctx, tid, targetUID)
	if err != nil {
		t.Fatalf("GetUser target after merge: %v", err)
	}
	if tgtAfter == nil {
		t.Fatal("target user not found after merge")
	}

	// Verify channel identities were merged.
	tgExt, err := p.GetExternalID(ctx, tid, targetUID, "channel.telegram")
	if err != nil {
		t.Fatalf("GetExternalID telegram: %v", err)
	}
	if tgExt != "tg-source" {
		t.Fatalf("target missing source's telegram channel, got %q", tgExt)
	}
	teamsExt, err := p.GetExternalID(ctx, tid, targetUID, "channel.teams")
	if err != nil {
		t.Fatalf("GetExternalID teams: %v", err)
	}
	if teamsExt != "teams-target" {
		t.Fatalf("target missing own teams channel, got %q", teamsExt)
	}

	// Message should now point to target user.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: entity.ConversationID("conv1")})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetMessages: got %d, want 1", len(msgs))
	}
	if msgs[0].UserID != targetUID {
		t.Fatalf("message UserID = %q, want %q", msgs[0].UserID, targetUID)
	}

	// Journal entry should point to target user.
	entries, err := p.GetJournal(ctx, tid, entity.JournalFilter{UserID: targetUID})
	if err != nil {
		t.Fatalf("GetJournal: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected journal entries for target user after merge")
	}
}

func TestMergeUser_MissingUser(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	now := time.Now().UTC().Truncate(time.Second)

	if err := p.SaveTenant(ctx, tid, entity.Tenant{Name: "test", CreatedAt: now}); err != nil {
		t.Fatalf("SaveTenant: %v", err)
	}

	existingUID := entity.UserID("existing")
	if err := p.SaveUser(ctx, tid, entity.User{ID: existingUID, Name: "Existing", CreatedAt: now}); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}

	// Source doesn't exist.
	if err := p.MergeUser(ctx, tid, entity.UserID("nonexistent"), existingUID); err == nil {
		t.Fatal("expected error when source user doesn't exist")
	}

	// Target doesn't exist.
	if err := p.MergeUser(ctx, tid, existingUID, entity.UserID("nonexistent")); err == nil {
		t.Fatal("expected error when target user doesn't exist")
	}
}
