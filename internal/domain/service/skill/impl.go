package skill

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/yaml"
)

// tenantStore is the minimal store interface needed by the skill service.
type tenantStore interface {
	GetTenant(ctx context.Context, id entity.TenantID) (*entity.Tenant, error)
}

// tenantCache tracks per-tenant sync state.
type tenantCache struct {
	mu       sync.Mutex
	lastSync time.Time
	syncing  bool
}

// Service implements SkillService: filesystem scanning, global-to-tenant sync
// with .state tracking, 2-level resolution (user > tenant), and TTL-based
// stale-while-revalidate caching.
type Service struct {
	globalDir string
	dataDir   string
	store     tenantStore
	ttl       time.Duration

	mu      sync.Mutex
	tenants map[entity.TenantID]*tenantCache
}

// New creates a new skill service.
func New(globalDir, dataDir string, store tenantStore) *Service {
	absGlobal, err := filepath.Abs(globalDir)
	if err == nil {
		globalDir = absGlobal
	}
	absData, err := filepath.Abs(dataDir)
	if err == nil {
		dataDir = absData
	}
	return &Service{
		globalDir: globalDir,
		dataDir:   dataDir,
		store:     store,
		ttl:       time.Hour,
		tenants:   make(map[entity.TenantID]*tenantCache),
	}
}

// getCache returns the tenant cache entry, creating it if needed.
func (s *Service) getCache(tenantID entity.TenantID) *tenantCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc, ok := s.tenants[tenantID]
	if !ok {
		tc = &tenantCache{}
		s.tenants[tenantID] = tc
	}
	return tc
}

// EnsureSync triggers a background sync for the tenant if the TTL has expired
// and no sync is already running. Non-blocking.
func (s *Service) EnsureSync(tenantID entity.TenantID) {
	tc := s.getCache(tenantID)
	tc.mu.Lock()
	if tc.syncing || time.Since(tc.lastSync) < s.ttl {
		tc.mu.Unlock()
		return
	}
	tc.syncing = true
	tc.mu.Unlock()

	go func() {
		s.syncTenant(tenantID)
		tc.mu.Lock()
		tc.lastSync = time.Now()
		tc.syncing = false
		tc.mu.Unlock()
	}()
}

// Sync synchronizes global skills into the tenant's skill directory (blocking).
func (s *Service) Sync(tenantID entity.TenantID) {
	s.syncTenant(tenantID)
}

// syncTenant performs the actual sync: copies allowed global skills to the
// tenant directory, removes disallowed synced skills, and updates .state files.
func (s *Service) syncTenant(tenantID entity.TenantID) {
	tenant, err := s.store.GetTenant(context.Background(), tenantID)
	if err != nil {
		slog.Warn("skill.sync.tenant_load", "err", err, "tenant_id", tenantID)
		return
	}
	if tenant == nil {
		slog.Warn("skill.sync.tenant_not_found", "tenant_id", tenantID)
		return
	}

	globalSkills, err := scanSkillDir(s.globalDir, entity.SkillLevelTenant)
	if err != nil {
		slog.Warn("skill.sync.scan_global", "err", err)
		return
	}

	// Build allowed set. Empty = all allowed.
	allowAll := len(tenant.AllowedSkills) == 0
	allowed := make(map[string]bool, len(tenant.AllowedSkills))
	for _, name := range tenant.AllowedSkills {
		allowed[strings.ToLower(name)] = true
	}

	tenantSkillsDir := filepath.Join(s.dataDir, "tenants", string(tenantID), "skills")
	if err := os.MkdirAll(tenantSkillsDir, 0o755); err != nil {
		slog.Warn("skill.sync.mkdir", "err", err, "dir", tenantSkillsDir)
		return
	}

	// Track which global skill names were synced (for cleanup pass).
	syncedNames := make(map[string]bool)

	for _, gs := range globalSkills {
		if !allowAll && !allowed[strings.ToLower(gs.Name)] {
			continue
		}
		syncedNames[gs.Name] = true

		dstDir := filepath.Join(tenantSkillsDir, gs.Name)
		globalHash := hashFile(filepath.Join(gs.Path, "SKILL.md"))

		st, _ := readState(dstDir)
		if st != nil {
			// Already synced from global. Check hash.
			if st.Source == "global" && st.Hash == globalHash {
				continue // up to date
			}
			if st.Source == "global" {
				// Hash changed -- remove old, copy fresh.
				if err := os.RemoveAll(dstDir); err != nil {
					slog.Warn("skill.sync.remove_old", "err", err, "dir", dstDir)
					continue
				}
			} else {
				continue // not our skill (tenant-owned with .state but different source)
			}
		} else {
			// Check if dir exists without .state -> tenant-owned, skip.
			if _, statErr := os.Stat(dstDir); statErr == nil {
				continue
			}
		}

		if err := copyDir(gs.Path, dstDir); err != nil {
			slog.Warn("skill.sync.copy", "err", err, "skill", gs.Name)
			continue
		}
		if err := writeState(dstDir, SkillState{
			Source:   "global",
			Hash:     globalHash,
			SyncedAt: time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			slog.Warn("skill.sync.write_state", "err", err, "skill", gs.Name)
		}
	}

	// Cleanup pass: remove synced skills that are no longer allowed.
	entries, err := os.ReadDir(tenantSkillsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if syncedNames[name] {
			continue // still allowed
		}
		st, _ := readState(filepath.Join(tenantSkillsDir, name))
		if st != nil && st.Source == "global" {
			// Synced from global but no longer allowed -- remove.
			if err := os.RemoveAll(filepath.Join(tenantSkillsDir, name)); err != nil {
				slog.Warn("skill.sync.cleanup", "err", err, "skill", name)
			}
		}
		// If no .state or source != global -> tenant-defined, leave alone.
	}
}

// List returns all skills accessible to the given tenant/user, with user
// skills overriding tenant skills of the same name (case-insensitive).
// Results are sorted by name.
func (s *Service) List(tenantID entity.TenantID, userID entity.UserID) ([]entity.Skill, error) {
	tenantSkillsDir := filepath.Join(s.dataDir, "tenants", string(tenantID), "skills")
	userSkillsDir := filepath.Join(s.dataDir, "users", string(userID), "skills")

	tenantSkills, err := scanSkillDir(tenantSkillsDir, entity.SkillLevelTenant)
	if err != nil {
		return nil, fmt.Errorf("scan tenant skills: %w", err)
	}
	userSkills, err := scanSkillDir(userSkillsDir, entity.SkillLevelUser)
	if err != nil {
		return nil, fmt.Errorf("scan user skills: %w", err)
	}

	merged := make(map[string]entity.Skill, len(tenantSkills)+len(userSkills))
	for _, s := range tenantSkills {
		merged[strings.ToLower(s.Name)] = s
	}
	for _, s := range userSkills {
		merged[strings.ToLower(s.Name)] = s // user overrides tenant
	}

	result := make([]entity.Skill, 0, len(merged))
	for _, s := range merged {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

// Resolve returns the best-matching skill for the given name, checking user
// skills first (case-insensitive). Returns (nil, nil) if not found.
func (s *Service) Resolve(name string, tenantID entity.TenantID, userID entity.UserID) (*entity.Skill, error) {
	lowerName := strings.ToLower(name)

	// Check user skills first.
	userSkillsDir := filepath.Join(s.dataDir, "users", string(userID), "skills")
	userSkills, err := scanSkillDir(userSkillsDir, entity.SkillLevelUser)
	if err != nil {
		return nil, fmt.Errorf("scan user skills: %w", err)
	}
	for _, sk := range userSkills {
		if strings.ToLower(sk.Name) == lowerName {
			return &sk, nil
		}
	}

	// Check tenant skills.
	tenantSkillsDir := filepath.Join(s.dataDir, "tenants", string(tenantID), "skills")
	tenantSkills, err := scanSkillDir(tenantSkillsDir, entity.SkillLevelTenant)
	if err != nil {
		return nil, fmt.Errorf("scan tenant skills: %w", err)
	}
	for _, sk := range tenantSkills {
		if strings.ToLower(sk.Name) == lowerName {
			return &sk, nil
		}
	}

	return nil, nil
}

// scanSkillDir scans root for immediate subdirectories containing SKILL.md.
// Returns (nil, nil) for nonexistent root. Skips dirs without SKILL.md or
// with invalid names.
func scanSkillDir(root string, level entity.SkillLevel) ([]entity.Skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skill dir %s: %w", root, err)
	}

	var skills []entity.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Validate directory name.
		if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, string(filepath.Separator)) {
			slog.Warn("skill.scan.invalid_name", "name", name, "dir", root)
			continue
		}

		skillMDPath := filepath.Join(root, name, "SKILL.md")
		content, err := os.ReadFile(skillMDPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // skip dirs without SKILL.md
			}
			slog.Warn("skill.scan.read", "err", err, "path", skillMDPath)
			continue
		}

		displayName, description, parseErr := yaml.ParseFrontmatter(content)
		if parseErr != nil {
			slog.Warn("skill.scan.parse", "err", parseErr, "path", skillMDPath)
			continue
		}
		if displayName == "" {
			displayName = name
		}

		skills = append(skills, entity.Skill{
			Name:        name,
			DisplayName: displayName,
			Description: description,
			Level:       level,
			Path:        filepath.Join(root, name),
		})
	}
	return skills, nil
}

// hashFile computes sha256 hex digest of a file's content. Returns empty
// string on error.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// copyDir copies all files and subdirectories from src to dst recursively.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
