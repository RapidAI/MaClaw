package agent

// tool_memory.go implements the memory tool handler as a standalone function.
// Delegates to corelib/memory so GUI, TUI, and server agents share behavior.

import "github.com/RapidAI/CodeClaw/corelib/memory"

// ToolMemory handles memory operations (save/recall/themes/delete/list).
// The optional loopIDFunc provides the current agent loop ID for scroll session
// scoping. Nil means no loop ID is available.
func ToolMemory(store *memory.Store, args map[string]interface{}, loopIDFunc ...func() string) string {
	var loopID string
	if len(loopIDFunc) > 0 && loopIDFunc[0] != nil {
		loopID = loopIDFunc[0]()
	}
	return memory.HandleTool(store, args, memory.ToolOptions{
		ProjectPath: StringArg(args, "project_path"),
		ContextHint: StringArg(args, "_context_hint"),
		LoopID:      loopID,
	})
}
