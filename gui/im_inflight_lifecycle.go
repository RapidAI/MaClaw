package main

import (
	"log"
	"strings"
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
	// Prefer session-owner working dir (top bar / tools); fall back to Projects list
	// only for non-desktop identities without an encoded project path.
	projectPath := ""
	if l.handler != nil {
		projectPath = l.handler.effectiveWorkingDirForUser(l.userID)
	}
	if projectPath == "" {
		projectPath = projectPathFromUserID(l.userID)
	}
	if projectPath == "" && strings.TrimSpace(l.userID) != desktopUserID && l.handler != nil {
		projectPath = l.handler.getCurrentProjectPath()
	}
	log.Printf("[InFlightTask] set user=%q project=%q text_len=%d", l.userID, projectPath, len([]rune(l.userText)))
	l.handler.memory.SetInFlightTask(l.userID, truncateRunes(l.userText, 200), projectPath)
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
	log.Printf("[InFlightTask] clear user=%q", l.userID)
	l.handler.clearInFlightTask(l.userID)
	_ = l.handler.memory.FlushNow()

	// Destroy scroll session for this agent loop (Requirement 4.5).
	if l.loopID != "" && l.handler.memoryStore != nil {
		if ss := l.handler.memoryStore.ScrollSessions(); ss != nil {
			ss.Destroy(l.loopID, l.userID)
		}
	}
}
