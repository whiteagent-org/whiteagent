package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestGroupModeRoundTrip verifies that GroupMode is persisted and retrieved
// correctly via SaveTenant and GetTenant (MSG-01).
func TestGroupModeRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Save a tenant with GroupMode="all".
	tid := entity.TenantID("gm-all-tenant")
	if err := p.SaveTenant(ctx, tid, entity.Tenant{
		ID:        tid,
		Name:      "All-Mode Tenant",
		GroupMode: "all",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTenant (all): %v", err)
	}

	got, err := p.GetTenant(ctx, tid)
	if err != nil {
		t.Fatalf("GetTenant (all): %v", err)
	}
	if got == nil {
		t.Fatal("GetTenant returned nil for existing tenant")
	}
	if got.GroupMode != "all" {
		t.Errorf("GroupMode: got %q, want %q", got.GroupMode, "all")
	}

	// Save a tenant with GroupMode="mention_only" (explicit default value).
	tid2 := entity.TenantID("gm-mention-tenant")
	if err := p.SaveTenant(ctx, tid2, entity.Tenant{
		ID:        tid2,
		Name:      "Mention-Only Tenant",
		GroupMode: "mention_only",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTenant (mention_only): %v", err)
	}

	got2, err := p.GetTenant(ctx, tid2)
	if err != nil {
		t.Fatalf("GetTenant (mention_only): %v", err)
	}
	if got2 == nil {
		t.Fatal("GetTenant returned nil for existing tenant")
	}
	if got2.GroupMode != "mention_only" {
		t.Errorf("GroupMode: got %q, want %q", got2.GroupMode, "mention_only")
	}
}

// TestGroupModeListTenants verifies that GroupMode is included in ListTenants results (MSG-01).
func TestGroupModeListTenants(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	tid := entity.TenantID("gm-list-tenant")
	if err := p.SaveTenant(ctx, tid, entity.Tenant{
		ID:        tid,
		Name:      "List Test Tenant",
		GroupMode: "all",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTenant: %v", err)
	}

	tenants, err := p.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}

	var found *entity.Tenant
	for i := range tenants {
		if tenants[i].ID == tid {
			found = &tenants[i]
			break
		}
	}
	if found == nil {
		t.Fatal("tenant not found in ListTenants result")
	}
	if found.GroupMode != "all" {
		t.Errorf("GroupMode via ListTenants: got %q, want %q", found.GroupMode, "all")
	}
}

// TestGroupModeUpdate verifies that GroupMode is updated on SaveTenant upsert (MSG-01).
func TestGroupModeUpdate(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	tid := entity.TenantID("gm-update-tenant")
	// Insert with mention_only.
	if err := p.SaveTenant(ctx, tid, entity.Tenant{
		ID:        tid,
		Name:      "Update Test",
		GroupMode: "mention_only",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTenant (initial): %v", err)
	}

	// Update to all.
	if err := p.SaveTenant(ctx, tid, entity.Tenant{
		ID:        tid,
		Name:      "Update Test",
		GroupMode: "all",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveTenant (update): %v", err)
	}

	got, err := p.GetTenant(ctx, tid)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.GroupMode != "all" {
		t.Errorf("GroupMode after update: got %q, want %q", got.GroupMode, "all")
	}
}
