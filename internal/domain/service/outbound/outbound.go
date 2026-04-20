// Package outbound provides the outbound message router that delivers agent
// responses from the message bus to the correct channel plugin. It converts
// entity.Message to dto.OutgoingMessage and delivers via channel.Send()
// with per-tenant error isolation (TNNT-04).
package outbound

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/mapper"
)

// NewHandler creates a MessageHandler that routes outbound messages to channel plugins.
// The handler converts entity.Message -> dto.OutgoingMessage and delivers via the
// appropriate channel plugin. After successful send, the external message ID from
// the platform response is persisted to the store for reply threading.
// Errors and panics are caught per-message so one tenant's failure never blocks
// the bus subscriber.
func NewHandler(channels map[string]port.ChannelEntry, m *mapper.Mapper, store port.StorePlugin) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		slog.Debug("outbound: entity",
			"kind", msg.Kind,
			"role", msg.Role,
			"target_id", msg.TargetID,
			"caused_by_id", msg.CausedByID,
			"content_len", len(msg.Content),
			"tenant", msg.TenantID,
			"chat_id", msg.ChatID,
		)

		outgoing, err := m.ToOutgoing(ctx, msg)
		if err != nil {
			slog.Error("outbound: ToOutgoing failed",
				"err", err,
				"tenant", msg.TenantID,
			)
			return nil
		}

		slog.Debug("outbound: dto",
			"kind", outgoing.Kind,
			"target_id", outgoing.TargetID,
			"content_len", len(outgoing.Content),
			"channel", outgoing.ChannelID,
			"chat", outgoing.ChatID,
			"attachments", len(outgoing.Attachments),
		)

		entry, ok := channels[outgoing.ChannelID]
		if !ok {
			slog.Error("outbound: channel not found",
				"channel", outgoing.ChannelID,
				"tenant", msg.TenantID,
			)
			return nil
		}

		// Send with panic recovery for tenant isolation (TNNT-04).
		result, err := safeSend(ctx, entry.Plugin, outgoing)
		if err != nil {
			slog.Error("outbound: send failed",
				"err", err,
				"tenant", msg.TenantID,
				"channel", outgoing.ChannelID,
			)
			return nil // always nil — one tenant's failure never blocks the bus
		}

		// Persist the external message ID for reply threading (fire-and-forget).
		if result.MessageID != "" {
			if err := store.UpdateExternalMessageID(ctx, msg.ID, result.MessageID); err != nil {
				slog.Warn("outbound: save external message ID failed",
					"err", err,
					"msg_id", msg.ID,
					"external_id", result.MessageID,
				)
			}
		}

		return nil // always nil — one tenant's failure never blocks the bus
	}
}

// safeSend calls channel.Send inside a deferred recover to catch panics from
// channel plugin code. Returns the SendResult and any error (including recovered panics).
func safeSend(ctx context.Context, ch port.ChannelPlugin, msg dto.OutgoingMessage) (result port.SendResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in channel.Send: %v", r)
		}
	}()
	return ch.Send(ctx, msg)
}
