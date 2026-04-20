package agent

import (
	"encoding/json"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	noreplyMW "github.com/whiteagent-org/whiteagent/internal/infra/plugins/middleware/noreply"
	reactedMW "github.com/whiteagent-org/whiteagent/internal/infra/plugins/middleware/reacted"
	replytoMW "github.com/whiteagent-org/whiteagent/internal/infra/plugins/middleware/replyto"
	cronCreateTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/cron_create"
	cronListTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/cron_list"
	cronSetStatusTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/cron_set_status"
	historyGetTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/history_get"
	historySearchTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/history_search"
	journalAppendTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/journal_append"
	journalSearchTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/journal_search"
	memoryGetTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/memory_get"
	memoryUpdateTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/memory_update"
	messageTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/message"
	secretDeleteTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/secret_delete"
	secretListTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/secret_list"
	secretRequestTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/secret_request"
	secretSetTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/secret_set"
	timeTool "github.com/whiteagent-org/whiteagent/internal/infra/plugins/tool/time"
)

// builtinTool pairs a tool plugin with its registry ID and optional config.
type builtinTool struct {
	plugin   port.ToolPlugin
	pluginID string
	config   json.RawMessage
}

// builtinTools returns the set of built-in tool plugins that ship with the
// runtime. These are registered into the plugin registry before .so loading,
// so an .so tool with a matching config ID will replace the built-in.
func builtinTools(agentCfg config.AgentConfig, runtimeCfg config.RuntimeConfig) []builtinTool {
	tz := runtimeCfg.Timezone

	var timeCfg json.RawMessage
	if tz != "" {
		timeCfg, _ = json.Marshal(map[string]string{"timezone": tz})
	}

	// cronCfg carries the same timezone for cron tools.
	var cronCfg json.RawMessage
	if tz != "" {
		cronCfg, _ = json.Marshal(map[string]string{"timezone": tz})
	}
	return []builtinTool{
		{plugin: memoryGetTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.memory_get"},
		{plugin: memoryUpdateTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.memory_update"},
		{plugin: journalAppendTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.journal_append"},
		{plugin: journalSearchTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.journal_search"},
		{plugin: messageTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.message"},
		{plugin: timeTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.time", config: timeCfg},
		{plugin: historySearchTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.history_search"},
		{plugin: historyGetTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.history_get"},
		{plugin: cronCreateTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.cron_create", config: cronCfg},
		{plugin: cronListTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.cron_list", config: cronCfg},
		{plugin: cronSetStatusTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.cron_set_status", config: cronCfg},
		{plugin: secretRequestTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.secret_request"},
		{plugin: secretSetTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.secret_set"},
		{plugin: secretListTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.secret_list"},
		{plugin: secretDeleteTool.NewPlugin().(port.ToolPlugin), pluginID: "tool.secret_delete"},
	}
}

// builtinMiddleware pairs a middleware plugin with its registry ID.
type builtinMiddleware struct {
	plugin   port.MiddlewarePlugin
	pluginID string
}

// builtinMiddlewares returns the set of built-in middleware plugins that ship
// with the runtime. These are registered before .so loading, so an .so
// middleware with a matching config ID will replace the built-in.
func builtinMiddlewares() []builtinMiddleware {
	return []builtinMiddleware{
		{plugin: replytoMW.NewPlugin().(port.MiddlewarePlugin), pluginID: "middleware.replyto"},
		{plugin: reactedMW.NewPlugin().(port.MiddlewarePlugin), pluginID: "middleware.reacted"},
		{plugin: noreplyMW.NewPlugin().(port.MiddlewarePlugin), pluginID: "middleware.noreply"},
	}
}
