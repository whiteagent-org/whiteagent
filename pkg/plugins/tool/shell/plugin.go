// Package shell implements a ToolPlugin that executes shell commands
// inside the user's sandbox via the SandboxAware interface.
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

const pluginID = "tool.shell"

const (
	defaultMaxOutput = 1024 * 1024 // 1 MB
	defaultTimeout   = 300         // 5 minutes
)

// Plugin implements port.ToolPlugin and port.SandboxAware for shell execution.
type Plugin struct {
	sandbox   port.SandboxPlugin
	maxOutput int
	timeout   int
}

type pluginConfig struct {
	MaxOutputSize int `json:"max_output_size"`
	Timeout       int `json:"timeout"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new shell tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return pluginID }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, _ string, raw json.RawMessage) error {
	p.maxOutput = defaultMaxOutput
	p.timeout = defaultTimeout

	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("shell: parse config: %w", err)
	}
	if cfg.MaxOutputSize > 0 {
		p.maxOutput = cfg.MaxOutputSize
	}
	if cfg.Timeout > 0 {
		p.timeout = cfg.Timeout
	}
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetSandbox injects the sandbox plugin via DI.
func (p *Plugin) SetSandbox(sb port.SandboxPlugin) { p.sandbox = sb }

func (p *Plugin) Name() string        { return "shell" }
func (p *Plugin) Description() string { return "Execute a shell command." }

func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to execute"}},"required":["command"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type shellArgs struct {
	Command string `json:"command"`
}

func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	if p.sandbox == nil {
		return "", fmt.Errorf("shell: requires sandbox")
	}

	var a shellArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("shell: parse args: %w", err)
	}
	if a.Command == "" {
		return "", fmt.Errorf("shell: command is required")
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(p.timeout)*time.Second)
	defer cancel()

	result, err := p.sandbox.Exec(execCtx, tc.UserID, port.ExecRequest{
		Command: "sh",
		Args:    []string{"-c", a.Command},
	})
	if err != nil {
		return "", fmt.Errorf("shell: exec: %w", err)
	}

	output := formatOutput(result)
	if p.maxOutput > 0 && len(output) > p.maxOutput {
		output = output[:p.maxOutput] + "\n[truncated]"
	}
	return output, nil
}

// formatOutput combines stdout, stderr, and exit code into a single string.
// Always appends "Exit code: N" so the LLM sees command success/failure even
// when stdout is empty (prevents agent looping on no-stdout commands).
func formatOutput(r port.ExecResult) string {
	var sb strings.Builder
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(r.Stderr)
	}
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("Exit code: %d", r.ExitCode))
	return sb.String()
}
