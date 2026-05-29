package main

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

type imProgressVisibilityFilter struct {
	mu                     sync.Mutex
	enabled                bool
	sentFirst              bool
	sentCommandStillActive bool
}

func newIMProgressVisibilityFilterFromConfig(cfg corelib.AppConfig) *imProgressVisibilityFilter {
	return &imProgressVisibilityFilter{enabled: cfg.IsIMProgressNudgeEnabled()}
}

func newIMProgressVisibilityFilter(app *App) *imProgressVisibilityFilter {
	if app == nil {
		return &imProgressVisibilityFilter{enabled: true}
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		return &imProgressVisibilityFilter{enabled: true}
	}
	return newIMProgressVisibilityFilterFromConfig(cfg)
}

func (f *imProgressVisibilityFilter) ShouldSend() bool {
	if f == nil || f.enabled {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.sentFirst {
		f.sentFirst = true
		return true
	}
	return false
}

func (f *imProgressVisibilityFilter) ShouldSendProgress(text string) bool {
	if text == "" || text == imHeartbeatMsg {
		return false
	}
	if isIMCommandStillRunningProgress(text) {
		return f.ShouldSendCommandStillRunningProgress()
	}
	return f.ShouldSend()
}

func (f *imProgressVisibilityFilter) ForwardProgressOrHeartbeat(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	if text == imHeartbeatMsg {
		return text, true
	}
	if isIMCommandStillRunningProgress(text) {
		if f.ShouldSendCommandStillRunningProgress() {
			return text, true
		}
		return imHeartbeatMsg, true
	}
	if f.ShouldSend() {
		return text, true
	}
	return imHeartbeatMsg, true
}

func (f *imProgressVisibilityFilter) ShouldSendCommandStillRunningProgress() bool {
	if f == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.enabled || f.sentCommandStillActive {
		return false
	}
	f.sentCommandStillActive = true
	return true
}

func isIMCommandStillRunningProgress(text string) bool {
	return strings.Contains(text, "\u547d\u4ee4\u4ecd\u5728\u6267\u884c\u4e2d") || strings.Contains(strings.ToLower(text), "command still running")
}
