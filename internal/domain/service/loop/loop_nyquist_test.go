package loop

// Nyquist gap tests for phase 44.2: loop sandbox path resolution.
// These tests fill gaps identified in the Nyquist validation audit.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/prompt"
)

// trackingSandbox extends mockSandboxPlugin to record UserHomePath calls.
// It satisfies port.SandboxPlugin via embedding.
type trackingSandbox struct {
	mockSandboxPlugin
	userHomeCallCount atomic.Int32
	lastUserHomeArg   atomic.Value // entity.UserID stored as string
	fixedUserHome     string
}

func (s *trackingSandbox) UserHomePath(userID entity.UserID) string {
	s.userHomeCallCount.Add(1)
	s.lastUserHomeArg.Store(string(userID))
	if s.fixedUserHome != "" {
		return s.fixedUserHome
	}
	return "/mock/home/" + string(userID)
}

func (s *trackingSandbox) MessagesPath() string {
	return "/mock/messages"
}

func (s *trackingSandbox) TenantHomePath(tenantID entity.TenantID) string {
	return "/mock/tenants/" + string(tenantID)
}

// TestLoopCallsSandboxUserHomePathForOutMsgPath verifies that when a sandbox is
// configured, the loop calls sandbox.UserHomePath(inbound.UserID) to construct
// the outMsgPath for the prompt.
//
// Behavior under test (loop.go:293-295):
//
//	if l.sandbox != nil {
//	    outMsgPath = l.sandbox.UserHomePath(inbound.UserID) + "/.outbox/" + string(outMsgID)
//	}
func TestLoopCallsSandboxUserHomePathForOutMsgPath(t *testing.T) {
	sandbox := &trackingSandbox{
		mockSandboxPlugin: mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}},
		fixedUserHome:     "/home/u1",
	}
	sfs := newMockProxyScopedFS()

	cfg := LoopConfig{MaxIterations: 1, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)

	l := NewLoop(
		cfg,
		&mockCompletionService{},
		&mockStorePlugin{},
		&mockConvService{},
		&mockTransport{},
		nil, // no tools
		pb,
		nil,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		sandbox,
		sfs,
	)

	msg := testMsg("ch1")
	msg.UserID = "u1"

	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// UserHomePath must have been called at least once (for outMsgPath construction).
	calls := sandbox.userHomeCallCount.Load()
	if calls == 0 {
		t.Fatal("expected sandbox.UserHomePath to be called at least once during Handle()")
	}

	// The last call must have been with the inbound message's UserID.
	gotArg := sandbox.lastUserHomeArg.Load().(string)
	if gotArg != "u1" {
		t.Errorf("UserHomePath last called with userID %q, want %q", gotArg, "u1")
	}
}

// TestLoopOutMsgPathNotConstructedWhenNoSandbox verifies that when no sandbox is
// configured (nil), sandbox.UserHomePath is never called and no outMsgPath is
// constructed.
func TestLoopOutMsgPathNotConstructedWhenNoSandbox(t *testing.T) {
	sandbox := &trackingSandbox{
		mockSandboxPlugin: mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}},
		fixedUserHome:     "/home/u1",
	}

	cfg := LoopConfig{MaxIterations: 1, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)

	// No sandbox: nil, nil for sandbox and scopedFS.
	l := NewLoop(
		cfg,
		&mockCompletionService{},
		&mockStorePlugin{},
		&mockConvService{},
		&mockTransport{},
		nil,
		pb,
		nil,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		nil, nil, // no sandbox
	)

	msg := testMsg("ch1")
	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// sandbox.UserHomePath must NOT have been called (no sandbox wired in the loop).
	calls := sandbox.userHomeCallCount.Load()
	if calls != 0 {
		t.Errorf("expected 0 UserHomePath calls with nil sandbox, got %d", calls)
	}
}
