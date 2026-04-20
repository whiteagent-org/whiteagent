package entity

import "testing"

func TestSkillLevelConstants(t *testing.T) {
	tests := []struct {
		level SkillLevel
		want  string
	}{
		{SkillLevelTenant, "tenant"},
		{SkillLevelUser, "user"},
	}
	for _, tt := range tests {
		if string(tt.level) != tt.want {
			t.Errorf("SkillLevel %q != %q", tt.level, tt.want)
		}
	}
}

func TestSkillStructFields(t *testing.T) {
	s := Skill{
		Name:        "test-skill",
		DisplayName: "Test Skill",
		Description: "A test skill",
		Level:       SkillLevelTenant,
		Path:        "/path/to/skill",
	}
	if s.Name != "test-skill" {
		t.Errorf("Name = %q, want test-skill", s.Name)
	}
	if s.DisplayName != "Test Skill" {
		t.Errorf("DisplayName = %q, want Test Skill", s.DisplayName)
	}
	if s.Description != "A test skill" {
		t.Errorf("Description = %q", s.Description)
	}
	if s.Level != SkillLevelTenant {
		t.Errorf("Level = %q, want tenant", s.Level)
	}
	if s.Path != "/path/to/skill" {
		t.Errorf("Path = %q", s.Path)
	}
}
