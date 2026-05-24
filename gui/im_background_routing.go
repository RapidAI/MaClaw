package main

import (
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) handleBackgroundIMRoute(msg IMUserMessage, providedLoopCtx *LoopContext, httpClient *http.Client, onProgress tool.ProgressCallback) (*IMAgentResponse, bool) {
	if !msg.IsBackground || h.bgManager == nil || providedLoopCtx != nil {
		return nil, false
	}
	slotKind := parseSlotKind(msg.BackgroundSlotKind)
	maxIter := h.getMaclawAgentMaxIterations()
	if msg.MinIterations > maxIter {
		maxIter = msg.MinIterations
	}

	loopCtx, waitC := h.bgManager.SpawnOrQueue(slotKind, msg.UserID, msg.Text, maxIter)
	if loopCtx == nil && waitC != nil {
		// Wait for the slot to become available, but with a timeout to prevent
		// goroutine leaks when the previous task never completes.
		select {
		case loopCtx = <-waitC:
			// slot acquired
		case <-time.After(2 * time.Minute):
			// Remove the pending task from the queue to prevent an orphan
			// LoopContext from being created when the slot eventually frees.
			h.bgManager.CancelPending(slotKind, waitC)
			return &IMAgentResponse{Error: "Background task slot occupied by a long-running task. Skipping this execution."}, true
		}
	}
	if loopCtx == nil {
		return &IMAgentResponse{Error: "Background task failed to start: unable to acquire execution slot."}, true
	}
	loopCtx.HTTPClient = httpClient
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "Background task accepted", truncateTraceText(msg.Text, 180), "", "")
	}

	// Bind external cancellation context (e.g. scheduler timeout) to the loop.
	// When the parent context is cancelled, the loop's CancelC closes automatically.
	loopCtx.BindParentContext(msg.CancelCtx)

	var systemPrompt string
	history := h.memory.Load(msg.UserID)
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0, loopCtx)
	} else {
		systemPrompt = h.buildSystemPrompt()
	}
	if activeSlot := h.memory.ActiveUnfinishedSlot(msg.UserID); activeSlot != nil {
		systemPrompt += buildUnfinishedSlotResumeContext(activeSlot)
		systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, activeSlot.LastTask)
	} else {
		systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, msg.Text)
	}
	platformKind := normalizeIMMessagePlatformKind(msg.Platform)
	if platformKind.IsDesktop() {
		systemPrompt += desktopWorkflowDocOverride()
	} else if platformKind.IsKnown() || msg.Platform != "" {
		systemPrompt += imWorkflowDocDeliveryRule()
	}

	result := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, nil, nil, nil, msg.MinIterations, msg.Platform)
	h.runEvidenceCollection(msg.UserID, msg.Text)

	if result != nil && result.Error != "" {
		loopCtx.SetLoopState(LoopStateFailed)
	} else {
		loopCtx.SetLoopState(LoopStateCompleted)
	}
	h.recordAgentLoopTerminalExperience(loopCtx, result)
	summaryText := ""
	errText := ""
	if result != nil {
		summaryText = firstNonEmptyTraceText(result.Text, result.TraceSummary)
		errText = result.Error
	}
	result = h.finalizeTraceResult(loopCtx, result, summaryText, errText)
	h.bgManager.Complete(loopCtx.ID)
	return result, true
}
