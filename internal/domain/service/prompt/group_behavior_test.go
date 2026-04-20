package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// TestGroupConversationBehaviorPresentForGroupMessage verifies that the prompt
// template includes the "Group Conversation Behavior" section when IsGroup()=true (MSG-04).
func TestGroupConversationBehaviorPresentForGroupMessage(t *testing.T) {
	msg := entity.Message{
		ChatID: entity.ChatID("chat-behavior-test"), IsGroup: true,
	}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "## Group Conversation Behavior") {
		t.Error("expected '## Group Conversation Behavior' heading in group message prompt")
	}
	if !strings.Contains(prompt, "is_mentioned") {
		t.Error("expected 'is_mentioned' guidance text in group message prompt")
	}
}

// TestGroupConversationBehaviorAbsentForDM verifies that the "Group Conversation
// Behavior" section is NOT included in the prompt for DM messages (MSG-04).
func TestGroupConversationBehaviorAbsentForDM(t *testing.T) {
	msg := entity.Message{
		// No GroupID -- DM
	}
	prompt := buildPrompt(t, msg)

	if strings.Contains(prompt, "## Group Conversation Behavior") {
		t.Error("'## Group Conversation Behavior' should NOT appear in DM prompt")
	}
}

// TestGroupConversationBehaviorGuidanceText verifies the specific guidance text
// about is_mentioned metadata and [[no_reply]] is present for group messages (MSG-04).
func TestGroupConversationBehaviorGuidanceText(t *testing.T) {
	msg := entity.Message{
		ChatID: entity.ChatID("chat-guidance-test"), IsGroup: true,
	}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "[[no_reply]]") {
		t.Error("expected [[no_reply]] guidance in Group Conversation Behavior section")
	}
	// Verify the core instruction about when to respond.
	if !strings.Contains(prompt, "Not every message requires a response") {
		t.Error("expected 'Not every message requires a response' in Group Conversation Behavior")
	}
}

// ---------------------------------------------------------------------------
// Context-aware memory tests (GMEM-03, GMEM-04, GMEM-05)
// ---------------------------------------------------------------------------

// mockMemoryStore implements both JournalReader and MemoryReader for testing.
type mockMemoryStore struct {
	mockJournalReader
	memory      *entity.Memory
	memoryOwner string // captured ownerType:ownerID for assertions
}

func (m *mockMemoryStore) GetMemory(_ context.Context, _ entity.TenantID, ownerType, ownerID string) (*entity.Memory, error) {
	m.memoryOwner = ownerType + ":" + ownerID
	return m.memory, nil
}

func TestGroupMemoryInjectedInGroupContext(t *testing.T) {
	store := &mockMemoryStore{
		memory: &entity.Memory{Content: "Group likes Go and pizza"},
	}
	pb, err := NewPromptBuilder(nil, nil, store, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t1"), Name: "Test"}
	user := &entity.User{ID: entity.UserID("u1"), Name: "Alice"}
	msg := entity.Message{
		TenantID: entity.TenantID("t1"),
		ChatID:   entity.ChatID("chat-mem-test"), IsGroup: true,
	}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, msg, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	prompt := msgs[0].Content

	if store.memoryOwner != "chat:chat-mem-test" {
		t.Errorf("expected memory fetched for chat:chat-mem-test, got %q", store.memoryOwner)
	}
	if !strings.Contains(prompt, "### Group Memory") {
		t.Error("expected '### Group Memory' heading in group prompt")
	}
	if !strings.Contains(prompt, "Group likes Go and pizza") {
		t.Error("expected group memory content in prompt")
	}
}

func TestUserMemoryInjectedInDMContext(t *testing.T) {
	store := &mockMemoryStore{
		memory: &entity.Memory{Content: "User prefers dark mode"},
	}
	pb, err := NewPromptBuilder(nil, nil, store, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t1"), Name: "Test"}
	user := &entity.User{ID: entity.UserID("u1"), Name: "Alice"}
	msg := entity.Message{TenantID: entity.TenantID("t1")}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, msg, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	prompt := msgs[0].Content

	if store.memoryOwner != "user:u1" {
		t.Errorf("expected memory fetched for user:u1, got %q", store.memoryOwner)
	}
	if !strings.Contains(prompt, "What you remember about them") {
		t.Error("expected user memory section in DM prompt")
	}
	if !strings.Contains(prompt, "User prefers dark mode") {
		t.Error("expected user memory content in prompt")
	}
	if strings.Contains(prompt, "### Group Memory") {
		t.Error("'### Group Memory' should NOT appear in DM prompt")
	}
}

func TestGroupMemoryInstructionsContextAware(t *testing.T) {
	// Group context: should mention shared group memory
	groupMsg := entity.Message{ChatID: entity.ChatID("chat-instr-test"), IsGroup: true}
	groupPrompt := buildPrompt(t, groupMsg)
	if !strings.Contains(groupPrompt, "Shared group memory") {
		t.Error("expected 'Shared group memory' in group memory instructions")
	}
	if !strings.Contains(groupPrompt, "All group members share this memory") {
		t.Error("expected 'All group members share this memory' in group instructions")
	}

	// DM context: should mention user memory
	dmMsg := entity.Message{}
	dmPrompt := buildPrompt(t, dmMsg)
	if !strings.Contains(dmPrompt, "Long-term user memory") {
		t.Error("expected 'Long-term user memory' in DM memory instructions")
	}
	if strings.Contains(dmPrompt, "Shared group memory") {
		t.Error("'Shared group memory' should NOT appear in DM prompt")
	}
}

func TestNoMemoryWhenNilStore(t *testing.T) {
	// When journals is nil (no store), memory should not appear.
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t1"), Name: "Test"}
	user := &entity.User{ID: entity.UserID("u1"), Name: "Alice"}
	msg := entity.Message{TenantID: entity.TenantID("t1")}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, msg, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	prompt := msgs[0].Content
	if strings.Contains(prompt, "What you remember about them") {
		t.Error("memory section should not appear when store is nil")
	}
}

func TestNoDMOnlyReferenceInMemoryInstructions(t *testing.T) {
	// Verify "DM-only" and "DM contexts only" references are removed
	dmMsg := entity.Message{}
	dmPrompt := buildPrompt(t, dmMsg)
	if strings.Contains(dmPrompt, "Available in DM contexts only") {
		t.Error("should not contain 'Available in DM contexts only'")
	}
	if strings.Contains(dmPrompt, "Memory tools are DM-only") {
		t.Error("should not contain 'Memory tools are DM-only'")
	}
	if strings.Contains(dmPrompt, "skip the memory update pass entirely") {
		t.Error("should not contain 'skip the memory update pass entirely'")
	}
}
