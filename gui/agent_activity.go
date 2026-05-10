package main

import (
	"fmt"
	"sync"
	"time"
)

// AgentActivity represents the current state of an active agent loop.
type AgentActivity struct {
	Source      string // "gui" or "im" — which channel started this loop
	Task        string // user's original request (truncated)
	Iteration   int    // current iteration
	MaxIter     int    // max iterations
	LastSummary string // last assistant reply summary (truncated)
	UpdatedAt   time.Time
}

func (h *IMMessageHandler) startAgentLoopActivity(platform, userText string, maxIter int) (agentLoopActivityReporter, func(), string) {
	activitySource := agentActivitySourceForPlatform(platform).String()
	reportActivity := func(iter, maxI int, summary string) {
		task := userText
		if len(task) > 100 {
			task = task[:100]
		}
		if len(summary) > 120 {
			summary = summary[:120]
		}
		h.agentActivity.Update(&AgentActivity{
			Source:      activitySource,
			Task:        task,
			Iteration:   iter,
			MaxIter:     maxI,
			LastSummary: summary,
		})
	}
	reportActivity(0, maxIter, "")
	cleanup := func() {
		h.agentActivity.Clear(activitySource)
	}
	return reportActivity, cleanup, h.agentActivity.FormatForPrompt(activitySource)
}

// agentActivityTTL — entries older than this are considered expired.
const agentActivityTTL = 5 * time.Minute

// AgentActivityStore is a process-local, thread-safe store for active agent
// loops. It allows the GUI AI assistant and IM channels to see each other's
// active tasks within the same desktop process.
type AgentActivityStore struct {
	mu    sync.RWMutex
	items map[string]*AgentActivity // source → activity (at most one per channel)
}

// NewAgentActivityStore creates a new empty store.
type agentLoopActivityReporter func(iter, maxIter int, summary string)

func NewAgentActivityStore() *AgentActivityStore {
	return &AgentActivityStore{items: make(map[string]*AgentActivity)}
}

// Update stores or updates the activity for a source channel.
func (s *AgentActivityStore) Update(a *AgentActivity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.UpdatedAt = time.Now()
	s.items[a.Source] = a
}

// Clear removes the activity for a source channel (loop finished).
func (s *AgentActivityStore) Clear(source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, source)
}

// FormatForPrompt returns a human-readable summary of active tasks from
// OTHER channels (excluding the given source), for injection into the
// system prompt. Returns empty string if no cross-channel activity.
func (s *AgentActivityStore) FormatForPrompt(excludeSource string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-agentActivityTTL)
	var lines []string
	for src, a := range s.items {
		if src == excludeSource || a.UpdatedAt.Before(cutoff) {
			continue
		}
		label := "IM 通道"
		switch normalizeAgentActivitySource(a.Source) {
		case agentActivitySourceGUI:
			label = "GUI AI 助手"
		case agentActivitySourceBrowserReplay:
			label = "浏览器回放"
		case agentActivitySourceGUIReplay:
			label = "GUI 桌面回放"
		}
		line := fmt.Sprintf("- [%s] 任务: %s", label, a.Task)
		if a.MaxIter > 0 {
			line += fmt.Sprintf(" (进度: %d/%d 轮)", a.Iteration, a.MaxIter)
		}
		if a.LastSummary != "" {
			line += " — 最新状态: " + a.LastSummary
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	result := "\n\n其他通道正在执行的任务：\n"
	for _, l := range lines {
		result += l + "\n"
	}
	result += "如果用户询问你在做什么、当前状态等，请基于上述信息回答。\n"
	return result
}
