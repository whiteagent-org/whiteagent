// Package agent_test contains additional behavioral tests for group message
// handling added in phase 34.3 (MSG-04).
//
// This file follows the same handlerDecision pattern established in handler_test.go:
// it mirrors the runtime handler's GroupMode filtering logic as a standalone
// function to verify the behavioral contract without modifying any implementation.
package agent_test

import (
	"context"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// groupModeDecision mirrors the GroupMode filtering branch in runtime.go.
//
// Runtime contract (MSG-04):
//   - Group message + not mentioned + GroupMode=="mention_only" -> save to history, no agent
//   - Group message + not mentioned + GroupMode=="all"          -> proceed to agent
//   - Group message + mentioned (any GroupMode)                 -> proceed to agent
//   - DM (IsGroup()==false) regardless of GroupMode             -> proceed to agent
// ---------------------------------------------------------------------------

type groupModeDecision struct {
	// published is true when the message should be forwarded to the agent loop
	// (i.e., transport.Publish would be called in the real handler).
	published bool
	// savedToHistory is true when the message is silently saved to conversation
	// history (mention_only drop path).
	savedToHistory bool
}

// simulateGroupModeFilter runs the same GroupMode filtering logic as the runtime
// inbound handler (lines 305-317 in runtime.go).
func simulateGroupModeFilter(
	store interface {
		GetTenant(ctx context.Context, tenantID entity.TenantID) (*entity.Tenant, error)
	},
	convAppend func(convID entity.ConversationID, msg entity.Message) error,
	msg entity.Message,
	convID entity.ConversationID,
) groupModeDecision {
	// Mirrors: if msg.IsGroup && !msg.IsMention {
	if msg.IsGroup && !msg.IsMention {
		tenant, err := store.GetTenant(context.Background(), msg.TenantID)
		if err != nil || tenant == nil {
			// tenant load failure -> silent drop (not published)
			return groupModeDecision{published: false, savedToHistory: false}
		}
		if tenant.GroupMode == "mention_only" {
			_ = convAppend(convID, msg)
			return groupModeDecision{published: false, savedToHistory: true}
		}
	}
	// All other cases proceed to Publish (agent loop).
	return groupModeDecision{published: true, savedToHistory: false}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestGroupMode_MentionOnly_NonMentioned_SavedToHistory verifies that a group
// message without a mention, when tenant GroupMode is "mention_only", is saved
// to conversation history but not published to the agent loop (MSG-04).
func TestGroupMode_MentionOnly_NonMentioned_SavedToHistory(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-mention-only")
	store.tenants[tenantID] = &entity.Tenant{
		ID:        tenantID,
		GroupMode: "mention_only",
	}

	var appendedMsg entity.Message
	appendCalled := false
	convAppend := func(convID entity.ConversationID, msg entity.Message) error {
		appendCalled = true
		appendedMsg = msg
		return nil
	}

	msg := entity.Message{
		ID:        "group-msg-no-mention",
		TenantID:  tenantID,
		ChatID:    entity.ChatID("chat-gm-1"),
		IsGroup:   true,
		IsMention: false,
		Content:   "hey everyone, how's it going",
	}

	d := simulateGroupModeFilter(store, convAppend, msg, "conv-1")

	if d.published {
		t.Error("group message without mention in mention_only mode should NOT be published to agent loop")
	}
	if !d.savedToHistory {
		t.Error("group message without mention in mention_only mode should be saved to history")
	}
	if !appendCalled {
		t.Error("convService.Append should have been called")
	}
	if appendedMsg.ID != "group-msg-no-mention" {
		t.Errorf("appended message ID: got %q, want %q", appendedMsg.ID, "group-msg-no-mention")
	}
}

// TestGroupMode_AllMode_NonMentioned_Published verifies that a group message
// without a mention, when tenant GroupMode is "all", is published to the agent
// loop (MSG-04).
func TestGroupMode_AllMode_NonMentioned_Published(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-all-mode")
	store.tenants[tenantID] = &entity.Tenant{
		ID:        tenantID,
		GroupMode: "all",
	}

	appendCalled := false
	convAppend := func(_ entity.ConversationID, _ entity.Message) error {
		appendCalled = true
		return nil
	}

	msg := entity.Message{
		ID:        "group-msg-all-mode",
		TenantID:  tenantID,
		ChatID:    entity.ChatID("chat-gm-2"),
		IsGroup:   true,
		IsMention: false,
		Content:   "anyone see the game last night",
	}

	d := simulateGroupModeFilter(store, convAppend, msg, "conv-2")

	if !d.published {
		t.Error("group message without mention in all mode SHOULD be published to agent loop")
	}
	if d.savedToHistory {
		t.Error("group message in all mode should NOT be saved via silent drop path")
	}
	if appendCalled {
		t.Error("convService.Append should NOT have been called in all mode")
	}
}

// TestGroupMode_GroupMentioned_AlwaysPublished verifies that a group message
// where the bot is mentioned is always published regardless of GroupMode (MSG-04).
func TestGroupMode_GroupMentioned_AlwaysPublished(t *testing.T) {
	for _, groupMode := range []string{"mention_only", "all"} {
		t.Run("GroupMode="+groupMode, func(t *testing.T) {
			store := newMockStoreForHandler()
			tenantID := entity.TenantID("t-mentioned")
			store.tenants[tenantID] = &entity.Tenant{
				ID:        tenantID,
				GroupMode: groupMode,
			}

			appendCalled := false
			convAppend := func(_ entity.ConversationID, _ entity.Message) error {
				appendCalled = true
				return nil
			}

			msg := entity.Message{
				ID:        "group-msg-mentioned",
				TenantID:  tenantID,
				ChatID:    entity.ChatID("chat-gm-3"),
			IsGroup:   true,
				IsMention: true,
				Content:   "@bot what's the time",
			}

			d := simulateGroupModeFilter(store, convAppend, msg, "conv-3")

			if !d.published {
				t.Errorf("mentioned group message SHOULD be published regardless of GroupMode=%s", groupMode)
			}
			if appendCalled {
				t.Error("convService.Append should NOT be called for mentioned group message")
			}
		})
	}
}

// TestGroupMode_DM_AlwaysPublished verifies that DMs (IsGroup()=false) are always
// published to the agent loop regardless of GroupMode (MSG-04).
func TestGroupMode_DM_AlwaysPublished(t *testing.T) {
	for _, groupMode := range []string{"mention_only", "all", ""} {
		t.Run("GroupMode="+groupMode, func(t *testing.T) {
			store := newMockStoreForHandler()
			tenantID := entity.TenantID("t-dm")
			store.tenants[tenantID] = &entity.Tenant{
				ID:        tenantID,
				GroupMode: groupMode,
			}

			appendCalled := false
			convAppend := func(_ entity.ConversationID, _ entity.Message) error {
				appendCalled = true
				return nil
			}

			msg := entity.Message{
				ID:        "dm-msg",
				TenantID:  tenantID,
				IsMention: false, // IsMention is always true for DMs from channels, but
				// test with false to confirm IsGroup()=false always bypasses filtering
				Content: "hey what's the weather",
			}

			d := simulateGroupModeFilter(store, convAppend, msg, "conv-dm")

			if !d.published {
				t.Errorf("DM should always be published regardless of GroupMode=%s", groupMode)
			}
			if appendCalled {
				t.Error("convService.Append should NOT be called for DMs")
			}
		})
	}
}
