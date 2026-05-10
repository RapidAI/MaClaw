package main

import "log"

type imInFlightLifecycle struct {
	handler          *IMMessageHandler
	userID           string
	userText         string
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
	projectPath := l.handler.getCurrentProjectPath()
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
	l.handler.clearInFlightTask(l.userID)
	_ = l.handler.memory.FlushNow()
}
