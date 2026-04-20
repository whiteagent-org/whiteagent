package port

import (
	"context"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// MessageHandler is the callback used by TransportPlugin for internal bus routing.
// Uses entity.Message (not dto.IncomingMessage) so that transport middleware,
// the agent loop, and future phases all work with the full domain type
// including Role and ToolCalls fields.
// Channel plugins use IncomingMessageHandler (in plugin_channel.go) instead;
// the runtime identity resolver bridges IncomingMessage -> entity.Message.
// Function type (not channel) for .so plugin boundary compatibility.
type MessageHandler func(ctx context.Context, msg entity.Message) error

// TransportPlugin provides topic-based publish/subscribe for internal message routing.
// Decouples channel plugins from agent processing. (PLUG-02, PLUG-03)
type TransportPlugin interface {
	Plugin
	Publish(ctx context.Context, topic string, msg entity.Message) error
	Subscribe(topic string, handler MessageHandler) error
	Unsubscribe(topic string, handler MessageHandler) error
}

// MiddlewareAware is an optional interface for transport plugins that accept
// middleware injection. The runtime type-asserts the transport plugin to this
// interface during wiring. Not part of TransportPlugin so non-middleware-aware
// transports remain valid.
type MiddlewareAware interface {
	SetMiddleware(mws []MiddlewarePlugin)
	MiddlewareIDs() []string
}
