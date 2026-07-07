package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/progress"
)

func (h *IMMessageHandler) prepareAgentLoopIteration(ctx *LoopContext, userID, userText string, iteration int, effectiveMax int, maxIter int, conversation []interface{}, sendProgress func(string), milestoneTracker *progress.AgentProgressTracker, isDebug func() bool) ([]interface{}, bool, string) {
	if ctx.Kind == LoopKindChat && ctx.StatusC != nil {
		drainStatusEvents(ctx, &conversation, sendProgress)
	}
	if ctx.IsCancelled() {
		ctx.SetLoopState(LoopStateStopped)
		return conversation, true, ""
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
	var injectedText string
	if injected, ok := h.pendingInjection.LoadAndDelete(userID); ok {
		injectedText, _ = injected.(string)
		if injectedText != "" {
			// At iteration 0, a guide reference in pendingInjection means it
			// arrived during an active loop's final moment, survived cleanup
			// (clearNonGuidePendingInjection preserves guide refs), and is now
			// being consumed by the NEXT loop. At iteration 0 there is no
			// "current plan" to re-evaluate, so downgrade to user-role supplement
			// (same semantics as pendingPreLoopGuide).
			if iteration == 0 && isGuideLaunchReferenceInjection(injectedText) {
				guideUserText := stripInjectionPrefix(injectedText)
				if guideUserText != "" {
					conversation = append(conversation, map[string]string{
						"role":    "user",
						"content": "[用户补充说明] " + guideUserText,
					})
					log.Printf("[injection] user=%s guide reference at iteration 0 downgraded to user-role: %s", userID, truncateForLog(guideUserText, 50))
				}
			} else {
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": injectedText,
				})
				log.Printf("[injection] user=%s injected supplementary message: %s", userID, truncateForLog(injectedText, 50))
			}
		}
	}
	// Pre-loop guide: guide-launch text that arrived before the agent loop
	// started (during preflight/intent-classification). Inject as user-role
	// supplement at iteration 0 so LLM treats it as additional user intent
	// rather than a mid-task replan directive. Discard if too old (stale from
	// a previous message that didn't start a loop).
	if preGuide, ok := h.pendingPreLoopGuide.LoadAndDelete(userID); ok {
		if entry, isEntry := preGuide.(*preLoopGuideEntry); isEntry && entry != nil && entry.Text != "" {
			if time.Since(entry.CreatedAt) <= preLoopGuideMaxAge {
				conversation = append(conversation, map[string]string{
					"role":    "user",
					"content": "[用户补充说明] " + entry.Text,
				})
				if injectedText == "" {
					injectedText = entry.Text
				} else {
					injectedText += "\n" + entry.Text
				}
				log.Printf("[injection] user=%s pre-loop guide supplement: %s", userID, truncateForLog(entry.Text, 50))
			} else {
				log.Printf("[injection] user=%s discarded stale pre-loop guide (age=%v): %s", userID, time.Since(entry.CreatedAt).Round(time.Second), truncateForLog(entry.Text, 50))
			}
		}
	}
	return conversation, false, injectedText
}
