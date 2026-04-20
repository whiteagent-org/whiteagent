package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// ---------------------------------------------------------------------------
// Mock SecretService
// ---------------------------------------------------------------------------

type mockSecretService struct {
	validateTokenFn func(ctx context.Context, tokenID string) (*entity.SecretToken, error)
	consumeTokenFn  func(ctx context.Context, tokenID string, values map[string]secret.SecretSubmission) error
}

func (m *mockSecretService) Set(_ context.Context, _ entity.TenantID, _ entity.UserID, _ string, _ []byte, _ entity.SecretMode) error {
	return nil
}
func (m *mockSecretService) Get(_ context.Context, _ entity.TenantID, _ entity.UserID, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockSecretService) Exists(_ context.Context, _ entity.TenantID, _ entity.UserID, _ string) (bool, error) {
	return false, nil
}
func (m *mockSecretService) List(_ context.Context, _ entity.TenantID, _ entity.UserID) ([]secret.SecretEntry, error) {
	return nil, nil
}
func (m *mockSecretService) Delete(_ context.Context, _ entity.TenantID, _ entity.UserID, _ string) error {
	return nil
}
func (m *mockSecretService) Redact(_ context.Context, text string, _ entity.TenantID, _ entity.UserID) string {
	return text
}
func (m *mockSecretService) EnvVars(_ context.Context, _ entity.TenantID, _ entity.UserID) ([]secret.SecretEnvEntry, error) {
	return nil, nil
}
func (m *mockSecretService) RequestEntry(_ context.Context, _ []string, _ map[string]entity.SecretMode, _ entity.TenantID, _ entity.UserID, _ entity.ConversationID, _ entity.ChatID) (string, error) {
	return "", nil
}
func (m *mockSecretService) ValidateToken(ctx context.Context, tokenID string) (*entity.SecretToken, error) {
	if m.validateTokenFn != nil {
		return m.validateTokenFn(ctx, tokenID)
	}
	return nil, fmt.Errorf("token not found")
}
func (m *mockSecretService) ConsumeToken(ctx context.Context, tokenID string, values map[string]secret.SecretSubmission) error {
	if m.consumeTokenFn != nil {
		return m.consumeTokenFn(ctx, tokenID, values)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func validToken(tokenID string) *entity.SecretToken {
	return &entity.SecretToken{
		TokenID:        tokenID,
		Keys:           []string{"API_KEY", "DB_PASS"},
		TenantID:       entity.TenantID("t1"),
		UserID:         entity.UserID("u1"),
		ConversationID: entity.ConversationID("c1"),
		ChatID:         entity.ChatID("chat-1"),
		ExpiresAt:      time.Now().Add(time.Hour),
		Used:           false,
	}
}

func newHandler(t *testing.T, svc secret.SecretService, callback func(context.Context, entity.Message) error) *secretHandler {
	t.Helper()
	h, err := newSecretHandler(svc, callback)
	if err != nil {
		t.Fatalf("newSecretHandler: %v", err)
	}
	return h
}

// makeRequest creates a request with the {token} path value set.
func makeRequest(method, tokenID, body string) *http.Request {
	url := "/secrets/" + tokenID
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	r := httptest.NewRequest(method, url, bodyReader)
	r.SetPathValue("token", tokenID)
	return r
}

// ---------------------------------------------------------------------------
// Tests: GET /secrets/{token}
// ---------------------------------------------------------------------------

// TestSecretFormRendersForValidToken verifies that a GET with a valid token
// returns 200 and renders the HTML form containing the key names.
func TestSecretFormRendersForValidToken(t *testing.T) {
	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
	}
	h := newHandler(t, svc, nil)

	r := makeRequest(http.MethodGet, "valid-token-123", "")
	w := httptest.NewRecorder()
	h.serveForm(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("GET valid token: status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "API_KEY") {
		t.Error("GET valid token: response body should contain key name 'API_KEY'")
	}
	if !strings.Contains(body, "DB_PASS") {
		t.Error("GET valid token: response body should contain key name 'DB_PASS'")
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET valid token: Content-Type = %q, want text/html", ct)
	}
}

// TestSecretFormShowsExpiredMessageForInvalidToken verifies that a GET with
// an invalid or expired token returns 200 with an expiry message (not a crash).
func TestSecretFormShowsExpiredMessageForInvalidToken(t *testing.T) {
	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, _ string) (*entity.SecretToken, error) {
			return nil, fmt.Errorf("token expired or not found")
		},
	}
	h := newHandler(t, svc, nil)

	r := makeRequest(http.MethodGet, "expired-token-abc", "")
	w := httptest.NewRecorder()
	h.serveForm(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("GET invalid token: status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "expired") && !strings.Contains(body, "already been used") {
		t.Errorf("GET invalid token: expected expiry message in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Tests: POST /secrets/{token}
// ---------------------------------------------------------------------------

// TestSecretSubmitSuccessWithValidToken verifies that a POST with a valid token
// and JSON body returns 200 {"ok":true} and invokes the callback.
func TestSecretSubmitSuccessWithValidToken(t *testing.T) {
	callbackInvoked := false
	var callbackMsg entity.Message

	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
		consumeTokenFn: func(_ context.Context, _ string, _ map[string]secret.SecretSubmission) error {
			return nil
		},
	}
	h := newHandler(t, svc, func(_ context.Context, msg entity.Message) error {
		callbackInvoked = true
		callbackMsg = msg
		return nil
	})

	payload, _ := json.Marshal(map[string]string{
		"API_KEY": "sk-test-value",
		"DB_PASS": "hunter2",
	})
	r := makeRequest(http.MethodPost, "valid-token-123", string(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("POST valid token: status = %d, want 200", w.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("POST valid token: decode response: %v", err)
	}
	if !resp["ok"] {
		t.Errorf("POST valid token: response ok = %v, want true", resp["ok"])
	}

	if !callbackInvoked {
		t.Error("POST valid token: callback was not invoked")
	}
	if callbackMsg.TenantID != entity.TenantID("t1") {
		t.Errorf("callback message TenantID = %q, want t1", callbackMsg.TenantID)
	}
	if callbackMsg.UserID != entity.UserID("u1") {
		t.Errorf("callback message UserID = %q, want u1", callbackMsg.UserID)
	}
}

// TestSecretSubmitRejectsExpiredToken verifies that a POST with an expired or
// invalid token returns 400 with a JSON error.
func TestSecretSubmitRejectsExpiredToken(t *testing.T) {
	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, _ string) (*entity.SecretToken, error) {
			return nil, fmt.Errorf("token expired")
		},
	}
	h := newHandler(t, svc, nil)

	payload, _ := json.Marshal(map[string]string{"API_KEY": "value"})
	r := makeRequest(http.MethodPost, "expired-token", string(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST expired token: status = %d, want 400", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("POST expired token: decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("POST expired token: expected error field in response")
	}
}

// TestSecretSubmitRejectsInvalidJSONBody verifies that a POST with a malformed
// JSON body returns 400 with a JSON error.
func TestSecretSubmitRejectsInvalidJSONBody(t *testing.T) {
	svc := &mockSecretService{}
	h := newHandler(t, svc, nil)

	r := makeRequest(http.MethodPost, "some-token", `not valid json {`)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST invalid JSON: status = %d, want 400", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("POST invalid JSON: decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("POST invalid JSON: expected error field in response")
	}
}

// ---------------------------------------------------------------------------
// Test: RegisterSecretRoutes wires the mux correctly
// ---------------------------------------------------------------------------

// TestRegisterSecretRoutesWiresHandlers verifies that RegisterSecretRoutes
// registers GET and POST handlers reachable via the gateway mux.
func TestRegisterSecretRoutesWiresHandlers(t *testing.T) {
	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
	}
	callback := func(_ context.Context, _ entity.Message) error { return nil }

	// Build a minimal gateway with just the mux (no real listener needed).
	mux := http.NewServeMux()
	gw := &Gateway{mux: mux}
	gw.RegisterSecretRoutes(svc, callback)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET should hit serveForm handler.
	resp, err := http.Get(srv.URL + "/secrets/test-token-id")
	if err != nil {
		t.Fatalf("GET /secrets/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /secrets/{token}: status = %d, want 200", resp.StatusCode)
	}

	// POST with valid JSON body should reach handleSubmit.
	payload := bytes.NewBufferString(`{"API_KEY":"val"}`)
	resp2, err := http.Post(srv.URL+"/secrets/test-token-id", "application/json", payload)
	if err != nil {
		t.Fatalf("POST /secrets/: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("POST /secrets/{token}: status = %d, want 200", resp2.StatusCode)
	}
}

// TestSecretSubmitStructuredPayloadWithMode verifies that a POST with the new
// structured format {"key": {"value": "...", "mode": "..."}} is accepted.
func TestSecretSubmitStructuredPayloadWithMode(t *testing.T) {
	var consumed map[string]secret.SecretSubmission

	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
		consumeTokenFn: func(_ context.Context, _ string, values map[string]secret.SecretSubmission) error {
			consumed = values
			return nil
		},
	}
	h := newHandler(t, svc, func(_ context.Context, _ entity.Message) error { return nil })

	payload, _ := json.Marshal(map[string]interface{}{
		"API_KEY": map[string]string{"value": "sk-test", "mode": "value"},
		"CERT":    map[string]string{"value": "cert-data", "mode": "file"},
	})
	r := makeRequest(http.MethodPost, "valid-token-123", string(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if consumed == nil {
		t.Fatal("consumeTokenFn was not called")
	}
	if string(consumed["API_KEY"].Value) != "sk-test" {
		t.Errorf("API_KEY value = %q, want sk-test", consumed["API_KEY"].Value)
	}
	if consumed["API_KEY"].Mode != entity.SecretModeValue {
		t.Errorf("API_KEY mode = %q, want value", consumed["API_KEY"].Mode)
	}
	if string(consumed["CERT"].Value) != "cert-data" {
		t.Errorf("CERT value = %q, want cert-data", consumed["CERT"].Value)
	}
	if consumed["CERT"].Mode != entity.SecretModeFile {
		t.Errorf("CERT mode = %q, want file", consumed["CERT"].Mode)
	}
}

// TestSecretSubmitBackwardCompatFlatPayload verifies that the old flat string
// format {"key": "value"} still works and defaults to value mode.
func TestSecretSubmitBackwardCompatFlatPayload(t *testing.T) {
	var consumed map[string]secret.SecretSubmission

	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
		consumeTokenFn: func(_ context.Context, _ string, values map[string]secret.SecretSubmission) error {
			consumed = values
			return nil
		},
	}
	h := newHandler(t, svc, func(_ context.Context, _ entity.Message) error { return nil })

	payload, _ := json.Marshal(map[string]string{
		"API_KEY": "sk-test-value",
	})
	r := makeRequest(http.MethodPost, "valid-token-123", string(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if consumed == nil {
		t.Fatal("consumeTokenFn was not called")
	}
	if string(consumed["API_KEY"].Value) != "sk-test-value" {
		t.Errorf("API_KEY value = %q, want sk-test-value", consumed["API_KEY"].Value)
	}
	if consumed["API_KEY"].Mode != entity.SecretModeValue {
		t.Errorf("API_KEY mode = %q, want value (backward compat default)", consumed["API_KEY"].Mode)
	}
}

// TestSecretSubmitEmptyValueWithFileMode verifies that a key marked as
// "File" + "No value" (empty value, mode=file) is still saved.
func TestSecretSubmitEmptyValueWithFileMode(t *testing.T) {
	var consumed map[string]secret.SecretSubmission

	svc := &mockSecretService{
		validateTokenFn: func(_ context.Context, tokenID string) (*entity.SecretToken, error) {
			return validToken(tokenID), nil
		},
		consumeTokenFn: func(_ context.Context, _ string, values map[string]secret.SecretSubmission) error {
			consumed = values
			return nil
		},
	}
	h := newHandler(t, svc, func(_ context.Context, _ entity.Message) error { return nil })

	// Simulate form with File checked + No value checked: value="" mode="file"
	payload, _ := json.Marshal(map[string]interface{}{
		"CERT": map[string]string{"value": "", "mode": "file"},
	})
	r := makeRequest(http.MethodPost, "valid-token-123", string(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if consumed == nil {
		t.Fatal("consumeTokenFn was not called")
	}
	if _, ok := consumed["CERT"]; !ok {
		t.Fatal("CERT key not found in consumed submissions")
	}
	if consumed["CERT"].Mode != entity.SecretModeFile {
		t.Errorf("CERT mode = %q, want file", consumed["CERT"].Mode)
	}
}
