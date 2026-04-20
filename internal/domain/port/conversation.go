package port

import (
	"context"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ConversationService manages DB-backed conversation state with ID resolution cache.
type ConversationService interface {
	ResolveConversation(ctx context.Context, msg entity.Message) (entity.ConversationID, error)
	RegisterConversation(tenantID entity.TenantID, convID entity.ConversationID)
	Append(ctx context.Context, convID entity.ConversationID, msg entity.Message) error
	GetHistory(ctx context.Context, convID entity.ConversationID, offset, limit int, upToID *entity.MessageID) ([]entity.Message, error)
	ResetConversation(ctx context.Context, convID entity.ConversationID) error
	SwitchConversation(msg entity.Message, targetConvID entity.ConversationID)
}

// ConversationResetter is the interface exposed to plugins via ConversationAware.
// It allows resolving and resetting conversations without access to Append/GetHistory.
type ConversationResetter interface {
	ResolveConversation(ctx context.Context, msg entity.Message) (entity.ConversationID, error)
	ResetConversation(ctx context.Context, convID entity.ConversationID) error
}

// JournalReader provides read access to journal entries.
// Implemented by StorePlugin; used by PromptBuilder for journal context injection.
type JournalReader interface {
	GetJournal(ctx context.Context, tenantID entity.TenantID, filter entity.JournalFilter) ([]entity.JournalEntry, error)
}

// SummaryReader provides read access to persisted conversation summaries.
// Implemented by StorePlugin; used by PromptBuilder for summary-backed context.
type SummaryReader interface {
	GetLatestSummary(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID) (*entity.Summary, error)
}

// MemoryReader provides read access to persistent memory.
// Implemented by StorePlugin; used by PromptBuilder for memory injection into system prompts.
type MemoryReader interface {
	GetMemory(ctx context.Context, tenantID entity.TenantID, ownerType, ownerID string) (*entity.Memory, error)
}

// ConversationAware is implemented by plugins that need conversation reset capabilities.
// The runtime type-asserts and injects after Init, before Start.
type ConversationAware interface {
	SetConversationResetter(ConversationResetter)
}

// TransportAware is implemented by plugins that need a TransportPlugin reference.
// The runtime type-asserts and injects after Init, before Start.
type TransportAware interface {
	SetTransport(TransportPlugin)
}

// StoreAware is implemented by runtime components that need a StorePlugin reference.
// The runtime type-asserts and injects after Init, before Start.
type StoreAware interface {
	SetStore(StorePlugin)
}
