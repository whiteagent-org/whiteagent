package telegram

import (
	"testing"
)

// TestBuildMetadataGroupMentionedSetsIsMentionedTrue verifies that buildMetadata
// sets is_mentioned="true" for a group message where the bot is mentioned (MSG-03).
func TestBuildMetadataGroupMentionedSetsIsMentionedTrue(t *testing.T) {
	msg := &tgMessage{
		Chat: tgChat{ID: 100, Type: "supergroup", Title: "Dev Group"},
		Date: 1709827200,
	}
	// isGroup=true, isMention=true
	meta := buildMetadata(msg, true, true)

	v, ok := meta["is_mentioned"]
	if !ok {
		t.Fatal("expected metadata key 'is_mentioned' to be present for group+mentioned message")
	}
	if v != "true" {
		t.Errorf("is_mentioned: got %q, want %q", v, "true")
	}
}

// TestBuildMetadataGroupNotMentionedSetsIsMentionedFalse verifies that buildMetadata
// sets is_mentioned="false" for a group message where the bot is NOT mentioned (MSG-03).
func TestBuildMetadataGroupNotMentionedSetsIsMentionedFalse(t *testing.T) {
	msg := &tgMessage{
		Chat: tgChat{ID: 100, Type: "supergroup", Title: "Dev Group"},
		Date: 1709827200,
	}
	// isGroup=true, isMention=false
	meta := buildMetadata(msg, true, false)

	v, ok := meta["is_mentioned"]
	if !ok {
		t.Fatal("expected metadata key 'is_mentioned' to be present for group+not-mentioned message")
	}
	if v != "false" {
		t.Errorf("is_mentioned: got %q, want %q", v, "false")
	}
}
