// Package onboarding implements the invite code join flow and entity creation
// for users, tenants, and groups.
package onboarding

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// ErrRejected indicates the join attempt was rejected (invalid code, policy, etc.).
var ErrRejected = errors.New("onboarding: rejected")

// defaultRejection is sent when no tenant-specific rejection message is configured.
const defaultRejection = "Please provide a valid invite code to get started."

// codeRe matches XXXX-XXXX invite codes (uppercase alphanumeric).
var codeRe = regexp.MustCompile(`[A-Z0-9]{4}-[A-Z0-9]{4}`)

// TryJoinResult holds the outcome of a successful TryJoin call.
type TryJoinResult struct {
	Tenant *entity.Tenant
	User   *entity.User
	Action string // "tenant_created", "user_joined", "auto_joined"
}

// Service handles invite code validation, user/tenant/group creation.
type Service struct {
	store port.StorePlugin
}

// NewService creates a new onboarding service.
func NewService(store port.StorePlugin) *Service {
	return &Service{store: store}
}

// TryJoin attempts to join a user via an invite code or open policy.
// Returns (result, rejectionMsg, error). Error is ErrRejected on rejection.
// When tenantID is non-empty, workspace lookup is skipped in the no-code path
// (used when the caller has already resolved the tenant, e.g. group context).
func (s *Service) TryJoin(ctx context.Context, channelID string, msg dto.IncomingMessage, tenantID entity.TenantID) (*TryJoinResult, string, error) {
	code := extractCode(msg.Content)

	if code != "" {
		return s.joinWithCode(ctx, channelID, code, msg)
	}

	return s.joinWithoutCode(ctx, channelID, msg, tenantID)
}

// CreateUser builds and persists a new user entity from an incoming message.
// Uses msg.UserName for the user's display name.
func (s *Service) CreateUser(ctx context.Context, tenantID entity.TenantID, channelID string, msg dto.IncomingMessage) (*entity.User, error) {
	user := entity.User{
		ID:               entity.UserID(util.NewID()),
		TenantID:         tenantID,
		Name:             msg.UserName,
		PreferredChannel: channelID,
		CreatedAt:        time.Now().UTC(),
	}

	if err := s.store.SaveUser(ctx, tenantID, user); err != nil {
		return nil, err
	}
	if err := s.store.AddUserIdentity(ctx, tenantID, channelID, msg.UserID, user.ID); err != nil {
		return nil, err
	}
	slog.Info("onboarding.user_created", "tenant", tenantID, "user", user.ID, "channel", channelID)
	return &user, nil
}

// CreateChat creates a new chat entity (DM or group) from an incoming message.
// For DM chats: UserID is looked up from the DTO, IsGroup=false.
// For group chats: IsGroup=true, Name from GroupName.
func (s *Service) CreateChat(ctx context.Context, tenantID entity.TenantID, channelID string, msg dto.IncomingMessage, agentID entity.AgentID) (*entity.Chat, error) {
	chat := entity.Chat{
		ID:             entity.ChatID(util.NewID()),
		TenantID:       tenantID,
		ChannelID:      channelID,
		ExternalChatID: msg.ChatID,
		IsGroup:        msg.IsGroup,
		Name:           msg.GroupName,
		AgentID:        agentID,
		CreatedAt:      time.Now().UTC(),
	}

	// For DMs, resolve the user ID so the chat is linked to the user.
	if !msg.IsGroup && msg.UserID != "" {
		user, err := s.store.GetUserByChannel(ctx, tenantID, channelID, msg.UserID)
		if err == nil && user != nil {
			chat.UserID = user.ID
		}
	}

	if err := s.store.SaveChat(ctx, tenantID, chat); err != nil {
		return nil, err
	}
	slog.Info("onboarding.chat_created", "tenant", tenantID, "chat", chat.ID, "channel", channelID, "is_group", chat.IsGroup)
	return &chat, nil
}

// joinWithCode handles tenant-creation and user-join code flows.
func (s *Service) joinWithCode(ctx context.Context, channelID, code string, msg dto.IncomingMessage) (*TryJoinResult, string, error) {
	inv, err := s.store.GetInviteCode(ctx, code)
	if err != nil || !validateCode(inv) {
		return nil, defaultRejection, ErrRejected
	}

	switch inv.Type {
	case "tenant":
		if inv.TargetID != "" {
			return s.linkTenant(ctx, channelID, code, inv, msg)
		}
		return s.createTenant(ctx, channelID, code, msg)
	case "user":
		if inv.TargetID != "" {
			return s.linkUser(ctx, channelID, code, inv, msg)
		}
		return s.joinUser(ctx, channelID, code, inv, msg)
	default:
		return nil, defaultRejection, ErrRejected
	}
}

// joinWithoutCode handles open-policy auto-join and rejection for invite_required/closed.
// When tenantID is non-empty, it is used directly instead of calling GetTenantByMapping.
func (s *Service) joinWithoutCode(ctx context.Context, channelID string, msg dto.IncomingMessage, tenantID entity.TenantID) (*TryJoinResult, string, error) {
	if tenantID.IsEmpty() {
		// No pre-resolved tenantID; resolve via workspace mapping.
		if msg.TenantID == "" {
			return nil, defaultRejection, ErrRejected
		}

		var err error
		tenantID, err = s.store.GetTenantByMapping(ctx, channelID, msg.TenantID)
		if err != nil || tenantID.IsEmpty() {
			return nil, defaultRejection, ErrRejected
		}
	}

	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, defaultRejection, ErrRejected
	}

	if tenant.JoinPolicy == entity.JoinPolicyOpen {
		user, err := s.CreateUser(ctx, tenantID, channelID, msg)
		if err != nil {
			return nil, defaultRejection, ErrRejected
		}
		return &TryJoinResult{
			Tenant: tenant,
			User:   user,
			Action: "auto_joined",
		}, "", nil
	}

	// invite_required or closed: reject
	rejection := tenant.RejectionMessage
	if rejection == "" {
		rejection = defaultRejection
	}
	return nil, rejection, ErrRejected
}

// createTenant creates a new tenant, agent, first user, and workspace mapping from a tenant-creation code.
func (s *Service) createTenant(ctx context.Context, channelID, code string, msg dto.IncomingMessage) (*TryJoinResult, string, error) {
	tenantID := entity.TenantID(util.NewID())

	// Tenant name fallback chain: msg.TenantName -> "{msg.UserName}'s Workspace" -> "Tenant-{code[:4]}"
	tenantName := msg.TenantName
	if tenantName == "" && msg.UserName != "" {
		tenantName = msg.UserName + "'s Workspace"
	}
	if tenantName == "" {
		tenantName = "Tenant-" + code[:4]
	}

	tenant := entity.Tenant{
		ID:               tenantID,
		Name:             tenantName,
		JoinPolicy:       entity.JoinPolicyInviteRequired,
		RejectionMessage: defaultRejection,
		GroupMode:        entity.GroupModeMentionOnly,
		CreatedAt:        time.Now().UTC(),
	}
	if err := s.store.SaveTenant(ctx, tenantID, tenant); err != nil {
		return nil, defaultRejection, ErrRejected
	}
	slog.Info("onboarding.tenant_created", "tenant", tenantID, "name", tenantName)

	// Agent name: msg.AgentName, fallback: tenantName + " Agent"
	agentName := msg.AgentName
	if agentName == "" {
		agentName = tenantName + " Agent"
	}

	agentID := entity.AgentID(util.NewID())
	agent := entity.Agent{
		ID:           agentID,
		TenantID:     tenantID,
		Name:         agentName,
		Instructions: entity.DefaultAgentInstructions(),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.store.SaveAgent(ctx, tenantID, agent); err != nil {
		return nil, defaultRejection, ErrRejected
	}
	slog.Info("onboarding.agent_created", "tenant", tenantID, "agent", agentID, "name", agentName)

	// Set default agent
	tenant.DefaultAgentID = agentID
	if err := s.store.SaveTenant(ctx, tenantID, tenant); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	// Create first user
	user, err := s.CreateUser(ctx, tenantID, channelID, msg)
	if err != nil {
		return nil, defaultRejection, ErrRejected
	}

	// Save tenant mapping if TenantID is present
	if msg.TenantID != "" {
		mapping := entity.TenantMapping{
			ChannelID:        channelID,
			ExternalTenantID: msg.TenantID,
			TenantID:         tenantID,
		}
		if err := s.store.SaveTenantMapping(ctx, mapping); err != nil {
			return nil, defaultRejection, ErrRejected
		}
	}

	// Mark code as used
	if err := s.store.UseInviteCode(ctx, code, user.ID); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	return &TryJoinResult{
		Tenant: &tenant,
		User:   user,
		Action: "tenant_created",
	}, "", nil
}

// joinUser creates a user in the code's tenant and marks the code as used.
func (s *Service) joinUser(ctx context.Context, channelID, code string, inv *entity.InviteCode, msg dto.IncomingMessage) (*TryJoinResult, string, error) {
	if inv.TenantID.IsEmpty() {
		return nil, defaultRejection, ErrRejected
	}

	tenant, err := s.store.GetTenant(ctx, inv.TenantID)
	if err != nil {
		return nil, defaultRejection, ErrRejected
	}

	user, err := s.CreateUser(ctx, inv.TenantID, channelID, msg)
	if err != nil {
		return nil, defaultRejection, ErrRejected
	}

	if err := s.store.UseInviteCode(ctx, code, user.ID); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	return &TryJoinResult{
		Tenant: tenant,
		User:   user,
		Action: "user_joined",
	}, "", nil
}

// linkUser adds the redeemer's channel to an existing target user.
// If the redeemer is already registered as a different user, merges them first.
func (s *Service) linkUser(ctx context.Context, channelID, code string, inv *entity.InviteCode, msg dto.IncomingMessage) (*TryJoinResult, string, error) {
	targetUserID := entity.UserID(inv.TargetID)

	user, err := s.store.GetUser(ctx, inv.TenantID, targetUserID)
	if err != nil || user == nil {
		return nil, defaultRejection, ErrRejected
	}

	tenant, err := s.store.GetTenant(ctx, inv.TenantID)
	if err != nil {
		return nil, defaultRejection, ErrRejected
	}

	// Check if redeemer already exists as a different user in this tenant.
	existing, _ := s.store.GetUserByChannel(ctx, inv.TenantID, channelID, msg.UserID)
	if existing != nil && existing.ID != targetUserID {
		if err := s.store.MergeUser(ctx, inv.TenantID, existing.ID, targetUserID); err != nil {
			return nil, defaultRejection, ErrRejected
		}
		// Re-fetch target user after merge.
		user, err = s.store.GetUser(ctx, inv.TenantID, targetUserID)
		if err != nil {
			return nil, defaultRejection, ErrRejected
		}
	}

	// Add new user identity for target user.
	if err := s.store.AddUserIdentity(ctx, inv.TenantID, channelID, msg.UserID, user.ID); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	if err := s.store.UseInviteCode(ctx, code, targetUserID); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	return &TryJoinResult{Tenant: tenant, User: user, Action: "user_linked"}, "", nil
}

// linkTenant creates a workspace mapping from the redeemer's workspace to an existing target tenant.
func (s *Service) linkTenant(ctx context.Context, channelID, code string, inv *entity.InviteCode, msg dto.IncomingMessage) (*TryJoinResult, string, error) {
	targetTenantID := entity.TenantID(inv.TargetID)

	tenant, err := s.store.GetTenant(ctx, targetTenantID)
	if err != nil || tenant == nil {
		return nil, defaultRejection, ErrRejected
	}

	if msg.TenantID == "" {
		return nil, defaultRejection, ErrRejected
	}

	// Check if workspace already mapped to a different tenant.
	existingTID, err := s.store.GetTenantByMapping(ctx, channelID, msg.TenantID)
	if err == nil && !existingTID.IsEmpty() && existingTID != targetTenantID {
		return nil, "This workspace is already linked to a different tenant.", ErrRejected
	}

	mapping := entity.TenantMapping{
		ChannelID:        channelID,
		ExternalTenantID: msg.TenantID,
		TenantID:         targetTenantID,
	}
	if err := s.store.SaveTenantMapping(ctx, mapping); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	if err := s.store.UseInviteCode(ctx, code, entity.UserID("system:tenant-link")); err != nil {
		return nil, defaultRejection, ErrRejected
	}

	return &TryJoinResult{Tenant: tenant, User: nil, Action: "tenant_linked"}, "", nil
}

// validateCode checks that an invite code is valid for redemption.
func validateCode(inv *entity.InviteCode) bool {
	if inv == nil {
		return false
	}
	if !inv.UsedBy.IsEmpty() {
		return false
	}
	if inv.RevokedAt != nil {
		return false
	}
	return true
}

// extractCode parses an XXXX-XXXX invite code from message text.
// Returns the matched code or empty string if none found.
func extractCode(content string) string {
	return codeRe.FindString(strings.ToUpper(content))
}
