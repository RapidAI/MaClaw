package progress

import (
	"sync"
	"time"
)

// TaskComplexity classifies the expected duration of a task.
type TaskComplexity int

const (
	// ComplexityLight is for tasks expected to complete in <30s (e.g. weather query).
	ComplexityLight TaskComplexity = iota
	// ComplexityMedium is for tasks expected to take 30s-3min (e.g. research).
	ComplexityMedium
	// ComplexityHeavy is for tasks expected to take >3min (e.g. coding workflow).
	ComplexityHeavy
)

// Pusher configuration constants.
const (
	// lightUpgradeTimeout is how long a light task can run before being
	// upgraded to medium (triggers first acknowledgment + milestone tracking).
	lightUpgradeTimeout = 30 * time.Second

	// mergeWindow is the minimum interval between milestone pushes.
	// Milestones within this window are merged into a single message.
	mergeWindow = 30 * time.Second

	// heartbeatInitialDelay is the silence duration before the first heartbeat.
	heartbeatInitialDelay = 90 * time.Second

	// heartbeatInterval is the interval between subsequent heartbeats.
	heartbeatInterval = 120 * time.Second

	// maxHeartbeats is the maximum number of heartbeat messages before going silent.
	maxHeartbeats = 3
)

// PushFunc is the callback used to deliver a progress message to the user.
type PushFunc func(text string)

// ProgressPusher implements the three-layer event-driven progress strategy:
//
//  1. Instant acknowledgment (once, for non-light tasks)
//  2. Milestone pushes (event-driven, with merge window)
//  3. Heartbeat fallback (when no milestones for 90s+)
type ProgressPusher struct {
	mu sync.Mutex

	buffer     *MilestoneBuffer
	pushFn     PushFunc
	complexity TaskComplexity

	// State tracking.
	ackSent        bool      // whether the initial acknowledgment was sent
	lastPushTime   time.Time // last time a progress message was pushed
	pendingMerge   []Milestone // milestones waiting in the merge window
	heartbeatCount int       // number of heartbeats sent
	silenced       bool      // true after maxHeartbeats — no more messages

	// For light→medium upgrade detection.
	upgraded bool
}

// NewProgressPusher creates a pusher for the given task complexity.
func NewProgressPusher(buffer *MilestoneBuffer, pushFn PushFunc, complexity TaskComplexity) *ProgressPusher {
	return &ProgressPusher{
		buffer:     buffer,
		pushFn:     pushFn,
		complexity: complexity,
	}
}

// OnMilestone is called each time a new milestone is recorded in the buffer.
// It decides whether to push a progress message based on the three-layer strategy.
func (p *ProgressPusher) OnMilestone(m Milestone) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.silenced {
		return
	}

	// Light tasks: don't push milestones until upgraded to medium.
	if p.complexity == ComplexityLight && !p.upgraded {
		return
	}

	// Layer 1: instant acknowledgment (once).
	if !p.ackSent && p.complexity != ComplexityLight {
		p.ackSent = true
		p.pushFn("收到，正在处理 🔄")
		p.lastPushTime = time.Now()
	}

	// Reset heartbeat counter — we have activity.
	p.heartbeatCount = 0

	// Layer 2: milestone push with merge window.
	p.pendingMerge = append(p.pendingMerge, m)

	if time.Since(p.lastPushTime) >= mergeWindow {
		p.flushMergeWindow()
	}
	// Otherwise, milestones accumulate until the merge window expires
	// (checked by Tick) or the task completes (checked by Flush).
}

// Tick should be called periodically (e.g. every 10s) by the agent loop.
// It handles merge window expiry and heartbeat fallback.
func (p *ProgressPusher) Tick() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.silenced {
		return
	}

	// Light task upgrade: if still running after lightUpgradeTimeout,
	// upgrade to medium and send the first acknowledgment.
	if p.complexity == ComplexityLight && !p.upgraded {
		if p.buffer.Elapsed() >= lightUpgradeTimeout {
			p.upgraded = true
			p.complexity = ComplexityMedium
			if !p.ackSent {
				p.ackSent = true
				p.pushFn("收到，正在处理 🔄")
				p.lastPushTime = time.Now()
			}
		}
		return // Light tasks don't push milestones until upgraded.
	}

	// Flush pending milestones if merge window expired.
	if len(p.pendingMerge) > 0 && time.Since(p.lastPushTime) >= mergeWindow {
		p.flushMergeWindow()
		return
	}

	// Layer 3: heartbeat fallback when no milestones for a while.
	sinceLastPush := time.Since(p.lastPushTime)
	if p.lastPushTime.IsZero() {
		// Never pushed anything — use task start time as reference.
		sinceLastPush = p.buffer.Elapsed()
	}
	if p.heartbeatCount == 0 && sinceLastPush >= heartbeatInitialDelay {
		p.sendHeartbeat()
	} else if p.heartbeatCount > 0 && sinceLastPush >= heartbeatInterval {
		p.sendHeartbeat()
	}
}

// Flush forces delivery of any pending milestones. Called when the task completes.
func (p *ProgressPusher) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pendingMerge) > 0 {
		p.flushMergeWindow()
	}
}

// flushMergeWindow merges pending milestones and pushes a single message.
// Caller must hold p.mu.
func (p *ProgressPusher) flushMergeWindow() {
	if len(p.pendingMerge) == 0 {
		return
	}

	msg := MergeMilestones(p.pendingMerge)
	p.pendingMerge = p.pendingMerge[:0]

	if msg != "" {
		p.pushFn(msg)
		p.lastPushTime = time.Now()
	}
}

// sendHeartbeat sends a heartbeat message when there's been no milestone activity.
// Caller must hold p.mu.
func (p *ProgressPusher) sendHeartbeat() {
	p.heartbeatCount++

	if p.heartbeatCount > maxHeartbeats {
		p.pushFn("任务耗时较长，完成后会立即通知你。")
		p.silenced = true
		return
	}

	summary := p.buffer.ProgressSummary()
	p.pushFn("仍在执行中，" + summary)
	p.lastPushTime = time.Now()
}

// UpgradeComplexity increases the pusher's complexity if the new level is higher.
// Thread-safe. Complexity can only go up, never down.
func (p *ProgressPusher) UpgradeComplexity(newComplexity TaskComplexity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if newComplexity > p.complexity {
		p.complexity = newComplexity
	}
}

// ClassifyComplexity determines task complexity from intent and message length.
// This is a heuristic — the pusher supports dynamic upgrade for misclassification.
func ClassifyComplexity(intentLabel string, msgRuneCount int) TaskComplexity {
	// Heavy tasks: coding, SSH, workflow, skill execution.
	switch intentLabel {
	case "coding", "ssh", "workflow", "bug_fix":
		return ComplexityHeavy
	}

	// Medium tasks: longer messages or content processing.
	if msgRuneCount > 80 {
		return ComplexityMedium
	}

	// Default: light.
	return ComplexityLight
}
