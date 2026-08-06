package main

import (
	"strings"
	"sync"
	"time"
)

// AgentActivity represents the current state of an active agent loop.
type AgentActivity struct {
	Source      string // "gui" or "im" — which channel started this loop
	OwnerID     string // assistant session owner; empty only for legacy global activities
	Task        string // user's original request (truncated)
	Iteration   int    // current iteration
	MaxIter     int    // max iterations
	LastSummary string // last assistant reply summary (truncated)
	UpdatedAt   time.Time
}

func (h *IMMessageHandler) startAgentLoopActivity(ownerID, platform, userText string, maxIter int) (agentLoopActivityReporter, func(), string) {
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
			OwnerID:     ownerID,
			Task:        task,
			Iteration:   iter,
			MaxIter:     maxI,
			LastSummary: summary,
		})
	}
	reportActivity(0, maxIter, "")
	cleanup := func() {
		h.agentActivity.ClearForOwner(activitySource, ownerID)
	}
	// Assistant tasks are session-isolated: activity is retained for UI/status
	// reporting only and is never copied into another conversation's prompt.
	return reportActivity, cleanup, ""
}

// agentActivityTTL — entries older than this are considered expired.
const agentActivityTTL = 5 * time.Minute

// AgentActivityStore is a process-local, thread-safe store for active agent
// loops. Entries are scoped by source and assistant owner so concurrent project
// tabs cannot overwrite or clear one another's status.
type AgentActivityStore struct {
	mu    sync.RWMutex
	items map[string]*AgentActivity // source + owner → activity
}

// NewAgentActivityStore creates a new empty store.
type agentLoopActivityReporter func(iter, maxIter int, summary string)

func NewAgentActivityStore() *AgentActivityStore {
	return &AgentActivityStore{items: make(map[string]*AgentActivity)}
}

func agentActivityKey(source, ownerID string) string {
	return strings.TrimSpace(source) + "\x00" + strings.TrimSpace(ownerID)
}

// Update stores or updates an owner-scoped activity.
func (s *AgentActivityStore) Update(a *AgentActivity) {
	if s == nil || a == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *a
	copy.UpdatedAt = time.Now()
	s.items[agentActivityKey(copy.Source, copy.OwnerID)] = &copy
}

// Clear removes the activity for a source channel (loop finished).
func (s *AgentActivityStore) Clear(source string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, agentActivityKey(source, ""))
}

// ClearForOwner removes one owner's activity without disturbing concurrent
// project or IM sessions. Empty owner preserves the old ownerless behavior.
func (s *AgentActivityStore) ClearForOwner(source, ownerID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, agentActivityKey(source, ownerID))
}

// ClearAll drops every activity in this store. Hardware runtimes own a private
// store, so teardown can safely clear it without touching any other device.
func (s *AgentActivityStore) ClearAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.items = make(map[string]*AgentActivity)
	s.mu.Unlock()
}
