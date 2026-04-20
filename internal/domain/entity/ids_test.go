package entity

import "testing"

// TestChatIDStringReturnsSelf verifies that ChatID.String() returns the underlying string value.
func TestChatIDStringReturnsSelf(t *testing.T) {
	id := ChatID("chat-abc-123")
	if got := id.String(); got != "chat-abc-123" {
		t.Errorf("ChatID.String(): got %q, want %q", got, "chat-abc-123")
	}
}

// TestChatIDIsEmptyReturnsTrueForZeroValue verifies that a zero-value ChatID reports as empty.
func TestChatIDIsEmptyReturnsTrueForZeroValue(t *testing.T) {
	var id ChatID
	if !id.IsEmpty() {
		t.Error("ChatID.IsEmpty(): zero value should return true")
	}
}

// TestChatIDIsEmptyReturnsFalseForNonEmpty verifies that a populated ChatID is not considered empty.
func TestChatIDIsEmptyReturnsFalseForNonEmpty(t *testing.T) {
	id := ChatID("chat-abc-123")
	if id.IsEmpty() {
		t.Error("ChatID.IsEmpty(): non-empty ChatID should return false")
	}
}

// TestChatIDFollowsEstablishedTypedIDPattern verifies ChatID behaves consistently
// with the other typed IDs (TenantID, UserID, etc.) in the codebase.
func TestChatIDFollowsEstablishedTypedIDPattern(t *testing.T) {
	// String conversion roundtrip
	raw := "chat-tenant-7"
	id := ChatID(raw)
	if id.String() != raw {
		t.Errorf("roundtrip failed: got %q, want %q", id.String(), raw)
	}

	// Empty string is empty
	emptyID := ChatID("")
	if !emptyID.IsEmpty() {
		t.Error("ChatID(\"\").IsEmpty() should be true")
	}

	// Non-empty is not empty
	nonEmpty := ChatID("x")
	if nonEmpty.IsEmpty() {
		t.Error("ChatID(\"x\").IsEmpty() should be false")
	}
}
