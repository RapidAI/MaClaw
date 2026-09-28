package guiapp

import (
	"encoding/json"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// noteSpilledCodingToolResult remembers that this turn spilled a full tool
// result. The next model request lists read_tool_result so the omitted middle
// can be paged. The static coding inventory stays unchanged until a spill
// actually happens.
func noteSpilledCodingToolResult(flag *atomic.Bool, preview string) {
	if flag != nil && strings.Contains(preview, toolresult.HandleFooterMarker) {
		flag.Store(true)
	}
}

func appendSpilledToolResultReader(tools []map[string]interface{}, needed bool) []map[string]interface{} {
	if !needed {
		return tools
	}
	for _, def := range tools {
		if extractToolName(def) == "read_tool_result" {
			return tools
		}
	}
	return append(tools, toolDefFromCore("read_tool_result", "分段回读被截断的工具完整输出（使用 [tool_result_handle] 的 id）", nil))
}

func codingReadToolResultExecution(handler *IMMessageHandler, argsJSON string) agent.ToolExecutionResult {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(argsJSON)), &args); err != nil || args == nil {
		args = map[string]interface{}{}
	}
	// The spill is stored under the runtime owner. A model-supplied session
	// key or path must not read another conversation's handle.
	delete(args, "path")
	if handler != nil {
		if owner := strings.TrimSpace(handler.currentRuntimeOrLegacyPolicyOwnerID()); owner != "" {
			args["session_key"] = owner
		}
	}
	text := agent.ToolReadToolResult(args)
	outcome := agent.ToolExecutionOutcomeOK
	if strings.HasPrefix(strings.TrimSpace(text), "error:") {
		outcome = agent.ToolExecutionOutcomeError
	}
	return agent.ToolExecutionResult{Result: text, Outcome: outcome}
}
