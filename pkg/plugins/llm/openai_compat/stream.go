package openai_compat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Wire format types for streaming chunks (unexported)
// ---------------------------------------------------------------------------

type chatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *chunkUsage   `json:"usage,omitempty"`
}

type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type chunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []chunkToolCall `json:"tool_calls,omitempty"`
}

type chunkToolCall struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function chunkFunctionCall `json:"function"`
}

type chunkFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chunkUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ---------------------------------------------------------------------------
// Stream accumulator
// ---------------------------------------------------------------------------

type streamAccumulator struct {
	content      strings.Builder
	toolCalls    map[int]*entity.ToolCall
	finishReason string
	tokensIn     int
	tokensOut    int
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolCalls: make(map[int]*entity.ToolCall),
	}
}

func (a *streamAccumulator) processChunk(chunk chatCompletionChunk) {
	// Final usage-only chunk (no choices).
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			a.tokensIn = chunk.Usage.PromptTokens
			a.tokensOut = chunk.Usage.CompletionTokens
		}
		return
	}

	choice := chunk.Choices[0]

	if choice.FinishReason != "" {
		a.finishReason = choice.FinishReason
	}

	if choice.Delta.Content != "" {
		a.content.WriteString(choice.Delta.Content)
	}

	for _, tc := range choice.Delta.ToolCalls {
		existing, ok := a.toolCalls[tc.Index]
		if !ok {
			existing = &entity.ToolCall{}
			a.toolCalls[tc.Index] = existing
		}
		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Function.Name != "" {
			existing.Name = tc.Function.Name
		}
		existing.Arguments += tc.Function.Arguments
	}

	// Usage may also appear in a chunk with choices.
	if chunk.Usage != nil {
		a.tokensIn = chunk.Usage.PromptTokens
		a.tokensOut = chunk.Usage.CompletionTokens
	}
}

func (a *streamAccumulator) result() *port.CompletionResponse {
	resp := &port.CompletionResponse{
		Content:      a.content.String(),
		FinishReason: a.finishReason,
		TokensIn:     a.tokensIn,
		TokensOut:    a.tokensOut,
	}

	// Build sorted ToolCalls slice from map (iterate 0..len-1).
	for i := 0; i < len(a.toolCalls); i++ {
		tc, ok := a.toolCalls[i]
		if !ok {
			continue
		}
		if tc.Arguments == "" {
			tc.Arguments = "{}"
		}
		resp.ToolCalls = append(resp.ToolCalls, *tc)
	}

	return resp
}

// ---------------------------------------------------------------------------
// SSE stream parser
// ---------------------------------------------------------------------------

// parseSSEStream reads an SSE response body line by line, accumulating content
// and tool call arguments into a complete CompletionResponse.
func parseSSEStream(body io.Reader) (*port.CompletionResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1MB buffer

	acc := newStreamAccumulator()

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines (SSE event boundary).
		if line == "" {
			continue
		}

		// Stream termination.
		if line == "data: [DONE]" {
			break
		}

		// Only process data lines; skip comments, event types, etc.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		acc.processChunk(chunk)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	return acc.result(), nil
}
