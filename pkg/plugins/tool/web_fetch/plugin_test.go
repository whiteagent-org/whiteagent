package web_fetch

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
	if p.ID() != "tool.web_fetch" {
		t.Fatalf("unexpected ID: %s", p.ID())
	}
	if p.Kind() != entity.PluginKindTool {
		t.Fatalf("unexpected Kind: %v", p.Kind())
	}
	if p.Name() != "web_fetch" {
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
	if p.userAgent != "whiteagent/1.0" {
		t.Fatalf("expected default user_agent, got %q", p.userAgent)
	}
	if p.maxChars != 5000 {
		t.Fatalf("expected default max_chars 5000, got %d", p.maxChars)
	}
	if p.timeout != 10*time.Second {
		t.Fatalf("expected default timeout 10s, got %v", p.timeout)
	}
}

func TestInitCustomConfig(t *testing.T) {
	cfg := json.RawMessage(`{"user_agent":"test-agent","max_chars":100,"timeout":"5s"}`)
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.userAgent != "test-agent" {
		t.Fatalf("expected user_agent test-agent, got %q", p.userAgent)
	}
	if p.maxChars != 100 {
		t.Fatalf("expected max_chars 100, got %d", p.maxChars)
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
// stripHTML
// ---------------------------------------------------------------------------

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"simple tags", "<p>hello</p>", "hello"},
		{"script block", `<script>var x=1;</script>hello`, "hello"},
		{"style block", `<style>.a{color:red}</style>hello`, "hello"},
		{"nested tags", `<div><span>hello</span> <b>world</b></div>`, "hello world"},
		{"whitespace collapse", "hello    \n\n   world", "hello world"},
		{"mixed content", `<html><head><style>body{}</style></head><body><h1>Title</h1><script>alert(1)</script><p>Content</p></body></html>`, "Title Content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.in)
			if got != tt.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

func initPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func TestExecuteInvalidArgs(t *testing.T) {
	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{bad`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.HasPrefix(result, "error:") {
		t.Fatalf("expected error string, got %q", result)
	}
}

func TestExecuteInvalidURL(t *testing.T) {
	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"ftp://example.com"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "http://") {
		t.Fatalf("expected URL scheme error, got %q", result)
	}
}

func TestExecuteHTMLResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "whiteagent/1.0" {
			t.Errorf("unexpected User-Agent: %s", ua)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Hello</h1><script>evil()</script><p>World</p></body></html>`))
	}))
	defer srv.Close()

	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "World") {
		t.Fatalf("expected stripped HTML content, got %q", result)
	}
	if strings.Contains(result, "<") {
		t.Fatalf("HTML tags should be stripped, got %q", result)
	}
	if strings.Contains(result, "evil") {
		t.Fatalf("script content should be stripped, got %q", result)
	}
}

func TestExecuteJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"key":"value"}`))
	}))
	defer srv.Close()

	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"key":"value"}` {
		t.Fatalf("JSON should be returned as-is, got %q", result)
	}
}

func TestExecuteTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", 200)))
	}))
	defer srv.Close()

	p := NewPlugin().(*Plugin)
	cfg := json.RawMessage(`{"max_chars":50}`)
	if err := p.Init(context.Background(), "", cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 50 {
		t.Fatalf("expected 50 chars, got %d", len(result))
	}
}

func TestExecuteMaxCharsOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("b", 200)))
	}))
	defer srv.Close()

	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`","max_chars":30}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 30 {
		t.Fatalf("expected 30 chars, got %d", len(result))
	}
}

func TestExecuteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := initPlugin(t)
	result, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(result, "404") {
		t.Fatalf("expected 404 in error, got %q", result)
	}
}

func TestExecuteCustomUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := NewPlugin().(*Plugin)
	cfg := json.RawMessage(`{"user_agent":"custom/2.0"}`)
	if err := p.Init(context.Background(), "", cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if gotUA != "custom/2.0" {
		t.Fatalf("expected User-Agent custom/2.0, got %q", gotUA)
	}
}
