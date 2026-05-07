package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/token"
)

func promptFootprint(messages []entity.Message) int {
	total := 0
	for _, msg := range messages {
		total += token.Count(msg.Content)
		if msg.Role == entity.RoleAssistant && len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				total += token.Count(string(data))
			}
		}
	}
	return total
}

func TestCompactionSignalNilWhenConfigMissing(t *testing.T) {
	conv := &mockConvService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "hello"},
		},
	}
	pb, err := NewPromptBuilder(nil, conv, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	_, signal, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if signal != nil {
		t.Fatalf("signal = %#v, want nil", signal)
	}
}

func TestCompactionSignalNilBelowThreshold(t *testing.T) {
	conv := &mockConvService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "hello"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "world"},
		},
	}
	pb, err := NewPromptBuilder(nil, conv, nil, nil, 64000, &config.CompactionConfig{Model: "compact", Threshold: 0.95}, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	_, signal, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if signal != nil {
		t.Fatalf("signal = %#v, want nil", signal)
	}
}

func TestCompactionSignalEmittedAtThreshold(t *testing.T) {
	conv := &mockConvService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "hello"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "world"},
		},
	}

	baseline, err := NewPromptBuilder(nil, conv, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder baseline: %v", err)
	}
	messages, _, err := baseline.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build baseline: %v", err)
	}
	footprint := promptFootprint(messages)
	tokenBudget := footprint + 8
	threshold := float64(footprint) / float64(tokenBudget)

	pb, err := NewPromptBuilder(nil, conv, nil, nil, tokenBudget, &config.CompactionConfig{Model: "compact", Threshold: threshold}, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	_, signal, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if signal == nil {
		t.Fatal("expected non-nil compaction signal")
	}
	if signal.LatestMessageID != "msg-2" {
		t.Fatalf("LatestMessageID = %q, want %q", signal.LatestMessageID, "msg-2")
	}
}

func TestCompactionSignalPrefersUpToIDBoundary(t *testing.T) {
	conv := &mockConvService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "hello"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "world"},
		},
	}

	baseline, err := NewPromptBuilder(nil, conv, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder baseline: %v", err)
	}
	messages, _, err := baseline.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build baseline: %v", err)
	}
	footprint := promptFootprint(messages)
	tokenBudget := footprint + 8
	threshold := float64(footprint) / float64(tokenBudget)

	pb, err := NewPromptBuilder(nil, conv, nil, nil, tokenBudget, &config.CompactionConfig{Model: "compact", Threshold: threshold}, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	upToID := entity.MessageID("msg-boundary")
	_, signal, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, &upToID)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if signal == nil {
		t.Fatal("expected non-nil compaction signal")
	}
	if signal.LatestMessageID != upToID {
		t.Fatalf("LatestMessageID = %q, want %q", signal.LatestMessageID, upToID)
	}
}

func TestCompactionSignalEmittedWithoutHistoryTruncation(t *testing.T) {
	baseTime := time.Date(2026, 4, 6, 3, 0, 0, 0, time.UTC)
	filler := strings.Repeat("x", 400)

	history := make([]entity.Message, 0, 64)
	for i := 0; i < 64; i++ {
		role := entity.RoleUser
		if i%2 == 1 {
			role = entity.RoleAssistant
		}
		history = append(history, entity.Message{
			ID:        entity.MessageID(fmt.Sprintf("msg-%02d", i)),
			Role:      role,
			Content:   filler,
			CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
		})
	}

	conv := &mockConvService{history: history}

	baseline, err := NewPromptBuilder(nil, conv, nil, nil, 200000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder baseline: %v", err)
	}
	fullMessages, _, err := baseline.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build baseline: %v", err)
	}

	fullFootprint := promptFootprint(fullMessages)
	threshold := 0.95
	tokenBudget := int(float64(fullFootprint)*0.85) + 1
	if float64(fullFootprint) <= threshold*float64(tokenBudget) {
		t.Fatalf("test setup invalid: full footprint %d does not exceed threshold %.2f of budget %d", fullFootprint, threshold, tokenBudget)
	}

	pb, err := NewPromptBuilder(nil, conv, &mockStoreReader{}, nil, tokenBudget, &config.CompactionConfig{Model: "compact", Threshold: threshold}, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	messages, signal, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{ID: "msg-inbound", TenantID: "tenant-1", ChatID: "chat-1"}, "conv-1", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if signal == nil {
		t.Fatal("expected non-nil compaction signal")
	}
	historyCount := 0
	for _, msg := range messages {
		if msg.Role != entity.RoleSystem {
			historyCount++
		}
	}
	if historyCount >= len(history) {
		t.Fatalf("expected prompt history to be windowed below %d messages, got %d", len(history), historyCount)
	}
	if got := float64(promptFootprint(messages)) / float64(tokenBudget); got < threshold {
		t.Fatalf("expected final prompt ratio %.3f to stay above threshold %.3f", got, threshold)
	}
}
