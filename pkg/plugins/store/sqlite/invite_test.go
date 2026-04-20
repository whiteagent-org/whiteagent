package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestInviteCodeCRUD(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	now := time.Now().UTC().Truncate(time.Second)

	// Save a user-type invite code.
	code := entity.InviteCode{
		Code:      "AAAA-BBBB",
		Type:      "user",
		TenantID:  tid,
		CreatedAt: now,
	}
	if err := p.SaveInviteCode(ctx, code); err != nil {
		t.Fatalf("SaveInviteCode: %v", err)
	}

	// GetInviteCode retrieves it.
	got, err := p.GetInviteCode(ctx, "AAAA-BBBB")
	if err != nil {
		t.Fatalf("GetInviteCode: %v", err)
	}
	if got == nil {
		t.Fatal("GetInviteCode returned nil")
	}
	if got.Type != "user" {
		t.Fatalf("Type = %q, want %q", got.Type, "user")
	}
	if got.TenantID != tid {
		t.Fatalf("TenantID = %q, want %q", got.TenantID, tid)
	}

	// GetInviteCode returns nil for missing code.
	missing, err := p.GetInviteCode(ctx, "ZZZZ-YYYY")
	if err != nil {
		t.Fatalf("GetInviteCode missing: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing code")
	}

	// Save a tenant-creation code (no tenant ID).
	tenantCode := entity.InviteCode{
		Code:      "CCCC-DDDD",
		Type:      "tenant",
		CreatedAt: now,
	}
	if err := p.SaveInviteCode(ctx, tenantCode); err != nil {
		t.Fatalf("SaveInviteCode tenant: %v", err)
	}

	// ListInviteCodes with no filter returns all.
	all, err := p.ListInviteCodes(ctx, entity.InviteCodeFilter{})
	if err != nil {
		t.Fatalf("ListInviteCodes all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListInviteCodes all: got %d, want 2", len(all))
	}

	// ListInviteCodes filtered by type.
	tenantOnly, err := p.ListInviteCodes(ctx, entity.InviteCodeFilter{Type: "tenant"})
	if err != nil {
		t.Fatalf("ListInviteCodes tenant: %v", err)
	}
	if len(tenantOnly) != 1 {
		t.Fatalf("ListInviteCodes tenant: got %d, want 1", len(tenantOnly))
	}
	if tenantOnly[0].Code != "CCCC-DDDD" {
		t.Fatalf("ListInviteCodes tenant: got %q, want %q", tenantOnly[0].Code, "CCCC-DDDD")
	}

	// ListInviteCodes filtered by tenant ID.
	byTenant, err := p.ListInviteCodes(ctx, entity.InviteCodeFilter{TenantID: tid})
	if err != nil {
		t.Fatalf("ListInviteCodes byTenant: %v", err)
	}
	if len(byTenant) != 1 {
		t.Fatalf("ListInviteCodes byTenant: got %d, want 1", len(byTenant))
	}

	// RevokeInviteCode by code only (no tenant ID param).
	if err := p.RevokeInviteCode(ctx, "AAAA-BBBB"); err != nil {
		t.Fatalf("RevokeInviteCode: %v", err)
	}
	revoked, _ := p.GetInviteCode(ctx, "AAAA-BBBB")
	if revoked.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set after revoke")
	}

	// UseInviteCode atomically marks as used.
	uid2 := entity.UserID("u2")
	if err := p.UseInviteCode(ctx, "CCCC-DDDD", uid2); err != nil {
		t.Fatalf("UseInviteCode: %v", err)
	}
	used, _ := p.GetInviteCode(ctx, "CCCC-DDDD")
	if used.UsedBy != uid2 {
		t.Fatalf("UsedBy = %q, want %q", used.UsedBy, uid2)
	}

	// UseInviteCode fails on already-used code.
	if err := p.UseInviteCode(ctx, "CCCC-DDDD", entity.UserID("u3")); err == nil {
		t.Fatal("expected error using already-used code")
	}

	// UseInviteCode fails on revoked code.
	if err := p.UseInviteCode(ctx, "AAAA-BBBB", entity.UserID("u3")); err == nil {
		t.Fatal("expected error using revoked code")
	}
}

func TestInviteCodeTargetIDRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	now := time.Now().UTC().Truncate(time.Second)

	// Save code with non-empty TargetID.
	code := entity.InviteCode{
		Code:      "TARG-0001",
		Type:      "user",
		TenantID:  tid,
		TargetID:  "some-user-id",
		CreatedAt: now,
	}
	if err := p.SaveInviteCode(ctx, code); err != nil {
		t.Fatalf("SaveInviteCode with TargetID: %v", err)
	}
	got, err := p.GetInviteCode(ctx, "TARG-0001")
	if err != nil {
		t.Fatalf("GetInviteCode: %v", err)
	}
	if got.TargetID != "some-user-id" {
		t.Fatalf("TargetID = %q, want %q", got.TargetID, "some-user-id")
	}

	// Save code with empty TargetID.
	code2 := entity.InviteCode{
		Code:      "TARG-0002",
		Type:      "user",
		TenantID:  tid,
		CreatedAt: now,
	}
	if err := p.SaveInviteCode(ctx, code2); err != nil {
		t.Fatalf("SaveInviteCode empty TargetID: %v", err)
	}
	got2, err := p.GetInviteCode(ctx, "TARG-0002")
	if err != nil {
		t.Fatalf("GetInviteCode: %v", err)
	}
	if got2.TargetID != "" {
		t.Fatalf("TargetID = %q, want empty", got2.TargetID)
	}

	// ListInviteCodes returns correct TargetID values.
	all, err := p.ListInviteCodes(ctx, entity.InviteCodeFilter{TenantID: tid})
	if err != nil {
		t.Fatalf("ListInviteCodes: %v", err)
	}
	targetIDs := make(map[string]string) // code -> targetID
	for _, ic := range all {
		targetIDs[ic.Code] = ic.TargetID
	}
	if targetIDs["TARG-0001"] != "some-user-id" {
		t.Fatalf("ListInviteCodes TARG-0001 TargetID = %q, want %q", targetIDs["TARG-0001"], "some-user-id")
	}
	if targetIDs["TARG-0002"] != "" {
		t.Fatalf("ListInviteCodes TARG-0002 TargetID = %q, want empty", targetIDs["TARG-0002"])
	}
}
