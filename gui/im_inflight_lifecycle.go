package main

import (
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
	markerSet        bool
	preserveOnFinish bool
}

func (h *IMMessageHandler) newInFlightLifecycle(userID, userText string) *imInFlightLifecycle {
	return &imInFlightLifecycle{handler: h, userID: userID, userText: userText}
}

func (l *imInFlightLifecycle) SetOnce() {
	if l == nil || l.handler == nil || l.markerSet {
		return
	}
	l.markerSet = true
	if strings.TrimSpace(l.loopID) == "" {
		l.loopID = fmt.Sprintf("legacy-%d", time.Now().UnixNano())
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
}

func (l *imInFlightLifecycle) PreserveOnFinish() {
	if l == nil {
		return
	}
	l.preserveOnFinish = true
}

func (l *imInFlightLifecycle) Cleanup() {
	if l == nil || l.handler == nil || !l.markerSet || l.preserveOnFinish {
		return
	}
	log.Printf("[InFlightTask] clear user=%q run=%q", l.userID, l.loopID)
	if err := l.handler.memory.CompleteInFlightCheckpointForRun(l.userID, l.loopID); err != nil {
		log.Printf("[InFlightTask] cleanup flush failed user=%q run=%q err=%v", l.userID, l.loopID, err)
	}

	// Destroy scroll session for this agent loop (Requirement 4.5).
	if l.loopID != "" && l.handler.memoryStore != nil {
		if ss := l.handler.memoryStore.ScrollSessions(); ss != nil {
			ss.Destroy(l.loopID, l.userID)
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
