package skill

// Nyquist gap tests for phase 44.2: skill service path layout.
// These tests fill the gap of explicitly asserting that skill paths do NOT
// contain a spurious "/home/whiteagent/" segment (the pre-fix layout was
// tenants/{id}/home/skills/ but the correct layout is tenants/{id}/skills/).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestSkillSyncPathDoesNotContainHome verifies that syncTenant writes skills to
// tenants/{id}/skills/ and NOT to tenants/{id}/home/skills/.
func TestSkillSyncPathDoesNotContainHome(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t-nyq")

	createSkillDir(t, globalDir, "code-review", "---\nname: Code Review\n---\n# CR")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID, AllowedSkills: []string{}},
	})
	svc := New(globalDir, dataDir, store)
	svc.syncTenant(tenantID)

	// The skill must exist at the correct path (no "home" segment).
	correctPath := filepath.Join(dataDir, "tenants", string(tenantID), "skills", "code-review", "SKILL.md")
	if _, err := os.Stat(correctPath); os.IsNotExist(err) {
		t.Errorf("skill not found at correct path %q (missing or wrong location)", correctPath)
	}

	// The path must not contain "/home/whiteagent/skills/" or "\home\whiteagent\skills\" (any OS).
	if strings.Contains(correctPath, string(filepath.Separator)+"home"+string(filepath.Separator)+"skills") {
		t.Errorf("skill sync path contains spurious /home/whiteagent/ segment: %q", correctPath)
	}

	// Confirm the old wrong path does not exist.
	wrongPath := filepath.Join(dataDir, "tenants", string(tenantID), "home", "skills")
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Errorf("old spurious /home/whiteagent/skills/ directory exists at %q — should not be created", wrongPath)
	}
}

// TestSkillListPathDoesNotContainHome verifies that List() reads skills from
// tenants/{id}/skills/ and users/{id}/skills/ (not from */home/whiteagent/skills/).
func TestSkillListPathDoesNotContainHome(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t-nyq")
	userID := entity.UserID("u-nyq")

	// Place skills at the CORRECT paths.
	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	userSkillsDir := filepath.Join(dataDir, "users", string(userID), "skills")
	createSkillDir(t, tenantSkillsDir, "web-search", "---\nname: WS\n---\n# WS")
	createSkillDir(t, userSkillsDir, "my-tool", "---\nname: My Tool\n---\n# MT")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skills, err := svc.List(tenantID, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills from correct paths, got %d", len(skills))
	}

	// Each skill's Path field must not contain /home/whiteagent/skills/.
	for _, sk := range skills {
		if strings.Contains(sk.Path, string(filepath.Separator)+"home"+string(filepath.Separator)+"skills") {
			t.Errorf("skill %q path contains spurious /home/whiteagent/ segment: %q", sk.Name, sk.Path)
		}
	}
}

// TestSkillResolvePathDoesNotContainHome verifies that Resolve() reads from
// users/{id}/skills/ and tenants/{id}/skills/ (not from */home/whiteagent/skills/).
func TestSkillResolvePathDoesNotContainHome(t *testing.T) {
	dataDir := t.TempDir()
	tenantID := entity.TenantID("t-nyq")
	userID := entity.UserID("u-nyq")

	tenantSkillsDir := filepath.Join(dataDir, "tenants", string(tenantID), "skills")
	createSkillDir(t, tenantSkillsDir, "web-search", "---\nname: WS\n---\n# WS")

	store := newMockStore(map[entity.TenantID]*entity.Tenant{
		tenantID: {ID: tenantID},
	})
	svc := New("", dataDir, store)

	skill, err := svc.Resolve("web-search", tenantID, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if skill == nil {
		t.Fatal("expected skill to be found at tenants/{id}/skills/ path")
	}

	// The resolved skill Path must not contain /home/whiteagent/skills/.
	if strings.Contains(skill.Path, string(filepath.Separator)+"home"+string(filepath.Separator)+"skills") {
		t.Errorf("resolved skill path contains spurious /home/whiteagent/ segment: %q", skill.Path)
	}
}
