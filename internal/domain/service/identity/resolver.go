package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
)

// Sentinel errors for identity resolution failures.
var (
	// ErrUnknownUser indicates the user is not registered in any tenant.
	// Caller should send: "Access denied. You are not registered."
	ErrUnknownUser = errors.New("unknown user")

	// ErrUnknownGroup indicates the group chat cannot be resolved (no workspace mapping).
	ErrUnknownGroup = errors.New("unknown group")
)

// ResolvedIdentity holds the domain IDs resolved from an external channel identity.
type ResolvedIdentity struct {
	TenantID entity.TenantID
	UserID   entity.UserID
	AgentID  entity.AgentID
	ChatID   entity.ChatID // Set for both DM and group chats.
	IsGroup  bool
}

// Resolver maps external channel identities to internal tenant/user/agent IDs.
// Results are cached in memory with unlimited TTL.
// The resolver is read-only for users -- it never calls store.SaveUser.
type Resolver struct {
	store      port.StorePlugin
	onboarding *onboarding.Service
	cache      *Cache
}

// NewResolver creates a new identity resolver.
func NewResolver(store port.StorePlugin, onboarding *onboarding.Service) *Resolver {
	return &Resolver{
		store:      store,
		onboarding: onboarding,
		cache:      NewCache(),
	}
}

// Resolve resolves an IncomingMessage's external channel identity to internal domain IDs.
// channelID is passed explicitly (not carried on the DTO).
// Returns ErrUnknownUser for unregistered DM users, ErrUnknownGroup for unregistered groups.
func (r *Resolver) Resolve(ctx context.Context, channelID string, msg dto.IncomingMessage) (ResolvedIdentity, error) {
	if msg.IsGroup {
		return r.resolveGroupInbound(ctx, channelID, msg)
	}
	return r.resolveUserInbound(ctx, channelID, msg)
}

// resolveUserInbound resolves a DM message sender to internal IDs.
// Uses tenant-first resolution via workspace mapping (no ListTenants iteration).
func (r *Resolver) resolveUserInbound(ctx context.Context, channelID string, msg dto.IncomingMessage) (ResolvedIdentity, error) {
	// Check cache first.
	if entry, ok := r.cache.GetUser(channelID, msg.UserID); ok {
		ri := ResolvedIdentity{
			TenantID: entry.tenantID,
			UserID:   entry.userID,
			AgentID:  entry.agentID,
		}
		// Resolve DM chat.
		chatID, isGroup, err := r.resolveChat(ctx, entry.tenantID, channelID, msg, entry.agentID)
		if err != nil {
			return ResolvedIdentity{}, err
		}
		ri.ChatID = chatID
		ri.IsGroup = isGroup
		return ri, nil
	}

	// Resolve tenant from workspace mapping.
	tenantID, err := r.store.GetTenantByMapping(ctx, channelID, msg.TenantID)
	if err != nil || tenantID.IsEmpty() {
		return ResolvedIdentity{}, ErrUnknownUser
	}

	// Look up user within the resolved tenant.
	user, err := r.store.GetUserByChannel(ctx, tenantID, channelID, msg.UserID)
	if err != nil || user == nil {
		return ResolvedIdentity{}, ErrUnknownUser
	}

	// Get tenant for DefaultAgentID.
	tenant, err := r.store.GetTenant(ctx, tenantID)
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("identity: get tenant %s: %w", tenantID, err)
	}

	ri := ResolvedIdentity{
		TenantID: tenantID,
		UserID:   user.ID,
		AgentID:  tenant.DefaultAgentID,
	}

	// Resolve DM chat.
	chatID, isGroup, chatErr := r.resolveChat(ctx, tenantID, channelID, msg, tenant.DefaultAgentID)
	if chatErr != nil {
		return ResolvedIdentity{}, chatErr
	}
	ri.ChatID = chatID
	ri.IsGroup = isGroup

	// Cache the resolved identity.
	r.cache.SetUser(channelID, msg.UserID, &userEntry{
		tenantID: tenantID,
		userID:   user.ID,
		agentID:  tenant.DefaultAgentID,
	})

	return ri, nil
}

// resolveChat resolves or creates a Chat entity for the given message.
// Works for both DM and group chats.
func (r *Resolver) resolveChat(ctx context.Context, tenantID entity.TenantID, channelID string, msg dto.IncomingMessage, agentID entity.AgentID) (entity.ChatID, bool, error) {
	// Check chat cache first.
	if entry, ok := r.cache.GetChat(channelID, msg.ChatID); ok {
		return entry.chatID, entry.isGroup, nil
	}

	// Look up existing chat.
	chat, err := r.store.GetChatByChannel(ctx, tenantID, channelID, msg.ChatID)
	if err != nil {
		return "", false, fmt.Errorf("identity: get chat: %w", err)
	}

	if chat != nil {
		r.cache.SetChat(channelID, msg.ChatID, &chatEntry{
			tenantID: tenantID,
			agentID:  chat.AgentID,
			chatID:   chat.ID,
			isGroup:  chat.IsGroup,
		})
		return chat.ID, chat.IsGroup, nil
	}

	// Chat not found -- create via onboarding.
	newChat, createErr := r.onboarding.CreateChat(ctx, tenantID, channelID, msg, agentID)
	if createErr != nil {
		return "", false, fmt.Errorf("identity: create chat: %w", createErr)
	}

	r.cache.SetChat(channelID, msg.ChatID, &chatEntry{
		tenantID: tenantID,
		agentID:  newChat.AgentID,
		chatID:   newChat.ID,
		isGroup:  newChat.IsGroup,
	})
	return newChat.ID, newChat.IsGroup, nil
}

// resolveGroupInbound resolves a group message to internal IDs.
// Uses workspace mapping for tenant resolution and auto-creates chats for registered users.
func (r *Resolver) resolveGroupInbound(ctx context.Context, channelID string, msg dto.IncomingMessage) (ResolvedIdentity, error) {
	// Check chat cache first.
	if entry, ok := r.cache.GetChat(channelID, msg.ChatID); ok {
		ri := ResolvedIdentity{
			TenantID: entry.tenantID,
			AgentID:  entry.agentID,
			ChatID:   entry.chatID,
			IsGroup:  entry.isGroup,
		}
		// Try to resolve the sender user within the chat's tenant.
		userID, err := r.resolveGroupSender(ctx, channelID, msg, entry.tenantID)
		if errors.Is(err, ErrUnknownUser) {
			return ri, ErrUnknownUser
		}
		if err != nil {
			return ResolvedIdentity{}, err
		}
		ri.UserID = userID
		return ri, nil
	}

	// Look up existing chat (with tenantID from workspace mapping).
	tenantID, wsErr := r.store.GetTenantByMapping(ctx, channelID, msg.TenantID)
	if wsErr != nil || tenantID.IsEmpty() {
		return ResolvedIdentity{}, ErrUnknownGroup
	}

	chat, err := r.store.GetChatByChannel(ctx, tenantID, channelID, msg.ChatID)
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("identity: get chat: %w", err)
	}

	if chat == nil {
		// No chat found -- auto-create via onboarding.
		tenant, tErr := r.store.GetTenant(ctx, tenantID)
		if tErr != nil {
			return ResolvedIdentity{}, fmt.Errorf("identity: get tenant %s for chat: %w", tenantID, tErr)
		}
		return r.autoCreateChat(ctx, channelID, msg, tenantID, tenant.DefaultAgentID)
	}

	return r.resolveExistingChat(ctx, channelID, msg, chat)
}

// autoCreateChat creates a chat entity via onboarding with the resolved agent ID.
// Chat entity is created regardless of whether the sender is registered.
// Returns ErrUnknownUser (with partial identity including ChatID) if the sender
// is not registered in the tenant.
func (r *Resolver) autoCreateChat(ctx context.Context, channelID string, msg dto.IncomingMessage, tenantID entity.TenantID, agentID entity.AgentID) (ResolvedIdentity, error) {
	chat, err := r.onboarding.CreateChat(ctx, tenantID, channelID, msg, agentID)
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("identity: auto-create chat: %w", err)
	}

	return r.resolveExistingChat(ctx, channelID, msg, chat)
}

// resolveExistingChat resolves identity for a known chat entity.
func (r *Resolver) resolveExistingChat(ctx context.Context, channelID string, msg dto.IncomingMessage, chat *entity.Chat) (ResolvedIdentity, error) {
	// Determine agent ID: chat-specific or tenant default.
	agentID := chat.AgentID
	if agentID.IsEmpty() {
		tenant, err := r.store.GetTenant(ctx, chat.TenantID)
		if err != nil {
			return ResolvedIdentity{}, fmt.Errorf("identity: get tenant %s for chat: %w", chat.TenantID, err)
		}
		agentID = tenant.DefaultAgentID
	}

	ri := ResolvedIdentity{
		TenantID: chat.TenantID,
		AgentID:  agentID,
		ChatID:   chat.ID,
		IsGroup:  chat.IsGroup,
	}

	// Cache the resolved chat identity.
	r.cache.SetChat(channelID, msg.ChatID, &chatEntry{
		tenantID: chat.TenantID,
		agentID:  agentID,
		chatID:   chat.ID,
		isGroup:  chat.IsGroup,
	})

	// Try to resolve the sender user within the chat's tenant.
	userID, err := r.resolveGroupSender(ctx, channelID, msg, chat.TenantID)
	if errors.Is(err, ErrUnknownUser) {
		// Return partial identity (TenantID, AgentID, ChatID filled) + ErrUnknownUser.
		return ri, ErrUnknownUser
	}
	if err != nil {
		return ResolvedIdentity{}, err
	}
	ri.UserID = userID

	return ri, nil
}

// resolveGroupSender looks up the message sender within the chat's tenant.
// Returns ErrUnknownUser if the sender is not registered. The resolver is
// read-only for users and never creates user entities.
func (r *Resolver) resolveGroupSender(ctx context.Context, channelID string, msg dto.IncomingMessage, tenantID entity.TenantID) (entity.UserID, error) {
	if msg.UserID == "" {
		return "", nil
	}
	user, err := r.store.GetUserByChannel(ctx, tenantID, channelID, msg.UserID)
	if err != nil {
		return "", fmt.Errorf("identity: group sender lookup: %w", err)
	}
	if user == nil {
		return "", ErrUnknownUser
	}
	return user.ID, nil
}
