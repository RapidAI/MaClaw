package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// CancelCurrentSession cancels the currently running chat session.
// It signals the loop to stop and waits (up to 10s) for it to exit so that
// a subsequent SendAIAssistantMessage call won't overlap with the old loop.
// Returns the cancelled task's user text (if any) for display purposes.
func (h *IMMessageHandler) CancelCurrentSession() (string, error) {
	ctx, userID, taskText := h.legacyLoopSnapshot()
	if userID != "" {
		return h.CancelSessionForUser(userID)
	}
	if ctx == nil {
		return "", fmt.Errorf("no active session to cancel")
	}
	ctx.Cancel()
	// Wait for the loop goroutine to finish so the chatLoopMu is released
	// before the caller sends a new message.
	select {
	case <-ctx.DoneC:
	case <-time.After(10 * time.Second):
		log.Printf("[CancelCurrentSession] timed out waiting for loop to exit")
	}
	return taskText, nil
}

func (h *IMMessageHandler) CancelSessionForUser(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("missing userID")
	}
	ctx := h.getSessionLoopCtx(userID)
	taskText := h.sessionLoopTaskText(userID)
	if ctx == nil {
		ctx, taskText, _ = h.legacyLoopSnapshotForUser(userID)
	}
	if ctx == nil {
		return "", fmt.Errorf("no active session to cancel")
	}
	h.markTaskCancelledByUser(userID)
	ctx.Cancel()
	select {
	case <-ctx.DoneC:
	case <-time.After(10 * time.Second):
		log.Printf("[CancelSessionForUser] timed out waiting for loop to exit user=%s", userID)
	}
	return taskText, nil
}

func (h *IMMessageHandler) legacyLoopSnapshot() (*LoopContext, string, string) {
	if h == nil {
		return nil, "", ""
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	return h.currentLoopCtx, strings.TrimSpace(h.lastUserID), h.lastUserText
}

func (h *IMMessageHandler) legacyLoopSnapshotForUser(userID string) (*LoopContext, string, bool) {
	userID = strings.TrimSpace(userID)
	if h == nil || userID == "" {
		return nil, "", false
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	if strings.TrimSpace(h.lastUserID) != userID {
		return nil, "", false
	}
	return h.currentLoopCtx, h.lastUserText, h.currentLoopCtx != nil
}

func (h *IMMessageHandler) runtimeTaskTextForOwner(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return h.sessionLoopTaskText(ownerID)
	}
	_, _, taskText := h.legacyLoopSnapshot()
	return taskText
}

func (h *IMMessageHandler) currentRuntimeTaskTextOrLegacy() (string, string) {
	ownerID, explicitRuntime := h.currentRuntimePolicyOwnerState()
	if !explicitRuntime || strings.TrimSpace(ownerID) == "" {
		return "", ""
	}
	return h.runtimeTaskTextForOwner(ownerID), ownerID
}

func (h *IMMessageHandler) sessionLoopTaskText(userID string) string {
	if h == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	if v, ok := h.sessionLoops.Load(userID); ok {
		state := v.(*sessionLoopState)
		state.stateMu.RLock()
		defer state.stateMu.RUnlock()
		return state.userText
	}
	return ""
}

func (h *IMMessageHandler) markTaskCancelledByUser(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	h.cancelledTaskBoundary.Store(userID, time.Now())
	h.pendingInjection.Delete(userID)
	if h.interruptHandler != nil {
		h.interruptHandler.ClearTracker(userID)
	}
}

func (h *IMMessageHandler) hasCancelledTaskBoundary(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	_, ok := h.cancelledTaskBoundary.Load(userID)
	return ok
}

func (h *IMMessageHandler) consumeCancelledTaskBoundary(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	_, ok := h.cancelledTaskBoundary.LoadAndDelete(userID)
	return ok
}

// InjectSupplementary stores a supplementary message for the running agent
// loop to consume at the start of its next iteration. Returns true if a loop
// is currently active (injection accepted), false otherwise.
func (h *IMMessageHandler) InjectSupplementary(userID, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || h.hasCancelledTaskBoundary(userID) || !h.hasActiveLoopForUser(userID) {
		return false
	}
	h.accumulateInjection(userID, "[用户补充] "+text)
	log.Printf("[inject-supplementary] user=%s text_len=%d", userID, len([]rune(text)))
	return true
}

// InjectGuideReference stores input-buffer guide-launch text as background
// reference for the next agent loop iteration. Unlike a normal supplementary
// message, this must influence reasoning without becoming a new user turn or
// causing the current session to finalize by itself.
func (h *IMMessageHandler) InjectGuideReference(userID, text string) bool {
	injection := buildGuideLaunchInjection(text)
	if injection == "" || !h.canAcceptGuideReferenceForUser(userID) {
		return false
	}
	h.accumulateInjection(userID, injection)
	log.Printf("[inject-guide-reference] user=%s text_len=%d", userID, len([]rune(text)))
	return true
}

const guideLaunchReferenceMarker = "[\u5f15\u5bfc\u53d1\u5c04\u53c2\u8003]"

const guideLaunchReferenceInstruction = "\u4ee5\u4e0b\u6587\u672c\u7531\u7528\u6237\u4ece\u9884\u8f93\u5165\u7f13\u51b2\u533a\u901a\u8fc7\u56de\u8f66\u56fe\u6807/\u6309\u94ae\u53d1\u5c04\u3002\u8bf7\u628a\u5b83\u4f5c\u4e3a\u4e0b\u4e00\u8f6e agent loop \u7684\u80cc\u666f\u53c2\u8003\uff0c\u7528\u6765\u5f71\u54cd\u63a8\u7406\u548c\u51b3\u7b56\u3002\u4e0d\u8981\u628a\u5b83\u5f53\u4f5c\u65b0\u7684\u7528\u6237\u56de\u5408\uff0c\u4e5f\u4e0d\u8981\u4ec5\u56e0\u4e3a\u8fd9\u6bb5\u53c2\u8003\u5c31\u7ed3\u675f\u5f53\u524d\u4f1a\u8bdd\u6216\u8f93\u51fa\u6700\u7ec8\u7b54\u6848\u3002"

func buildGuideLaunchInjection(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return guideLaunchReferenceMarker + "\n" + guideLaunchReferenceInstruction + "\n" + text
}

func isGuideLaunchReferenceInjection(text string) bool {
	if !strings.Contains(text, guideLaunchReferenceMarker) {
		return false
	}
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if isGuideLaunchReferenceHeader(lines, i) {
			return true
		}
	}
	return false
}

func isGuideLaunchReferenceHeader(lines []string, i int) bool {
	return i+1 < len(lines) &&
		strings.TrimSpace(lines[i]) == guideLaunchReferenceMarker &&
		strings.TrimSpace(lines[i+1]) == guideLaunchReferenceInstruction
}

func (h *IMMessageHandler) hasPendingGuideReferenceInjection(userID string) bool {
	if h == nil || userID == "" {
		return false
	}
	pending, ok := h.pendingInjection.Load(userID)
	if !ok {
		return false
	}
	text, _ := pending.(string)
	return isGuideLaunchReferenceInjection(text)
}

// accumulateInjection appends text to the pending injection for the given
// user. If no pending injection exists, it creates one. If one already
// exists (from a prior injection in the same iteration window), the new
// text is appended with a newline separator.
//
// This is the single write path for pendingInjection; all callers
// (InjectSupplementary, interrupt handler Merge, HandleCorrection Merge)
// must use this method instead of calling pendingInjection.Store directly.
func (h *IMMessageHandler) accumulateInjection(userID, prefixedText string) {
	if strings.TrimSpace(prefixedText) == "" {
		return
	}
	for {
		existing, loaded := h.pendingInjection.Load(userID)
		if !loaded {
			if _, raced := h.pendingInjection.LoadOrStore(userID, prefixedText); !raced {
				return
			}
			continue
		}
		oldText, _ := existing.(string)
		combined := oldText + "\n" + prefixedText
		if h.pendingInjection.CompareAndSwap(userID, existing, combined) {
			return
		}
	}
}

// parseSlotKind converts a string slot kind to the SlotKind enum.
// Defaults to SlotKindScheduled for unknown values.
func parseSlotKind(s string) SlotKind {
	return normalizeSlotKind(s)
}

// drainStatusEvents non-blockingly drains all pending StatusEvents from the
// LoopContext's StatusC channel, injecting each as a system message into the
// conversation and forwarding to the user via sendProgress.
func drainStatusEvents(ctx *LoopContext, conversation *[]interface{}, sendProgress func(string)) {
	for {
		select {
		case evt := <-ctx.StatusC:
			statusMsg := fmt.Sprintf("[鍚庡彴浜嬩欢] %s", evt.Message)
			*conversation = append(*conversation, map[string]string{
				"role": "system", "content": statusMsg,
			})
			sendProgress(fmt.Sprintf("馃摗 %s", evt.Message))
		default:
			return
		}
	}
}

func (h *IMMessageHandler) clearInFlightTask(userID string) {
	if h == nil || h.memory == nil {
		return
	}
	h.memory.ClearInFlightTask(userID)
}

func (h *IMMessageHandler) prepareIMLoopContext(provided *LoopContext, msg IMUserMessage, httpClient *http.Client, skipNeedsConfirmGate bool, isAskUserResponse bool) *LoopContext {
	loopCtx := provided
	if loopCtx == nil {
		loopCtx = NewLoopContext("chat", h.getMaclawAgentMaxIterations(), httpClient)
	}
	if strings.TrimSpace(loopCtx.Runtime.RequestID) == "" {
		policyOwnerID := strings.TrimSpace(loopCtx.Runtime.PolicyOwnerID)
		loopCtx.Runtime = runtimeContextFromIMMessage(msg)
		if policyOwnerID != "" {
			loopCtx.Runtime.PolicyOwnerID = policyOwnerID
			loopCtx.Runtime.WorkflowOwnerID = policyOwnerID
		}
	}
	if loopCtx.HTTPClient == nil {
		loopCtx.HTTPClient = httpClient
	}
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI request accepted", h.runtimeTraceSummary(loopCtx, msg.Text), "", "")
	}
	if h.bgManager != nil && loopCtx.StatusC == nil {
		loopCtx.StatusC = h.bgManager.statusC
	}
	loopCtx.SkipNeedsConfirmGate = skipNeedsConfirmGate
	loopCtx.IsAskUserResponse = isAskUserResponse
	return loopCtx
}

func (h *IMMessageHandler) beginAgentLoopRuntime(ctx *LoopContext, userID, userText, platform string) func() {
	requestID := ""
	loopID := ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	cleanupForegroundQoS := func() {}
	if h != nil && h.app != nil && isForegroundAgentLoopRuntime(ctx) {
		cleanupForegroundQoS = h.app.beginForegroundAgentLoop(userID, requestID, loopID)
	} else if ctx != nil && ctx.Kind == LoopKindBackground {
		log.Printf("[agent-qos] foreground_skip_background owner=%q request_id=%q loop=%q slot=%s", userID, requestID, loopID, ctx.SlotKind.String())
	}
	// Write to per-session state (primary, race-free).
	state := h.getSessionLoop(userID)
	state.stateMu.Lock()
	state.loopCtx = ctx
	state.userText = userText
	state.endedAt = time.Time{}
	state.stateMu.Unlock()

	// Write to legacy global fields (deprecated, kept for tool functions that
	// don't have access to userID). Under concurrency these may be overwritten
	// by another session — tools should migrate to LoopContext parameter passing.
	h.globalLoopMu.Lock()
	h.currentLoopCtx = ctx
	h.lastUserText = userText
	h.lastUserID = userID
	h.globalLoopMu.Unlock()

	ctx.Platform = platform
	ctx.UserID = userID
	if h.traceService != nil && ctx.RunID != "" {
		h.traceService.SetRunLoopID(ctx.RunID, ctx.ID)
		h.appendTraceEvent(ctx, "loop.started", "info", "Agent loop started", h.runtimeTraceSummary(ctx, userText), "", "")
	}
	return func() {
		cleanupForegroundQoS()
		h.clearNonGuidePendingInjection(userID)
		// Clear per-session state.
		state.stateMu.Lock()
		state.loopCtx = nil
		state.userText = ""
		state.endedAt = time.Now()
		state.stateMu.Unlock()
		// Clear legacy global fields only if they still point to THIS loop.
		// Under concurrency another loop may have overwritten them — don't
		// clobber the other loop's state.
		h.globalLoopMu.Lock()
		if h.currentLoopCtx == ctx {
			h.currentLoopCtx = nil
			h.lastUserText = ""
			h.lastUserID = ""
		}
		h.globalLoopMu.Unlock()
		ctx.Done()
	}
}

func isForegroundAgentLoopRuntime(ctx *LoopContext) bool {
	return ctx == nil || ctx.Kind != LoopKindBackground
}

func (h *IMMessageHandler) clearNonGuidePendingInjection(userID string) {
	for {
		pending, ok := h.pendingInjection.Load(userID)
		if !ok {
			return
		}
		text, _ := pending.(string)
		if guideOnly := trimToGuideLaunchReferenceInjection(text); guideOnly != "" {
			if guideOnly == text || h.pendingInjection.CompareAndSwap(userID, pending, guideOnly) {
				return
			}
			continue
		}
		if h.pendingInjection.CompareAndDelete(userID, pending) {
			return
		}
	}
}

func trimToGuideLaunchReferenceInjection(text string) string {
	if !strings.Contains(text, guideLaunchReferenceMarker) {
		return ""
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for i := 0; i+1 < len(lines); i++ {
		if isGuideLaunchReferenceHeader(lines, i) {
			kept = append(kept, lines[i], lines[i+1])
			i++
			for i+1 < len(lines) && !isGuideLaunchReferenceHeader(lines, i+1) {
				i++
				if isLegacyInjectionPrefixLine(lines[i]) {
					break
				}
				kept = append(kept, lines[i])
			}
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isLegacyInjectionPrefixLine(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, guideLaunchReferenceMarker) {
		return false
	}
	idx := strings.Index(line, "] ")
	return idx > 0 && idx < 80
}
