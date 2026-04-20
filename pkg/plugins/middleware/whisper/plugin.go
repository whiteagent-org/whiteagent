// Package whisper implements a middleware plugin that converts voice and
// video_note attachments to text using an OpenAI-compatible STT API.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

const pluginID = "middleware.whisper"

// pluginConfig holds the STT API configuration.
type pluginConfig struct {
	APIBase string `json:"api_base"` // e.g., "https://api.openai.com/v1"
	APIKey  string `json:"api_key"`  // e.g., "env:OPENAI_API_KEY"
	Model   string `json:"model"`    // e.g., "whisper-1", default "whisper-1"
}

// Plugin implements port.MiddlewarePlugin for speech-to-text via Whisper API.
type Plugin struct {
	id     string
	config pluginConfig
	client *http.Client
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindMiddleware}
}

// NewPlugin creates a new whisper middleware plugin instance.
func NewPlugin() port.Plugin { return &Plugin{id: pluginID} }

func (p *Plugin) ID() string             { return p.id }
func (p *Plugin) Kind() entity.PluginKind { return entity.PluginKindMiddleware }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

// Init unmarshals config, validates required fields, and creates the HTTP client.
func (p *Plugin) Init(_ context.Context, id string, cfg json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	if err := json.Unmarshal(cfg, &p.config); err != nil {
		return fmt.Errorf("%s: unmarshal config: %w", pluginID, err)
	}
	if p.config.APIBase == "" {
		return fmt.Errorf("%s: api_base is required", pluginID)
	}
	if p.config.APIKey == "" {
		return fmt.Errorf("%s: api_key is required", pluginID)
	}
	if p.config.Model == "" {
		p.config.Model = "whisper-1"
	}
	p.client = &http.Client{Timeout: 30 * time.Second}
	return nil
}

// Start is a no-op.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// Wrap returns a MessageHandler that transcribes voice/video_note attachments
// before passing the message to the next handler.
func (p *Plugin) Wrap(next port.MessageHandler) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		if !hasTranscribable(msg.Attachments) {
			return next(ctx, msg)
		}

		log := logger.FromCtx(ctx)
		var kept []entity.Attachment

		for _, att := range msg.Attachments {
			if att.Kind != "voice" && att.Kind != "video_note" {
				kept = append(kept, att)
				continue
			}

			text, err := p.transcribe(ctx, att)
			if err != nil {
				log.Warn("transcription failed",
					"plugin", pluginID,
					"kind", att.Kind,
					"path", att.Path,
					"error", err,
				)
				// Keep attachment and append failure notice.
				kept = append(kept, att)
				msg.Content = appendContent(msg.Content, "[transcription failed]")
				continue
			}

			// Success: append transcribed text, drop the attachment.
			msg.Content = appendContent(msg.Content, text)
		}

		msg.Attachments = kept
		return next(ctx, msg)
	}
}

// hasTranscribable returns true if any attachment is voice or video_note.
func hasTranscribable(attachments []entity.Attachment) bool {
	for _, a := range attachments {
		if a.Kind == "voice" || a.Kind == "video_note" {
			return true
		}
	}
	return false
}

// appendContent joins two strings with a newline separator if both are non-empty.
func appendContent(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "\n" + addition
}

// sttResponse is the JSON response from the OpenAI-compatible STT API.
type sttResponse struct {
	Text string `json:"text"`
}

// transcribe sends an audio file to the STT API and returns the transcribed text.
func (p *Plugin) transcribe(ctx context.Context, att entity.Attachment) (string, error) {
	f, err := os.Open(att.Path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Build multipart form.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	filename := att.Filename
	if filename == "" {
		filename = filepath.Base(att.Path)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("copy file data: %w", err)
	}
	if err := writer.WriteField("model", p.config.Model); err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	url := strings.TrimRight(p.config.APIBase, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("STT API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result sttResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.Text, nil
}
