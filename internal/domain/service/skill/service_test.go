package skill

import (
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestSkillServiceInterfaceCompiles is a compile-time check that the
// SkillService interface has the expected method signatures.
func TestSkillServiceInterfaceCompiles(t *testing.T) {
	var _ func(SkillService) = func(svc SkillService) {
		_, _ = svc.Resolve("name", entity.TenantID("t"), entity.UserID("u"))
		_, _ = svc.List(entity.TenantID("t"), entity.UserID("u"))
		svc.Sync(entity.TenantID("t"))
	}
}
