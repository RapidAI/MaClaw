package guiapp

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// snapshotInterruptedSessionsForShutdown records every active or pre-loop turn
// before cancellation. The caller waits for loop cleanup before the final flush.
func (h *IMMessageHandler) snapshotInterruptedSessionsForShutdown() {
	if h == nil || h.memory == nil {
		return
	}
	seen := make(map[string]*LoopContext)
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
		if ctx != nil {
			seen[userID] = ctx
			h.snapshotInterruptedSessionForShutdown(userID, userText, ctx, state)
		}
		return true
	})
	h.inFlightTurns.Range(func(key, value any) bool {
		userID, _ := key.(string)
		turn, _ := value.(*inFlightTurn)
		if turn == nil || turn.ctx == nil || strings.TrimSpace(userID) == "" {
			return true
		}
		if previous := seen[userID]; previous == turn.ctx {
			return true
		}
		h.snapshotInterruptedSessionForShutdown(userID, turn.userText, turn.ctx, nil)
		return true
	})
}

func (h *IMMessageHandler) snapshotInterruptedSessionForShutdown(userID, userText string, ctx *LoopContext, state *sessionLoopState) {
	if ctx == nil {
		return
	}
	if ctx.UserCancelled() {
		h.retireUserCancelCheckpoint(userID, ctx.ID)
		return
	}
	if ctx.IsCancelled() {
		return
	}
	entries := h.memory.Load(userID)
	taskText := strings.TrimSpace(userText)
	if taskText == "" {
		taskText = strings.TrimSpace(ctx.Description)
	}
	if taskText == "" {
		taskText = extractOriginalUserTask(entries)
	}
	if taskText == "" {
		log.Printf("[ShutdownSnapshot] skip user=%q reason=no_task_text", userID)
		ctx.Cancel()
		return
	}
	projectPath := h.effectiveWorkingDirForUser(userID)
	if projectPath == "" {
		projectPath = projectPathFromUserID(userID)
	}
	if state != nil {
		state.stateMu.RLock()
		stillActive := state.loopCtx == ctx && !ctx.IsCancelled()
		state.stateMu.RUnlock()
		if !stillActive {
			log.Printf("[ShutdownSnapshot] skip user=%q reason=state_changed_during_snapshot", userID)
			if ctx.UserCancelled() {
				h.retireUserCancelCheckpoint(userID, ctx.ID)
				return
			}
			ctx.Cancel()
			return
		}
	}
	now := time.Now()
	slot := &agent.UnfinishedTaskSlot{
		SlotID:      shutdownRecoverySlotID(now),
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
	if !h.memory.ReplaceInFlightWithUnfinishedSlot(userID, slot) {
		log.Printf("[ShutdownSnapshot] skip user=%q reason=invalid_slot", userID)
		ctx.Cancel()
		return
	}
	log.Printf("[ShutdownSnapshot] saved unfinished slot user=%q project=%q task=%q",
		userID, projectPath, truncateRunes(taskText, 80))
	ctx.Cancel()
}

func (h *IMMessageHandler) waitForInterruptedSessionsForShutdown() {
	if h == nil {
		return
	}
	type waitItem struct {
		userID string
		done   <-chan struct{}
	}
	var items []waitItem
	seen := make(map[*LoopContext]struct{})
	h.sessionLoops.Range(func(key, value any) bool {
		userID, _ := key.(string)
		state, _ := value.(*sessionLoopState)
		if state == nil {
			return true
		}
		state.stateMu.RLock()
		ctx := state.loopCtx
		state.stateMu.RUnlock()
		if ctx != nil {
			seen[ctx] = struct{}{}
			items = append(items, waitItem{userID: userID, done: ctx.DoneC})
		}
		return true
	})
	h.inFlightTurns.Range(func(key, value any) bool {
		userID, _ := key.(string)
		turn, _ := value.(*inFlightTurn)
		if turn == nil || turn.ctx == nil {
			return true
		}
		if _, ok := seen[turn.ctx]; ok {
			return true
		}
		items = append(items, waitItem{userID: userID, done: turn.done})
		return true
	})
	var wg sync.WaitGroup
	var timeoutMu sync.Mutex
	var timedOut []string
	for _, item := range items {
		if item.done == nil {
			continue
		}
		wg.Add(1)
		go func(item waitItem) {
			defer wg.Done()
			select {
			case <-item.done:
			case <-time.After(shutdownLoopCleanupWait):
				timeoutMu.Lock()
				timedOut = append(timedOut, item.userID)
				timeoutMu.Unlock()
			}
		}(item)
	}
	// Wait for every waiter. Closing a channel from a parallel timeout races
	// the send and panics on quit.
	wg.Wait()
	timeoutMu.Lock()
	defer timeoutMu.Unlock()
	for _, userID := range timedOut {
		log.Printf("[ShutdownSnapshot] timed out waiting for saved loop cleanup user=%q", userID)
	}
}

// shutdownLoopCleanupWait bounds how long quit waits for a cancelled agent
// loop to write its last progress. The resume slot is already saved; a longer
// wait is what made the tray Quit feel stuck.
const shutdownLoopCleanupWait = 500 * time.Millisecond

func shutdownRecoverySlotID(now time.Time) string {
	return fmt.Sprintf("shutdown-%d-%d", now.UnixMilli(), time.Now().UnixNano())
}
