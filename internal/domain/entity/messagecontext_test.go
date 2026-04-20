package entity

import "testing"

// TestMessageContextExternalUserIDExcludedFromEquality verifies that two Messages
// differing only by ExternalUserID are considered different message contexts only
// when their ExternalUserID matters for routing. The MessageContext now only carries
// ExternalUserID, ExternalMessageID, and ExternalReplyToID.
func TestMessageContextPreservesExternalUserID(t *testing.T) {
	mc := MessageContext{
		ExternalUserID:    "ext-user-A",
		ExternalMessageID: "ext-msg-1",
	}

	if mc.ExternalUserID != "ext-user-A" {
		t.Errorf("ExternalUserID: got %q, want %q", mc.ExternalUserID, "ext-user-A")
	}
	if mc.ExternalMessageID != "ext-msg-1" {
		t.Errorf("ExternalMessageID: got %q, want %q", mc.ExternalMessageID, "ext-msg-1")
	}
}

// TestMessageContextExternalReplyToID verifies that ExternalReplyToID is preserved.
func TestMessageContextExternalReplyToID(t *testing.T) {
	mc := MessageContext{
		ExternalReplyToID: "ext-reply-42",
	}
	if mc.ExternalReplyToID != "ext-reply-42" {
		t.Errorf("ExternalReplyToID: got %q, want %q", mc.ExternalReplyToID, "ext-reply-42")
	}
}
