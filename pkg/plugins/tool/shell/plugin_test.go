package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockSandbox implements port.SandboxPlugin for testing.
type mockSandbox struct {
	execFn func(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error)
}

func (m *mockSandbox) ID() string                                                { return "sandbox.mock" }
func (m *mockSandbox) Kind() entity.PluginKind                                   { return entity.PluginKindSandbox }
func (m *mockSandbox) Status() entity.PluginState                                { return entity.PluginStateHealthy }
func (m *mockSandbox) Init(_ context.Context, _ string, _ json.RawMessage) error { return nil }
func (m *mockSandbox) Start(_ context.Context) error                             { return nil }
func (m *mockSandbox) Stop(_ context.Context) error                              { return nil }
func (m *mockSandbox) Ensure(_ context.Context, _ entity.UserID) (string, error) {
	return "/workspace", nil
}
func (m *mockSandbox) Release(_ context.Context, _ entity.UserID) error { return nil }
func (m *mockSandbox) UserHomePath(_ entity.UserID) string              { return "/home/whiteagent" }
func (m *mockSandbox) TenantHomePath(_ entity.TenantID) string          { return "/tenant" }
func (m *mockSandbox) MessagesPath() string                             { return "/messages" }

func (m *mockSandbox) Exec(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	if m.execFn != nil {
		return m.execFn(ctx, userID, req)
	}
	return port.ExecResult{}, nil
}

func newPlugin(t *testing.T, sb port.SandboxPlugin) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if sb != nil {
		p.SetSandbox(sb)
	}
	return p
}

func TestMetadata(t *testing.T) {
	p := NewPlugin().(*Plugin)
	if p.ID() != pluginID {
		t.Errorf("ID = %q, want %q", p.ID(), pluginID)
	}
	if p.Kind() != entity.PluginKindTool {
		t.Errorf("Kind = %v, want Tool", p.Kind())
	}
	if p.Name() != "shell" {
		t.Errorf("Name = %q, want %q", p.Name(), "shell")
	}
	if p.Description() == "" {
		t.Error("Description is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(p.Parameters(), &schema); err != nil {
		t.Fatalf("Parameters is not valid JSON: %v", err)
	}
}

func TestInitConfig(t *testing.T) {
	p := NewPlugin().(*Plugin)
	cfg := `{"max_output_size": 2048, "timeout": 60}`
	if err := p.Init(context.Background(), "", json.RawMessage(cfg)); err != nil {
		t.Fatal(err)
	}
	if p.maxOutput != 2048 {
		t.Errorf("maxOutput = %d, want 2048", p.maxOutput)
	}
	if p.timeout != 60 {
		t.Errorf("timeout = %d, want 60", p.timeout)
	}
}

func TestInitDefaults(t *testing.T) {
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if p.maxOutput != defaultMaxOutput {
		t.Errorf("maxOutput = %d, want %d", p.maxOutput, defaultMaxOutput)
	}
	if p.timeout != defaultTimeout {
		t.Errorf("timeout = %d, want %d", p.timeout, defaultTimeout)
	}
}

func TestExecuteSuccess(t *testing.T) {
	sb := &mockSandbox{
		execFn: func(_ context.Context, _ entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
			if req.Command != "sh" || len(req.Args) != 2 || req.Args[0] != "-c" || req.Args[1] != "echo hello" {
				t.Errorf("unexpected exec request: %+v", req)
			}
			return port.ExecResult{Stdout: "hello\n", ExitCode: 0}, nil
		},
	}

	p := newPlugin(t, sb)
	result, err := p.Execute(context.Background(), port.ToolContext{
		UserID: entity.UserID("u1"),
	}, json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello\n\nExit code: 0" {
		t.Errorf("result = %q, want %q", result, "hello\n\nExit code: 0")
	}
}

func TestExecuteNonZeroExitCode(t *testing.T) {
	sb := &mockSandbox{
		execFn: func(_ context.Context, _ entity.UserID, _ port.ExecRequest) (port.ExecResult, error) {
			return port.ExecResult{Stderr: "not found\n", ExitCode: 1}, nil
		},
	}

	p := newPlugin(t, sb)
	result, err := p.Execute(context.Background(), port.ToolContext{
		UserID: entity.UserID("u1"),
	}, json.RawMessage(`{"command":"ls /nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Error("expected non-empty result for non-zero exit code")
	}
}

func TestExecuteNoSandbox(t *testing.T) {
	p := newPlugin(t, nil)
	_, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"command":"ls"}`))
	if err == nil {
		t.Fatal("expected error when sandbox is nil")
	}
}

func TestExecuteEmptyCommand(t *testing.T) {
	sb := &mockSandbox{}
	p := newPlugin(t, sb)
	_, err := p.Execute(context.Background(), port.ToolContext{}, json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestManifest(t *testing.T) {
	m := Manifest()
	if m.Kind != entity.PluginKindTool {
		t.Errorf("Manifest().Kind = %v, want Tool", m.Kind)
	}
}

func TestExecuteTruncation(t *testing.T) {
	longOutput := make([]byte, 2048)
	for i := range longOutput {
		longOutput[i] = 'x'
	}

	sb := &mockSandbox{
		execFn: func(_ context.Context, _ entity.UserID, _ port.ExecRequest) (port.ExecResult, error) {
			return port.ExecResult{Stdout: string(longOutput)}, nil
		},
	}

	p := newPlugin(t, sb)
	p.maxOutput = 1024

	result, err := p.Execute(context.Background(), port.ToolContext{
		UserID: entity.UserID("u1"),
	}, json.RawMessage(`{"command":"cat bigfile"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > 1024+len("\n[truncated]") {
		t.Errorf("result length = %d, expected truncated to ~1024", len(result))
	}
}

func TestExecuteExecError(t *testing.T) {
	sb := &mockSandbox{
		execFn: func(_ context.Context, _ entity.UserID, _ port.ExecRequest) (port.ExecResult, error) {
			return port.ExecResult{}, fmt.Errorf("sandbox unavailable")
		},
	}

	p := newPlugin(t, sb)
	_, err := p.Execute(context.Background(), port.ToolContext{
		UserID: entity.UserID("u1"),
	}, json.RawMessage(`{"command":"ls"}`))
	if err == nil {
		t.Fatal("expected error when sandbox.Exec fails")
	}
}

func TestFormatOutput(t *testing.T) {
	tests := []struct {
		name   string
		result port.ExecResult
		want   string
	}{
		{
			name:   "stdout only",
			result: port.ExecResult{Stdout: "hello"},
			want:   "hello\nExit code: 0",
		},
		{
			name:   "stderr only with exit code",
			result: port.ExecResult{Stderr: "err", ExitCode: 1},
			want:   "STDERR:\nerr\nExit code: 1",
		},
		{
			name:   "both streams",
			result: port.ExecResult{Stdout: "out", Stderr: "err"},
			want:   "out\nSTDERR:\nerr\nExit code: 0",
		},
		{
			name:   "no output",
			result: port.ExecResult{},
			want:   "Exit code: 0",
		},
		{
			name:   "no output non-zero",
			result: port.ExecResult{ExitCode: 1},
			want:   "Exit code: 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOutput(tt.result)
			if got != tt.want {
				t.Errorf("formatOutput = %q, want %q", got, tt.want)
			}
		})
	}
}
