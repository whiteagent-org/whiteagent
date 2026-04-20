// Package openai_compat implements an OpenAI-compatible LLM plugin.
// It works with any OpenAI-compatible API: OpenAI, Ollama, OpenRouter, etc.
// Uses raw net/http -- no external SDK.
package openai_compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Wire format types (unexported)
// ---------------------------------------------------------------------------

type chatCompletionRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Tools         []chatTool     `json:"tools,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ---------------------------------------------------------------------------
// Plugin config
// ---------------------------------------------------------------------------

type config struct {
	EndpointID string `json:"endpoint_id"`
	APIBase    string `json:"api_base"`
	APIKey     string `json:"api_key"`
}

// ---------------------------------------------------------------------------
// Plugin
// ---------------------------------------------------------------------------

// Plugin implements port.LLMPlugin for OpenAI-compatible APIs.
type Plugin struct {
	cfg    config
	client *http.Client
	id     string
}

// Manifest returns the plugin manifest for pre-instantiation validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{
		Kind: entity.PluginKindLLM,
	}
}

// NewPlugin creates a new Plugin instance.
func NewPlugin() port.Plugin {
	return &Plugin{
		id: "llm.openai_compat",
	}
}

func (p *Plugin) ID() string             { return p.id }
func (p *Plugin) Kind() entity.PluginKind { return entity.PluginKindLLM }

// Init parses endpoint config and creates an HTTP client.
func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	if err := json.Unmarshal(raw, &p.cfg); err != nil {
		return fmt.Errorf("openai_compat: parse config: %w", err)
	}
	if p.cfg.APIBase == "" {
		return fmt.Errorf("openai_compat: api_base is required")
	}
	// Override plugin ID from endpoint_id if provided.
	if p.cfg.EndpointID != "" {
		p.id = "llm." + p.cfg.EndpointID
	}
	p.client = &http.Client{Timeout: 120 * time.Second}
	return nil
}

func (p *Plugin) Start(context.Context) error        { return nil }
func (p *Plugin) Stop(context.Context) error         { return nil }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

// Complete sends a completion request to the OpenAI-compatible API and returns
// the accumulated response. Streaming is always enabled internally.
func (p *Plugin) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("openai_compat: build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.APIBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai_compat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai_compat: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		pe := &port.ProviderError{
			StatusCode: resp.StatusCode,
			Message:    string(snippet),
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					pe.RetryAfter = time.Duration(secs) * time.Second
				} else if t, err := time.Parse(http.TimeFormat, ra); err == nil {
					pe.RetryAfter = time.Until(t)
				}
			}
		}
		return nil, pe
	}

	return parseSSEStream(resp.Body)
}

// buildRequestBody converts domain types to the OpenAI wire format.
func (p *Plugin) buildRequestBody(req port.CompletionRequest) ([]byte, error) {
	wireReq := chatCompletionRequest{
		Model:         req.Model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	// Convert messages.
	for _, msg := range req.Messages {
		cm := chatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
		// Assistant turns with tool calls.
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: chatFunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		// Tool result turns.
		if msg.ToolCallID != "" {
			cm.ToolCallID = msg.ToolCallID
			cm.Name = msg.ToolName
		}
		wireReq.Messages = append(wireReq.Messages, cm)
	}

	// Convert tool definitions.
	for _, td := range req.Tools {
		wireReq.Tools = append(wireReq.Tools, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.Parameters,
			},
		})
	}

	return json.Marshal(wireReq)
}
