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
	h.globalLoopMu.RLock()
	ctx := h.currentLoopCtx
	taskText := h.lastUserText
	h.globalLoopMu.RUnlock()
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

// InjectSupplementary stores a supplementary message for the running agent
// loop to consume at the start of its next iteration. Returns true if a loop
// is currently active (injection accepted), false otherwise.
func (h *IMMessageHandler) InjectSupplementary(userID, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !h.hasActiveLoopForUser(userID) {
		return false
	}
	h.accumulateInjection(userID, "[用户补充] "+text)
	log.Printf("[inject-supplementary] user=%s text=%s", userID, truncateForLog(text, 60))
	return true
}

// InjectGuideReference stores input-buffer guide-launch text as background
// reference for the next agent loop iteration. Unlike a normal supplementary
// message, this must influence reasoning without becoming a new user turn or
// causing the current session to finalize by itself.
func (h *IMMessageHandler) InjectGuideReference(userID, text string) bool {
	injection := buildGuideLaunchInjection(text)
	if injection == "" || !h.hasActiveLoopForUser(userID) {
		return false
	}
	h.accumulateInjection(userID, injection)
	log.Printf("[inject-guide-reference] user=%s text=%s", userID, truncateForLog(text, 60))
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
	if loopCtx.HTTPClient == nil {
		loopCtx.HTTPClient = httpClient
	}
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI request accepted", truncateTraceText(msg.Text, 180), "", "")
	}
	if h.bgManager != nil && loopCtx.StatusC == nil {
		loopCtx.StatusC = h.bgManager.statusC
	}
	loopCtx.SkipNeedsConfirmGate = skipNeedsConfirmGate
	loopCtx.IsAskUserResponse = isAskUserResponse
	return loopCtx
}

func (h *IMMessageHandler) beginAgentLoopRuntime(ctx *LoopContext, userID, userText, platform string) func() {
	// Write to per-session state (primary, race-free).
	state := h.getSessionLoop(userID)
	state.loopCtx = ctx
	state.userText = userText

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
		h.appendTraceEvent(ctx, "loop.started", "info", "Agent loop started", truncateTraceText(userText, 180), "", "")
	}
	return func() {
		h.pendingInjection.Delete(userID)
		// Clear per-session state.
		state.loopCtx = nil
		state.userText = ""
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
