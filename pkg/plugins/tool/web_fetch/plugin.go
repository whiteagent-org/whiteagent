// Package web_fetch implements a ToolPlugin that fetches a URL and returns its
// content with HTML tags stripped.
package web_fetch

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

//go:embed instructions.tmpl
var instructionsText string

const pluginID = "tool.web_fetch"

// Plugin implements port.ToolPlugin for fetching web pages.
type Plugin struct {
	userAgent string
	maxChars  int
	timeout   time.Duration
	client    *http.Client
}

// pluginConfig is the optional config passed to Init.
type pluginConfig struct {
	UserAgent string `json:"user_agent"`
	MaxChars  int    `json:"max_chars"`
	Timeout   string `json:"timeout"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new web_fetch tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return pluginID }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, _ string, raw json.RawMessage) error {
	p.userAgent = "whiteagent/1.0"
	p.maxChars = 5000
	p.timeout = 10 * time.Second

	if len(raw) > 0 && string(raw) != "null" {
		var cfg pluginConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("web_fetch: parse config: %w", err)
		}
		if cfg.UserAgent != "" {
			p.userAgent = cfg.UserAgent
		}
		if cfg.MaxChars > 0 {
			p.maxChars = cfg.MaxChars
		}
		if cfg.Timeout != "" {
			d, err := time.ParseDuration(cfg.Timeout)
			if err != nil {
				return fmt.Errorf("web_fetch: parse timeout: %w", err)
			}
			p.timeout = d
		}
	}

	p.client = &http.Client{Timeout: p.timeout}
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "web_fetch" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Fetches a web page by URL and returns its text content with HTML tags removed."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"The URL to fetch (must start with http:// or https://)."},"max_chars":{"type":"integer","description":"Maximum number of characters to return. Defaults to the configured limit."}},"required":["url"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

// fetchArgs is the expected JSON input for the web_fetch tool.
type fetchArgs struct {
	URL      string `json:"url"`
	MaxChars int    `json:"max_chars"`
}

// Regex patterns for HTML stripping.
var (
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reTags       = regexp.MustCompile(`<[^>]+>`)
	reWhitespace = regexp.MustCompile(`\s+`)
)

// Execute fetches the given URL and returns its text content.
func (p *Plugin) Execute(ctx context.Context, _ port.ToolContext, args json.RawMessage) (string, error) {
	log := logger.FromCtx(ctx)

	var a fetchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		log.Debug("web_fetch: invalid arguments", "error", err)
		return "error: invalid arguments: " + err.Error(), nil
	}

	log.Debug("web_fetch: executing", "url", a.URL, "max_chars", a.MaxChars)

	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		log.Debug("web_fetch: invalid URL scheme", "url", a.URL)
		return "error: URL must start with http:// or https://", nil
	}

	maxChars := p.maxChars
	if a.MaxChars > 0 {
		maxChars = a.MaxChars
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, a.URL, nil)
	if err != nil {
		log.Debug("web_fetch: request creation failed", "error", err)
		return "error: " + err.Error(), nil
	}
	req.Header.Set("User-Agent", p.userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		log.Debug("web_fetch: request failed", "url", a.URL, "error", err)
		return "error: " + err.Error(), nil
	}
	defer resp.Body.Close()

	log.Debug("web_fetch: response received", "url", a.URL, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("error: HTTP %d %s", resp.StatusCode, resp.Status), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debug("web_fetch: read body failed", "error", err)
		return "error: reading response: " + err.Error(), nil
	}

	text := string(body)
	contentType := resp.Header.Get("Content-Type")

	// JSON responses are returned as-is (truncated).
	if !strings.Contains(contentType, "application/json") {
		text = stripHTML(text)
	}

	if len(text) > maxChars {
		text = text[:maxChars]
	}

	log.Debug("web_fetch: done", "url", a.URL, "result_len", len(text), "truncated", len(text) == maxChars)

	return text, nil
}

// stripHTML removes script/style blocks, HTML tags, and collapses whitespace.
func stripHTML(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reTags.ReplaceAllString(s, " ")
	s = reWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
