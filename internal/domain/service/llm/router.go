// Package llm provides model routing and completion with failover for LLM calls.
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// RouteSelection pairs an endpoint ID with a model name.
type RouteSelection struct {
	EndpointID string
	Model      string
}

// ModelRouter selects the ordered chain of endpoint+model pairs to try.
type ModelRouter interface {
	Select(ctx context.Context) []RouteSelection
}

// router is a stateless ModelRouter backed by a precomputed chain.
type router struct {
	chain []RouteSelection
}

// NewRouter creates a ModelRouter from a primary "endpoint_id:model_name" string
// and optional fallback entries in the same format.
func NewRouter(primary string, fallbacks []string) (ModelRouter, error) {
	sel, err := parseRoute(primary)
	if err != nil {
		return nil, fmt.Errorf("parse primary route %q: %w", primary, err)
	}

	chain := make([]RouteSelection, 0, 1+len(fallbacks))
	chain = append(chain, sel)

	for i, fb := range fallbacks {
		sel, err := parseRoute(fb)
		if err != nil {
			return nil, fmt.Errorf("parse fallback[%d] route %q: %w", i, fb, err)
		}
		chain = append(chain, sel)
	}

	return &router{chain: chain}, nil
}

// Select returns the routing chain, optionally prepending overrides from the
// ModelOverride holder in context.
func (r *router) Select(ctx context.Context) []RouteSelection {
	holder := port.ModelOverrideFromCtx(ctx)
	if holder == nil {
		return r.chain
	}

	overrides, ok := holder.Get()
	if !ok {
		return r.chain
	}

	// Prepend valid overrides to a copy of the chain.
	result := make([]RouteSelection, 0, len(overrides)+len(r.chain))
	for _, o := range overrides {
		sel, err := parseRoute(o)
		if err != nil {
			slog.Error("llm.router.invalid_override", "override", o, "err", err)
			continue
		}
		result = append(result, sel)
	}
	result = append(result, r.chain...)
	return result
}

// parseRoute splits "endpoint_id:model_name" into a RouteSelection.
func parseRoute(s string) (RouteSelection, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return RouteSelection{}, fmt.Errorf("must be \"endpoint_id:model_name\", got %q", s)
	}
	return RouteSelection{EndpointID: parts[0], Model: parts[1]}, nil
}
