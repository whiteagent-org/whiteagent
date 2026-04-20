// Package memory implements an in-memory pub/sub message bus as a TransportPlugin.
// It provides topic-based fan-out delivery with middleware pipeline support,
// per-topic buffered channels for async dispatch, and graceful drain on Stop.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

const (
	pluginID         = "transport.memory"
	defaultQueueSize = 256
)

// ErrBusStopped is returned by Publish after Stop has been called.
var ErrBusStopped = fmt.Errorf("%s: bus is stopped", pluginID)

// Compile-time interface assertions.
var (
	_ port.TransportPlugin = (*Plugin)(nil)
	_ port.MiddlewareAware = (*Plugin)(nil)
)

// config is the JSON shape expected in the plugin's config block.
type config struct {
	QueueSize  int      `json:"queue_size"`
	Middleware []string `json:"middleware"` // ordered middleware plugin IDs
}

// Plugin implements port.TransportPlugin and port.MiddlewareAware.
type Plugin struct {
	id           string
	cfg          config
	queues       map[string]chan entity.Message    // topic -> buffered channel
	subscribers  map[string][]port.MessageHandler  // topic -> wrapped handlers
	middleware   []port.MiddlewarePlugin
	middlewareIDs []string
	started      atomic.Bool
	stopped      atomic.Bool
	done         chan struct{} // closed when all dispatch goroutines exit
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTransport}
}

// NewPlugin returns a new bus plugin instance with initialized internal state.
func NewPlugin() port.Plugin {
	return &Plugin{
		id:          pluginID,
		queues:      make(map[string]chan entity.Message),
		subscribers: make(map[string][]port.MessageHandler),
		done:        make(chan struct{}),
	}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return p.id }

// Kind returns the plugin kind.
func (p *Plugin) Kind() entity.PluginKind { return entity.PluginKindTransport }

// Status returns the plugin health state.
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

// Init parses config, applies defaults, and creates per-topic buffered channels.
func (p *Plugin) Init(ctx context.Context, id string, raw json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	ctx = logger.WithComponent(ctx, pluginID)
	log := logger.FromCtx(ctx)

	if err := json.Unmarshal(raw, &p.cfg); err != nil {
		return fmt.Errorf("%s: parse config: %w", pluginID, err)
	}
	if p.cfg.QueueSize <= 0 {
		p.cfg.QueueSize = defaultQueueSize
	}
	p.middlewareIDs = p.cfg.Middleware

	// Pre-create per-topic buffered channels.
	p.queues[entity.TopicInbound] = make(chan entity.Message, p.cfg.QueueSize)
	p.queues[entity.TopicOutbound] = make(chan entity.Message, p.cfg.QueueSize)

	log.Info("bus plugin initialized", "queue_size", p.cfg.QueueSize, "middleware", p.cfg.Middleware)
	return nil
}

// Start launches one dispatch goroutine per topic and marks the bus as started.
// Subscribe, Unsubscribe, and SetMiddleware are rejected after Start.
func (p *Plugin) Start(ctx context.Context) error {
	if p.started.Load() {
		return fmt.Errorf("%s: already started", pluginID)
	}
	p.started.Store(true)

	var wg sync.WaitGroup
	for topic, ch := range p.queues {
		wg.Add(1)
		go p.dispatchLoop(ctx, topic, ch, &wg)
	}

	// Close done when all dispatch goroutines complete.
	go func() {
		wg.Wait()
		close(p.done)
	}()
	return nil
}

// Stop signals all dispatch goroutines to drain remaining messages and exit.
// After Stop, Publish returns ErrBusStopped. Respects ctx deadline for drain wait.
func (p *Plugin) Stop(ctx context.Context) error {
	if !p.stopped.CompareAndSwap(false, true) {
		return nil // already stopped
	}
	// Close all topic channels — dispatch goroutines drain remaining messages then exit.
	for _, ch := range p.queues {
		close(ch)
	}
	// Wait for drain or context deadline.
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Publish enqueues a message to the specified topic's buffered channel.
// Blocks when the queue is full but respects ctx cancellation.
func (p *Plugin) Publish(ctx context.Context, topic string, msg entity.Message) error {
	if p.stopped.Load() {
		return ErrBusStopped
	}
	ch, ok := p.queues[topic]
	if !ok {
		return fmt.Errorf("%s: unknown topic %q", pluginID, topic)
	}
	select {
	case ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscribe registers a handler for the given topic, wrapping it through the
// middleware chain. Must be called before Start.
func (p *Plugin) Subscribe(topic string, handler port.MessageHandler) error {
	if p.started.Load() {
		return fmt.Errorf("%s: cannot subscribe after Start()", pluginID)
	}
	if topic != entity.TopicInbound && topic != entity.TopicOutbound {
		return fmt.Errorf("%s: unknown topic %q", pluginID, topic)
	}
	// Wrap handler through middleware chain in reverse order so first middleware is outermost.
	wrapped := handler
	for i := len(p.middleware) - 1; i >= 0; i-- {
		mw := p.middleware[i]
		inner := mw.Wrap(wrapped)
		mwID := mw.ID()
		wrapped = func(ctx context.Context, msg entity.Message) error {
			slog.Debug("middleware.handle", "middleware", mwID, "topic", topic, "message_id", msg.ID)
			return inner(ctx, msg)
		}
	}
	p.subscribers[topic] = append(p.subscribers[topic], wrapped)
	return nil
}

// Unsubscribe removes a handler from the given topic. Must be called before Start.
// Callers must pass the same function value used in Subscribe (pointer equality).
func (p *Plugin) Unsubscribe(topic string, handler port.MessageHandler) error {
	if p.started.Load() {
		return fmt.Errorf("%s: cannot unsubscribe after Start()", pluginID)
	}
	if topic != entity.TopicInbound && topic != entity.TopicOutbound {
		return fmt.Errorf("%s: unknown topic %q", pluginID, topic)
	}
	subs := p.subscribers[topic]
	for i, h := range subs {
		if &h == &handler {
			p.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			return nil
		}
	}
	return nil
}

// SetMiddleware sets the middleware chain. Must be called before Start.
// Per the MiddlewareAware interface, this method has no error return.
func (p *Plugin) SetMiddleware(mws []port.MiddlewarePlugin) {
	if p.started.Load() {
		return // silently ignore after Start
	}
	p.middleware = mws
}

// MiddlewareIDs returns the ordered middleware plugin IDs from config.
func (p *Plugin) MiddlewareIDs() []string {
	return p.middlewareIDs
}

// dispatchLoop reads messages from a topic's channel and delivers to all
// subscribers sequentially. Recovers from panics in middleware/handlers.
func (p *Plugin) dispatchLoop(ctx context.Context, topic string, ch <-chan entity.Message, wg *sync.WaitGroup) {
	defer wg.Done()
	log := logger.FromCtx(ctx)
	for msg := range ch {
		for _, handler := range p.subscribers[topic] {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error("middleware panic recovered", "topic", topic, "panic", r, "stack", string(debug.Stack()))
					}
				}()
				if err := handler(ctx, msg); err != nil {
					log.Error("subscriber error", "topic", topic, "err", err)
				}
			}()
		}
	}
}
