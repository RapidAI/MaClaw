package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/progress"
)

func (h *IMMessageHandler) prepareAgentLoopIteration(ctx *LoopContext, userID, userText string, iteration int, effectiveMax int, maxIter int, conversation []interface{}, sendProgress func(string), milestoneTracker *progress.AgentProgressTracker, isDebug func() bool) ([]interface{}, bool) {
	if ctx.Kind == LoopKindChat && ctx.StatusC != nil {
		drainStatusEvents(ctx, &conversation, sendProgress)
	}
	if ctx.IsCancelled() {
		ctx.SetLoopState(LoopStateStopped)
		return conversation, true
	}
	if h.traceService != nil && ctx.RunID != "" {
		h.traceService.UpdateRun(ctx.RunID, TraceRunStatusRunning, firstNonEmptyTraceText(ctx.Description, userText), "")
	}
	if iteration > 0 {
		if iteration >= effectiveMax && ctx.Kind == LoopKindChat {
			sendProgress("Approaching the reasoning limit; finishing from current information.")
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": "[Finalization requirement]\nYou are approaching the maximum reasoning rounds. Stop expanding the search scope and produce the best final answer from current information. If a deliverable cannot be completed, clearly state what is done, what is missing, and the current visible end state.\n[/Finalization requirement]",
			})
		}
		if isDebug != nil && isDebug() {
			if maxIter > 0 || h.loopMaxOverride > 0 {
				sendProgress(fmt.Sprintf("Agent reasoning (%d/%d)...", iteration+1, effectiveMax))
			} else {
				sendProgress(fmt.Sprintf("Agent reasoning (%d)...", iteration+1))
			}
		} else if milestoneTracker != nil {
			milestoneTracker.Tick()
		}
	}
	if injected, ok := h.pendingInjection.LoadAndDelete(userID); ok {
		injectedText, _ := injected.(string)
		if injectedText != "" {
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": injectedText,
			})
			log.Printf("[injection] user=%s injected supplementary message: %s", userID, truncateForLog(injectedText, 50))
		}
	}
	return conversation, false
}
