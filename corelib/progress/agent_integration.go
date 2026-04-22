package progress

import (
	"encoding/json"
	"sync"
	"time"
)

// AgentProgressTracker integrates the milestone-based progress system into
// an agent loop. It wraps MilestoneBuffer + ProgressPusher and provides
// simple methods for the agent loop to call at key points.
//
// Usage in agent loop:
//
//	tracker := progress.NewAgentProgressTracker(onProgress, taskDesc, intentLabel, taskEmbed)
//	defer tracker.Stop()
//
//	// In the iteration loop, call Tick periodically:
//	tracker.Tick()
//
//	// After each tool call completes:
//	tracker.RecordToolCall(toolName, argsJSON, completed)
//
//	// When the loop finishes:
//	tracker.Stop()
type AgentProgressTracker struct {
	mu     sync.Mutex
	buffer *MilestoneBuffer
	pusher *ProgressPusher

	tickerDone chan struct{}
	stopped    bool
}

// NewAgentProgressTracker creates a tracker for one agent loop execution.
// onProgress is the callback to deliver progress messages (same as the existing
// onProgress callback in runAgentLoop). taskEmbed may be nil if embedding is
// not available.
func NewAgentProgressTracker(
	onProgress func(string),
	taskDesc string,
	intentLabel string,
	taskEmbed []float32,
) *AgentProgressTracker {
	buf := NewMilestoneBuffer(64)
	buf.Reset(taskDesc, intentLabel, taskEmbed)

	msgLen := len([]rune(taskDesc))
	complexity := ClassifyComplexity(intentLabel, msgLen)

	pushFn := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	pusher := NewProgressPusher(buf, pushFn, complexity)

	t := &AgentProgressTracker{
		buffer:     buf,
		pusher:     pusher,
		tickerDone: make(chan struct{}),
	}

	// Background ticker for merge window expiry and heartbeat.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.pusher.Tick()
			case <-t.tickerDone:
				return
			}
		}
	}()

	return t
}

// RecordToolCall records a tool execution as a milestone. argsJSON is the
// raw JSON string from the tool call (will be parsed to extract summary args).
// completed should be true when the tool call has finished.
func (t *AgentProgressTracker) RecordToolCall(toolName string, argsJSON string, completed bool) {
	var args map[string]any
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}

	m := ExtractMilestone(toolName, args, completed)
	if m == nil {
		return
	}

	t.buffer.Record(*m)
	t.pusher.OnMilestone(*m)
}

// Tick should be called periodically by the agent loop (e.g. at the start
// of each iteration). It handles merge window expiry and heartbeat.
// This is in addition to the background ticker — calling it from the loop
// ensures timely delivery even if the background ticker is slightly delayed.
func (t *AgentProgressTracker) Tick() {
	t.pusher.Tick()
}

// Buffer returns the underlying MilestoneBuffer for use by the message
// scheduler (semantic relevance computation) and interrupt layer.
func (t *AgentProgressTracker) Buffer() *MilestoneBuffer {
	return t.buffer
}

// Stop flushes any pending milestones and stops the background ticker.
// Safe to call multiple times.
func (t *AgentProgressTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}
	t.stopped = true
	close(t.tickerDone)
	t.pusher.Flush()
}

// RefineIntent updates the task intent label and re-evaluates complexity.
// Call this after the intent classifier has run (which may happen after
// the tracker is created). If the new intent indicates a heavier task,
// the pusher's complexity is upgraded accordingly.
func (t *AgentProgressTracker) RefineIntent(intentLabel string) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Update buffer's intent via its public API.
	t.buffer.SetTaskIntent(intentLabel)
	msgLen := t.buffer.TaskDescRuneLen()

	// Upgrade pusher complexity if needed via its public API.
	newComplexity := ClassifyComplexity(intentLabel, msgLen)
	t.pusher.UpgradeComplexity(newComplexity)
}
