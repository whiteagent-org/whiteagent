package web_search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Manifest / metadata
// ---------------------------------------------------------------------------

func TestManifest(t *testing.T) {
	m := Manifest()
	if m.Kind != entity.PluginKindTool {
		t.Fatalf("expected PluginKindTool, got %v", m.Kind)
	}
}

func TestPluginMetadata(t *testing.T) {
	p := NewPlugin().(*Plugin)
	if p.ID() != "tool.web_search" {
		t.Fatalf("unexpected ID: %s", p.ID())
	}
	if p.Kind() != entity.PluginKindTool {
		t.Fatalf("unexpected Kind: %v", p.Kind())
	}
	if p.Name() != "web_search" {
		t.Fatalf("unexpected Name: %s", p.Name())
	}
	if p.Status() != entity.PluginStateHealthy {
		t.Fatalf("unexpected Status: %v", p.Status())
	}
	if p.Description() == "" {
		t.Fatal("Description should not be empty")
	}
	if p.Instructions() == "" {
		t.Fatal("Instructions should not be empty")
	}

	var schema map[string]any
	if err := json.Unmarshal(p.Parameters(), &schema); err != nil {
		t.Fatalf("Parameters is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestInitDefaults(t *testing.T) {
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", nil); err != nil {
		t.Fatalf("Init with nil config: %v", err)
	}
	if p.timeout != 10*time.Second {
		t.Fatalf("expected default timeout 10s, got %v", p.timeout)
	}
	if p.apiKey != "" {
		t.Fatalf("expected empty api_key, got %q", p.apiKey)
	}
}

func TestInitCustomConfig(t *testing.T) {
	cfg := json.RawMessage(`{"api_key":"test-key","timeout":"5s"}`)
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.apiKey != "test-key" {
		t.Fatalf("expected api_key test-key, got %q", p.apiKey)
	}
	if p.timeout != 5*time.Second {
		t.Fatalf("expected timeout 5s, got %v", p.timeout)
	}
}

func TestInitInvalidJSON(t *testing.T) {
	p := NewPlugin().(*Plugin)
	err := p.Init(context.Background(), "", json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON config")
	}
}

func TestInitInvalidTimeout(t *testing.T) {
	p := NewPlugin().(*Plugin)
	err := p.Init(context.Background(), "", json.RawMessage(`{"timeout":"nope"}`))
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func initPlugin(t *testing.T, apiKey string) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	cfg := json.RawMessage(`{"api_key":"` + apiKey + `"}`)
	if err := p.Init(context.Background(), "", cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func TestExecuteInvalidArgs(t *testing.T) {
	p := initPlugin(t, "key")
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{bad`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Fatalf("expected error string, got %q", result)
	}
}

func TestExecuteEmptyQuery(t *testing.T) {
	p := initPlugin(t, "key")
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"query":""}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "empty") {
		t.Fatalf("expected empty query error, got %q", result)
	}
}

func TestExecuteWhitespaceQuery(t *testing.T) {
	p := initPlugin(t, "key")
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"query":"   "}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "empty") {
		t.Fatalf("expected empty query error, got %q", result)
	}
}

func TestExecuteMissingAPIKey(t *testing.T) {
	p := NewPlugin().(*Plugin)
	p.Init(context.Background(), "", nil)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "api_key") {
		t.Fatalf("expected missing api_key error, got %q", result)
	}
}

func TestExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Errorf("unexpected token: %s", r.Header.Get("X-Subscription-Token"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected Accept: %s", r.Header.Get("Accept"))
		}
		q := r.URL.Query().Get("q")
		if q != "golang testing" {
			t.Errorf("unexpected query: %s", q)
		}
		count := r.URL.Query().Get("count")
		if count != "3" {
			t.Errorf("unexpected count: %s", count)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[
			{"title":"Result One","url":"https://one.com","description":"First result"},
			{"title":"Result Two","url":"https://two.com","description":"Second result"}
		]}}`))
	}))
	defer srv.Close()

	p := initPlugin(t, "test-key")
	// Point the plugin's client at the test server by overriding the API URL.
	// Since the URL is hardcoded, we override the client transport instead.
	origTransport := p.client.Transport
	p.client.Transport = rewriteTransport{target: srv.URL, wrap: origTransport}

	result, err := p.Execute(context.Background(), port.ToolContext{},
		json.RawMessage(`{"query":"golang testing","count":3}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1. Result One") {
		t.Fatalf("expected numbered result, got %q", result)
	}
	if !strings.Contains(result, "https://one.com") {
		t.Fatalf("expected URL in result, got %q", result)
	}
	if !strings.Contains(result, "2. Result Two") {
		t.Fatalf("expected second result, got %q", result)
	}
}

func TestExecuteNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	p := initPlugin(t, "test-key")
	p.client.Transport = rewriteTransport{target: srv.URL}

	result, err := p.Execute(context.Background(), port.ToolContext{},
		json.RawMessage(`{"query":"obscure query"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No results found." {
		t.Fatalf("expected no results message, got %q", result)
	}
}

func TestExecuteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := initPlugin(t, "test-key")
	p.client.Transport = rewriteTransport{target: srv.URL}

	result, err := p.Execute(context.Background(), port.ToolContext{},
		json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "429") {
		t.Fatalf("expected 429 in error, got %q", result)
	}
}

func TestExecuteDefaultCount(t *testing.T) {
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	p := initPlugin(t, "key")
	p.client.Transport = rewriteTransport{target: srv.URL}

	p.Execute(context.Background(), port.ToolContext{},
		json.RawMessage(`{"query":"test"}`))
	if gotCount != "5" {
		t.Fatalf("expected default count 5, got %s", gotCount)
	}
}

func TestExecuteCountClamped(t *testing.T) {
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	p := initPlugin(t, "key")
	p.client.Transport = rewriteTransport{target: srv.URL}

	// count=20 exceeds max, should be clamped to default 5
	p.Execute(context.Background(), port.ToolContext{},
		json.RawMessage(`{"query":"test","count":20}`))
	if gotCount != "5" {
		t.Fatalf("expected clamped count 5, got %s", gotCount)
	}
}

// rewriteTransport redirects all requests to the test server.
type rewriteTransport struct {
	target string
	wrap   http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.target, "http://")
	transport := rt.wrap
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(req)
}
