// Package identity provides identity resolution between external channel
// identities and internal tenant/user/agent IDs.
package identity

import (
	"sync"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// userEntry holds the resolved internal IDs for a DM user.
type userEntry struct {
	tenantID entity.TenantID
	userID   entity.UserID
	agentID  entity.AgentID
}

// chatEntry holds the resolved internal IDs for a chat (DM or group).
type chatEntry struct {
	tenantID entity.TenantID
	agentID  entity.AgentID
	chatID   entity.ChatID
	isGroup  bool
}

// Cache is an in-memory identity cache with unlimited TTL.
// It avoids repeated store lookups for the same external identity.
type Cache struct {
	mu    sync.RWMutex
	users map[string]*userEntry
	chats map[string]*chatEntry
}

// NewCache creates a new empty identity cache.
func NewCache() *Cache {
	return &Cache{
		users: make(map[string]*userEntry),
		chats: make(map[string]*chatEntry),
	}
}

// userKey builds the cache key for a user lookup.
func userKey(channelID, userExternalID string) string {
	return channelID + ":" + userExternalID
}

// chatKey builds the cache key for a chat lookup.
func chatKey(channelID, chatExternalID string) string {
	return channelID + ":" + chatExternalID
}

// GetUser returns the cached user entry for the given channel identity, if any.
func (c *Cache) GetUser(channelID, userExternalID string) (*userEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.users[userKey(channelID, userExternalID)]
	return e, ok
}

// SetUser caches a resolved user entry.
func (c *Cache) SetUser(channelID, userExternalID string, entry *userEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.users[userKey(channelID, userExternalID)] = entry
}

// GetChat returns the cached chat entry for the given channel identity, if any.
func (c *Cache) GetChat(channelID, chatExternalID string) (*chatEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.chats[chatKey(channelID, chatExternalID)]
	return e, ok
}

// SetChat caches a resolved chat entry.
func (c *Cache) SetChat(channelID, chatExternalID string, entry *chatEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chats[chatKey(channelID, chatExternalID)] = entry
}
