// Package file_read implements a ToolPlugin that reads file contents via sandbox.
package file_read

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin and port.SandboxAware for reading files.
type Plugin struct {
	id         string
	sandbox    port.SandboxPlugin
	linesLimit int
}

type pluginConfig struct {
	LinesLimit int `json:"lines_limit"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new file_read tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{linesLimit: 1000} }

func (p *Plugin) ID() string                    { return p.id }
func (p *Plugin) Kind() entity.PluginKind       { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState    { return entity.PluginStateHealthy }
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("file_read: plugin ID is required")
	}
	p.id = id
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("file_read: parse config: %w", err)
	}
	if cfg.LinesLimit > 0 {
		p.linesLimit = cfg.LinesLimit
	}
	return nil
}

// SetSandbox injects the sandbox plugin via DI.
func (p *Plugin) SetSandbox(sb port.SandboxPlugin) { p.sandbox = sb }

func (p *Plugin) Name() string { return "file_read" }
func (p *Plugin) Description() string {
	return "Read lines from a file at the given path, with optional offset and limit for pagination."
}
func (p *Plugin) Instructions() string { return "" }

func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute path to the file to read"},"offset":{"type":"integer","description":"Line number to start reading from (1-based, default 1)"},"limit":{"type":"integer","description":"Number of lines to read (default: configured limit)"}},"required":["path"]}`)
}

type fileReadArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	if p.sandbox == nil {
		return "", fmt.Errorf("file_read requires sandbox")
	}

	var a fileReadArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("file_read: parse args: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("file_read: path is required")
	}

	// Defaults.
	if a.Offset <= 0 {
		a.Offset = 1
	}
	if a.Limit <= 0 {
		a.Limit = p.linesLimit
	}

	// Step 1: get total line count via wc -l.
	wcResult, err := p.sandbox.Exec(ctx, tc.UserID, port.ExecRequest{
		Command: "wc",
		Args:    []string{"-l", a.Path},
	})
	if err != nil {
		return "", fmt.Errorf("file_read: exec wc: %w", err)
	}
	if wcResult.ExitCode != 0 {
		errMsg := wcResult.Stderr
		if errMsg == "" {
			errMsg = wcResult.Stdout
		}
		return fmt.Sprintf("Error reading file: %s", errMsg), nil
	}

	fields := strings.Fields(strings.TrimSpace(wcResult.Stdout))
	if len(fields) == 0 {
		return "Error reading file: unexpected wc output", nil
	}
	total, err := strconv.Atoi(fields[0])
	if err != nil {
		return fmt.Sprintf("Error reading file: cannot parse line count: %s", fields[0]), nil
	}

	// Step 2: check offset bounds.
	if a.Offset > total {
		return fmt.Sprintf("Offset %d exceeds file length of %d lines", a.Offset, total), nil
	}

	// Step 3: read lines via sed.
	endLine := a.Offset + a.Limit - 1
	sedResult, err := p.sandbox.Exec(ctx, tc.UserID, port.ExecRequest{
		Command: "sed",
		Args:    []string{"-n", fmt.Sprintf("%d,%dp", a.Offset, endLine), a.Path},
	})
	if err != nil {
		return "", fmt.Errorf("file_read: exec sed: %w", err)
	}
	if sedResult.ExitCode != 0 {
		errMsg := sedResult.Stderr
		if errMsg == "" {
			errMsg = sedResult.Stdout
		}
		return fmt.Sprintf("Error reading file: %s", errMsg), nil
	}

	// Step 4: format output with line numbers.
	raw := strings.TrimRight(sedResult.Stdout, "\n")
	if raw == "" {
		return fmt.Sprintf("--- 0 lines (%d-%d of %d) ---", a.Offset, a.Offset-1, total), nil
	}
	lines := strings.Split(raw, "\n")

	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%6d\t%s\n", a.Offset+i, line)
	}
	fmt.Fprintf(&b, "--- %d lines (%d-%d of %d) ---", len(lines), a.Offset, a.Offset+len(lines)-1, total)

	return b.String(), nil
}
