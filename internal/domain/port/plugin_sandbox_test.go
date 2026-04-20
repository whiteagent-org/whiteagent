package port

import (
	"context"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestSandboxPluginInterfaceCompiles is a compile-time check that the
// SandboxPlugin interface has the expected method signatures.
func TestSandboxPluginInterfaceCompiles(t *testing.T) {
	// Verify the interface shape by declaring a function that accepts it.
	var _ func(SandboxPlugin) = func(sp SandboxPlugin) {
		ctx := context.Background()
		uid := entity.UserID("u-1")

		_, _ = sp.Ensure(ctx, uid)
		_, _ = sp.Exec(ctx, uid, ExecRequest{
			Command: "echo",
			Args:    []string{"hello"},
			Env:     map[string]string{"K": "V"},
			Mounts:  []Mount{{Source: "/a", Target: "/b", ReadOnly: true}},
			WorkDir: "/tmp",
		})
		_ = sp.Release(ctx, uid)
	}
}

// TestSandboxAwareInterfaceCompiles verifies SandboxAware DI interface.
func TestSandboxAwareInterfaceCompiles(t *testing.T) {
	var _ func(SandboxAware) = func(sa SandboxAware) {
		sa.SetSandbox(nil)
	}
}

func TestExecResultFields(t *testing.T) {
	r := ExecResult{Stdout: "out", Stderr: "err", ExitCode: 1}
	if r.Stdout != "out" || r.Stderr != "err" || r.ExitCode != 1 {
		t.Errorf("ExecResult fields unexpected: %+v", r)
	}
}
