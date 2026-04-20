package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

const defaultBaseURL = "https://api.telegram.org"

// apiClient is a raw Telegram Bot API client.
type apiClient struct {
	baseURL    string
	token      string
	log        *slog.Logger
	httpClient *http.Client
}

// newAPIClient creates a new API client with a 45-second timeout
// (sufficient for long-polling getUpdates with 30s timeout).
func newAPIClient(token string, log *slog.Logger) *apiClient {
	return &apiClient{
		baseURL: defaultBaseURL,
		token:   token,
		log:     log,
		httpClient: &http.Client{
			Timeout: 0, // No global timeout — per-request context controls cancellation
		},
	}
}

// call sends a JSON POST to the Telegram Bot API.
// Logs and returns an error on 429 (rate limit) — no automatic retry per CONTEXT.md.
func (c *apiClient) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)

	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("telegram api: marshal params: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telegram api: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram api %s: %w", method, err)
	}
	defer resp.Body.Close()

	var apiResp tgApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("telegram api %s: decode response: %w", method, err)
	}

	if apiResp.ErrorCode == 429 {
		c.log.Warn("telegram api: rate limited", "method", method, "description", apiResp.Description)
		return nil, fmt.Errorf("telegram api %s: rate limited: %s", method, apiResp.Description)
	}

	if !apiResp.OK {
		return nil, fmt.Errorf("telegram api %s: error %d: %s", method, apiResp.ErrorCode, apiResp.Description)
	}

	return apiResp.Result, nil
}

// uploadFile sends a multipart POST for file uploads (sendPhoto, sendVoice, etc.).
// All params are added as form fields; the file at filePath is attached under fieldName.
func (c *apiClient) uploadFile(ctx context.Context, method string, params map[string]any, fieldName, filePath string) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Add all params as form fields.
	for k, v := range params {
		if err := w.WriteField(k, fmt.Sprintf("%v", v)); err != nil {
			return nil, fmt.Errorf("telegram api: write field %s: %w", k, err)
		}
	}

	// Attach file.
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("telegram api: open file %s: %w", filePath, err)
	}
	defer f.Close()

	part, err := w.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("telegram api: create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("telegram api: copy file: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("telegram api: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("telegram api: create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram api %s: %w", method, err)
	}
	defer resp.Body.Close()

	var apiResp tgApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("telegram api %s: decode response: %w", method, err)
	}

	if apiResp.ErrorCode == 429 {
		c.log.Warn("telegram api: rate limited", "method", method, "description", apiResp.Description)
		return nil, fmt.Errorf("telegram api %s: rate limited: %s", method, apiResp.Description)
	}

	if !apiResp.OK {
		return nil, fmt.Errorf("telegram api %s: error %d: %s", method, apiResp.ErrorCode, apiResp.Description)
	}

	return apiResp.Result, nil
}

// getMe calls the Telegram getMe endpoint and returns the bot user info.
func (c *apiClient) getMe(ctx context.Context) (*tgUser, error) {
	result, err := c.call(ctx, "getMe", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("telegram api getMe: %w", err)
	}
	var user tgUser
	if err := json.Unmarshal(result, &user); err != nil {
		return nil, fmt.Errorf("telegram api getMe: unmarshal: %w", err)
	}
	return &user, nil
}

// downloadFile calls getFile to retrieve the remote file_path and file_size.
// Does NOT download bytes — caller uses downloadToPath for that.
func (c *apiClient) downloadFile(ctx context.Context, fileID string) (filePath string, fileSize int64, err error) {
	result, err := c.call(ctx, "getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return "", 0, fmt.Errorf("telegram api getFile: %w", err)
	}

	var f tgFile
	if err := json.Unmarshal(result, &f); err != nil {
		return "", 0, fmt.Errorf("telegram api getFile: unmarshal: %w", err)
	}

	return f.FilePath, f.FileSize, nil
}

// downloadToPath downloads a file from Telegram servers to localPath using streaming io.Copy.
func (c *apiClient) downloadToPath(ctx context.Context, remoteFilePath, localPath string) error {
	url := fmt.Sprintf("%s/file/bot%s/%s", c.baseURL, c.token, remoteFilePath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("telegram api download: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram api download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram api download: status %d", resp.StatusCode)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("telegram api download: create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("telegram api download: copy: %w", err)
	}

	return nil
}
