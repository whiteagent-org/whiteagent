package loader

import (
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestKindInitOrderIncludesSandbox verifies that kindInitOrder contains
// entity.PluginKindSandbox and that it is positioned after PluginKindLLM and
// before PluginKindTool, matching the required lifecycle ordering:
// Store, Transport, Channel, LLM, Sandbox, Tool, Middleware.
func TestKindInitOrderIncludesSandbox(t *testing.T) {
	sandboxIdx := -1
	llmIdx := -1
	toolIdx := -1

	for i, kind := range kindInitOrder {
		switch kind {
		case entity.PluginKindSandbox:
			sandboxIdx = i
		case entity.PluginKindLLM:
			llmIdx = i
		case entity.PluginKindTool:
			toolIdx = i
		}
	}

	if sandboxIdx == -1 {
		t.Fatal("kindInitOrder does not contain PluginKindSandbox")
	}

	if llmIdx == -1 {
		t.Fatal("kindInitOrder does not contain PluginKindLLM")
	}

	if toolIdx == -1 {
		t.Fatal("kindInitOrder does not contain PluginKindTool")
	}

	if sandboxIdx <= llmIdx {
		t.Errorf("PluginKindSandbox (index %d) must come after PluginKindLLM (index %d)", sandboxIdx, llmIdx)
	}

	if sandboxIdx >= toolIdx {
		t.Errorf("PluginKindSandbox (index %d) must come before PluginKindTool (index %d)", sandboxIdx, toolIdx)
	}
}
