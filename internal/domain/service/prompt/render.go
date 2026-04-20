package prompt

import (
	"encoding/json"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/token"
)

func countPromptFootprint(messages []entity.Message) int {
	total := 0
	for _, msg := range messages {
		rendered := msg.Content
		if msg.Role == entity.RoleAssistant && len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				rendered += string(data)
			}
		}
		total += token.Count(rendered)
	}
	return total
}

// messageGroup is a slice of messages that must be kept or evicted as a unit.
// Tool call groups (assistant + tool results) form a single group.
type messageGroup struct {
	messages []entity.Message
	tokens   int
}

// groupMessages partitions messages into atomic groups. An assistant message
// with tool calls is grouped with its subsequent matching tool-result messages.
// All other messages form standalone groups.
func groupMessages(msgs []entity.Message) []messageGroup {
	if len(msgs) == 0 {
		return nil
	}

	var groups []messageGroup
	i := 0
	for i < len(msgs) {
		m := msgs[i]

		// Assistant with tool calls: collect matching tool results.
		if m.Role == entity.RoleAssistant && len(m.ToolCalls) > 0 {
			// Build set of expected tool call IDs.
			expected := make(map[string]struct{}, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				expected[tc.ID] = struct{}{}
			}

			group := []entity.Message{m}
			j := i + 1
			for j < len(msgs) && msgs[j].Role == entity.RoleTool {
				if _, ok := expected[msgs[j].ToolCallID]; ok {
					group = append(group, msgs[j])
					delete(expected, msgs[j].ToolCallID)
					j++
				} else {
					break
				}
			}
			groups = append(groups, messageGroup{messages: group})
			i = j
			continue
		}

		// Standalone message.
		groups = append(groups, messageGroup{messages: []entity.Message{m}})
		i++
	}

	return groups
}

// enrichMessages returns a defensive copy of msgs with context blocks prepended
// to user messages, assistant messages with content, and tool-result messages.
// userNames maps UserIDs to display names for user-role messages (may be nil).
func enrichMessages(msgs []entity.Message, userNames map[entity.UserID]string, messagesPath string) []entity.Message {
	enriched := make([]entity.Message, len(msgs))
	copy(enriched, msgs)
	for i := range enriched {
		m := &enriched[i]
		if m.Evicted {
			// Evicted tool-result messages are omitted entirely.
			if m.Role == entity.RoleTool {
				m.Content = ""
				continue
			}
			// Evicted assistant messages with empty content and no tool calls are omitted.
			if m.Role == entity.RoleAssistant && m.Content == "" && len(m.ToolCalls) == 0 {
				m.Content = ""
				continue
			}
			// Evicted user messages (and assistant messages with content/tool calls)
			// keep a self-closing placeholder.
			if block := buildContextBlock(*m, userNames, messagesPath); block != "" {
				m.Content = block
			} else {
				m.Content = ""
			}
			continue
		}
		if m.Role == entity.RoleUser ||
			(m.Role == entity.RoleAssistant && m.Content != "") ||
			m.Role == entity.RoleTool {
			if block := buildContextBlock(*m, userNames, messagesPath); block != "" {
				m.Content = block + m.Content
			}
		}
	}
	return enriched
}

// countGroupTokens estimates the token cost of a message group.
// For assistant messages with tool calls, includes the JSON-encoded tool calls.
func countGroupTokens(g messageGroup) int {
	total := 0
	for _, m := range g.messages {
		if m.Evicted {
			// Evicted tool results and empty assistant messages produce no output.
			if m.Role == entity.RoleTool {
				continue
			}
			if m.Role == entity.RoleAssistant && m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			block := buildContextBlock(m, nil, "/messages")
			total += token.Count(block)
			continue
		}
		// Build the full rendered text for this message.
		rendered := ""
		if block := buildContextBlock(m, nil, "/messages"); block != "" {
			rendered += block
		}
		rendered += m.Content

		// Include tool call arguments in token count for assistant messages.
		if m.Role == entity.RoleAssistant && len(m.ToolCalls) > 0 {
			if data, err := json.Marshal(m.ToolCalls); err == nil {
				rendered += string(data)
			}
		}

		total += token.Count(rendered)
	}
	return total
}

// windowMessages selects groups from the end (newest first) that fit within
// the token budget. Stops at the first group that exceeds remaining budget
// (no skipping). Returns messages in chronological order.
func windowMessages(groups []messageGroup, budget int) []entity.Message {
	remaining := budget
	// Walk from newest to oldest.
	firstIncluded := len(groups) // exclusive start index
	for i := len(groups) - 1; i >= 0; i-- {
		if groups[i].tokens <= remaining {
			remaining -= groups[i].tokens
			firstIncluded = i
		} else {
			break
		}
	}

	// Flatten included groups in chronological order.
	var result []entity.Message
	for i := firstIncluded; i < len(groups); i++ {
		result = append(result, groups[i].messages...)
	}
	return result
}
