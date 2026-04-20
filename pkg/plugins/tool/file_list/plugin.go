// Package file_list implements a ToolPlugin that lists directory contents
// by absolute path.
package file_list

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

const pluginID = "tool.file_list"

// Plugin implements port.ToolPlugin for listing directory contents.
type Plugin struct{}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new file_list tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return pluginID }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, _ string, _ json.RawMessage) error {
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

func (p *Plugin) Name() string { return "file_list" }
func (p *Plugin) Description() string {
	return "List the immediate contents of a directory at a given absolute path."
}

func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute path to the directory to list"}},"required":["path"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type listArgs struct {
	Path string `json:"path"`
}

func (p *Plugin) Execute(_ context.Context, _ port.ToolContext, args json.RawMessage) (string, error) {
	var a listArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("file_list: parse args: %w", err)
	}

	info, err := os.Stat(a.Path)
	if err != nil {
		return "", fmt.Errorf("file_list: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("file_list: path is a file, use file_read instead")
	}

	entries, err := os.ReadDir(a.Path)
	if err != nil {
		return "", fmt.Errorf("file_list: %w", err)
	}

	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString(e.Name() + "/ (directory)\n")
		} else {
			sb.WriteString(e.Name() + " (file)\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
