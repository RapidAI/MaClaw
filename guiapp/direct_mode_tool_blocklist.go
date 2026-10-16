package guiapp

// ---------------------------------------------------------------------------
// Direct-mode tool blocklist: used by SubAgent orchestration to prevent the
// main loop from receiving coding tools while implementation is owned by the
// internal CodingSubAgent.
// ---------------------------------------------------------------------------

// directModeMainLoopBlocklist lists tools that the main loop must not receive
// while implementation is owned by the internal CodingSubAgent.
var directModeMainLoopBlocklist = map[string]bool{
	"bash":               true,
	"write_file":         true,
	"edit_file":          true,
	"edit_lines":         true,
	"craft_tool":         true,
	"parallel_execute":   true,
	"create_session":     true,
	"send_and_observe":   true,
	"control_session":    true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"list_sessions":      true,
	"send_input":         true,
}

func init() {
	for name := range disabledExternalCodingSessionTools {
		directModeMainLoopBlocklist[name] = true
	}
}

// isDirectModeBlockedTool returns true if the main loop must not receive this
// tool while direct mode delegates code changes to CodingSubAgent.
func isDirectModeBlockedTool(name string) bool {
	return directModeMainLoopBlocklist[name]
}

// mainLoopInDirectMode reports that the orchestrator step would strip coding
// tools on this turn. The owner is workflowPolicyOwnerID, the same key that
// step uses. A phase that cannot host a SubAgent makes the step deactivate
// the orchestrator and leave the tool list alone, so a seal that runs first
// must not treat that orchestrator as direct mode: doc-only still needs bash.
func (h *IMMessageHandler) mainLoopInDirectMode(userID string, ctx *LoopContext) bool {
	if h == nil || h.taskOrchestratorRegistry == nil {
		return false
	}
	ownerID := h.workflowPolicyOwnerID(userID, ctx)
	orch := h.taskOrchestratorRegistry.Get(ownerID)
	if orch == nil || !orch.IsActive() {
		return false
	}
	if allowed, _ := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
		return false
	}
	handles := orch.ReadyTaskHandles(1)
	if len(handles) == 0 {
		return false
	}
	mode, ok := orch.ResolveExecutionModeForTaskRun(handles[0].Task, handles[0].RunID)
	return ok && mode == TaskExecModeDirect
}
