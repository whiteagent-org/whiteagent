package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// Mock store for testing
// ---------------------------------------------------------------------------

type mockTenantStore struct {
	tenants map[entity.TenantID]*entity.Tenant
}

func (m *mockTenantStore) GetTenant(_ context.Context, id entity.TenantID) (*entity.Tenant, error) {
	t, ok := m.tenants[id]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func newMockStore(tenants map[entity.TenantID]*entity.Tenant) *mockTenantStore {
	return &mockTenantStore{tenants: tenants}
}

// ---------------------------------------------------------------------------
// Helper: create a skill directory with SKILL.md
// ---------------------------------------------------------------------------

func createSkillDir(t *testing.T, root, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md in %s: %v", dir, err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// scanSkillDir tests
// ---------------------------------------------------------------------------

func TestScanSkillDir_ValidSkills(t *testing.T) {
	root := t.TempDir()
	createSkillDir(t, root, "web-search", "---\nname: Web Search\ndescription: Search the web\n---\n# Web Search")
	createSkillDir(t, root, "code-review", "---\nname: Code Review\ndescription: Review code\n---\n# Code Review")

	skills, err := scanSkillDir(root, entity.SkillLevelTenant)
	if err != nil {
		t.Fatalf("scanSkillDir: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	// Skills should be returned (order depends on readdir, check both exist)
	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
		if s.Level != entity.SkillLevelTenant {
			t.Errorf("skill %s: expected level tenant, got %s", s.Name, s.Level)
		}
	}
	if !names["web-search"] || !names["code-review"] {
		t.Errorf("expected web-search and code-review, got %v", names)
	}
}

func TestScanSkillDir_ParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	createSkillDir(t, root, "my-skill", "---\nname: My Skill Display\ndescription: Does things\n---\n# My Skill")

	skills, err := scanSkillDir(root, entity.SkillLevelTenant)
	if err != nil {
		t.Fatalf("scanSkillDir: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.DisplayName != "My Skill Display" {
		t.Errorf("DisplayName = %q, want %q", s.DisplayName, "My Skill Display")
	}
	if s.Description != "Does things" {
		t.Errorf("Description = %q, want %q", s.Description, "Does things")
	}
}

func TestScanSkillDir_FallbackDisplayName(t *testing.T) {
	root := t.TempDir()
	createSkillDir(t, root, "my-skill", "---\ndescription: No name field\n---\n# Content")

	skills, err := scanSkillDir(root, entity.SkillLevelTenant)
	if err != nil {
		t.Fatalf("scanSkillDir: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].DisplayName != "my-skill" {
		t.Errorf("DisplayName = %q, want %q (fallback to dir name)", skills[0].DisplayName, "my-skill")
	}
}

func TestScanSkillDir_SkipsMissingSkillMD(t *testing.T) {
	root := t.TempDir()
	// Create a directory without SKILL.md
	if err := os.MkdirAll(filepath.Join(root, "no-skill"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createSkillDir(t, root, "valid-skill", "---\nname: Valid\n---\n# Valid")

	skills, err := scanSkillDir(root, entity.SkillLevelTenant)
	if err != nil {
		t.Fatalf("scanSkillDir: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (skipping no-skill), got %d", len(skills))
	}
	if skills[0].Name != "valid-skill" {
		t.Errorf("expected valid-skill, got %s", skills[0].Name)
	}
}

func TestScanSkillDir_NonexistentDir(t *testing.T) {
	skills, err := scanSkillDir("/nonexistent/dir", entity.SkillLevelTenant)
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills, got %v", skills)
	}
}

// ---------------------------------------------------------------------------
// syncTenant tests
// ---------------------------------------------------------------------------

func TestSyncTenant_CopiesAllowedGlobalSkills(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: Web Search\n---\n# WS")
	createSkillDir(t, globalDir, "code-review", "---\nname: Code Review\n---\n# CR")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}}, // empty = all
	})

	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	// Both skills should be synced
	for _, name := range []string{"web-search", "code-review"} {
		skillMD := filepath.Join(tenantSkillsDir, name, "SKILL.md")
		if _, err := os.Stat(skillMD); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after sync", skillMD)
		}
		// .state file should exist
		stateFile := filepath.Join(tenantSkillsDir, name, ".state")
		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			t.Errorf("expected .state file for %s", name)
		}
		st, err := readState(filepath.Join(tenantSkillsDir, name))
		if err != nil {
			t.Fatalf("readState %s: %v", name, err)
		}
		if st.Source != "global" {
			t.Errorf("%s: source = %q, want global", name, st.Source)
		}
	}
}

func TestSyncTenant_RespectsAllowedSkills(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: Web Search\n---\n# WS")
	createSkillDir(t, globalDir, "code-review", "---\nname: Code Review\n---\n# CR")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{"web-search"}}, // only web-search
	})

	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	// web-search should exist
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search", "SKILL.md")); os.IsNotExist(err) {
		t.Error("expected web-search to be synced")
	}
	// code-review should NOT exist
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "code-review")); !os.IsNotExist(err) {
		t.Error("expected code-review to NOT be synced")
	}
}

func TestSyncTenant_RemovesDisallowedSyncedSkills(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: Web Search\n---\n# WS")

	// First sync: allow all
	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search", "SKILL.md")); os.IsNotExist(err) {
		t.Fatal("expected web-search after first sync")
	}

	// Second sync: disallow web-search (specify only "other")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, AllowedSkills: []string{"other"}}
	svc.syncTenant(tenantID)

	// web-search should be removed (it has .state source=global)
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search")); !os.IsNotExist(err) {
		t.Error("expected web-search to be removed after disallow")
	}
}

func TestSyncTenant_PreservesTenantDefinedSkills(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	// Create a tenant-defined skill (no .state file)
	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	createSkillDir(t, tenantSkillsDir, "custom-skill", "---\nname: Custom\n---\n# Custom")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{"other"}},
	})
	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	// Tenant-defined skill must NOT be removed
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "custom-skill", "SKILL.md")); os.IsNotExist(err) {
		t.Error("expected tenant-defined skill to be preserved")
	}
}

func TestSyncTenant_UpdatesOnHashChange(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: Web Search\n---\n# Version 1")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	st1, _ := readState(filepath.Join(tenantSkillsDir, "web-search"))
	hash1 := st1.Hash

	// Modify global skill
	os.WriteFile(filepath.Join(globalDir, "web-search", "SKILL.md"), []byte("---\nname: Web Search\n---\n# Version 2"), 0o644)

	svc.syncTenant(tenantID)

	st2, _ := readState(filepath.Join(tenantSkillsDir, "web-search"))
	if st2.Hash == hash1 {
		t.Error("expected hash to change after update")
	}

	// Content should be updated
	content, _ := os.ReadFile(filepath.Join(tenantSkillsDir, "web-search", "SKILL.md"))
	if !strings.Contains(string(content), "Version 2") {
		t.Error("expected updated content after sync")
	}
}

func TestSyncTenant_PreservesSupportingFiles(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: Web Search\n---\n# WS")
	// Add a supporting file
	os.WriteFile(filepath.Join(globalDir, "web-search", "rules.txt"), []byte("rule1"), 0o644)

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	rulesFile := filepath.Join(tenantSkillsDir, "web-search", "rules.txt")
	data, err := os.ReadFile(rulesFile)
	if err != nil {
		t.Fatalf("expected supporting file rules.txt: %v", err)
	}
	if string(data) != "rule1" {
		t.Errorf("rules.txt content = %q, want %q", data, "rule1")
	}
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

func TestList_MergedView(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")
	userID := entity.UserID("u1")

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	userSkillsDir := filepath.Join(dataDir, "users", string(userID), "skills")

	createSkillDir(t, tenantSkillsDir, "web-search", "---\nname: Web Search\n---\n# Tenant WS")
	createSkillDir(t, tenantSkillsDir, "code-review", "---\nname: Code Review\n---\n# Tenant CR")
	createSkillDir(t, userSkillsDir, "web-search", "---\nname: My Web Search\n---\n# User WS")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skills, err := svc.List(tenantID, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (user overrides tenant), got %d", len(skills))
	}

	// Find web-search - should be user level
	for _, s := range skills {
		if strings.EqualFold(s.Name, "web-search") {
			if s.Level != entity.SkillLevelUser {
				t.Errorf("web-search level = %s, want user (user overrides tenant)", s.Level)
			}
			if s.DisplayName != "My Web Search" {
				t.Errorf("web-search DisplayName = %q, want %q", s.DisplayName, "My Web Search")
			}
		}
	}
}

func TestList_CaseInsensitiveDedup(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")
	userID := entity.UserID("u1")

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	userSkillsDir := filepath.Join(dataDir, "users", string(userID), "skills")

	createSkillDir(t, tenantSkillsDir, "Web-Search", "---\nname: Tenant WS\n---\n# Tenant")
	createSkillDir(t, userSkillsDir, "web-search", "---\nname: User WS\n---\n# User")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skills, err := svc.List(tenantID, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (case-insensitive dedup), got %d", len(skills))
	}
	if skills[0].Level != entity.SkillLevelUser {
		t.Errorf("expected user-level skill to win, got %s", skills[0].Level)
	}
}

// ---------------------------------------------------------------------------
// Resolve tests
// ---------------------------------------------------------------------------

func TestResolve_UserOverTenant(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")
	userID := entity.UserID("u1")

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	userSkillsDir := filepath.Join(dataDir, "users", string(userID), "skills")

	createSkillDir(t, tenantSkillsDir, "web-search", "---\nname: Tenant WS\n---\n# Tenant")
	createSkillDir(t, userSkillsDir, "web-search", "---\nname: User WS\n---\n# User")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skill, err := svc.Resolve("web-search", tenantID, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Level != entity.SkillLevelUser {
		t.Errorf("expected user-level skill, got %s", skill.Level)
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")
	userID := entity.UserID("u1")

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	createSkillDir(t, tenantSkillsDir, "Web-Search", "---\nname: WS\n---\n# WS")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skill, err := svc.Resolve("web-search", tenantID, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if skill == nil {
		t.Fatal("expected non-nil skill for case-insensitive match")
	}
}

func TestResolve_NotFound(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")
	userID := entity.UserID("u1")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skill, err := svc.Resolve("nonexistent", tenantID, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if skill != nil {
		t.Errorf("expected nil skill for nonexistent, got %+v", skill)
	}
}

// ---------------------------------------------------------------------------
// EnsureSync tests
// ---------------------------------------------------------------------------

func TestEnsureSync_TTLRespected(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: WS\n---\n# WS")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.ttl = 2 * time.Second // long enough to not expire during test

	// First call should trigger sync
	svc.EnsureSync(tenantID)
	// Wait for background goroutine
	time.Sleep(200 * time.Millisecond)

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search", "SKILL.md")); os.IsNotExist(err) {
		t.Fatal("expected skill to be synced after EnsureSync")
	}

	// Remove the synced skill manually to detect if second call triggers sync
	os.RemoveAll(filepath.Join(tenantSkillsDir, "web-search"))

	// Second call within TTL should NOT trigger sync
	svc.EnsureSync(tenantID)
	time.Sleep(100 * time.Millisecond)

	// Skill should still be missing (no re-sync within TTL)
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("expected skill to NOT be re-synced within TTL")
	}
}

func TestEnsureSync_SkipsIfAlreadySyncing(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t1")

	createSkillDir(t, globalDir, "web-search", "---\nname: WS\n---\n# WS")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.ttl = 0 // always expired

	// Manually set syncing flag
	tc := svc.getCache(tenantID)
	tc.mu.Lock()
	tc.syncing = true
	tc.mu.Unlock()

	// Call EnsureSync -- should skip because syncing is true
	svc.EnsureSync(tenantID)
	time.Sleep(50 * time.Millisecond)

	// Skill should NOT be synced (skipped)
	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	if _, err := os.Stat(filepath.Join(tenantSkillsDir, "web-search")); !os.IsNotExist(err) {
		t.Error("expected sync to be skipped when already syncing")
	}
}
