package port

// MiddlewarePlugin wraps MessageHandler callbacks for cross-cutting concerns.
// Applied by the runtime to both transport subscribers and channel inbound handlers.
// Registration order is determined by plugin config order (no Order() method needed).
type MiddlewarePlugin interface {
	Plugin
	Wrap(next MessageHandler) MessageHandler
}
