package guiapp

import (
	"crypto/rand"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type imInFlightLifecycle struct {
	handler          *IMMessageHandler
	userID           string
	userText         string
	loopID           string
	contextLoopID    string
	markerSet        bool
	preserveOnFinish bool
	leaseStop        chan struct{}
}

// startInFlightLeaseRefresh extends runID's lease until stop is closed. The
// lease means this turn is still inside the agent loop, including a tool that
// emits no tokens. Closing stop is required; the first refresh waits one renew
// interval because the checkpoint that created the marker is already fresh.
func startInFlightLeaseRefresh(stop <-chan struct{}, memory *agent.ConversationMemory, userID, runID string) {
	if memory == nil || strings.TrimSpace(runID) == "" || stop == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	runID = strings.TrimSpace(runID)
	go func() {
		ticker := time.NewTicker(agent.InFlightTaskRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				memory.RefreshInFlightTaskForRun(userID, runID)
			}
		}
	}()
}

// newInFlightRecoveryRunID identifies one turn's recovery marker. The loop
// context id is a stable kind such as "chat" and is reused by every turn, so
// it cannot be the run id: lease expiry and completion would then treat a
// later turn as the owner of an earlier turn's unfinished slot.
func newInFlightRecoveryRunID() string {
	var entropy [4]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("run-%d-%x", time.Now().UnixNano(), entropy)
}

func (h *IMMessageHandler) newInFlightLifecycle(userID, userText string) *imInFlightLifecycle {
	return &imInFlightLifecycle{handler: h, userID: userID, userText: userText}
}

func (l *imInFlightLifecycle) SetOnce() {
	if l == nil || l.handler == nil || l.handler.memory == nil {
		return
	}
	if l.markerSet {
		// Later tool commits are progress. Refreshing here keeps a live legacy
		// turn from being published as unfinished between checkpoints.
		l.handler.memory.RefreshInFlightTaskForRun(l.userID, l.loopID)
		return
	}
	l.markerSet = true
	if strings.TrimSpace(l.loopID) == "" {
		l.loopID = newInFlightRecoveryRunID()
		log.Printf("[InFlightTask] generated missing run id user=%q run=%q", l.userID, l.loopID)
	}
	// Prefer an explicit tab bind (top bar / tools); a project-tab owner ID's
	// encoded path ("desktop-user:<path>") comes next and must win over the
	// global workspace fallback inside effectiveWorkingDirForUser — otherwise a
	// project-tab task would be stamped with the main tab's directory.
	projectPath := ""
	if l.handler != nil && l.handler.app != nil {
		projectPath = strings.TrimSpace(l.handler.app.BoundWorkingDirForOwner(l.userID))
	}
	if projectPath == "" {
		projectPath = projectPathFromUserID(l.userID)
	}
	if projectPath == "" && l.handler != nil {
		projectPath = l.handler.effectiveWorkingDirForUser(l.userID)
	}
	if projectPath == "" && strings.TrimSpace(l.userID) != desktopUserID && l.handler != nil {
		projectPath = l.handler.getCurrentProjectPath()
	}
	log.Printf("[InFlightTask] set user=%q project=%q text_len=%d", l.userID, projectPath, len([]rune(l.userText)))
	l.handler.memory.SetInFlightTaskForRun(l.userID, truncateRunes(l.userText, 200), projectPath, l.loopID)
	if err := l.handler.memory.FlushNow(); err != nil {
		log.Printf("[InFlightTask] flush failed: %v", err)
	}
	l.ensureLeaseRefresh()
}

func (l *imInFlightLifecycle) ensureLeaseRefresh() {
	if l == nil || l.leaseStop != nil || l.handler == nil || l.handler.memory == nil {
		return
	}
	runID := strings.TrimSpace(l.loopID)
	if runID == "" {
		return
	}
	stop := make(chan struct{})
	l.leaseStop = stop
	startInFlightLeaseRefresh(stop, l.handler.memory, l.userID, runID)
}

func (l *imInFlightLifecycle) stopLeaseRefresh() {
	if l == nil || l.leaseStop == nil {
		return
	}
	close(l.leaseStop)
	l.leaseStop = nil
}

func (l *imInFlightLifecycle) PreserveOnFinish() {
	if l == nil {
		return
	}
	l.preserveOnFinish = true
}

func (l *imInFlightLifecycle) Cleanup() {
	if l == nil {
		return
	}
	// Stop even when the marker is preserved. A ticker that outlives the turn
	// would renew a failed run forever and the lease would never become a slot.
	l.stopLeaseRefresh()
	if l.handler == nil || l.handler.memory == nil || !l.markerSet || l.preserveOnFinish {
		return
	}
	log.Printf("[InFlightTask] clear user=%q run=%q", l.userID, l.loopID)
	if err := l.handler.memory.CompleteInFlightCheckpointForRun(l.userID, l.loopID); err != nil {
		log.Printf("[InFlightTask] cleanup flush failed user=%q run=%q err=%v", l.userID, l.loopID, err)
	}

	// Destroy scroll session for this agent loop (Requirement 4.5).
	// Recall sessions are keyed by the loop context id, which is not the
	// per-turn recovery id.
	if l.handler.memoryStore != nil {
		if ss := l.handler.memoryStore.ScrollSessions(); ss != nil {
			if id := strings.TrimSpace(l.loopID); id != "" {
				ss.Destroy(id, l.userID)
			}
			if id := strings.TrimSpace(l.contextLoopID); id != "" && id != strings.TrimSpace(l.loopID) {
				ss.Destroy(id, l.userID)
			}
		}
	}
}

// persistRecoveryCheckpoint is the GUI boundary for a durable tool-progress
// checkpoint. Conversation context and its run-scoped marker are flushed by a
// single ConversationMemory operation; callers must stop the loop on error.
func (h *IMMessageHandler) persistRecoveryCheckpoint(userID, task, projectPath, runID string, history []agent.ConversationEntry, checkpoint agent.InFlightCheckpoint) error {
	if h == nil || h.memory == nil {
		return nil
	}
	return h.memory.PersistInFlightCheckpoint(
		userID,
		history,
		truncateRunes(task, 200),
		projectPath,
		runID,
		checkpoint,
	)
}

// persistSharedInteractivePause commits a protocol-paired interactive pause
// and clears the same run's pre-tool recovery marker as one disk transition.
func (h *IMMessageHandler) persistSharedInteractivePause(userID, runID string, history []agent.ConversationEntry) error {
	if h == nil || h.memory == nil {
		return nil
	}
	return h.memory.SaveAndCompleteInFlightCheckpointForRun(userID, runID, history)
}
