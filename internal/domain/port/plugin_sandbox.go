package port

import (
	"context"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// Sandbox plugin port
// ---------------------------------------------------------------------------

// ExecRequest describes a command to execute inside a sandbox.
type ExecRequest struct {
	Command string
	Args    []string
	Env     map[string]string
	Mounts  []Mount
	WorkDir string
}

// Mount describes a filesystem bind mount into the sandbox.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// ExecResult holds the output of a sandboxed command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SandboxPlugin manages isolated execution environments for users.
// Ensure creates or reuses a sandbox for the given user and returns its
// working directory. Exec runs a command inside the user's sandbox.
// Release tears down the sandbox and frees resources.
type SandboxPlugin interface {
	Plugin
	Ensure(ctx context.Context, userID entity.UserID) (workDir string, err error)
	Exec(ctx context.Context, userID entity.UserID, req ExecRequest) (ExecResult, error)
	Release(ctx context.Context, userID entity.UserID) error
	UserHomePath(userID entity.UserID) string
	TenantHomePath(tenantID entity.TenantID) string
	MessagesPath() string
}

// MountEnsurer is an optional interface for sandbox implementations that
// support bind mounts at container creation time. Callers type-assert to
// check support; when unavailable, the plain Ensure method is used instead.
type MountEnsurer interface {
	EnsureWithMounts(ctx context.Context, userID entity.UserID, mounts []Mount) (workDir string, err error)
}

// FileTransferable is an optional interface that sandbox implementations may
// support. It enables copying files between the host and the sandbox container
// via tar archives (Docker cp). Callers type-assert to check support.
type FileTransferable interface {
	CopyTo(ctx context.Context, userID entity.UserID, hostPath, containerPath string) error
	CopyFrom(ctx context.Context, userID entity.UserID, containerPath, hostPath string) error
}

// SandboxAware is a dependency injection interface for plugins that need
// access to the sandbox plugin.
type SandboxAware interface {
	SetSandbox(SandboxPlugin)
}
