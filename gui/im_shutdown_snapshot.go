package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// snapshotInterruptedSessionsForShutdown persists an unfinished-task slot for
// every session that still has an active agent loop when the app is closing.
//
// Previously a graceful exit left no recovery marker at all: the in-flight
// marker lifecycle only covers crashes and max-rounds termination, so a task
// that was mid-flight when the user closed the window silently lost its
// resume entry point. Persisting a slot here lets the next launch continue
// the task when the user opens the restored (or task-list) tab.
//
// The slot uses UnfinishedTaskSlotSourceAppExit: reopening the tab IS the
// user's resume intent, so the message pipeline binds these slots
// automatically (applyAppExitAutoResumeDecision) instead of showing a
// "continue previous task?" banner.
func (h *IMMessageHandler) snapshotInterruptedSessionsForShutdown() {
	if h == nil || h.memory == nil {
		return
	}
	h.sessionLoops.Range(func(key, value any) bool {
		userID, _ := key.(string)
		state, ok := value.(*sessionLoopState)
		if !ok || state == nil || strings.TrimSpace(userID) == "" {
			return true
		}
		state.stateMu.RLock()
		ctx := state.loopCtx
		userText := state.userText
		state.stateMu.RUnlock()
		if ctx == nil || ctx.IsCancelled() {
			return true
		}

		// Never overwrite a slot the user has not decided on yet — an older
		// pending slot is a deliberate user-facing state, not stale data.
		if unfinishedSlotNeedsDecision(h.memory.GetUnfinishedSlot(userID)) {
			log.Printf("[ShutdownSnapshot] skip user=%q reason=pending_slot_exists", userID)
			ctx.Cancel()
			return true
		}

		entries := h.memory.Load(userID)
		taskText := strings.TrimSpace(userText)
		if taskText == "" {
			// Background loops carry their task in Description, not userText.
			taskText = strings.TrimSpace(ctx.Description)
		}
		if taskText == "" {
			taskText = extractOriginalUserTask(entries)
		}
		if taskText == "" {
			log.Printf("[ShutdownSnapshot] skip user=%q reason=no_task_text", userID)
			ctx.Cancel()
			return true
		}

		projectPath := h.effectiveWorkingDirForUser(userID)
		if projectPath == "" {
			projectPath = projectPathFromUserID(userID)
		}

		// Re-check right before writing: the loop may have finished while we
		// were loading entries above — its cleanup clears state.loopCtx (see
		// im_loop_control.go), and natural completion does not cancel the
		// context, so identity comparison is the reliable signal. Writing an
		// "interrupted" slot for a finished loop would resurrect a completed
		// task on the next launch. Likewise, a slot that appeared meanwhile
		// is newer state and must not be overwritten.
		state.stateMu.RLock()
		stillActive := state.loopCtx == ctx && !ctx.IsCancelled()
		state.stateMu.RUnlock()
		if !stillActive || unfinishedSlotNeedsDecision(h.memory.GetUnfinishedSlot(userID)) {
			log.Printf("[ShutdownSnapshot] skip user=%q reason=state_changed_during_snapshot", userID)
			ctx.Cancel()
			return true
		}

		now := time.Now()
		slot := &agent.UnfinishedTaskSlot{
			SlotID:      fmt.Sprintf("shutdown-%d", now.UnixMilli()),
			UserID:      userID,
			ProjectPath: projectPath,
			Status:      agent.UnfinishedTaskSlotStatusInterrupted,
			LastTask:    truncateRunes(taskText, 200),
			Summary:     extractProgressSummary(entries),
			ResumePrompt: "The application was closed while this task was still in progress. " +
				"Resume from the saved context and continue the original task instead of starting over.",
			Source:    agent.UnfinishedTaskSlotSourceAppExit,
			CreatedAt: now,
			UpdatedAt: now,
		}
		h.memory.UpsertUnfinishedSlot(userID, slot)
		// Clear any stale in-flight marker so the next launch does not convert
		// it into a duplicate slot via recoverInterruptedTaskSlot.
		h.memory.ClearInFlightTask(userID)
		log.Printf("[ShutdownSnapshot] saved unfinished slot user=%q project=%q task=%q",
			userID, projectPath, truncateRunes(taskText, 80))

		ctx.Cancel()
		return true
	})
}
