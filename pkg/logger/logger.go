// Package logger provides a thin wrapper around log/slog with context propagation.
// It is placed under pkg/ so downstream packages (including plugins) can import it
// without creating import cycles.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// loggerKey is an unexported type used as the context key for the logger.
// Using a zero-value struct ensures uniqueness — no other package can construct
// this key without importing the logger package.
type loggerKey struct{}

// Init initialises the global default slog logger with JSON output to stderr.
// Must be called once at startup before any other logger usage.
// Calling Init twice replaces the global logger (used for two-phase init in main).
func Init(level slog.Level) {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(h))
}

// ParseLevel parses a log level string into slog.Level.
// Accepts "DEBUG", "INFO", "WARN", "ERROR" (case-insensitive).
// Returns slog.LevelInfo and an error for unknown values.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo, fmt.Errorf("invalid log level %q: %w", s, err)
	}
	return l, nil
}

// WithLogger stores the given logger in ctx and returns the new context.
// Downstream code retrieves it via FromCtx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// FromCtx retrieves the logger stored in ctx.
// Falls back to slog.Default() if no logger is stored — this means components
// work correctly in tests and early startup without explicit initialisation.
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithTenantID attaches tenant_id to the context logger and stores the enriched
// logger back in ctx. Downstream components call logger.FromCtx(ctx) to retrieve
// a logger that already carries the tenant_id attribute.
func WithTenantID(ctx context.Context, tenantID entity.TenantID) context.Context {
	l := FromCtx(ctx).With("tenant_id", tenantID.String())
	return WithLogger(ctx, l)
}

// WithComponent attaches a component name to the context logger.
// Use this at the entry point of each infrastructure component (e.g., plugin Init).
func WithComponent(ctx context.Context, component string) context.Context {
	l := FromCtx(ctx).With("component", component)
	return WithLogger(ctx, l)
}
