package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stubPlugin is a test double for port.LLMPlugin.
type stubPlugin struct {
	completeFunc func(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error)
	calls        int
}

func (s *stubPlugin) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	s.calls++
	return s.completeFunc(ctx, req)
}

func (s *stubPlugin) ID() string                                                { return "stub" }
func (s *stubPlugin) Kind() entity.PluginKind                                   { return entity.PluginKindLLM }
func (s *stubPlugin) Init(_ context.Context, _ string, _ json.RawMessage) error { return nil }
func (s *stubPlugin) Start(_ context.Context) error                             { return nil }
func (s *stubPlugin) Stop(_ context.Context) error                              { return nil }
func (s *stubPlugin) Status() entity.PluginState                                { return entity.PluginStateHealthy }

// staticRouter returns a fixed chain.
type staticRouter struct {
	chain []RouteSelection
}

func (r *staticRouter) Select(_ context.Context) []RouteSelection { return r.chain }

func okResponse() (*port.CompletionResponse, error) {
	return &port.CompletionResponse{Content: "ok"}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFallbackOnProviderError(t *testing.T) {
	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return nil, &port.ProviderError{StatusCode: 500, Message: "internal"}
	}}
	fallback := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
			{EndpointID: "b", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": primary, "b": fallback},
		30*time.Second,
	)

	resp, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("expected 1 call each, got primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestFallbackOnTransientError(t *testing.T) {
	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return nil, errors.New("connection reset")
	}}
	fallback := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
			{EndpointID: "b", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": primary, "b": fallback},
		30*time.Second,
	)

	resp, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
}

func TestNoCooldownOnTransientError(t *testing.T) {
	callCount := 0
	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("connection reset")
		}
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": primary},
		30*time.Second,
	)

	// First call: transient error, no fallback available → exhausted.
	_, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when single provider fails")
	}

	// Second call (immediate): provider should NOT be cooled down.
	resp, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected success on retry, got error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
}

func TestCooldownOnProviderError(t *testing.T) {
	callCount := 0
	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		callCount++
		if callCount == 1 {
			return nil, &port.ProviderError{StatusCode: 429, Message: "rate limited"}
		}
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": primary},
		30*time.Second,
	)

	// First call: provider error → cooled down.
	_, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when single provider fails")
	}

	// Second call (immediate): provider should be cooled down → exhausted without call.
	_, err = svc.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when provider is cooled down")
	}
	if callCount != 1 {
		t.Fatalf("expected provider called once (cooled down on second), got %d", callCount)
	}
}

func TestContextCancellationBreaksIteration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		cancel() // simulate timeout during call
		return nil, context.Canceled
	}}
	fallback := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
			{EndpointID: "b", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": primary, "b": fallback},
		30*time.Second,
	)

	_, err := svc.Complete(ctx, port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	// Fallback should NOT have been called.
	if fallback.calls != 0 {
		t.Fatalf("expected fallback not called on context cancel, got %d calls", fallback.calls)
	}
}

func TestAllProvidersExhausted(t *testing.T) {
	fail := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		return nil, &port.ProviderError{StatusCode: 503, Message: "unavailable"}
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "m"},
			{EndpointID: "b", Model: "m"},
		}},
		map[string]port.LLMPlugin{"a": fail, "b": fail},
		30*time.Second,
	)

	_, err := svc.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !errors.Is(err, err) { // just check it's non-nil
		t.Fatal("unexpected")
	}
}

func TestExplicitModelOverridesRouteModel(t *testing.T) {
	var capturedReq port.CompletionRequest
	plugin := &stubPlugin{completeFunc: func(_ context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
		capturedReq = req
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "route-model"},
		}},
		map[string]port.LLMPlugin{"a": plugin},
		30*time.Second,
	)

	resp, err := svc.Complete(context.Background(), port.CompletionRequest{Model: "override-model"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedReq.Model != "override-model" {
		t.Fatalf("plugin model = %q, want %q", capturedReq.Model, "override-model")
	}
	if resp.Model != "override-model" {
		t.Fatalf("response model = %q, want %q", resp.Model, "override-model")
	}
}

func TestExplicitRoutedModelTargetsSpecifiedEndpoint(t *testing.T) {
	var (
		primaryCalled  bool
		fallbackCalled bool
		capturedReq    port.CompletionRequest
	)
	primary := &stubPlugin{completeFunc: func(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
		primaryCalled = true
		return nil, &port.ProviderError{StatusCode: 500, Message: "should not be called"}
	}}
	fallback := &stubPlugin{completeFunc: func(_ context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
		fallbackCalled = true
		capturedReq = req
		return okResponse()
	}}

	svc := NewCompletionService(
		&staticRouter{chain: []RouteSelection{
			{EndpointID: "a", Model: "route-model"},
			{EndpointID: "b", Model: "fallback-model"},
		}},
		map[string]port.LLMPlugin{"a": primary, "b": fallback},
		30*time.Second,
	)

	resp, err := svc.Complete(context.Background(), port.CompletionRequest{Model: "b:override-model"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if primaryCalled {
		t.Fatal("primary endpoint should not be called for routed model override")
	}
	if !fallbackCalled {
		t.Fatal("expected specified endpoint to be called")
	}
	if capturedReq.Model != "override-model" {
		t.Fatalf("plugin model = %q, want %q", capturedReq.Model, "override-model")
	}
	if resp.EndpointID != "b" {
		t.Fatalf("response endpoint = %q, want %q", resp.EndpointID, "b")
	}
	if resp.Model != "override-model" {
		t.Fatalf("response model = %q, want %q", resp.Model, "override-model")
	}
}
