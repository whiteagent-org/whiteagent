package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// ---------------------------------------------------------------------------
// Mocks for proxy tests
// ---------------------------------------------------------------------------

// mockSecretEnvProvider implements secretEnvProvider for testing.
type mockSecretEnvProvider struct {
	entries []secret.SecretEnvEntry
	err     error
	calls   int
}

func (m *mockSecretEnvProvider) EnvVars(_ context.Context, _ entity.TenantID, _ entity.UserID) ([]secret.SecretEnvEntry, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.entries, nil
}

// mockSandboxPlugin implements port.SandboxPlugin for proxy tests.
type mockSandboxPlugin struct {
	ensureCalls  int
	execCalls    []port.ExecRequest
	releaseCalls int
	execResult   port.ExecResult
	execErr      error
}

func (m *mockSandboxPlugin) ID() string                                          { return "mock-sandbox" }
func (m *mockSandboxPlugin) Kind() entity.PluginKind                             { return entity.PluginKindSandbox }
func (m *mockSandboxPlugin) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (m *mockSandboxPlugin) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockSandboxPlugin) Start(context.Context) error                         { return nil }
func (m *mockSandboxPlugin) Stop(context.Context) error                          { return nil }

func (m *mockSandboxPlugin) Ensure(_ context.Context, _ entity.UserID) (string, error) {
	m.ensureCalls++
	return "/work", nil
}
func (m *mockSandboxPlugin) Exec(_ context.Context, _ entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	m.execCalls = append(m.execCalls, req)
	return m.execResult, m.execErr
}
func (m *mockSandboxPlugin) Release(_ context.Context, _ entity.UserID) error {
	m.releaseCalls++
	return nil
}
func (m *mockSandboxPlugin) UserHomePath(userID entity.UserID) string {
	return "/mock/users/" + string(userID)
}
func (m *mockSandboxPlugin) TenantHomePath(tenantID entity.TenantID) string {
	return "/mock/tenants/" + string(tenantID)
}
func (m *mockSandboxPlugin) MessagesPath() string {
	return "/mock/messages"
}

// mockTransferableSandbox implements both SandboxPlugin and FileTransferable.
type mockTransferableSandbox struct {
	mockSandboxPlugin
	copyToCalls   []copyCall
	copyFromCalls []copyCall
	copyToErr     error
	copyFromErr   error
}

type copyCall struct {
	userID        entity.UserID
	hostPath      string
	containerPath string
}

func (m *mockTransferableSandbox) CopyTo(_ context.Context, userID entity.UserID, hostPath, containerPath string) error {
	m.copyToCalls = append(m.copyToCalls, copyCall{userID: userID, hostPath: hostPath, containerPath: containerPath})
	return m.copyToErr
}

func (m *mockTransferableSandbox) CopyFrom(_ context.Context, userID entity.UserID, containerPath, hostPath string) error {
	m.copyFromCalls = append(m.copyFromCalls, copyCall{userID: userID, hostPath: containerPath, containerPath: hostPath})
	return m.copyFromErr
}

// mockMountableSandbox implements SandboxPlugin, FileTransferable, and MountEnsurer.
type mockMountableSandbox struct {
	mockTransferableSandbox
	ensureWithMountsCalls [][]port.Mount
}

func (m *mockMountableSandbox) EnsureWithMounts(_ context.Context, _ entity.UserID, mounts []port.Mount) (string, error) {
	m.ensureWithMountsCalls = append(m.ensureWithMountsCalls, mounts)
	return "/work", nil
}

// mockScopedFS implements port.ScopedFS for testing.
type mockProxyScopedFS struct {
	dirs     map[string]string // scope:id -> path
	ensured  []string
	ensureOK bool
}

func newMockProxyScopedFS() *mockProxyScopedFS {
	return &mockProxyScopedFS{dirs: make(map[string]string), ensureOK: true}
}

func (m *mockProxyScopedFS) EnsureDir(scope port.Scope, id string) (string, error) {
	key := fmt.Sprintf("%s:%s", scope, id)
	m.ensured = append(m.ensured, key)
	if p, ok := m.dirs[key]; ok {
		return p, nil
	}
	p := "/mock/" + scope.String() + "/" + id
	m.dirs[key] = p
	return p, nil
}

func (m *mockProxyScopedFS) GetDir(scope port.Scope, id string) (string, error) {
	key := fmt.Sprintf("%s:%s", scope, id)
	if p, ok := m.dirs[key]; ok {
		return p, nil
	}
	p := "/mock/" + scope.String() + "/" + id
	m.dirs[key] = p
	return p, nil
}

func (m *mockProxyScopedFS) BaseDir() string                  { return "/mock" }
func (m *mockProxyScopedFS) Cleanup(port.Scope, string) error { return nil }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestProxyExecMergesSecretEnvVars(t *testing.T) {
	secrets := &mockSecretEnvProvider{
		entries: []secret.SecretEnvEntry{
			{Key: "api-key", Value: "secret123", Mode: entity.SecretModeValue},
			{Key: "my token", Value: "tok456", Mode: entity.SecretModeValue},
		},
	}
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()

	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := port.ExecRequest{Command: "echo", Args: []string{"hello"}}
	_, err = proxy.Exec(context.Background(), "u1", req)
	if err != nil {
		t.Fatal(err)
	}

	if len(sandbox.execCalls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(sandbox.execCalls))
	}
	got := sandbox.execCalls[0]
	if got.Env["API_KEY"] != "secret123" {
		t.Errorf("API_KEY = %q, want %q", got.Env["API_KEY"], "secret123")
	}
	if got.Env["MY_TOKEN"] != "tok456" {
		t.Errorf("MY_TOKEN = %q, want %q", got.Env["MY_TOKEN"], "tok456")
	}
}

func TestProxyNormalizeEnvKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"api-key", "API_KEY"},
		{"my token", "MY_TOKEN"},
		{"SIMPLE", "SIMPLE"},
		{"a-b c-d", "A_B_C_D"},
	}
	for _, tt := range tests {
		got := normalizeEnvKey(tt.in)
		if got != tt.want {
			t.Errorf("normalizeEnvKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProxyExecSetsDefaultWorkDir(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	req := port.ExecRequest{Command: "ls"}
	proxy.Exec(context.Background(), "u1", req)

	if sandbox.execCalls[0].WorkDir != "/home/whiteagent" {
		t.Errorf("WorkDir = %q, want /home/whiteagent", sandbox.execCalls[0].WorkDir)
	}
}

func TestProxyExecPreservesExplicitWorkDir(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	req := port.ExecRequest{Command: "ls", WorkDir: "/custom"}
	proxy.Exec(context.Background(), "u1", req)

	if sandbox.execCalls[0].WorkDir != "/custom" {
		t.Errorf("WorkDir = %q, want /custom", sandbox.execCalls[0].WorkDir)
	}
}

func TestProxyExecNoCopyTo(t *testing.T) {
	sandbox := &mockTransferableSandbox{
		mockSandboxPlugin: mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}},
	}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")

	// Exec should not trigger any CopyTo calls (CopyTo removed).
	proxy.Exec(context.Background(), "u1", port.ExecRequest{Command: "ls"})

	if len(sandbox.copyToCalls) != 0 {
		t.Errorf("expected 0 CopyTo calls, got %d", len(sandbox.copyToCalls))
	}
}

func TestProxyExecNoMkdirPrepend(t *testing.T) {
	sandbox := &mockTransferableSandbox{
		mockSandboxPlugin: mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}},
	}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")

	// Shell-style exec (sh -c) should pass through unchanged (no mkdir prefix).
	proxy.Exec(context.Background(), "u1", port.ExecRequest{
		Command: "sh",
		Args:    []string{"-c", "echo hello"},
	})

	if len(sandbox.execCalls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(sandbox.execCalls))
	}
	got := sandbox.execCalls[0]
	if got.Command != "sh" {
		t.Errorf("Command = %q, want sh", got.Command)
	}
	if len(got.Args) != 2 || got.Args[0] != "-c" {
		t.Fatalf("Args = %v, want ['-c', ...]", got.Args)
	}
	if got.Args[1] != "echo hello" {
		t.Errorf("Args[1] = %q, want %q", got.Args[1], "echo hello")
	}
}

func TestProxyHarvestOutgoingRenamesDir(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}

	tmpDir := t.TempDir()
	userHome := filepath.Join(tmpDir, "users", "u1")
	messagesDir := filepath.Join(tmpDir, "messages", "t1", "conv1")
	msgDir := filepath.Join(messagesDir, "out1")
	outboxDir := filepath.Join(userHome, ".outbox", "out1")
	// Create outbox with a file (simulates what the LLM wrote).
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Ensure parent conv messages dir exists (but NOT msgDir itself).
	if err := os.MkdirAll(messagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outboxDir, "hello.txt"), []byte("helooo"), 0o644); err != nil {
		t.Fatal(err)
	}

	sfs := newMockProxyScopedFS()
	sfs.dirs["user:u1"] = userHome
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", messagesDir, "/messages")

	dir, err := proxy.HarvestOutgoing(context.Background(), "out1")
	if err != nil {
		t.Fatal(err)
	}
	if dir != msgDir {
		t.Errorf("dir = %q, want %q", dir, msgDir)
	}

	// File should be at the message dir path (outbox was renamed to it).
	data, err := os.ReadFile(filepath.Join(msgDir, "hello.txt"))
	if err != nil {
		t.Fatalf("expected hello.txt in message dir: %v", err)
	}
	if string(data) != "helooo" {
		t.Errorf("file content = %q, want %q", string(data), "helooo")
	}

	// Outbox dir should no longer exist (it was renamed).
	if _, err := os.Stat(outboxDir); !os.IsNotExist(err) {
		t.Error("expected outbox dir to be gone after rename")
	}
}

func TestProxyHarvestOutgoingEmptyOutbox(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}

	tmpDir := t.TempDir()
	userHome := filepath.Join(tmpDir, "users", "u1")
	if err := os.MkdirAll(userHome, 0o755); err != nil {
		t.Fatal(err)
	}

	sfs := newMockProxyScopedFS()
	sfs.dirs["user:u1"] = userHome
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")

	// No outbox dir exists — harvest should return dir with tenant/conv structure.
	dir, err := proxy.HarvestOutgoing(context.Background(), "out1")
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Error("expected non-empty host directory path")
	}
	// Path must include tenant and conversation IDs, not just message ID.
	wantSuffix := filepath.Join("t1", "conv1", "out1")
	if !filepath.IsAbs(dir) || !contains(dir, wantSuffix) {
		t.Errorf("dir = %q, want path containing %q", dir, wantSuffix)
	}
}

// TestProxyHarvestOutgoingNoInboundAttachments verifies that when the proxy is
// created without a caller-provided messagesDir (no inbound attachments), the
// harvest path still uses the correct messages/{tenant}/{conv}/{msg} structure.
func TestProxyHarvestOutgoingNoInboundAttachments(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}

	tmpDir := t.TempDir()
	userHome := filepath.Join(tmpDir, "users", "u1")
	outboxDir := filepath.Join(userHome, ".outbox", "out1")
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outboxDir, "result.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	sfs := newMockProxyScopedFS()
	sfs.dirs["user:u1"] = userHome
	// Pre-set the message scope path so it resolves under tmpDir.
	sfs.dirs["message:"+filepath.Join("t1", "conv1")] = filepath.Join(tmpDir, "messages", "t1", "conv1")
	secrets := &mockSecretEnvProvider{entries: nil}

	// No messagesDir from caller — proxy computes it via scopedFS.
	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	dir, err := proxy.HarvestOutgoing(context.Background(), "out1")
	if err != nil {
		t.Fatal(err)
	}

	// Path must follow messages/{tenant}/{conv}/{msg} structure.
	wantSuffix := filepath.Join("t1", "conv1", "out1")
	if !contains(dir, wantSuffix) {
		t.Errorf("dir = %q, want path containing %q", dir, wantSuffix)
	}

	// File should be harvested.
	data, err := os.ReadFile(filepath.Join(dir, "result.png"))
	if err != nil {
		t.Fatalf("expected result.png in message dir: %v", err)
	}
	if string(data) != "png" {
		t.Errorf("file content = %q, want %q", string(data), "png")
	}
}

// TestProxyHarvestOutgoingEmptyOutboxDir verifies that when the outbox directory
// exists but is empty (PrepareOutbox created it, but no files were written),
// HarvestOutgoing returns early without creating the message directory.
func TestProxyHarvestOutgoingEmptyOutboxDir(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}

	tmpDir := t.TempDir()
	userHome := filepath.Join(tmpDir, "users", "u1")
	outboxDir := filepath.Join(userHome, ".outbox", "out1")
	// Create empty outbox dir (simulates PrepareOutbox with no LLM file output).
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatal(err)
	}

	messagesDir := filepath.Join(tmpDir, "messages", "t1", "conv1")

	sfs := newMockProxyScopedFS()
	sfs.dirs["user:u1"] = userHome
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", messagesDir, "/messages")

	dir, err := proxy.HarvestOutgoing(context.Background(), "out1")
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Error("expected non-empty host directory path")
	}

	// Message directory should NOT have been created.
	msgDir := filepath.Join(messagesDir, "out1")
	if _, err := os.Stat(msgDir); !os.IsNotExist(err) {
		t.Errorf("expected message dir %q to not exist (empty outbox should not create it)", msgDir)
	}

	// Conversation directory should NOT have been created either.
	if _, err := os.Stat(messagesDir); !os.IsNotExist(err) {
		t.Errorf("expected conversation dir %q to not exist", messagesDir)
	}

	// Empty outbox dir should have been cleaned up.
	if _, err := os.Stat(outboxDir); !os.IsNotExist(err) {
		t.Error("expected empty outbox dir to be cleaned up")
	}
}

// contains checks if path contains the given substring.
func contains(path, sub string) bool {
	return strings.Contains(path, sub)
}

func TestProxyEnsureDelegatesToReal(t *testing.T) {
	sandbox := &mockSandboxPlugin{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	_, err := proxy.Ensure(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.ensureCalls != 1 {
		t.Errorf("expected 1 Ensure call, got %d", sandbox.ensureCalls)
	}
}

func TestProxyReleaseDelegatesToReal(t *testing.T) {
	sandbox := &mockSandboxPlugin{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	err := proxy.Release(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.releaseCalls != 1 {
		t.Errorf("expected 1 Release call, got %d", sandbox.releaseCalls)
	}
}

func TestProxyEnsureForwardsMounts(t *testing.T) {
	sandbox := &mockMountableSandbox{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	msgsDir := filepath.Join(t.TempDir(), "messages", "t1", "conv1")

	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", msgsDir, "/messages")
	if err != nil {
		t.Fatal(err)
	}

	_, err = proxy.Ensure(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}

	// EnsureWithMounts should have been called (not plain Ensure).
	if len(sandbox.ensureWithMountsCalls) != 1 {
		t.Fatalf("expected 1 EnsureWithMounts call, got %d", len(sandbox.ensureWithMountsCalls))
	}
	if sandbox.ensureCalls != 0 {
		t.Errorf("expected 0 plain Ensure calls, got %d", sandbox.ensureCalls)
	}

	mounts := sandbox.ensureWithMountsCalls[0]
	if len(mounts) != 3 {
		t.Fatalf("expected 3 mounts, got %d", len(mounts))
	}

	// First mount: userHome -> /home/whiteagent (RW).
	if mounts[0].Target != "/home/whiteagent" {
		t.Errorf("mount[0].Target = %q, want /home/whiteagent", mounts[0].Target)
	}
	if mounts[0].ReadOnly {
		t.Error("mount[0] should be RW")
	}

	// Second mount: tenantHome -> /tenant (RO).
	if mounts[1].Target != "/tenant" {
		t.Errorf("mount[1].Target = %q, want /tenant", mounts[1].Target)
	}
	if !mounts[1].ReadOnly {
		t.Error("mount[1] should be RO")
	}

	// Third mount: messagesDir -> /messages (RO).
	if mounts[2].Target != "/messages" {
		t.Errorf("mount[2].Target = %q, want /messages", mounts[2].Target)
	}
	if !mounts[2].ReadOnly {
		t.Error("mount[2] should be RO")
	}
	if mounts[2].Source != msgsDir {
		t.Errorf("mount[2].Source = %q, want %q", mounts[2].Source, msgsDir)
	}
}

func TestProxyEnsureSkipsMessagesMountWhenNoTarget(t *testing.T) {
	sandbox := &mockMountableSandbox{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	// No messagesTarget — messages mount should be skipped even though
	// messagesDir is computed internally by the proxy.
	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = proxy.Ensure(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}

	mounts := sandbox.ensureWithMountsCalls[0]
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts (no messages mount without target), got %d", len(mounts))
	}
}

func TestProxyEnsureAlwaysMountsMessagesWhenTargetSet(t *testing.T) {
	sandbox := &mockMountableSandbox{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	// No caller-provided messagesDir, but messagesTarget is set.
	// Proxy should compute messagesDir and mount it.
	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "/messages")
	if err != nil {
		t.Fatal(err)
	}

	_, err = proxy.Ensure(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}

	mounts := sandbox.ensureWithMountsCalls[0]
	if len(mounts) != 3 {
		t.Fatalf("expected 3 mounts (messages always mounted when target set), got %d", len(mounts))
	}
	if mounts[2].Target != "/messages" {
		t.Errorf("mount[2].Target = %q, want /messages", mounts[2].Target)
	}
	if !mounts[2].ReadOnly {
		t.Error("mount[2] should be RO")
	}
}

func TestProxyEnsureFallsBackForNonMountable(t *testing.T) {
	// Plain mock (no MountEnsurer).
	sandbox := &mockSandboxPlugin{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = proxy.Ensure(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}

	// Plain Ensure should have been called.
	if sandbox.ensureCalls != 1 {
		t.Errorf("expected 1 plain Ensure call, got %d", sandbox.ensureCalls)
	}
}

func TestProxyPrepareOutboxExecsMkdir(t *testing.T) {
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	err := proxy.PrepareOutbox(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}

	if len(sandbox.execCalls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(sandbox.execCalls))
	}
	got := sandbox.execCalls[0]
	if got.Command != "mkdir" {
		t.Errorf("Command = %q, want mkdir", got.Command)
	}
	if len(got.Args) != 2 || got.Args[0] != "-p" || got.Args[1] != "/home/whiteagent/.outbox/out1" {
		t.Errorf("Args = %v, want ['-p', '/home/whiteagent/.outbox/out1']", got.Args)
	}
	if got.WorkDir != "/home/whiteagent" {
		t.Errorf("WorkDir = %q, want /home/whiteagent", got.WorkDir)
	}
}

func TestProxyPathMethodsDelegateToReal(t *testing.T) {
	sandbox := &mockSandboxPlugin{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if got := proxy.UserHomePath("u1"); got != "/mock/users/u1" {
		t.Errorf("UserHomePath = %q, want /mock/users/u1", got)
	}
	if got := proxy.TenantHomePath("t1"); got != "/mock/tenants/t1" {
		t.Errorf("TenantHomePath = %q, want /mock/tenants/t1", got)
	}
	if got := proxy.MessagesPath(); got != "/mock/messages" {
		t.Errorf("MessagesPath = %q, want /mock/messages", got)
	}
}

func TestProxyNewUsesEnsureDirForTenant(t *testing.T) {
	sandbox := &mockSandboxPlugin{}
	sfs := newMockProxyScopedFS()
	secrets := &mockSecretEnvProvider{entries: nil}

	_, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Both user and tenant should use EnsureDir (not GetDir).
	foundUser := false
	foundTenant := false
	for _, key := range sfs.ensured {
		if key == "user:u1" {
			foundUser = true
		}
		if key == "tenant:t1" {
			foundTenant = true
		}
	}
	if !foundUser {
		t.Error("expected EnsureDir for user scope")
	}
	if !foundTenant {
		t.Error("expected EnsureDir for tenant scope (not GetDir)")
	}
}

func TestProxySecretsResolvedFreshPerExec(t *testing.T) {
	secrets := &mockSecretEnvProvider{entries: []secret.SecretEnvEntry{{Key: "k", Value: "v", Mode: entity.SecretModeValue}}}
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")

	proxy.Exec(context.Background(), "u1", port.ExecRequest{Command: "a"})
	proxy.Exec(context.Background(), "u1", port.ExecRequest{Command: "b"})

	if secrets.calls != 2 {
		t.Errorf("expected EnvVars called 2 times, got %d", secrets.calls)
	}
}

func TestProxyExecFileModeSetsPathEnvVar(t *testing.T) {
	secrets := &mockSecretEnvProvider{
		entries: []secret.SecretEnvEntry{
			{Key: "api-key", Value: "secret123", Mode: entity.SecretModeValue},
			{Key: "cert", Value: "cert-content", Mode: entity.SecretModeFile},
		},
	}
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()

	proxy, err := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := port.ExecRequest{Command: "echo", Args: []string{"hello"}}
	_, err = proxy.Exec(context.Background(), "u1", req)
	if err != nil {
		t.Fatal(err)
	}

	// With file mode secrets, we expect: mkdir, write file, user command, cleanup = 4 exec calls.
	if len(sandbox.execCalls) < 3 {
		t.Fatalf("expected at least 3 exec calls (mkdir + write + user cmd), got %d", len(sandbox.execCalls))
	}

	// Find the user's actual command (echo hello).
	var userReq *port.ExecRequest
	for i := range sandbox.execCalls {
		if sandbox.execCalls[i].Command == "echo" {
			userReq = &sandbox.execCalls[i]
			break
		}
	}
	if userReq == nil {
		t.Fatal("user command (echo) not found in exec calls")
	}

	// Value mode secret should be set to the value directly.
	if userReq.Env["API_KEY"] != "secret123" {
		t.Errorf("API_KEY = %q, want %q", userReq.Env["API_KEY"], "secret123")
	}
	// File mode secret should be set to a path.
	if userReq.Env["CERT"] != "/tmp/secrets/CERT" {
		t.Errorf("CERT = %q, want /tmp/secrets/CERT", userReq.Env["CERT"])
	}

	// Last call should be the cleanup (rm -rf /tmp/secrets).
	lastCall := sandbox.execCalls[len(sandbox.execCalls)-1]
	if lastCall.Command != "rm" {
		t.Errorf("last call Command = %q, want rm (cleanup)", lastCall.Command)
	}
}

func TestProxyExecNoFileSecretsSkipsFileOps(t *testing.T) {
	secrets := &mockSecretEnvProvider{
		entries: []secret.SecretEnvEntry{
			{Key: "api-key", Value: "secret123", Mode: entity.SecretModeValue},
		},
	}
	sandbox := &mockSandboxPlugin{execResult: port.ExecResult{ExitCode: 0}}
	sfs := newMockProxyScopedFS()

	proxy, _ := newSandboxProxy(sandbox, secrets, sfs, "t1", "u1", "out1", "conv1", "", "")

	req := port.ExecRequest{Command: "echo", Args: []string{"hello"}}
	proxy.Exec(context.Background(), "u1", req)

	// Only the user command should be executed (no mkdir, no write, no cleanup).
	if len(sandbox.execCalls) != 1 {
		t.Fatalf("expected 1 exec call (user cmd only), got %d", len(sandbox.execCalls))
	}
	if sandbox.execCalls[0].Command != "echo" {
		t.Errorf("Command = %q, want echo", sandbox.execCalls[0].Command)
	}
}
