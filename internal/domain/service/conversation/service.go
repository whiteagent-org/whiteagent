// Package conversation implements the DB-backed conversation service
// with in-memory conversation ID resolution cache.
package conversation

import (
	"context"
	"fmt"
	"sync"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// Service implements port.ConversationService with a sync.Map-based cache
// for conversation ID resolution.
type Service struct {
	store   port.StorePlugin
	cache   sync.Map // Message.ChatID.String() -> entity.ConversationID
	reverse sync.Map // entity.ConversationID -> string (identity key)
	tenants sync.Map // entity.ConversationID -> entity.TenantID
}

// NewService creates a ConversationService backed by the given store.
func NewService(store port.StorePlugin) *Service {
	return &Service{store: store}
}

// storeMapping saves the bidirectional mapping between identity key and conversation ID.
func (s *Service) storeMapping(key string, convID entity.ConversationID, tenantID entity.TenantID) {
	s.cache.Store(key, convID)
	s.reverse.Store(convID, key)
	s.tenants.Store(convID, tenantID)
}

// ResolveConversation returns a ConversationID for the given Message.
// Resolution order: cache hit -> DB lookup -> generate new UUIDv7.
func (s *Service) ResolveConversation(ctx context.Context, msg entity.Message) (entity.ConversationID, error) {
	key := msg.ChatID.String()

	// 1. Cache hit.
	if v, ok := s.cache.Load(key); ok {
		return v.(entity.ConversationID), nil
	}

	// 2. DB lookup.
	convID, err := s.store.GetLastConversationID(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("resolve conversation: %w", err)
	}
	if !convID.IsEmpty() {
		s.storeMapping(key, convID, msg.TenantID)
		return convID, nil
	}

	// 3. Generate new.
	convID = entity.ConversationID(util.NewID())
	s.storeMapping(key, convID, msg.TenantID)
	return convID, nil
}

// RegisterConversation makes a conversation ID known to the service (populates
// the tenants map) without touching the chat-identity cache. Used by the agent
// loop when a message arrives with a pre-set ConversationID (e.g. from the
// cron scheduler) so that GetHistory can resolve the tenant.
func (s *Service) RegisterConversation(tenantID entity.TenantID, convID entity.ConversationID) {
	s.tenants.Store(convID, tenantID)
}

// Append stamps the ConversationID on the message and persists it via the store.
func (s *Service) Append(ctx context.Context, convID entity.ConversationID, msg entity.Message) error {
	msg.ConversationID = convID
	if err := s.store.SaveMessage(ctx, msg); err != nil {
		return fmt.Errorf("conversation append: %w", err)
	}
	return nil
}

// GetHistory returns messages for the given conversation in chronological order.
// When limit > 0, returns the last `limit` messages (offset skips from the tail end).
// When limit is 0, returns all messages.
func (s *Service) GetHistory(ctx context.Context, convID entity.ConversationID, offset, limit int, upToID *entity.MessageID) ([]entity.Message, error) {
	v, ok := s.tenants.Load(convID)
	if !ok {
		return nil, nil // conversation not yet resolved
	}
	tenantID := v.(entity.TenantID)

	filter := port.MessageFilter{
		ConversationID: convID,
		Limit:          limit,
		Offset:         offset,
		Tail:           limit > 0,
		UpToID:         upToID,
	}
	msgs, err := s.store.GetMessages(ctx, tenantID, filter)
	if err != nil {
		return nil, fmt.Errorf("conversation get history: %w", err)
	}
	return msgs, nil
}

// SwitchConversation updates the cache to point the given Message's chat
// identity to a different conversation. Called by the runtime when reply-based
// routing detects the user quoted a message from another conversation.
func (s *Service) SwitchConversation(msg entity.Message, targetConvID entity.ConversationID) {
	key := msg.ChatID.String()
	// Delete old reverse mapping if exists.
	if oldConvID, ok := s.cache.Load(key); ok {
		s.reverse.Delete(oldConvID)
		s.tenants.Delete(oldConvID)
	}
	s.storeMapping(key, targetConvID, msg.TenantID)
}

// ResetConversation invalidates the cached conversation ID and generates a new one.
// Unknown conversation IDs are a no-op (returns nil).
func (s *Service) ResetConversation(ctx context.Context, convID entity.ConversationID) error {
	// Load identity key from reverse map.
	v, ok := s.reverse.Load(convID)
	if !ok {
		return nil // unknown -- no-op
	}
	key := v.(string)

	// Load tenantID before deleting.
	tv, ok := s.tenants.Load(convID)
	if !ok {
		return nil
	}
	tenantID := tv.(entity.TenantID)

	// Delete old mappings.
	s.cache.Delete(key)
	s.reverse.Delete(convID)
	s.tenants.Delete(convID)

	// Generate new and store immediately.
	newConvID := entity.ConversationID(util.NewID())
	s.storeMapping(key, newConvID, tenantID)
	return nil
}
