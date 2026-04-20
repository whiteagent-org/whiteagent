package entity

import "testing"

// TestIsGroupReturnsTrueWhenSet verifies that Message.IsGroup is true when set.
func TestIsGroupReturnsTrueWhenSet(t *testing.T) {
	m := Message{ChatID: ChatID("chat-123"), IsGroup: true}
	if !m.IsGroup {
		t.Error("IsGroup: expected true for group message")
	}
}

// TestIsGroupReturnsFalseForDMMessage verifies that Message.IsGroup
// is false for DM messages.
func TestIsGroupReturnsFalseForDMMessage(t *testing.T) {
	m := Message{
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   "chat-456",
		IsGroup:  false,
	}
	if m.IsGroup {
		t.Error("IsGroup: expected false for DM message")
	}
}

// TestIsGroupIsEagerBoolField verifies that IsGroup is an eager bool field
// set at creation time, not derived from other fields.
func TestIsGroupIsEagerBoolField(t *testing.T) {
	// A message with a ChatID but IsGroup=false is a DM chat.
	m := Message{
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   "chat-dm-42",
		IsGroup:  false,
	}
	if m.IsGroup {
		t.Error("IsGroup: should be false even with ChatID set (DM chat)")
	}
}
