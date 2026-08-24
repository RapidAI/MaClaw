package main

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// executeTUIRegisteredTool keeps document pages aligned with the model that
// actually received the current turn. The context budget is host-selected from
// the routed configuration and is deliberately not added to the tool argument
// map, so an LLM cannot enlarge its own document payload.
//
// The shared registry remains usable by non-agent callers with its stable
// legacy defaults. TUI loop callbacks use this narrow host adapter because
// their active model can change on a per-turn basis after registry startup.
func executeTUIRegisteredTool(ctx context.Context, registry *agent.CoreToolRegistry, name string, args map[string]interface{}, cfg corelib.MaclawLLMConfig) string {
	if name == "read_document" {
		return agent.ToolReadDocumentWithContext(args, cfg.EffectiveContextTokens())
	}
	if registry == nil {
		return `{"error":"no tool registry"}`
	}
	return registry.ExecuteCtx(ctx, name, args)
}

// projectTUIToolResult is the model-facing half of TUI tool execution. It
// preserves the full runtime result for spill/read-back while scaling document
// and text-reader previews to the routed model's usable input context.
func projectTUIToolResult(name string, result agent.ToolExecutionResult, cfg corelib.MaclawLLMConfig) string {
	proj, err := agent.ProjectToolResultWithContext(name, "", result.Result, cfg.EffectiveContextTokens())
	if err == nil || proj.Preview != "" {
		return proj.Preview
	}
	return agent.TruncateToolResultForToolWithSession(name, "", result.Result)
}
