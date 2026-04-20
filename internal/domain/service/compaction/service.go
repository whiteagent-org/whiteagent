package compaction

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/llm"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

//go:embed compaction.tmpl
var compactionTemplate string

var requiredSections = []string{"Goals", "Files", "State", "Decisions"}

type templateMessage struct {
	ID          entity.MessageID
	Role        entity.Role
	Content     string
	Attachments []string
}

type templateData struct {
	Summaries []entity.Summary
	Messages  []templateMessage
}

type Service struct {
	completion             llm.CompletionService
	convService            port.ConversationService
	store                  port.StorePlugin
	model                  string
	preserveRecentMessages int
	tmpl                   *template.Template
}

type Request struct {
	TenantID        entity.TenantID
	ConversationID  entity.ConversationID
	LatestMessageID entity.MessageID
}

func New(completion llm.CompletionService, convService port.ConversationService, store port.StorePlugin, cfg *config.CompactionConfig) (*Service, error) {
	switch {
	case completion == nil:
		return nil, errors.New("completion service is required")
	case convService == nil:
		return nil, errors.New("conversation service is required")
	case store == nil:
		return nil, errors.New("store is required")
	case cfg == nil:
		return nil, errors.New("compaction config is required")
	case strings.TrimSpace(cfg.Model) == "":
		return nil, errors.New("compaction model is required")
	case cfg.PreserveRecentMessages <= 0:
		return nil, errors.New("preserve recent messages must be greater than zero")
	}

	tmpl, err := template.New("compaction").Parse(compactionTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse compaction template: %w", err)
	}

	return &Service{
		completion:             completion,
		convService:            convService,
		store:                  store,
		model:                  cfg.Model,
		preserveRecentMessages: cfg.PreserveRecentMessages,
		tmpl:                   tmpl,
	}, nil
}

func (s *Service) Compact(ctx context.Context, req Request) error {
	latestSummary, err := s.store.GetLatestSummary(ctx, req.TenantID, req.ConversationID)
	if err != nil {
		return fmt.Errorf("get latest summary: %w", err)
	}

	s.convService.RegisterConversation(req.TenantID, req.ConversationID)
	history, err := s.convService.GetHistory(ctx, req.ConversationID, 0, 0, &req.LatestMessageID)
	if err != nil {
		return fmt.Errorf("get history: %w", err)
	}
	history = trimSummarizedMessages(history, latestSummary)
	history = trimPreservedTail(history, s.preserveRecentMessages)
	if len(history) == 0 {
		return nil
	}

	var priorSummaries []entity.Summary
	if latestSummary != nil {
		priorSummaries = []entity.Summary{*latestSummary}
	}

	rendered, err := s.renderPrompt(priorSummaries, history)
	if err != nil {
		return fmt.Errorf("render prompt: %w", err)
	}

	resp, err := s.completion.Complete(ctx, port.CompletionRequest{
		TenantID: req.TenantID,
		Messages: []entity.Message{
			{Role: entity.RoleUser, Content: rendered},
		},
		Model: s.model,
	})
	if err != nil {
		return fmt.Errorf("complete compaction summary: %w", err)
	}

	normalized := normalizeSummary(resp.Content, history)
	summary := entity.Summary{
		ID:             util.NewID(),
		TenantID:       req.TenantID,
		ConversationID: req.ConversationID,
		Content:        normalized,
		MessageID:      history[len(history)-1].ID,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.store.SaveSummary(ctx, req.TenantID, summary); err != nil {
		return fmt.Errorf("save summary: %w", err)
	}

	return nil
}

func (s *Service) renderPrompt(summaries []entity.Summary, history []entity.Message) (string, error) {
	data := templateData{
		Summaries: summaries,
		Messages:  make([]templateMessage, 0, len(history)),
	}
	for _, msg := range history {
		data.Messages = append(data.Messages, templateMessage{
			ID:          msg.ID,
			Role:        msg.Role,
			Content:     msg.Content,
			Attachments: attachmentRefs(msg.Attachments),
		})
	}

	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func trimSummarizedMessages(history []entity.Message, latestSummary *entity.Summary) []entity.Message {
	if latestSummary == nil || latestSummary.MessageID.IsEmpty() {
		return append([]entity.Message(nil), history...)
	}

	trimmed := make([]entity.Message, 0, len(history))
	for _, msg := range history {
		if msg.ID.IsEmpty() || string(msg.ID) <= string(latestSummary.MessageID) {
			continue
		}
		trimmed = append(trimmed, msg)
	}
	return trimmed
}

func trimPreservedTail(history []entity.Message, preserveRecentMessages int) []entity.Message {
	if preserveRecentMessages <= 0 {
		return append([]entity.Message(nil), history...)
	}
	if len(history) <= preserveRecentMessages {
		return nil
	}
	return append([]entity.Message(nil), history[:len(history)-preserveRecentMessages]...)
}

func attachmentRefs(attachments []entity.Attachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	refs := make([]string, 0, len(attachments))
	for _, att := range attachments {
		name := strings.TrimSpace(att.Path)
		if name == "" {
			name = strings.TrimSpace(att.Filename)
		}
		if name == "" {
			name = strings.TrimSpace(att.ID)
		}
		if name == "" {
			name = "attachment"
		}

		desc := strings.TrimSpace(att.Caption)
		if desc == "" {
			desc = strings.TrimSpace(att.Kind)
		}
		if desc == "" {
			desc = "attachment"
		}
		refs = append(refs, fmt.Sprintf("%s: %s", name, desc))
	}
	return refs
}

func normalizeSummary(raw string, history []entity.Message) string {
	sections := parseSections(raw)
	sections["Files"] = normalizeFilesSection(history)

	var buf strings.Builder
	for i, name := range requiredSections {
		content := strings.TrimSpace(sections[name])
		if content == "" {
			content = "None."
		}
		buf.WriteString("## ")
		buf.WriteString(name)
		buf.WriteString("\n")
		buf.WriteString(content)
		if i < len(requiredSections)-1 {
			buf.WriteString("\n\n")
		}
	}
	return buf.String()
}

func parseSections(raw string) map[string]string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	sections := make(map[string]string, len(requiredSections))

	var current string
	var body []string
	flush := func() {
		if current == "" {
			return
		}
		sections[current] = strings.TrimSpace(strings.Join(body, "\n"))
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			next := canonicalSection(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			if next != "" {
				flush()
				current = next
				body = body[:0]
				continue
			}
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()
	return sections
}

func canonicalSection(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "goals":
		return "Goals"
	case "files":
		return "Files"
	case "state":
		return "State"
	case "decisions":
		return "Decisions"
	default:
		return ""
	}
}

func normalizeFilesSection(history []entity.Message) string {
	seen := make(map[string]struct{})
	lines := make([]string, 0)
	for _, msg := range history {
		for _, ref := range attachmentRefs(msg.Attachments) {
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			lines = append(lines, "- "+ref)
		}
	}
	if len(lines) == 0 {
		return "None."
	}
	return strings.Join(lines, "\n")
}
