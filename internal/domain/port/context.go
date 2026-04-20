package port

import (
	"context"
	"sync"
)

// ModelOverride is a mutable holder for model override routes.
// Created per-message in the agent loop, writable by tools and middleware,
// readable by the router on each iteration.
type ModelOverride struct {
	mu     sync.Mutex
	values []string // "endpoint_id:model_name" entries
}

// Set replaces the override routes with the given endpoint:model strings.
func (o *ModelOverride) Set(endpoints []string) {
	o.mu.Lock()
	o.values = endpoints
	o.mu.Unlock()
}

// Get returns the current override routes.
// Returns (nil, false) if no override is set.
func (o *ModelOverride) Get() ([]string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.values) == 0 {
		return nil, false
	}
	return o.values, true
}

// Clear removes the override.
func (o *ModelOverride) Clear() {
	o.mu.Lock()
	o.values = nil
	o.mu.Unlock()
}

// ModelOverrideAware is an optional interface for plugins that need to set
// model overrides during execution. The holder is injected per-message.
type ModelOverrideAware interface {
	SetModelOverride(override *ModelOverride)
}

// modelOverrideKey is the context key for the model override holder.
type modelOverrideKey struct{}

// WithModelOverride returns a context carrying the given ModelOverride holder.
func WithModelOverride(ctx context.Context, override *ModelOverride) context.Context {
	return context.WithValue(ctx, modelOverrideKey{}, override)
}

// ModelOverrideFromCtx extracts the ModelOverride holder from context.
// Returns nil if not set.
func ModelOverrideFromCtx(ctx context.Context) *ModelOverride {
	v, _ := ctx.Value(modelOverrideKey{}).(*ModelOverride)
	return v
}
