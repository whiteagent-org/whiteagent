// Package skill defines the skill service interface.
// Implementation is provided in a later phase.
package skill

import "github.com/whiteagent-org/whiteagent/internal/domain/entity"

// SkillService resolves and lists skills available to agents.
type SkillService interface {
	// Resolve returns the best-matching skill for the given name,
	// considering tenant and user overrides.
	Resolve(name string, tenantID entity.TenantID, userID entity.UserID) (*entity.Skill, error)

	// List returns all skills accessible to the given tenant/user.
	// AllowedSkills filtering is applied via sync (tenant directory only
	// contains allowed skills).
	List(tenantID entity.TenantID, userID entity.UserID) ([]entity.Skill, error)

	// Sync synchronizes global skills into the tenant's skill directory.
	Sync(tenantID entity.TenantID)
}
