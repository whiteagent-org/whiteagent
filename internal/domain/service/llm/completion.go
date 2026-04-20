package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// CompletionService routes LLM completion requests through a failover chain
// with cool-down tracking per endpoint+model.
type CompletionService interface {
	Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error)
}

// completionService implements CompletionService with cool-down tracking.
type completionService struct {
	router    ModelRouter
	plugins   map[string]port.LLMPlugin // bare endpoint ID -> plugin
	cooldown  map[string]time.Time      // "endpoint:model" -> earliest retry
	mu        sync.Mutex
	defaultCD time.Duration
}

// NewCompletionService creates a CompletionService that routes through the given
// ModelRouter, uses the plugin map for actual LLM calls, and applies the specified
// cool-down duration after provider errors.
func NewCompletionService(router ModelRouter, plugins map[string]port.LLMPlugin, cooldownDuration time.Duration) CompletionService {
	return &completionService{
		router:    router,
		plugins:   plugins,
		cooldown:  make(map[string]time.Time),
		defaultCD: cooldownDuration,
	}
}

// Complete iterates the routing chain, skipping cooled-down endpoints, and returns
// the first successful response. On provider error, the endpoint+model is cooled down
// using RetryAfter from ProviderError (or the default duration).
func (s *completionService) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	chain := s.router.Select(ctx)
	if req.Model != "" {
		if sel, err := parseRoute(req.Model); err == nil {
			chain = []RouteSelection{sel}
			req.Model = sel.Model
		}
	}

	for _, sel := range chain {
		model := sel.Model
		if req.Model != "" {
			model = req.Model
		}
		key := sel.EndpointID + ":" + model

		if s.isCooledDown(key) {
			slog.Warn("llm.completion.skipping_cooled_down", "endpoint", sel.EndpointID, "model", model)
			continue
		}

		plugin, ok := s.plugins[sel.EndpointID]
		if !ok {
			slog.Error("llm.completion.endpoint_not_found", "endpoint", sel.EndpointID)
			continue
		}

		slog.Info("llm.completion.calling", "endpoint", sel.EndpointID, "model", model)

		req.Model = model
		req.Stream = true

		resp, err := plugin.Complete(ctx, req)
		if err != nil {
			// Context cancelled (timeout) — no point trying more providers.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("context cancelled during LLM call to %s:%s: %w", sel.EndpointID, sel.Model, err)
			}

			slog.Warn("llm.completion.failover", "endpoint", sel.EndpointID, "model", model, "err", err)

			// Only cooldown on explicit provider errors (HTTP error responses).
			// Transient errors (network, stream interruption) should not prevent
			// the provider from being tried on the next request.
			var pe *port.ProviderError
			if errors.As(err, &pe) {
				s.setCoolDown(key, pe.RetryAfter)
			}
			continue
		}

		resp.EndpointID = sel.EndpointID
		resp.Model = model
		return resp, nil
	}

	return nil, fmt.Errorf("all LLM providers exhausted: tried %d endpoints, all failed or cooled down", len(chain))
}

// isCooledDown checks whether the given endpoint+model key is still in cool-down.
// Expired entries are lazily evicted.
func (s *completionService) isCooledDown(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.cooldown[key]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(s.cooldown, key)
		return false
	}
	return true
}

// setCoolDown marks the given endpoint+model key as cooled down. If retryAfter
// is zero, the default cool-down duration is used.
func (s *completionService) setCoolDown(key string, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = s.defaultCD
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cooldown[key] = time.Now().Add(retryAfter)
}
