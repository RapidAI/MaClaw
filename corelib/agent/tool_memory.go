package agent

// tool_memory.go implements the memory tool handler as a standalone function.
// Delegates to corelib/memory so GUI, TUI, and server agents share behavior.

import "github.com/RapidAI/CodeClaw/corelib/memory"

// ToolMemory handles memory operations (save/recall/themes/delete/list).
func ToolMemory(store *memory.Store, args map[string]interface{}) string {
	return memory.HandleTool(store, args, memory.ToolOptions{
		ProjectPath: StringArg(args, "project_path"),
		ContextHint: StringArg(args, "_context_hint"),
	})
}
