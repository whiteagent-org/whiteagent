package mapper

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
)

// TestToMessagePropagatesIsMentionTrue verifies that the mapper copies
// IsMention=true from IncomingMessage DTO to entity.Message (MSG-02).
func TestToMessagePropagatesIsMentionTrue(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants:           []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:             map[string]*entity.User{"ch1" + "u-ext": {ID: "u1", TenantID: "t1"}},
		workspaceMappings: map[string]entity.TenantID{"ch1:bot1": "t1"},
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{}
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:         "msg-mention-true",
		TenantID:   "bot1",
		UserID:     "u-ext",
		ChatID:     "group-chat-1",
		Content:    "hey @bot",
		IsGroup:    true,
		IsMention:  true,
		ReceivedAt: time.Now(),
	}

	msg, _, err := m.ToMessage(context.Background(), incoming, "ch1")
	if err != nil {
		t.Fatalf("ToMessage: %v", err)
	}
	if !msg.IsMention {
		t.Errorf("IsMention: got false, want true — mapper must propagate IsMention from DTO")
	}
}

// TestToMessagePropagatesIsMentionFalse verifies that the mapper copies
// IsMention=false from IncomingMessage DTO to entity.Message (MSG-02).
func TestToMessagePropagatesIsMentionFalse(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants:           []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:             map[string]*entity.User{"ch1" + "u-ext": {ID: "u1", TenantID: "t1"}},
		workspaceMappings: map[string]entity.TenantID{"ch1:bot1": "t1"},
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{}
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:         "msg-mention-false",
		TenantID:   "bot1",
		UserID:     "u-ext",
		ChatID:     "group-chat-1",
		Content:    "just chatting",
		IsGroup:    true,
		IsMention:  false,
		ReceivedAt: time.Now(),
	}

	msg, _, err := m.ToMessage(context.Background(), incoming, "ch1")
	if err != nil {
		t.Fatalf("ToMessage: %v", err)
	}
	if msg.IsMention {
		t.Errorf("IsMention: got true, want false — mapper must propagate IsMention=false from DTO")
	}
}
