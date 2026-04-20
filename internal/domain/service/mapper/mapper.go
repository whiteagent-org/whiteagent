// Package mapper converts between DTO and entity message types.
// It is a domain service used by the runtime to bridge channel plugins (DTO world)
// and the message bus (entity.Message world). It orchestrates identity resolution
// internally and performs store lookups for outbound TargetID resolution.
package mapper

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// Mapper converts between dto.IncomingMessage/dto.OutgoingMessage and entity.Message.
type Mapper struct {
	store    port.StorePlugin
	resolver *identity.Resolver
}

// NewMapper creates a new message mapper with store and identity resolver dependencies.
func NewMapper(store port.StorePlugin, resolver *identity.Resolver) *Mapper {
	return &Mapper{store: store, resolver: resolver}
}

// ToMessage converts a dto.IncomingMessage to an entity.Message by resolving
// identity internally. channelID is passed explicitly from the runtime closure.
// Returns the entity.Message, the ConversationID of the replied-to message (empty if
// no reply or not found), and any identity resolution error.
func (m *Mapper) ToMessage(ctx context.Context, msg dto.IncomingMessage, channelID string) (entity.Message, entity.ConversationID, error) {
	ri, err := m.resolver.Resolve(ctx, channelID, msg)
	if err != nil {
		// On ErrUnknownUser, build a partial entity.Message with the partial identity
		// so the caller (runtime handler) can extract TenantID for group context.
		if errors.Is(err, identity.ErrUnknownUser) {
			partial := entity.Message{
				TenantID: ri.TenantID,
				AgentID:  ri.AgentID,
				ChatID:   ri.ChatID,
				IsGroup:  ri.IsGroup,
			}
			return partial, "", err
		}
		return entity.Message{}, "", err
	}

	// Update chat delivery and indication from DTO (always fresh).
	chat, chatErr := m.store.GetChat(ctx, ri.TenantID, ri.ChatID)
	if chatErr != nil {
		slog.Warn("mapper: get chat failed", "err", chatErr, "chat_id", ri.ChatID)
	}
	if chat != nil {
		chat.Delivery = msg.Delivery
		chat.Indication = msg.Indication
		if saveErr := m.store.SaveChat(ctx, ri.TenantID, *chat); saveErr != nil {
			slog.Warn("mapper: save chat delivery failed", "err", saveErr, "chat_id", ri.ChatID)
		}
	}

	entityMsg := entity.Message{
		ID:       entity.MessageID(util.NewID()),
		TenantID: ri.TenantID,
		UserID:   ri.UserID,
		AgentID:  ri.AgentID,
		ChatID:   ri.ChatID,
		IsGroup:  ri.IsGroup,
		MessageContext: entity.MessageContext{
			ExternalUserID:    msg.UserID,
			ExternalMessageID: msg.ID,
			ExternalReplyToID: msg.ReplyToID,
		},
		Kind:        entity.MessageKindMessage, // inbound is always a message, never a reaction
		IsMention:   msg.IsMention,
		Role:        entity.RoleUser,
		Content:     msg.Content,
		Attachments: convertAttachmentsToEntity(msg.Attachments),
		Metadata:    msg.Metadata,
		CreatedAt:   msg.ReceivedAt,
	}

	// Resolve reply: look up the replied-to message by its external platform ID.
	var replyConvID entity.ConversationID
	if msg.ReplyToID != "" {
		msgs, err := m.store.GetMessages(ctx, ri.TenantID, port.MessageFilter{
			ChatID:            ri.ChatID,
			ExternalMessageID: msg.ReplyToID,
			Limit:             1,
		})
		if err != nil {
			slog.Warn("mapper: reply lookup failed", "err", err, "reply_to", msg.ReplyToID)
		} else if len(msgs) > 0 {
			entityMsg.RepliedToID = msgs[0].ID
			replyConvID = msgs[0].ConversationID
		} else {
			slog.Warn("mapper: replied-to message not found", "reply_to", msg.ReplyToID)
		}
	}

	return entityMsg, replyConvID, nil
}

// ToOutgoing converts an entity.Message to a dto.OutgoingMessage.
// Fetches the chat entity to populate ChannelID, ChatID, and Delivery from
// the centralized chat record. When msg.TargetID is set, it performs a store
// lookup to resolve the internal message ID to the platform's external message ID.
func (m *Mapper) ToOutgoing(ctx context.Context, msg entity.Message) (dto.OutgoingMessage, error) {
	// Fetch chat entity for delivery routing data.
	chat, err := m.store.GetChat(ctx, msg.TenantID, msg.ChatID)
	if err != nil {
		return dto.OutgoingMessage{}, err
	}
	if chat == nil {
		slog.Warn("mapper: chat not found for outgoing", "chat_id", msg.ChatID, "tenant", msg.TenantID)
		return dto.OutgoingMessage{}, nil
	}

	outgoing := dto.OutgoingMessage{
		ID:          string(msg.ID),
		ChannelID:   chat.ChannelID,
		ChatID:      chat.ExternalChatID,
		Kind:        string(msg.Kind),
		Content:     msg.Content,
		Attachments: convertAttachmentsToDTO(msg.Attachments),
		Metadata:    msg.Metadata,
		Delivery:    chat.Delivery,
	}

	// Ensure Delivery always has chat_id populated for channel routing.
	if outgoing.Delivery == nil {
		outgoing.Delivery = make(map[string]string)
	}
	if outgoing.Delivery["chat_id"] == "" && chat.ExternalChatID != "" {
		outgoing.Delivery["chat_id"] = chat.ExternalChatID
	}

	// Resolve internal TargetID to external message ID for platform reply threading.
	if msg.TargetID != "" {
		msgs, err := m.store.GetMessages(ctx, msg.TenantID, port.MessageFilter{
			MessageID: msg.TargetID,
			Limit:     1,
		})
		if err != nil {
			slog.Warn("mapper: TargetID lookup failed", "target_id", msg.TargetID, "err", err)
		} else if len(msgs) > 0 && msgs[0].MessageContext.ExternalMessageID != "" {
			outgoing.TargetID = msgs[0].MessageContext.ExternalMessageID
		} else {
			slog.Debug("mapper: TargetID not found or has no external ID", "target_id", msg.TargetID)
		}
	}

	return outgoing, nil
}

// convertAttachmentsToEntity converts a slice of dto.Attachment to entity.Attachment.
func convertAttachmentsToEntity(atts []dto.Attachment) []entity.Attachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]entity.Attachment, len(atts))
	for i, a := range atts {
		id := a.ID
		if id == "" {
			id = uuid.New().String()
		}
		out[i] = entity.Attachment{
			ID:       id,
			Kind:     a.Kind,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Size:     a.Size,
			Path:     a.Path,
			Caption:  a.Caption,
		}
	}
	return out
}

// convertAttachmentsToDTO converts a slice of entity.Attachment to dto.Attachment.
func convertAttachmentsToDTO(atts []entity.Attachment) []dto.Attachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]dto.Attachment, len(atts))
	for i, a := range atts {
		out[i] = dto.Attachment{
			ID:       a.ID,
			Kind:     a.Kind,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Size:     a.Size,
			Path:     a.Path,
			Caption:  a.Caption,
		}
	}
	return out
}
