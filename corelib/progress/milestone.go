// Package progress provides event-driven progress tracking for agent loop
// execution. It replaces the previous timer-based "nudge" approach with a
// milestone-based system where progress messages are only sent when there
// is new, substantive information to report.
//
// Core components:
//   - MilestoneBuffer: per-task ring buffer that records tool-call milestones
//   - Extractor: declarative rules that turn (toolName, args, result) into
//     human-readable milestone summaries
//   - Pusher: event-driven progress delivery with merge windows and heartbeat
//
// The MilestoneBuffer also serves as the shared data source for the message
// scheduler (semantic relevance computation) and the interrupt layer (partial
// output collection on cancel).
package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Milestone represents one observable step completed during an agent loop.
type Milestone struct {
	Time      time.Time // when the milestone was recorded
	Tool      string    // tool name (e.g. "web_search", "write_file")
	Summary   string    // human-readable summary extracted from tool call
	Phase     string    // coarse phase tag: "searching"/"writing"/"executing"/...
	Completed bool      // true when the step finished (vs. started)
}

// MilestoneBuffer is a per-task, thread-safe ring buffer of milestones.
// It also caches the current task description and its embedding vector so
// that the message scheduler can compute semantic relevance without
// re-embedding on every incoming message.
type MilestoneBuffer struct {
	mu sync.RWMutex

	milestones []Milestone
	maxSize    int // ring buffer capacity; oldest entries evicted when full

	// Task context — set once via Reset() at the start of each agent loop.
	taskDesc   string    // user's original message / task description
	taskIntent string    // intent label from UIC (e.g. "coding", "ssh")
	taskEmbed  []float32 // embedding vector of taskDesc (may be nil)
	startTime  time.Time
}

// NewMilestoneBuffer creates a buffer with the given capacity.
func NewMilestoneBuffer(capacity int) *MilestoneBuffer {
	if capacity <= 0 {
		capacity = 64
	}
	return &MilestoneBuffer{
		milestones: make([]Milestone, 0, capacity),
		maxSize:    capacity,
	}
}

// Reset clears the buffer and sets the task context for a new agent loop.
func (b *MilestoneBuffer) Reset(taskDesc, taskIntent string, taskEmbed []float32) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.milestones = b.milestones[:0]
	b.taskDesc = taskDesc
	b.taskIntent = taskIntent
	b.taskEmbed = taskEmbed
	b.startTime = time.Now()
}

// Record appends a milestone. If the buffer is full, the oldest entry is evicted.
func (b *MilestoneBuffer) Record(m Milestone) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.milestones) >= b.maxSize {
		// Shift left by 1 — simple and cache-friendly for small buffers.
		copy(b.milestones, b.milestones[1:])
		b.milestones = b.milestones[:b.maxSize-1]
	}
	b.milestones = append(b.milestones, m)
}

// Since returns all milestones recorded after time t.
func (b *MilestoneBuffer) Since(t time.Time) []Milestone {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i, m := range b.milestones {
		if m.Time.After(t) {
			out := make([]Milestone, len(b.milestones)-i)
			copy(out, b.milestones[i:])
			return out
		}
	}
	return nil
}

// Latest returns the most recent milestone, or nil if the buffer is empty.
func (b *MilestoneBuffer) Latest() *Milestone {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.milestones) == 0 {
		return nil
	}
	m := b.milestones[len(b.milestones)-1]
	return &m
}

// Len returns the number of milestones in the buffer.
func (b *MilestoneBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.milestones)
}

// CompletedCount returns the number of completed (non-silent) milestones.
func (b *MilestoneBuffer) CompletedCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	n := 0
	for _, m := range b.milestones {
		if m.Completed {
			n++
		}
	}
	return n
}

// All returns a copy of all milestones.
func (b *MilestoneBuffer) All() []Milestone {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]Milestone, len(b.milestones))
	copy(out, b.milestones)
	return out
}

// TaskDesc returns the current task description.
func (b *MilestoneBuffer) TaskDesc() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.taskDesc
}

// TaskIntent returns the current task intent label.
func (b *MilestoneBuffer) TaskIntent() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.taskIntent
}

// TaskEmbed returns the current task embedding vector (may be nil).
func (b *MilestoneBuffer) TaskEmbed() []float32 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.taskEmbed == nil {
		return nil
	}
	out := make([]float32, len(b.taskEmbed))
	copy(out, b.taskEmbed)
	return out
}

// SetTaskIntent updates the task intent label. Thread-safe.
func (b *MilestoneBuffer) SetTaskIntent(intent string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.taskIntent = intent
}

// TaskDescRuneLen returns the rune length of the task description. Thread-safe.
func (b *MilestoneBuffer) TaskDescRuneLen() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len([]rune(b.taskDesc))
}

// Elapsed returns the duration since the task started.
func (b *MilestoneBuffer) Elapsed() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return time.Since(b.startTime)
}

// ProgressSummary returns a human-readable summary of the current progress.
// Used by StatusQuery and heartbeat messages.
func (b *MilestoneBuffer) ProgressSummary() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.milestones) == 0 {
		elapsed := time.Since(b.startTime).Truncate(time.Second)
		return fmt.Sprintf("正在处理中（已耗时 %s）...", formatDuration(elapsed))
	}

	completed := 0
	var lastSummary string
	for _, m := range b.milestones {
		if m.Completed {
			completed++
			lastSummary = m.Summary
		}
	}

	latest := b.milestones[len(b.milestones)-1]
	elapsed := time.Since(b.startTime).Truncate(time.Second)

	if !latest.Completed {
		// Currently executing a step.
		return fmt.Sprintf("已完成 %d 个步骤，当前: %s（已耗时 %s）",
			completed, latest.Summary, formatDuration(elapsed))
	}

	return fmt.Sprintf("已完成 %d 个步骤，最近: %s（已耗时 %s）",
		completed, lastSummary, formatDuration(elapsed))
}

// CompletedOutputSummary returns a summary of completed milestones for use
// when a task is interrupted (Replace/Insert). Lists the completed steps
// so the user knows what was accomplished before interruption.
func (b *MilestoneBuffer) CompletedOutputSummary() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var parts []string
	for _, m := range b.milestones {
		if m.Completed && m.Summary != "" {
			parts = append(parts, m.Summary)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) <= 3 {
		return strings.Join(parts, ", ")
	}
	return fmt.Sprintf("%s 等 %d 个步骤", strings.Join(parts[:3], ", "), len(parts))
}

// formatDuration formats a duration in a human-friendly way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if secs == 0 {
		return fmt.Sprintf("%d 分钟", mins)
	}
	return fmt.Sprintf("%d 分 %d 秒", mins, secs)
}
