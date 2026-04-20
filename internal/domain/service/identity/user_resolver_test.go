package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// mockUserStore is a minimal store mock for UserResolver tests.
type mockUserStore struct {
	users map[string]*entity.User // "tenantID:userID" → user
	err   error
}

func (m *mockUserStore) GetUser(_ context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := string(tenantID) + ":" + string(userID)
	return m.users[key], nil
}

func TestResolveUsersBasic(t *testing.T) {
	store := &mockUserStore{
		users: map[string]*entity.User{
			"t1:u1": {ID: "u1", TenantID: "t1", Name: "Alice"},
			"t1:u2": {ID: "u2", TenantID: "t1", Name: "Bob"},
		},
	}
	r := NewUserResolver(store)

	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"u1", "u2"})
	if len(result) != 2 {
		t.Fatalf("expected 2 users, got %d", len(result))
	}
	if result["u1"].Name != "Alice" {
		t.Errorf("expected Alice, got %s", result["u1"].Name)
	}
	if result["u2"].Name != "Bob" {
		t.Errorf("expected Bob, got %s", result["u2"].Name)
	}
}

func TestResolveUsersCacheHit(t *testing.T) {
	store := &mockUserStore{
		users: map[string]*entity.User{
			"t1:u1": {ID: "u1", TenantID: "t1", Name: "Alice"},
		},
	}
	r := NewUserResolver(store)

	// First call populates cache.
	r.ResolveUsers(context.Background(), "t1", []entity.UserID{"u1"})

	// Remove from store to prove cache is used.
	delete(store.users, "t1:u1")
	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"u1"})
	if len(result) != 1 {
		t.Fatalf("expected 1 user from cache, got %d", len(result))
	}
	if result["u1"].Name != "Alice" {
		t.Errorf("expected Alice from cache, got %s", result["u1"].Name)
	}
}

func TestResolveUsersEmptyIDs(t *testing.T) {
	r := NewUserResolver(&mockUserStore{users: map[string]*entity.User{}})
	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"", ""})
	if len(result) != 0 {
		t.Errorf("expected 0 users for empty IDs, got %d", len(result))
	}
}

func TestResolveUsersDeduplicates(t *testing.T) {
	store := &mockUserStore{
		users: map[string]*entity.User{
			"t1:u1": {ID: "u1", TenantID: "t1", Name: "Alice"},
		},
	}
	r := NewUserResolver(store)
	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"u1", "u1", "u1"})
	if len(result) != 1 {
		t.Errorf("expected 1 user after dedup, got %d", len(result))
	}
}

func TestResolveUsersStoreError(t *testing.T) {
	store := &mockUserStore{err: errors.New("db error")}
	r := NewUserResolver(store)
	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"u1"})
	if len(result) != 0 {
		t.Errorf("expected 0 users on store error, got %d", len(result))
	}
}

func TestResolveUsersNotFound(t *testing.T) {
	store := &mockUserStore{users: map[string]*entity.User{}}
	r := NewUserResolver(store)
	result := r.ResolveUsers(context.Background(), "t1", []entity.UserID{"nonexistent"})
	if len(result) != 0 {
		t.Errorf("expected 0 users for nonexistent, got %d", len(result))
	}
}
