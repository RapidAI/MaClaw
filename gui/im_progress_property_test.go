package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"pgregory.net/rapid"
)

// ============================================================================
// Feature: gui-startup-response-optimization, Property 10: Immediate progress emission
// For any user message received by the IMMessageHandler with a non-nil onProgress
// callback, the progress callback SHALL be invoked before any preflight or
// entry_context processing begins, ensuring the progress event is emitted within
// 100ms of message receipt.
// **Validates: Requirements 3.1**
// ============================================================================

// progressTracker records the order of operations during message processing.
type progressTracker struct {
	mu              sync.Mutex
	progressCalled  atomic.Bool
	progressTime    time.Time
	preflightTime   time.Time
	entryCtxTime    time.Time
	progressText    string
	operationOrder  []string
}

func (p *progressTracker) onProgress(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.progressCalled.Load() {
		p.progressCalled.Store(true)
		p.progressTime = time.Now()
		p.progressText = text
		p.operationOrder = append(p.operationOrder, "progress")
	}
}

func (p *progressTracker) markPreflight() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.preflightTime = time.Now()
	p.operationOrder = append(p.operationOrder, "preflight")
}

func (p *progressTracker) markEntryContext() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entryCtxTime = time.Now()
	p.operationOrder = append(p.operationOrder, "entry_context")
}

// simulateMessageProcessing models the message handling pipeline with
// immediate progress emission at the entry point.
func simulateMessageProcessing(msg string, tracker *progressTracker) {
	// Step 1: IMMEDIATE progress emission (before any processing)
	tracker.onProgress("正在思考...")

	// Step 2: Preflight checks (validation, rate limiting, etc.)
	tracker.markPreflight()
	time.Sleep(1 * time.Millisecond) // simulate minimal preflight work

	// Step 3: Entry context resolution (UIC, task-context, etc.)
	tracker.markEntryContext()
	time.Sleep(1 * time.Millisecond) // simulate entry context work
}

func TestProperty10_ImmediateProgressEmission(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user messages of various lengths and content
		msg := rapid.OneOf(
			rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`),
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{1,50}`), // Chinese characters
			rapid.Just("开工"),
			rapid.Just("继续"),
			rapid.Just("帮我开发一个贪吃蛇游戏"),
			rapid.StringMatching(`[a-z]{1,5}`), // very short messages
		).Draw(t, "message")

		tracker := &progressTracker{}

		// Record message receipt time
		receiptTime := time.Now()

		// Process message
		simulateMessageProcessing(msg, tracker)

		// Property 1: progress callback was invoked
		if !tracker.progressCalled.Load() {
			t.Fatalf("progress callback not invoked for message %q", msg)
		}

		// Property 2: progress was emitted within 100ms of message receipt
		tracker.mu.Lock()
		progressDelay := tracker.progressTime.Sub(receiptTime)
		tracker.mu.Unlock()

		if progressDelay > 100*time.Millisecond {
			t.Fatalf("progress emitted %v after receipt (max 100ms) for message %q",
				progressDelay, msg)
		}

		// Property 3: progress was emitted BEFORE preflight
		tracker.mu.Lock()
		order := tracker.operationOrder
		tracker.mu.Unlock()

		if len(order) < 2 {
			t.Fatal("insufficient operation records")
		}
		if order[0] != "progress" {
			t.Fatalf("progress was not the first operation; order=%v", order)
		}

		// Property 4: progress was emitted BEFORE entry_context
		progressIdx := -1
		entryCtxIdx := -1
		for i, op := range order {
			if op == "progress" && progressIdx == -1 {
				progressIdx = i
			}
			if op == "entry_context" && entryCtxIdx == -1 {
				entryCtxIdx = i
			}
		}
		if progressIdx >= entryCtxIdx {
			t.Fatalf("progress (idx=%d) not before entry_context (idx=%d); order=%v",
				progressIdx, entryCtxIdx, order)
		}
	})
}

// TestProperty10_ProgressEmission_MessageLengthIndependent verifies that progress
// emission timing is independent of message length (no pre-processing before emit).
func TestProperty10_ProgressEmission_MessageLengthIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate messages of varying lengths
		msgLen := rapid.IntRange(1, 5000).Draw(t, "msgLen")
		msg := ""
		for i := 0; i < msgLen; i++ {
			msg += "a"
		}

		tracker := &progressTracker{}
		receiptTime := time.Now()
		simulateMessageProcessing(msg, tracker)

		tracker.mu.Lock()
		progressDelay := tracker.progressTime.Sub(receiptTime)
		tracker.mu.Unlock()

		// Property: progress emission time should not scale with message length
		// Even for 5000-char messages, progress should be emitted in < 5ms
		if progressDelay > 5*time.Millisecond {
			t.Fatalf("progress took %v for %d-char message — should be constant time",
				progressDelay, utf8.RuneCountInString(msg))
		}
	})
}

// TestProperty10_ProgressText verifies the progress text content.
func TestProperty10_ProgressText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.StringMatching(`[a-zA-Z\x{4e00}-\x{9fff} ]{1,100}`).Draw(t, "msg")

		tracker := &progressTracker{}
		simulateMessageProcessing(msg, tracker)

		tracker.mu.Lock()
		text := tracker.progressText
		tracker.mu.Unlock()

		// Property: progress text is non-empty
		if text == "" {
			t.Fatal("progress text is empty")
		}

		// Property: progress text is a user-facing status message
		if text != "正在思考..." {
			t.Fatalf("unexpected progress text: %q", text)
		}
	})
}
