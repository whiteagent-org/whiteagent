// Package web_search implements a ToolPlugin that searches the web using the
// Brave Search API and returns formatted results.
package web_search

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

//go:embed instructions.tmpl
var instructionsText string

const pluginID = "tool.web_search"

// Plugin implements port.ToolPlugin for web searching via Brave Search API.
type Plugin struct {
	apiKey  string
	timeout time.Duration
	client  *http.Client
}

// pluginConfig is the required config passed to Init.
type pluginConfig struct {
	APIKey  string `json:"api_key"`
	Timeout string `json:"timeout"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new web_search tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return pluginID }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, _ string, raw json.RawMessage) error {
	p.timeout = 10 * time.Second

	if len(raw) > 0 && string(raw) != "null" {
		var cfg pluginConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("web_search: parse config: %w", err)
		}
		p.apiKey = cfg.APIKey
		if cfg.Timeout != "" {
			d, err := time.ParseDuration(cfg.Timeout)
			if err != nil {
				return fmt.Errorf("web_search: parse timeout: %w", err)
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
func (p *Plugin) Name() string { return "web_search" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Searches the web and returns a list of results with titles, URLs, and descriptions."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The search query."},"count":{"type":"integer","description":"Number of results to return (1-10, default 5)."}},"required":["query"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

// searchArgs is the expected JSON input for the web_search tool.
type searchArgs struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

// braveResponse represents the relevant parts of the Brave Search API response.
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// Execute searches the web using the Brave Search API.
func (p *Plugin) Execute(ctx context.Context, _ port.ToolContext, args json.RawMessage) (string, error) {
	log := logger.FromCtx(ctx)

	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		log.Debug("web_search: invalid arguments", "error", err)
		return "error: invalid arguments: " + err.Error(), nil
	}

	log.Debug("web_search: executing", "query", a.Query, "count", a.Count)

	if strings.TrimSpace(a.Query) == "" {
		log.Debug("web_search: empty query")
		return "error: query must not be empty", nil
	}

	if p.apiKey == "" {
		log.Debug("web_search: missing api_key")
		return "error: web_search plugin is not configured (missing api_key)", nil
	}

	count := a.Count
	if count < 1 || count > 10 {
		count = 5
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	u := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(a.Query) + "&count=" + strconv.Itoa(count)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		log.Debug("web_search: request creation failed", "error", err)
		return "error: " + err.Error(), nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		log.Debug("web_search: request failed", "query", a.Query, "error", err)
		return "error: " + err.Error(), nil
	}
	defer resp.Body.Close()

	log.Debug("web_search: response received", "query", a.Query, "status", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("error: Brave API returned HTTP %d", resp.StatusCode), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debug("web_search: read body failed", "error", err)
		return "error: reading response: " + err.Error(), nil
	}

	var br braveResponse
	if err := json.Unmarshal(body, &br); err != nil {
		log.Debug("web_search: parse response failed", "error", err)
		return "error: parsing response: " + err.Error(), nil
	}

	log.Debug("web_search: done", "query", a.Query, "result_count", len(br.Web.Results))

	if len(br.Web.Results) == 0 {
		return "No results found.", nil
	}

	var sb strings.Builder
	for i, r := range br.Web.Results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n%s\n%s", i+1, r.Title, r.URL, r.Description))
	}

	return sb.String(), nil
}
