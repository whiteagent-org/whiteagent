package port

import (
	"context"
	"net/http"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
)

// SendResult carries the platform message ID and timestamp returned by a
// successful channel Send. Used by the outbound handler to persist the
// external message ID for reply threading and deduplication.
type SendResult struct {
	MessageID string
	Timestamp time.Time
}

// IncomingMessageHandler is the callback type for channel plugins.
// Channel plugins call this when a user message arrives. The runtime wires
// identity resolution and message mapping between this handler and the bus.
type IncomingMessageHandler func(ctx context.Context, msg dto.IncomingMessage) error

// IndicatorAware is an optional interface for channel plugins that support
// typing/activity indicators. The agent loop calls Indicate before processing;
// the returned stop function is deferred to guarantee cleanup on all exit paths.
type IndicatorAware interface {
	Indicate(ctx context.Context, indication map[string]string) (stop func())
}

// ReactionAware is a marker interface for channel plugins that support emoji reactions.
type ReactionAware interface {
	SupportsReactions() // marker method
}

// ChannelCapabilities holds resolved capabilities for a channel plugin.
// Fields are set once at startup from interface checks.
type ChannelCapabilities struct {
	Indication bool // true if channel implements IndicatorAware
	Reactions  bool // true if channel implements ReactionAware
}

// ChannelEntry pairs a channel plugin with its resolved capabilities.
type ChannelEntry struct {
	Plugin       ChannelPlugin
	Capabilities ChannelCapabilities
}

// ChannelPlugin ingests messages from external platforms and delivers responses.
// The runtime provides the inbound handler via SetMessageHandler; the channel
// calls it when a user message arrives. Send uses msg.TenantID from OutgoingMessage.
// (PLUG-02, PLUG-03, CHAN-04)
type ChannelPlugin interface {
	Plugin
	SetMessageHandler(handler IncomingMessageHandler)
	Send(ctx context.Context, msg dto.OutgoingMessage) (SendResult, error)
	RegisterRoutes(mux *http.ServeMux)
}
