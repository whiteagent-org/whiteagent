package gateway

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

//go:embed templates/secret_form.html
var secretFormHTML string

//go:embed templates/secret_expired.html
var secretExpiredHTML string

// secretHandler serves the secret entry web form and processes submissions.
type secretHandler struct {
	secretSvc   secret.SecretService
	callback    func(context.Context, entity.Message) error
	tmpl        *template.Template
	expiredTmpl *template.Template
}

type formData struct {
	Keys      []string
	ModesJSON template.JS // JSON-encoded mode hints for JavaScript
	Token     string
}

// newSecretHandler creates a handler with a parsed HTML template.
func newSecretHandler(secretSvc secret.SecretService, callback func(context.Context, entity.Message) error) (*secretHandler, error) {
	tmpl, err := template.New("secret_form").Parse(secretFormHTML)
	if err != nil {
		return nil, fmt.Errorf("parse secret form template: %w", err)
	}
	expiredTmpl, err := template.New("secret_expired").Parse(secretExpiredHTML)
	if err != nil {
		return nil, fmt.Errorf("parse secret expired template: %w", err)
	}
	return &secretHandler{
		secretSvc:   secretSvc,
		callback:    callback,
		tmpl:        tmpl,
		expiredTmpl: expiredTmpl,
	}, nil
}

// RegisterSecretRoutes registers GET and POST /secrets/{token} on the gateway mux.
func (g *Gateway) RegisterSecretRoutes(secretSvc secret.SecretService, callback func(context.Context, entity.Message) error) {
	h, err := newSecretHandler(secretSvc, callback)
	if err != nil {
		slog.Error("failed to create secret handler", "err", err)
		return
	}
	g.mux.HandleFunc("GET /secrets/{token}", h.serveForm)
	g.mux.HandleFunc("POST /secrets/{token}", h.handleSubmit)
}

// serveForm renders the secret entry form for a valid token.
func (h *secretHandler) serveForm(w http.ResponseWriter, r *http.Request) {
	tokenID := r.PathValue("token")
	token, err := h.secretSvc.ValidateToken(r.Context(), tokenID)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if err := h.expiredTmpl.Execute(w, nil); err != nil {
			slog.Error("secret expired render error", "err", err)
		}
		return
	}

	// Encode modes as JSON for JavaScript.
	modesMap := make(map[string]string, len(token.Modes))
	for k, v := range token.Modes {
		modesMap[k] = string(v)
	}
	modesJSON, _ := json.Marshal(modesMap)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.Execute(w, formData{Keys: token.Keys, ModesJSON: template.JS(modesJSON), Token: tokenID}); err != nil {
		slog.Error("secret form render error", "err", err)
	}
}

// secretSubmitEntry represents a single secret in the form POST payload.
// Supports both structured format {"value": "...", "mode": "..."} and
// flat string format "value" for backward compatibility.
type secretSubmitEntry struct {
	Value string `json:"value"`
	Mode  string `json:"mode"`
}

// handleSubmit processes the secret form submission.
func (h *secretHandler) handleSubmit(w http.ResponseWriter, r *http.Request) {
	tokenID := r.PathValue("token")

	// Parse raw JSON to handle both flat strings and structured objects.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
		return
	}

	token, err := h.secretSvc.ValidateToken(r.Context(), tokenID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "This link has expired or has already been used."})
		return
	}

	// Convert to SecretSubmission map, handling both payload formats.
	submissions := make(map[string]secret.SecretSubmission, len(raw))
	for k, v := range raw {
		// Try structured format first: {"value": "...", "mode": "..."}
		var entry secretSubmitEntry
		if err := json.Unmarshal(v, &entry); err == nil && entry.Value != "" || (err == nil && entry.Mode != "") {
			mode := entity.SecretMode(entry.Mode)
			if mode == "" {
				mode = entity.SecretModeValue
			}
			submissions[k] = secret.SecretSubmission{Value: []byte(entry.Value), Mode: mode}
			continue
		}
		// Fall back to flat string format: "value"
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			submissions[k] = secret.SecretSubmission{Value: []byte(str), Mode: entity.SecretModeValue}
			continue
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid value for key: " + k})
		return
	}

	if err := h.secretSvc.ConsumeToken(r.Context(), tokenID, submissions); err != nil {
		slog.Error("secret consume error", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save secrets."})
		return
	}

	// Build notification message to send back to the originating conversation.
	keyNames := make([]string, 0, len(submissions))
	for k := range submissions {
		keyNames = append(keyNames, k)
	}

	notifyMsg := entity.Message{
		ID:             entity.MessageID(util.NewID()),
		Kind:           entity.MessageKindMessage,
		Role:           entity.RoleUser,
		Content:        fmt.Sprintf("The secrets were set: %s. If there was a pending task that required these credentials, continue with it.", strings.Join(keyNames, ", ")),
		TenantID:       token.TenantID,
		UserID:         token.UserID,
		ConversationID: token.ConversationID,
		ChatID:         token.ChatID,
	}

	if h.callback != nil {
		if err := h.callback(r.Context(), notifyMsg); err != nil {
			slog.Error("secret notification callback error", "err", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
