package identity

import (
	"context"
	"log/slog"
	"sync"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// userGetter is a narrow interface for looking up users by ID.
// port.StorePlugin satisfies this interface.
type userGetter interface {
	GetUser(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error)
}

// UserResolver resolves internal UserIDs to User entities.
// Results are cached in memory to avoid repeated store lookups.
type UserResolver struct {
	store userGetter
	mu    sync.RWMutex
	cache map[string]*entity.User // "tenantID:userID" → user
}

// NewUserResolver creates a new UserResolver backed by the given store.
func NewUserResolver(store userGetter) *UserResolver {
	return &UserResolver{
		store: store,
		cache: make(map[string]*entity.User),
	}
}

// cacheKey builds a composite key for the user cache.
func cacheKey(tenantID entity.TenantID, userID entity.UserID) string {
	return string(tenantID) + ":" + string(userID)
}

// ResolveUsers takes a tenantID and a slice of UserIDs, returns map[UserID]*entity.User.
// Deduplicates, caches, and skips empty IDs. Errors on individual lookups are non-fatal.
func (r *UserResolver) ResolveUsers(ctx context.Context, tenantID entity.TenantID, userIDs []entity.UserID) map[entity.UserID]*entity.User {
	result := make(map[entity.UserID]*entity.User, len(userIDs))

	// Deduplicate and filter empty IDs.
	toFetch := make([]entity.UserID, 0, len(userIDs))
	seen := make(map[entity.UserID]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid.IsEmpty() {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}

		// Check cache.
		key := cacheKey(tenantID, uid)
		r.mu.RLock()
		cached, ok := r.cache[key]
		r.mu.RUnlock()
		if ok {
			result[uid] = cached
			continue
		}
		toFetch = append(toFetch, uid)
	}

	// Fetch uncached users from store.
	for _, uid := range toFetch {
		user, err := r.store.GetUser(ctx, tenantID, uid)
		if err != nil {
			slog.Warn("user_resolver.lookup_error", "tenant_id", tenantID, "user_id", uid, "err", err)
			continue
		}
		if user == nil {
			continue
		}
		key := cacheKey(tenantID, uid)
		r.mu.Lock()
		r.cache[key] = user
		r.mu.Unlock()
		result[uid] = user
	}

	return result
}
