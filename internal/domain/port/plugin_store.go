package port

import (
	"context"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// MessageFilter specifies retrieval criteria for GetMessages.
// All fields are optional. Zero value means "no filter on that field".
type MessageFilter struct {
	ConversationID    entity.ConversationID
	ChatID            entity.ChatID
	ExternalMessageID string
	UserID            entity.UserID
	MessageID         entity.MessageID // single message lookup by ID
	Limit             int
	Offset            int
	Before            *time.Time
	After             *time.Time
	Query             string            // FTS5 full-text search
	Roles             []entity.Role     // Filter by message role (e.g. user, assistant); empty means no filter
	Tail              bool              // When true with Limit>0, returns last N rows in chronological order
	UpToID            *entity.MessageID // include messages with id <= this value (UUIDv7 lexicographic)
	Evicted           *bool             // nil=all, true=only evicted, false=only non-evicted
}

// StorePlugin is the persistence interface for tenant-scoped storage.
// All methods require an explicit tenantID for structural query scoping (TNNT-02).
// GetChatByChannel requires tenantID (identity resolver resolves tenant via
// workspace mapping before calling this).
// MigrateToLatest runs schema migrations at plugin Init time (PERS-01).
type StorePlugin interface {
	Plugin
	MigrateToLatest(ctx context.Context) error
	GetTenant(ctx context.Context, tenantID entity.TenantID) (*entity.Tenant, error)
	SaveTenant(ctx context.Context, tenantID entity.TenantID, tenant entity.Tenant) error
	ListTenants(ctx context.Context) ([]entity.Tenant, error)
	GetAgent(ctx context.Context, tenantID entity.TenantID, agentID entity.AgentID) (*entity.Agent, error)
	SaveAgent(ctx context.Context, tenantID entity.TenantID, agent entity.Agent) error
	ListAgents(ctx context.Context, tenantID entity.TenantID) ([]entity.Agent, error)
	GetUserByChannel(ctx context.Context, tenantID entity.TenantID, channelID, userExternalID string) (*entity.User, error)
	GetUser(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error)
	SearchUsers(ctx context.Context, tenantID entity.TenantID, query string) ([]entity.User, error)
	SaveUser(ctx context.Context, tenantID entity.TenantID, user entity.User) error
	SaveChat(ctx context.Context, tenantID entity.TenantID, chat entity.Chat) error
	GetChat(ctx context.Context, tenantID entity.TenantID, chatID entity.ChatID) (*entity.Chat, error)
	GetChatByChannel(ctx context.Context, tenantID entity.TenantID, channelID, externalChatID string) (*entity.Chat, error)
	GetDMChat(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.Chat, error)
	SearchChats(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, query string) ([]entity.Chat, error)

	// -- Memory --
	GetMemory(ctx context.Context, tenantID entity.TenantID, ownerType, ownerID string) (*entity.Memory, error)
	SaveMemory(ctx context.Context, tenantID entity.TenantID, memory entity.Memory) error
	AppendJournal(ctx context.Context, tenantID entity.TenantID, entry entity.JournalEntry) error
	GetJournal(ctx context.Context, tenantID entity.TenantID, filter entity.JournalFilter) ([]entity.JournalEntry, error)
	SaveSummary(ctx context.Context, tenantID entity.TenantID, summary entity.Summary) error
	GetLatestSummary(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID) (*entity.Summary, error)
	ListSummaries(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID, offset, limit int) ([]entity.Summary, error)
	DeleteTenant(ctx context.Context, tenantID entity.TenantID) error
	DeleteUser(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) error
	ListUsers(ctx context.Context, tenantID entity.TenantID) ([]entity.User, error)
	SaveInviteCode(ctx context.Context, code entity.InviteCode) error
	GetInviteCode(ctx context.Context, code string) (*entity.InviteCode, error)
	ListInviteCodes(ctx context.Context, filter entity.InviteCodeFilter) ([]entity.InviteCode, error)
	RevokeInviteCode(ctx context.Context, code string) error
	UseInviteCode(ctx context.Context, code string, userID entity.UserID) error

	// -- Tenant mappings --
	SaveTenantMapping(ctx context.Context, mapping entity.TenantMapping) error
	GetTenantByMapping(ctx context.Context, channelID, externalTenantID string) (entity.TenantID, error)
	DeleteTenantMapping(ctx context.Context, channelID, externalTenantID string) error
	ListTenantMappings(ctx context.Context, tenantID entity.TenantID) ([]entity.TenantMapping, error)

	// -- User identity lookups --
	GetExternalID(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, channelID string) (string, error)
	AddUserIdentity(ctx context.Context, tenantID entity.TenantID, channelID, externalID string, userID entity.UserID) error

	// -- User merge --
	MergeUser(ctx context.Context, tenantID entity.TenantID, fromID, toID entity.UserID) error

	// -- Cron entries --
	SaveCronEntry(ctx context.Context, tenantID entity.TenantID, entry entity.CronEntry) error
	GetCronEntry(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID) (*entity.CronEntry, error)
	ListCronEntries(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.CronEntry, error)
	ListActiveCronEntries(ctx context.Context) ([]entity.CronEntry, error) // cross-tenant
	DeleteCronEntry(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID) error
	UpdateCronStatus(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, status string) error
	UpdateCronNextRun(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, nextRunAt *time.Time) error

	// -- Cron runs --
	InsertCronRun(ctx context.Context, tenantID entity.TenantID, run entity.CronRun) error
	UpdateCronRun(ctx context.Context, tenantID entity.TenantID, runID entity.CronRunID, status string, errMsg string, finishedAt *time.Time) error
	ListCronRuns(ctx context.Context, tenantID entity.TenantID, cronEntryID entity.CronEntryID, limit int) ([]entity.CronRun, error)

	// -- Messages --
	SaveMessage(ctx context.Context, msg entity.Message) error
	GetMessages(ctx context.Context, tenantID entity.TenantID, filter MessageFilter) ([]entity.Message, error)
	GetLastConversationID(ctx context.Context, msg entity.Message) (entity.ConversationID, error)
	EvictMessages(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID, msgIDs []entity.MessageID) error

	UpdateExternalMessageID(ctx context.Context, msgID entity.MessageID, externalMsgID string) error

	// -- Error log --
	AppendErrorLog(ctx context.Context, tenantID entity.TenantID, entry entity.ErrorLogEntry) error
	GetErrorLog(ctx context.Context, tenantID entity.TenantID, filter entity.ErrorLogFilter) ([]entity.ErrorLogEntry, error)

	// -- Secrets --
	SaveSecret(ctx context.Context, tenantID entity.TenantID, s entity.Secret) error
	GetSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (*entity.Secret, error)
	ListSecrets(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.Secret, error)
	DeleteSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error

	// -- Secret tokens --
	SaveSecretToken(ctx context.Context, token entity.SecretToken) error
	GetSecretToken(ctx context.Context, tokenID string) (*entity.SecretToken, error)
	ConsumeSecretToken(ctx context.Context, tokenID string) error
}
