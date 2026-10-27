package guiapp

import (
	"log"
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// codingContinuationResumeNote is the prompt-only state for a single-task
// session wave. A spent iteration budget is host accounting, not the state
// of the work: the files already changed are that state.
func codingContinuationResumeNote() string {
	return "The previous round ended before the work did. The files already changed are the current state. Continue from them; do not redo completed work or repeat a search whose result is already known."
}

// codingWorkbenchIterationExhausted reports a coding step that spent its
// whole iteration budget. The step keeps its own failure record, but the
// session task is unfinished and may continue in another round.
func codingWorkbenchIterationExhausted(results []v2.TaskRunResult) bool {
	for _, result := range results {
		if result.Status != v2.TaskFailed {
			continue
		}
		if codingLoopIterationExhausted(result.Error) {
			return true
		}
	}
	return false
}

// sessionRoundCodingTask returns the request a host session wave repeats.
// The wave header is a host instruction; a task title, a request
// classification, and a new-plan decision use the user's own wording.
func sessionRoundCodingTask(userText string) string {
	text := strings.TrimSpace(userText)
	if !isSessionRoundContinuation(text) {
		return text
	}
	const marker = "\n\n任务：\n"
	if idx := strings.Index(text, marker); idx >= 0 {
		if task := strings.TrimSpace(text[idx+len(marker):]); task != "" {
			return task
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(text, "[系统续轮]"))
}

// codingWorkbenchContinuationTask is the work the next round repeats. A bare
// continue names no requirement; the session's stored plan or goal is that
// task. Anything else keeps the user's own wording.
func (h *IMMessageHandler) codingWorkbenchContinuationTask(userID, userText string) string {
	current := sessionRoundCodingTask(userText)
	if !codingWorkbenchContinueCue(current) {
		return current
	}
	if h == nil {
		return current
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if goal := strings.TrimSpace(codingPlanResumeGoal(current, mem)); goal != "" && !codingWorkbenchContinueCue(goal) {
		return goal
	}
	return current
}

// continueCodingWorkbenchAfterIterationBudget arms the same session-round
// continuation the chat loop uses when a coding step spent its iteration
// budget. The workbench still returns its report, and the next round resumes
// the open plan step instead of waiting for the user to send another message.
func (h *IMMessageHandler) continueCodingWorkbenchAfterIterationBudget(ctx *LoopContext, userID, userText string, results []v2.TaskRunResult, toolCalls int) (string, bool) {
	if h == nil || ctx == nil || isGoalSystemContinuation(userText) || !codingWorkbenchIterationExhausted(results) {
		return "", false
	}
	// A workflow document phase owns its own retry and resume. A session
	// wave there would bypass the workflow router and leave the phase waiting.
	// WorkflowAgentLoop alone is not that signal: an armed pure-coding
	// session also carries it while the workbench, not a phase, runs.
	if ctx.WorkflowDocPhase {
		return "", false
	}
	task := h.codingWorkbenchContinuationTask(userID, userText)
	// The coding workbench keeps its own plan and sticky memory, so the
	// max-rounds recovery slot the chat loop writes is not needed here. The
	// resolved task below is the wave's text.
	resp, continued := h.beginSessionRoundContinuation(ctx, userID, task, nil, toolCalls)
	if !continued || resp == nil {
		return "", false
	}
	log.Printf("[coding-workbench] iteration budget spent; the next session round resumes the open work user=%s tools=%d task=%q", userID, toolCalls, truncateRunes(task, 80))
	return resp.Text, true
}
