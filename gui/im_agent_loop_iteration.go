package main

import (
	"fmt"
	"log"
	"strings"
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
	conversation, injectedText := h.appendPendingSteerInjections(userID, conversation, iteration)
	return conversation, false, injectedText
}

// appendPendingSteerInjections drains mid-loop pendingInjection and pre-loop
// guide launches into the conversation. Shared by the main agent loop and pure
// coding SubAgents (local CodingSubAgent / remote RemoteCodingSubAgent), which
// otherwise never see buffer-queue guide launches.
//
// Returns the (possibly extended) conversation and a non-empty injectedText
// summary when anything was applied.
func (h *IMMessageHandler) appendPendingSteerInjections(userID string, conversation []interface{}, iteration int) ([]interface{}, string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return conversation, ""
	}
	var injectedText string
	if injected, ok := h.pendingInjection.LoadAndDelete(userID); ok {
		raw, _ := injected.(string)
		for _, pending := range splitPendingInjections(raw) {
			if isGuideLaunchReferenceInjection(pending) {
				// Guide reference (from buffer queue fire button): always inject
				// as user-role message regardless of iteration. This matches
				// Codex's "steer" behavior — the user's steering text lands as
				// a real user message so LLM treats it with full compliance.
				// The guide is a supplement/correction to the current task, NOT
				// a cancellation — the agent should complete the original
				// request while incorporating this additional guidance.
				guideUserText := stripInjectionPrefix(pending)
				if guideUserText != "" {
					conversation = append(conversation, map[string]string{
						"role":    "user",
						"content": buildLiveSteerUserMessage(guideUserText),
					})
					// Return the user-facing text (not the English wrapper) so
					// callers that gate on injectedText see real steering content.
					if injectedText == "" {
						injectedText = guideUserText
					} else {
						injectedText += "\n" + guideUserText
					}
					log.Printf("[injection] user=%s guide reference as user-role (iteration=%d): %s", userID, iteration, truncateForLog(guideUserText, 50))
				} else {
					log.Printf("[injection] user=%s discarded empty guide reference wrapper (iteration=%d)", userID, iteration)
				}
			} else {
				// Non-guide injections (e.g. inline interrupt, agent view
				// submit) keep the system-role behavior.
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": pending,
				})
				if injectedText == "" {
					injectedText = pending
				} else {
					injectedText += "\n" + pending
				}
				log.Printf("[injection] user=%s injected supplementary message: %s", userID, truncateForLog(pending, 50))
			}
		}
	}
	// Pre-loop guide: guide-launch text that arrived before the agent loop
	// started (during preflight/intent-classification), or during pure coding
	// before session loopCtx was registered. Inject as user-role supplement.
	// Discard if too old (stale from a previous message that didn't start a loop).
	if preGuide, ok := h.pendingPreLoopGuide.LoadAndDelete(userID); ok {
		if entry, isEntry := preGuide.(*preLoopGuideEntry); isEntry && entry != nil && entry.Text != "" {
			if time.Since(entry.CreatedAt) <= preLoopGuideMaxAge {
				conversation = append(conversation, map[string]string{
					"role":    "user",
					"content": buildLiveSteerUserMessage(entry.Text),
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
	return conversation, injectedText
}

// buildLiveSteerUserMessage gives the model the user's actual interruption and
// conversational intent. The transport/UI deliberately does not synthesize an
// acknowledgement: the next real model response should demonstrate that it
// understood the interruption in whatever way fits the current context.
func buildLiveSteerUserMessage(text string) string {
	return "[The user spoke while you were working]\n" + strings.TrimSpace(text) +
		"\n\nTreat this as live steering within the current conversation. Re-check the next step and incorporate the user's addition or correction before continuing. Let the next visible response naturally show that you understood it, as in a real multi-person conversation: respond to the substance when useful, or simply let the changed work demonstrate it. Do not emit a canned receipt, do not mechanically say that it was received or attached, and do not force a quotation or acknowledgement when that would sound unnatural. Do not treat it as a separate new task."
}
