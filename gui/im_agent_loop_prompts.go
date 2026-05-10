package main

import (
	"fmt"
	"strings"
)

func (h *IMMessageHandler) injectAgentLoopHarnessPrompts(ctx *LoopContext, conversation []interface{}, phase agentLoopPhase, iteration int, effectiveMax int, loopGoalAnchor *GoalAnchor, loopProgressTracker *HarnessProgressTracker, trialState *trialReflectState) ([]interface{}, int) {
	if phase.Stage == agentStageRecover && (phase.ConsecutiveNoTool >= 2 || phase.ConsecutiveEmptyResponses >= 1) {
		conversation = pruneStaleNoToolTurns(conversation)
	}

	systemMessagesStart := len(conversation)
	if loopGoalAnchor != nil && loopGoalAnchor.ShouldAnchor(iteration) {
		var progressSummary string
		if loopProgressTracker != nil {
			progressSummary = loopProgressTracker.Summary()
		} else {
			progressSummary = fmt.Sprintf("閺夆晩鍘洪崬?%d/%d", iteration, effectiveMax)
		}
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": loopGoalAnchor.BuildAnchorContent(progressSummary),
		})
	}

	if loopProgressTracker != nil {
		if checklist := loopProgressTracker.BuildChecklistContent(); checklist != "" {
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": "[妫ｅ啯鎯?濞寸姾顕ф慨鐔枫€掗崨顓炵]\n" + checklist + "\n[/濞寸姾顕ф慨鐔枫€掗崨顓炵]",
			})
		}
	}
	if trialState != nil && trialState.enabled && strings.TrimSpace(trialState.pendingNote) != "" {
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": trialState.pendingNote,
		})
		if h.traceService != nil && ctx.RunID != "" {
			h.appendTraceEvent(ctx, "trial.adjusted", "info", "Injected reflection note", truncateTraceText(trialState.pendingNote, 220), "", "")
		}
		trialState.pendingNote = ""
	}
	return conversation, systemMessagesStart
}
