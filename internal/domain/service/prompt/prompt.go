// Package prompt assembles system prompts from tenant, agent, user instructions
// and tool definitions using an embedded Go template.
package prompt

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/pkg/token"
	"github.com/whiteagent-org/whiteagent/pkg/util"
	"github.com/whiteagent-org/whiteagent/pkg/xml"
)

//go:embed prompt.tmpl
var promptTemplate string

// metadataEntry holds a single key-value pair for generic channel context rendering.
type metadataEntry struct {
	Key   string
	Value string
}

// promptData holds the template execution data.
type promptData struct {
	TenantID           string
	TenantName         string
	TenantInstructions string
	AgentID            string
	AgentName          string
	AgentInstructions  string
	UserID             string
	UserName           string
	Memory             string
	ToolDescriptions   string
	ToolInstructions   string
	IsGroup            bool
	IsCron             bool
	HasReactions       bool
	ChannelContext     []metadataEntry
	SkillsBlock        string
	OutMsgPath         string
}

// CompactionSignal tells the caller that the assembled prompt footprint has
// reached the configured compaction threshold for the current turn.
type CompactionSignal struct {
	LatestMessageID entity.MessageID
}

// SkillLister returns available skills for a given tenant+user.
type SkillLister interface {
	List(tenantID entity.TenantID, userID entity.UserID) ([]entity.Skill, error)
}

// PathProvider resolves container-side paths for prompt rendering.
type PathProvider interface {
	UserHomePath(userID entity.UserID) string
	TenantHomePath(tenantID entity.TenantID) string
	MessagesPath() string
}

// emptyPathProvider is a defensive fallback when no PathProvider is given.
type emptyPathProvider struct{}

func (emptyPathProvider) UserHomePath(entity.UserID) string     { return "" }
func (emptyPathProvider) TenantHomePath(entity.TenantID) string { return "" }
func (emptyPathProvider) MessagesPath() string                  { return "/messages" }

// PromptBuilder assembles system prompts from tenant, agent, user instructions
// and tool definitions using an embedded Go template. It fetches conversation
// history internally and applies token-based windowing.
type PromptBuilder struct {
	tmpl        *template.Template
	tools       map[string]port.ToolPlugin
	conv        port.ConversationService
	summaries   port.SummaryReader
	memory      port.MemoryReader
	users       *identity.UserResolver
	tokenBudget int
	compaction  *config.CompactionConfig
	skills      SkillLister
	paths       PathProvider
}

// NewPromptBuilder creates a PromptBuilder that uses all registered tools.
// conv may be nil (history fetching skipped). store may be nil (memory and
// summary injection skipped). users may be nil (user name resolution skipped).
// tokenBudget of 0 disables windowing. skills may be nil (skills injection
// skipped). paths provides container-side path resolution; nil uses a
// defensive fallback.
//
// store is typed as port.JournalReader for backwards compatibility with the
// runtime wiring (storePlugin satisfies it), but the journal interface itself
// is no longer consulted -- only the SummaryReader and MemoryReader views,
// extracted via type assertion below, are used.
func NewPromptBuilder(tools map[string]port.ToolPlugin, conv port.ConversationService, store port.JournalReader, users *identity.UserResolver, tokenBudget int, compactionCfg *config.CompactionConfig, skills SkillLister, paths PathProvider) (*PromptBuilder, error) {
	tmpl, err := template.New("system").Parse(promptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse prompt template: %w", err)
	}
	var mem port.MemoryReader
	if mr, ok := store.(port.MemoryReader); ok {
		mem = mr
	}
	var summaries port.SummaryReader
	if sr, ok := store.(port.SummaryReader); ok {
		summaries = sr
	}
	if paths == nil {
		paths = emptyPathProvider{}
	}
	return &PromptBuilder{
		tmpl:        tmpl,
		tools:       tools,
		conv:        conv,
		summaries:   summaries,
		memory:      mem,
		users:       users,
		tokenBudget: tokenBudget,
		compaction:  compactionCfg,
		skills:      skills,
		paths:       paths,
	}, nil
}

// Build assembles a full message list: system prompt followed by token-windowed
// conversation history. It fetches history from ConversationService internally.
// Tenant, agent, and user may be nil -- missing fields become empty strings.
func (b *PromptBuilder) Build(ctx context.Context, tenant *entity.Tenant, agent *entity.Agent, user *entity.User, msg entity.Message, convID entity.ConversationID, caps port.ChannelCapabilities, upToID *entity.MessageID, outMsgPath ...string) ([]entity.Message, *CompactionSignal, error) {
	// Fetch memory from store based on context (group or user).
	var memoryContent string
	if b.memory != nil && tenant != nil {
		var ownerType, ownerID string
		if msg.IsGroup {
			ownerType = "chat"
			ownerID = msg.ChatID.String()
		} else if user != nil {
			ownerType = "user"
			ownerID = user.ID.String()
		}
		if ownerID != "" {
			mem, err := b.memory.GetMemory(ctx, tenant.ID, ownerType, ownerID)
			if err != nil {
				slog.Warn("prompt.build.memory_fetch_error", "err", err)
			} else if mem != nil {
				memoryContent = mem.Content
			}
		}
	}

	var outPath string
	if len(outMsgPath) > 0 {
		outPath = outMsgPath[0]
	}
	systemPrompt := b.renderSystem(tenant, agent, user, msg, memoryContent, caps, outPath)

	slog.Debug("prompt.build", "stage", "system_prompt", "content", "")

	// Budget layout: tokenBudget = systemTokens + summaryTokens + historyBudget.
	// History is windowed last, against whatever remains after the system prompt
	// and (optional) compacted-context summary block.
	systemTokens := token.Count(systemPrompt)

	tenantID := msg.TenantID
	if tenantID.IsEmpty() && tenant != nil {
		tenantID = tenant.ID
	}

	var (
		historyMsgs   []entity.Message
		latestSummary *entity.Summary
	)
	if b.conv != nil && convID != "" {
		fullHistory, err := b.fetchFullHistory(ctx, convID, upToID)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch history: %w", err)
		}
		latestSummary, err = b.fetchLatestSummary(ctx, tenantID, convID)
		if err != nil {
			slog.Warn("prompt.build.summary_fetch_error", "err", err, "conversation_id", convID)
		}
		historyMsgs = trimHistoryAfterSummary(fullHistory, latestSummaryMessageID(latestSummary))
	}

	// Inject compacted context (summary of the current conversation) between
	// system prompt and active history. Summary is scoped to convID and only
	// exists after compaction has run for this conversation.
	var compactedMsg *entity.Message
	summaryTokens := 0
	if latestSummary != nil {
		content := renderCompactedBlock(convID, *latestSummary)
		compactedMsg = &entity.Message{Role: entity.RoleSystem, Content: content}
		summaryTokens = countPromptFootprint([]entity.Message{*compactedMsg})
	}

	historyBudget := b.tokenBudget - systemTokens - summaryTokens
	if b.tokenBudget > 0 && historyBudget <= 0 {
		slog.Warn("prompt.build.budget_exceeded",
			"system_tokens", systemTokens,
			"summary_tokens", summaryTokens,
			"token_budget", b.tokenBudget,
		)
	}
	if b.tokenBudget > 0 {
		historyMsgs = windowHistoryWithBudget(historyMsgs, historyBudget)
	}

	// Resolve user names for history messages.
	userNames := make(map[entity.UserID]string)
	if b.users != nil && len(historyMsgs) > 0 && tenant != nil {
		var userIDs []entity.UserID
		for _, m := range historyMsgs {
			if m.Role == entity.RoleUser && !m.UserID.IsEmpty() {
				userIDs = append(userIDs, m.UserID)
			}
		}
		if len(userIDs) > 0 {
			resolved := b.users.ResolveUsers(ctx, tenant.ID, userIDs)
			for uid, u := range resolved {
				userNames[uid] = u.Name
			}
		}
	}
	// Add current user to map (already loaded, avoids redundant lookup).
	if user != nil && !user.ID.IsEmpty() {
		userNames[user.ID] = user.Name
	}

	enriched := enrichMessages(historyMsgs, userNames, b.paths.MessagesPath())

	messages := make([]entity.Message, 0, 2+len(enriched))
	messages = append(messages, entity.Message{
		Role:    entity.RoleSystem,
		Content: systemPrompt,
	})
	if compactedMsg != nil {
		messages = append(messages, *compactedMsg)
	}
	messages = append(messages, enriched...)

	historyTokens := countPromptFootprint(enriched)
	totalTokens := systemTokens + summaryTokens + historyTokens
	compactionSignal := b.buildCompactionSignal(totalTokens, msg, historyMsgs, upToID)

	slog.Debug("prompt.build.complete",
		"token_budget", b.tokenBudget,
		"total_tokens", totalTokens,
		"system_tokens", systemTokens,
		"summary_tokens", summaryTokens,
		"history_tokens", historyTokens,
		"message_count", len(messages),
		"history_message_count", len(enriched),
		"has_compacted", compactedMsg != nil,
		"compaction_signal", compactionSignal != nil,
	)

	return messages, compactionSignal, nil
}

func (b *PromptBuilder) buildCompactionSignal(pressureTokens int, currentMsg entity.Message, historyMsgs []entity.Message, upToID *entity.MessageID) *CompactionSignal {
	if b.compaction == nil || b.tokenBudget <= 0 {
		return nil
	}
	if float64(pressureTokens)/float64(b.tokenBudget) < b.compaction.Threshold {
		return nil
	}

	var latestMessageID entity.MessageID
	switch {
	case upToID != nil && !upToID.IsEmpty():
		latestMessageID = *upToID
	case len(historyMsgs) > 0:
		latestMessageID = historyMsgs[len(historyMsgs)-1].ID
	default:
		latestMessageID = currentMsg.ID
	}
	if latestMessageID.IsEmpty() {
		return nil
	}
	return &CompactionSignal{LatestMessageID: latestMessageID}
}

func (b *PromptBuilder) fetchFullHistory(ctx context.Context, convID entity.ConversationID, upToID *entity.MessageID) ([]entity.Message, error) {
	if b.conv == nil || convID == "" {
		return nil, nil
	}
	return b.conv.GetHistory(ctx, convID, 0, 0, upToID)
}

func (b *PromptBuilder) fetchLatestSummary(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID) (*entity.Summary, error) {
	if b.summaries == nil || tenantID.IsEmpty() || convID == "" {
		return nil, nil
	}
	return b.summaries.GetLatestSummary(ctx, tenantID, convID)
}

func latestSummaryMessageID(summary *entity.Summary) entity.MessageID {
	if summary == nil {
		return ""
	}
	return summary.MessageID
}

func trimHistoryAfterSummary(history []entity.Message, latestSummaryMessageID entity.MessageID) []entity.Message {
	if latestSummaryMessageID.IsEmpty() {
		return append([]entity.Message(nil), history...)
	}

	trimmed := make([]entity.Message, 0, len(history))
	for _, msg := range history {
		if msg.ID.IsEmpty() || string(msg.ID) <= string(latestSummaryMessageID) {
			continue
		}
		trimmed = append(trimmed, msg)
	}
	return trimmed
}

// renderSystem produces the system prompt string from template data.
func (b *PromptBuilder) renderSystem(tenant *entity.Tenant, agent *entity.Agent, user *entity.User, msg entity.Message, memory string, caps port.ChannelCapabilities, outMsgPath string) string {
	data := promptData{}
	if tenant != nil {
		data.TenantID = tenant.ID.String()
		data.TenantName = tenant.Name
		data.TenantInstructions = tenant.Instructions
	}
	if agent != nil {
		data.AgentID = agent.ID.String()
		data.AgentName = agent.Name
		data.AgentInstructions = agent.Instructions
	}
	if user != nil {
		data.UserID = user.ID.String()
		data.UserName = user.Name
	}

	data.Memory = memory
	data.IsGroup = msg.IsGroup
	data.IsCron = msg.Kind == entity.MessageKindCron
	data.HasReactions = caps.Reactions
	data.ChannelContext = buildChannelContext(msg)
	data.OutMsgPath = outMsgPath

	filtered := b.filterTools(tenant, caps)
	data.ToolDescriptions = buildToolXML(filtered)
	data.ToolInstructions = buildToolInstructions(filtered)

	// Skills injection.
	if b.skills != nil {
		var tenantID entity.TenantID
		var userID entity.UserID
		if tenant != nil {
			tenantID = tenant.ID
		}
		if user != nil {
			userID = user.ID
		}
		skills, err := b.skills.List(tenantID, userID)
		if err != nil {
			slog.Warn("prompt.skills.list_error", "err", err)
			skills = nil
		}
		userHome := b.paths.UserHomePath(userID)
		tenantHome := b.paths.TenantHomePath(tenantID)
		data.SkillsBlock = buildSkillsXML(skills, userHome, tenantHome)
	}

	var buf bytes.Buffer
	_ = b.tmpl.Execute(&buf, data)
	return buf.String()
}

// buildSkillsXML produces a <skills> XML block with skill names as tags
// and container-side paths derived from level.
func buildSkillsXML(skills []entity.Skill, userHome, tenantHome string) string {
	xb := xml.NewBuilder()
	xb.OpenTag("skills")
	for _, s := range skills {
		xb.OpenTag("skill")
		xb.Child("name", s.Name)
		xb.Child("description", s.Description)
		var containerPath string
		if s.Level == entity.SkillLevelUser {
			containerPath = userHome + "/skills/" + s.Name
		} else {
			containerPath = tenantHome + "/skills/" + s.Name
		}
		xb.Child("path", containerPath)
		xb.CloseTag("skill")
	}
	xb.CloseTag("skills")
	return xb.String()
}

func windowHistoryWithBudget(history []entity.Message, budget int) []entity.Message {
	if len(history) == 0 || budget <= 0 {
		return nil
	}
	groups := groupMessages(history)
	for i := range groups {
		groups[i].tokens = countGroupTokens(groups[i])
	}
	return windowMessages(groups, budget)
}

func renderSummaryBlock(summary entity.Summary) string {
	xb := xml.NewBuilder()
	var attrs []xml.Attr
	if summary.ID != "" {
		attrs = append(attrs, xml.Attr{Key: "id", Value: summary.ID})
	}
	if !summary.CreatedAt.IsZero() {
		attrs = append(attrs, xml.Attr{Key: "ts", Value: util.FormatTimestampUTC(summary.CreatedAt)})
	}
	xb.OpenTag("summary", attrs...)
	xb.Child("content", summary.Content)
	xb.CloseTag("summary")
	return xb.String()
}

// renderCompactedBlock wraps the latest conversation summary in a
// <wa_compacted_context> XML system-role content block. It is rendered only
// when compaction has produced a summary for the current convID.
func renderCompactedBlock(convID entity.ConversationID, summary entity.Summary) string {
	var buf strings.Builder
	buf.WriteString("<wa_compacted_context")
	if convID != "" {
		buf.WriteString(` conversation_id="`)
		buf.WriteString(convID.String())
		buf.WriteString(`"`)
	}
	buf.WriteString(">\n")
	rendered := renderSummaryBlock(summary)
	buf.WriteString(rendered)
	if !strings.HasSuffix(rendered, "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString("</wa_compacted_context>\n")
	return buf.String()
}

// FilteredToolDefs returns port.ToolDef entries filtered by tenant.AllowedTools
// and channel capabilities. If AllowedTools is empty or nil, all tools are included.
// The reaction tool is excluded when caps.Reactions is false.
func (b *PromptBuilder) FilteredToolDefs(tenant *entity.Tenant, caps port.ChannelCapabilities) []port.ToolDef {
	filtered := b.filterTools(tenant, caps)
	defs := make([]port.ToolDef, 0, len(filtered))
	for _, t := range filtered {
		defs = append(defs, port.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

// filterTools applies tenant.AllowedTools filtering and channel capability filtering.
// Empty or nil AllowedTools means all tools are allowed.
// The reaction tool is excluded when caps.Reactions is false.
func (b *PromptBuilder) filterTools(tenant *entity.Tenant, caps port.ChannelCapabilities) []port.ToolPlugin {
	var allowed map[string]struct{}
	if tenant != nil && len(tenant.AllowedTools) > 0 {
		allowed = make(map[string]struct{}, len(tenant.AllowedTools))
		for _, name := range tenant.AllowedTools {
			allowed[name] = struct{}{}
		}
	}

	tools := make([]port.ToolPlugin, 0, len(b.tools))
	for _, t := range b.tools {
		if allowed != nil {
			if _, ok := allowed[t.Name()]; !ok {
				continue
			}
		}
		if !caps.Reactions && t.Name() == "reaction" {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}

// buildChannelContext extracts string metadata from msg and returns a sorted
// slice of metadataEntry. Channel info is not injected (LLM doesn't need it;
// channel_type comes from metadata if the channel plugin sets it).
func buildChannelContext(msg entity.Message) []metadataEntry {
	kv := make(map[string]string)
	for k, v := range msg.Metadata {
		kv[k] = v
	}
	if len(kv) == 0 {
		return nil
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]metadataEntry, len(keys))
	for i, k := range keys {
		entries[i] = metadataEntry{Key: k, Value: kv[k]}
	}
	return entries
}

// buildContextBlock produces a <wa_msg_context> XML block for a message.
// Returns "" if the message has no ID and no timestamp.
// userNames maps UserIDs to display names; may be nil.
// messagesPath is the container-side prefix for attachment paths (e.g. "/messages").
func buildContextBlock(msg entity.Message, userNames map[entity.UserID]string, messagesPath string) string {
	hasID := !msg.ID.IsEmpty()
	hasTS := !msg.CreatedAt.IsZero()
	if !hasID && !hasTS {
		return ""
	}

	var attrs []xml.Attr
	if hasID {
		attrs = append(attrs, xml.Attr{Key: "msg_id", Value: msg.ID.String()})
	}
	if hasTS {
		attrs = append(attrs, xml.Attr{Key: "ts", Value: util.FormatTimestampUTC(msg.CreatedAt)})
	}
	if !msg.RepliedToID.IsEmpty() {
		attrs = append(attrs, xml.Attr{Key: "reply_to", Value: msg.RepliedToID.String()})
	}
	if msg.Role == entity.RoleUser && !msg.UserID.IsEmpty() {
		attrs = append(attrs, xml.Attr{Key: "user_id", Value: msg.UserID.String()})
		if name, ok := userNames[msg.UserID]; ok && name != "" {
			attrs = append(attrs, xml.Attr{Key: "user_name", Value: name})
		}
	}

	// Evicted messages render as self-closing tags with no content body.
	if msg.Evicted {
		attrs = append(attrs, xml.Attr{Key: "evicted", Value: ""})
		xb := xml.NewBuilder()
		xb.SelfCloseTag("wa_msg_context", attrs...)
		return xb.String() + "\n\n"
	}
	xb := xml.NewBuilder()
	if len(msg.Attachments) == 0 {
		xb.SelfCloseTag("wa_msg_context", attrs...)
		return xb.String() + "\n\n"
	}

	xb.OpenTag("wa_msg_context", attrs...)
	for i, att := range msg.Attachments {
		xb.OpenTag("attachment", xml.Attr{Key: "idx", Value: strconv.Itoa(i)})
		xb.Child("kind", att.Kind)
		xb.Child("filename", att.Filename)
		xb.Child("size", util.FormatSize(att.Size))
		xb.Child("mime_type", att.MimeType)
		xb.Child("path", messagesPath+"/"+msg.ID.String()+"/"+att.Filename)
		xb.Child("caption", att.Caption)
		xb.CloseTag("attachment")
	}
	xb.CloseTag("wa_msg_context")
	return xb.String() + "\n"
}

// buildToolXML produces an XML block describing the available tools.
func buildToolXML(tools []port.ToolPlugin) string {
	if len(tools) == 0 {
		return "<tools/>"
	}
	xb := xml.NewBuilder()
	xb.OpenTag("tools")
	for _, t := range tools {
		params := t.Parameters()
		var compact bytes.Buffer
		if err := json.Compact(&compact, params); err == nil {
			params = compact.Bytes()
		}
		xb.OpenTag("tool", xml.Attr{Key: "name", Value: t.Name()})
		xb.Child("description", t.Description())
		xb.ChildRaw("parameters", string(params))
		xb.CloseTag("tool")
	}
	xb.CloseTag("tools")
	return xb.String()
}

// buildToolInstructions collects non-empty Instructions() from tools and joins
// them with blank lines. Returns "" if no tool has instructions.
func buildToolInstructions(tools []port.ToolPlugin) string {
	var parts []string
	for _, t := range tools {
		if inst := t.Instructions(); inst != "" {
			parts = append(parts, inst)
		}
	}
	return strings.Join(parts, "\n\n")
}
