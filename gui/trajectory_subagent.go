package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// startSubAgentTrajectory opens a trajectory session for a SubAgent loop when
// llm_trajectory_logging is enabled. Callers should defer flushSubAgentTrajectory.
func startSubAgentTrajectory(
	h *IMMessageHandler,
	kind, sessionID, userID, platform, parentSessionID string,
	cfg corelib.MaclawLLMConfig,
	systemPrompt string,
	tools []map[string]interface{},
) *TrajectoryRecorder {
	if h == nil {
		return nil
	}
	recorder := h.newTrajectoryRecorderIfEnabled()
	if recorder == nil {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = fmt.Sprintf("%s-%d", kind, time.Now().UnixNano())
	}
	provider := ""
	if h != nil {
		provider = h.getMaclawLLMProviders().Current
	}
	recorder.StartSessionWithMeta(sessionID, provider, cfg.Model, cfg.Protocol, userID, platform, kind, parentSessionID, tools)
	if strings.TrimSpace(systemPrompt) != "" {
		recorder.Record("system", systemPrompt, nil, "", "")
	}
	return recorder
}

// appendSubAgentLoopResult records a RunLoop HistoryDelta into an open session.
// skipLeadingUser should be false for subagent loops (user turn not pre-recorded).
// Does not stamp outcome — call sealSubAgentTrajectory after all loops finish.
func appendSubAgentLoopResult(recorder *TrajectoryRecorder, result agent.LoopResult, skipLeadingUser bool) {
	if recorder == nil {
		return
	}
	recorder.RecordHistoryDelta(result.HistoryDelta, skipLeadingUser)
}

// sealSubAgentTrajectory stamps final outcome metadata. Safe to call once after
// main + optional post-loop verify/fix cycles.
func sealSubAgentTrajectory(recorder *TrajectoryRecorder, result agent.LoopResult) {
	if recorder == nil {
		return
	}
	if reason := unpairedCloseReasonFromLoopResult(result); reason != "" {
		recorder.CloseUnpairedToolCalls(reason)
	}
	recorder.SetOutcomeFromLoopResult(result)
}

// finishSubAgentTrajectory records HistoryDelta + outcome in one step.
// Prefer append + seal when post-loop work may produce additional turns.
func finishSubAgentTrajectory(recorder *TrajectoryRecorder, result agent.LoopResult) {
	if recorder == nil {
		return
	}
	// SubAgent start only recorded system; HistoryDelta includes the user turn.
	appendSubAgentLoopResult(recorder, result, false)
	sealSubAgentTrajectory(recorder, result)
}

// flushSubAgentTrajectory flushes a subagent session, stamping a failure outcome
// if the loop aborted before seal/finish (e.g. panic).
func flushSubAgentTrajectory(recorder *TrajectoryRecorder) {
	if recorder == nil {
		return
	}
	if !recorder.HasOutcome() {
		// Abort mid-batch must still pair tool_calls for training consumers.
		recorder.CloseUnpairedToolCalls("subagent aborted")
		recorder.SetOutcome("error", "subagent loop aborted before completion", -1, -1, -1, -1)
	}
	recorder.Flush()
}

// recordSubAgentPostLoopVerify records an automatic verification bash invocation.
// argsJSON should be the exact arguments passed to ExecuteTool; outcome is free-form
// (typically "succeeded" / "failed").
func recordSubAgentPostLoopVerify(recorder *TrajectoryRecorder, round int, verifyCmd, argsJSON, verifyResult, outcome string) {
	if recorder == nil {
		return
	}
	id := fmt.Sprintf("post-loop-verify-%d", round)
	if strings.TrimSpace(argsJSON) == "" {
		argsJSON = fmt.Sprintf(`{"command":%q}`, verifyCmd)
	}
	if strings.TrimSpace(outcome) == "" {
		outcome = "unknown"
	}
	recorder.Record("system", fmt.Sprintf("[post-loop verify] round %d: %s", round, verifyCmd), nil, "", "")
	recorder.RecordEntry(TrajectoryEntry{
		Role:       "tool",
		Content:    map[string]interface{}{"name": "bash", "arguments": argsJSON},
		ToolCallID: id,
		ToolName:   "bash",
	})
	recorder.RecordEntry(TrajectoryEntry{
		Role:        "tool_result",
		Content:     verifyResult,
		ToolCallID:  id,
		ToolName:    "bash",
		ToolOutcome: outcome,
	})
}
