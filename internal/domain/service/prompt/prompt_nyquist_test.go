package prompt

// Nyquist gap tests for phase 44.2: prompt path rendering.
// These tests fill gaps identified in the Nyquist validation audit.

import (
	"context"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// TestBuildSkillsXML_UserSkillUsesUserHomePath verifies that a user-level skill
// gets a container path of userHome+"/skills/"+name.
func TestBuildSkillsXML_UserSkillUsesUserHomePath(t *testing.T) {
	skills := []entity.Skill{
		{Name: "my-skill", Level: entity.SkillLevelUser},
	}
	got := buildSkillsXML(skills, "/home/alice", "/tenant/acme")

	want := "/home/alice/skills/my-skill"
	if !strings.Contains(got, want) {
		t.Errorf("user skill container path: want %q in output, got:\n%s", want, got)
	}
}

// TestBuildSkillsXML_TenantSkillUsesTenantHomePath verifies that a tenant-level
// skill gets a container path of tenantHome+"/skills/"+name.
func TestBuildSkillsXML_TenantSkillUsesTenantHomePath(t *testing.T) {
	skills := []entity.Skill{
		{Name: "web-search", Level: entity.SkillLevelTenant},
	}
	got := buildSkillsXML(skills, "/home/alice", "/tenant/acme")

	want := "/tenant/acme/skills/web-search"
	if !strings.Contains(got, want) {
		t.Errorf("tenant skill container path: want %q in output, got:\n%s", want, got)
	}
}

// TestBuildSkillsXML_UserSkillDoesNotUseTenantHome verifies that a user-level
// skill does NOT use the tenant home path.
func TestBuildSkillsXML_UserSkillDoesNotUseTenantHome(t *testing.T) {
	skills := []entity.Skill{
		{Name: "my-skill", Level: entity.SkillLevelUser},
	}
	got := buildSkillsXML(skills, "/home/alice", "/tenant/acme")

	notWant := "/tenant/acme/skills/my-skill"
	if strings.Contains(got, notWant) {
		t.Errorf("user skill should not use tenant home path, but found %q in:\n%s", notWant, got)
	}
}

// TestBuildSkillsXML_TenantSkillDoesNotUseUserHome verifies that a tenant-level
// skill does NOT use the user home path.
func TestBuildSkillsXML_TenantSkillDoesNotUseUserHome(t *testing.T) {
	skills := []entity.Skill{
		{Name: "web-search", Level: entity.SkillLevelTenant},
	}
	got := buildSkillsXML(skills, "/home/alice", "/tenant/acme")

	notWant := "/home/alice/skills/web-search"
	if strings.Contains(got, notWant) {
		t.Errorf("tenant skill should not use user home path, but found %q in:\n%s", notWant, got)
	}
}

// TestPathProviderCalledDuringBuildWithSkills verifies that PromptBuilder uses
// the PathProvider to resolve container paths when rendering skills in Build().
// The user home path from PathProvider must appear in the system prompt.
func TestPathProviderCalledDuringBuildWithSkills(t *testing.T) {
	skills := &mockSkillLister{skills: []entity.Skill{
		{Name: "custom-skill", Description: "Custom", Level: entity.SkillLevelUser},
	}}
	paths := &mockPathProvider{
		userHome:   "/container/users/u99",
		tenantHome: "/container/tenants/t99",
		messages:   "/messages",
	}
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, skills, paths)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: "t99"}
	user := &entity.User{ID: "u99"}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, entity.Message{}, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := msgs[0].Content

	// The container path derived from PathProvider.UserHomePath must appear.
	wantPath := "/container/users/u99/skills/custom-skill"
	if !strings.Contains(sys, wantPath) {
		t.Errorf("system prompt missing container path %q derived from PathProvider; got prompt (first 500 chars):\n%.500s", wantPath, sys)
	}
}
