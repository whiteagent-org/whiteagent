package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func setup(t *testing.T) *localSandbox {
	t.Helper()
	s := NewPlugin().(*localSandbox)
	s.dataDir = t.TempDir()
	return s
}

func TestManifest(t *testing.T) {
	m := Manifest()
	if m.Kind != entity.PluginKindSandbox {
		t.Fatalf("want kind %q, got %q", entity.PluginKindSandbox, m.Kind)
	}
}

func TestKindAndID(t *testing.T) {
	s := setup(t)
	if s.Kind() != entity.PluginKindSandbox {
		t.Fatalf("want kind sandbox, got %s", s.Kind())
	}
}

func TestStatusHealthy(t *testing.T) {
	s := setup(t)
	if s.Status() != entity.PluginStateHealthy {
		t.Fatalf("want healthy, got %s", s.Status())
	}
}

func TestStartStop(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestEnsureCreatesDirectory(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-abc")

	workDir, err := s.Ensure(ctx, userID)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	expected := filepath.Join(s.dataDir, string(userID))
	if workDir != expected {
		t.Fatalf("want workDir %q, got %q", expected, workDir)
	}

	info, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("workDir is not a directory")
	}
}

func TestEnsureIdempotent(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-idem")

	dir1, err := s.Ensure(ctx, userID)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	dir2, err := s.Ensure(ctx, userID)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if dir1 != dir2 {
		t.Fatalf("idempotent mismatch: %q vs %q", dir1, dir2)
	}
}

func TestExecEchoStdout(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-exec")

	if _, err := s.Ensure(ctx, userID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	result, err := s.Exec(ctx, userID, port.ExecRequest{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "hello" {
		t.Fatalf("want stdout 'hello', got %q", got)
	}
	if result.ExitCode != 0 {
		t.Fatalf("want exit code 0, got %d", result.ExitCode)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-exit")

	if _, err := s.Ensure(ctx, userID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	result, err := s.Exec(ctx, userID, port.ExecRequest{
		Command: "sh",
		Args:    []string{"-c", "exit 42"},
	})
	if err != nil {
		t.Fatalf("Exec should not return error for non-zero exit: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("want exit code 42, got %d", result.ExitCode)
	}
}

func TestExecEnvVars(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-env")

	if _, err := s.Ensure(ctx, userID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	result, err := s.Exec(ctx, userID, port.ExecRequest{
		Command: "sh",
		Args:    []string{"-c", "echo $TEST_VAR"},
		Env:     map[string]string{"TEST_VAR": "sandbox_value"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != "sandbox_value" {
		t.Fatalf("want 'sandbox_value', got %q", got)
	}
}

func TestExecExplicitWorkDir(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-wd")

	if _, err := s.Ensure(ctx, userID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	customDir := t.TempDir()
	// Resolve symlinks for macOS /var -> /private/var.
	customDir, _ = filepath.EvalSymlinks(customDir)
	result, err := s.Exec(ctx, userID, port.ExecRequest{
		Command: "pwd",
		WorkDir: customDir,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(result.Stdout); got != customDir {
		t.Fatalf("want workDir %q, got %q", customDir, got)
	}
}

func TestExecCommandNotFound(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-notfound")

	if _, err := s.Ensure(ctx, userID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	_, err := s.Exec(ctx, userID, port.ExecRequest{
		Command: "nonexistent_command_xyz_abc",
	})
	if err == nil {
		t.Fatal("expected error for command not found")
	}
}

func TestRelease(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	userID := entity.UserID("user-release")

	if err := s.Release(ctx, userID); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestLocalSandboxUserHomePath(t *testing.T) {
	s := setup(t)
	got := s.UserHomePath("user-abc")
	want := filepath.Join(s.dataDir, "users", "user-abc")
	if got != want {
		t.Errorf("UserHomePath = %q, want %q", got, want)
	}
}

func TestLocalSandboxTenantHomePath(t *testing.T) {
	s := setup(t)
	got := s.TenantHomePath("tenant-xyz")
	want := filepath.Join(s.dataDir, "tenants", "tenant-xyz")
	if got != want {
		t.Errorf("TenantHomePath = %q, want %q", got, want)
	}
}

func TestLocalSandboxMessagesPath(t *testing.T) {
	s := setup(t)
	got := s.MessagesPath()
	want := filepath.Join(s.dataDir, "messages")
	if got != want {
		t.Errorf("MessagesPath = %q, want %q", got, want)
	}
}

func TestInitDataDirBackwardCompat(t *testing.T) {
	s := NewPlugin().(*localSandbox)
	ctx := context.Background()

	// base_dir should still work for backward compat
	err := s.Init(ctx, "sandbox.local", []byte(`{"base_dir":"/tmp/test-ws"}`))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s.dataDir != "/tmp/test-ws" {
		t.Fatalf("want dataDir /tmp/test-ws, got %q", s.dataDir)
	}
}

func TestInitDataDirField(t *testing.T) {
	s := NewPlugin().(*localSandbox)
	ctx := context.Background()

	err := s.Init(ctx, "sandbox.local", []byte(`{"data_dir":"/tmp/test-data"}`))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s.dataDir != "/tmp/test-data" {
		t.Fatalf("want dataDir /tmp/test-data, got %q", s.dataDir)
	}
}

func TestInitParsesBaseDir(t *testing.T) {
	s := NewPlugin().(*localSandbox)
	ctx := context.Background()

	err := s.Init(ctx, "sandbox.local", []byte(`{"base_dir":"/tmp/test-workspaces"}`))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s.dataDir != "/tmp/test-workspaces" {
		t.Fatalf("want dataDir /tmp/test-workspaces, got %q", s.dataDir)
	}
}

func TestInitDefaultBaseDir(t *testing.T) {
	s := NewPlugin().(*localSandbox)
	ctx := context.Background()

	err := s.Init(ctx, "sandbox.local", []byte(`{}`))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Default is ./data/workspaces resolved to absolute path.
	if !filepath.IsAbs(s.dataDir) {
		t.Fatalf("want absolute dataDir, got %q", s.dataDir)
	}
}
