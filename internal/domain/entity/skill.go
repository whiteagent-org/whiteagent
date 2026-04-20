package entity

// ---------------------------------------------------------------------------
// Skill entity
// ---------------------------------------------------------------------------

// SkillLevel identifies the scope at which a skill is defined.
type SkillLevel string

const (
	SkillLevelTenant SkillLevel = "tenant"
	SkillLevelUser   SkillLevel = "user"
)

// Skill represents a named capability that an agent can use.
type Skill struct {
	Name        string     // Unique skill identifier (directory name, used for matching/resolution)
	DisplayName string     // Human-readable name from frontmatter; falls back to Name
	Description string     // Human-readable description from frontmatter
	Level       SkillLevel // Scope: tenant or user
	Path        string     // Absolute filesystem path to skill directory
}
