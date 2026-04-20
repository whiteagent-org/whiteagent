// Package local implements a local filesystem sandbox plugin that executes
// commands as subprocesses. It is built as a .so plugin via build-plugins.sh.
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Compile-time interface assertion.
var _ port.SandboxPlugin = (*localSandbox)(nil)

type localConfig struct {
	DataDir string `json:"data_dir"`
	BaseDir string `json:"base_dir"` // backward compat
}

type localSandbox struct {
	id      string
	dataDir string
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindSandbox}
}

// NewPlugin returns a new local sandbox plugin instance.
func NewPlugin() port.Plugin {
	return &localSandbox{}
}

func (s *localSandbox) ID() string              { return s.id }
func (s *localSandbox) Kind() entity.PluginKind { return entity.PluginKindSandbox }
func (s *localSandbox) Status() entity.PluginState { return entity.PluginStateHealthy }
func (s *localSandbox) Start(_ context.Context) error { return nil }
func (s *localSandbox) Stop(_ context.Context) error  { return nil }

func (s *localSandbox) Init(_ context.Context, id string, cfg json.RawMessage) error {
	s.id = id

	var c localConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &c); err != nil {
			return fmt.Errorf("sandbox-local: parse config: %w", err)
		}
	}

	// data_dir takes precedence; fall back to base_dir for backward compat.
	dir := c.DataDir
	if dir == "" {
		dir = c.BaseDir
	}
	if dir == "" {
		dir = "./data/workspaces"
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("sandbox-local: abs path: %w", err)
	}
	s.dataDir = absDir
	return nil
}

// Ensure creates the per-user workspace directory and returns its path.
// Idempotent: calling twice for the same user succeeds without error.
func (s *localSandbox) Ensure(_ context.Context, userID entity.UserID) (string, error) {
	dir := filepath.Join(s.dataDir, string(userID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("sandbox-local: ensure dir: %w", err)
	}
	return dir, nil
}

// Exec runs a command inside the user's workspace directory.
// Non-zero exit codes are returned in ExecResult (not as errors).
// Only non-ExitError failures (e.g. command not found) return an error.
func (s *localSandbox) Exec(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	if len(req.Mounts) > 0 {
		slog.Warn("sandbox-local: mounts ignored (local sandbox does not support mounts)",
			"user_id", userID, "mounts", len(req.Mounts))
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = filepath.Join(s.dataDir, string(userID))
	}

	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Dir = workDir

	// Inherit current environment and append request env vars.
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return port.ExecResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		return port.ExecResult{}, fmt.Errorf("sandbox-local: exec: %w", err)
	}

	return port.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}, nil
}

// Release is a no-op for the local sandbox -- workspaces persist on disk.
func (s *localSandbox) Release(_ context.Context, _ entity.UserID) error {
	return nil
}

func (s *localSandbox) UserHomePath(userID entity.UserID) string {
	return filepath.Join(s.dataDir, "users", string(userID))
}

func (s *localSandbox) TenantHomePath(tenantID entity.TenantID) string {
	return filepath.Join(s.dataDir, "tenants", string(tenantID))
}

func (s *localSandbox) MessagesPath() string {
	return filepath.Join(s.dataDir, "messages")
}
