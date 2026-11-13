package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestIMMessageSerializationCancelsWhileWaitingForOwner(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "lansenger:profile:group:user"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: "lansenger_local", Text: "first"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: "lansenger_local", Text: "second", CancelCtx: ctx}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	cancel()
	select {
	case result := <-resultC:
		if !result.Handled || result.Response == nil || result.Response.Error != context.Canceled.Error() {
			t.Fatalf("canceled wait result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled owner wait did not return promptly")
	}
}

func TestIMMessageSerializationAllowsDifferentOwnersConcurrently(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: "desktop-user", Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()

	acquired := make(chan imMessageSerializationResult, 1)
	go func() {
		acquired <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: "desktop-user:C:/tasks/a", Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	select {
	case second := <-acquired:
		second.Unlock()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("different owner waited on another owner's serialization mutex")
	}
}

func TestIMMessageSerializationSerializesSameOwner(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/same"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})

	acquired := make(chan imMessageSerializationResult, 1)
	go func() {
		acquired <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	select {
	case second := <-acquired:
		second.Unlock()
		first.Unlock()
		t.Fatal("same owner acquired serialization mutex while first loop still held it")
	case <-time.After(100 * time.Millisecond):
	}

	first.Unlock()
	select {
	case second := <-acquired:
		second.Unlock()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("same owner did not acquire serialization mutex after first loop released it")
	}
}

// A turn claims the session lock for its whole window — minutes, not seconds.
// A queued message must keep waiting for it: the lock wait is bounded by
// owner liveness, so shrinking the orphan grace must NOT produce the
// recovery error while the holder is a live turn.
func TestIMMessageSerializationQueuedBehindClaimedTurnDoesNotFail(t *testing.T) {
	prev := ownerTurnLockTimeout
	ownerTurnLockTimeout = 150 * time.Millisecond
	defer func() { ownerTurnLockTimeout = prev }()

	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/queued"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()

	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	// Well past the orphan grace, a claimed lock is still a live turn: the
	// queued message must be waiting, not failed with a recovery error.
	select {
	case res := <-resultC:
		res.Unlock()
		t.Fatalf("queued message returned while owner turn held the lock: %#v", res.Response)
	case <-time.After(500 * time.Millisecond):
	}

	first.Unlock()
	select {
	case res := <-resultC:
		res.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("queued message did not acquire after the owner window closed")
	}
}

// A claimed wait is bounded too: past ownerTurnLiveWaitCeiling the holder is
// indistinguishable from wedged (or self-locked), and queueing forever is
// worse than the recoverable error the lock wait used to surface.
func TestIMMessageSerializationFailsPastLiveWaitCeiling(t *testing.T) {
	prevOrphan, prevCeiling := ownerTurnLockTimeout, ownerTurnLiveWaitCeiling
	ownerTurnLockTimeout = 10 * time.Second
	ownerTurnLiveWaitCeiling = 150 * time.Millisecond
	defer func() { ownerTurnLockTimeout, ownerTurnLiveWaitCeiling = prevOrphan, prevCeiling }()

	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/wedged"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()

	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	select {
	case res := <-resultC:
		if !res.Handled || res.Response == nil || !strings.Contains(res.Response.Text, "系统正在恢复中") {
			t.Fatalf("ceiling result = %#v", res.Response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claimed wait past the live ceiling did not surface the recovery response")
	}
}

// The recovery response exists for the lock nobody claims — a held state.mu
// with no window, loop, or in-flight turn behind it. That is the only case
// that may error out instead of queueing.
func TestIMMessageSerializationFailsOnOrphanedHeldLock(t *testing.T) {
	prev := ownerTurnLockTimeout
	ownerTurnLockTimeout = 120 * time.Millisecond
	defer func() { ownerTurnLockTimeout = prev }()

	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/leaked"
	state := h.getSessionLoop(owner)
	state.mu.Lock()
	defer state.mu.Unlock()

	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	select {
	case res := <-resultC:
		if !res.Handled || res.Response == nil || !strings.Contains(res.Response.Text, "系统正在恢复中") {
			t.Fatalf("orphan lock result = %#v", res.Response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("orphan lock wait did not surface the recovery response")
	}
}

// A turn admitted before its LoopContext was published is invisible to the
// entry interrupt check. Once the loop publishes mid-wait, the queued
// message gets the same steer shot it would have had at entry — here /stop
// resolves immediately instead of queueing behind the whole turn.
func TestIMMessageSerializationQueuedMessageSteersPublishedLoop(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory(), interruptHandler: newIMInterruptHandler(nil)}
	h.interruptHandler.handler = h
	owner := "desktop-user:C:/tasks/steer"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()

	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "/stop"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	// Let the waiter settle into the queue before the owner publishes.
	time.Sleep(300 * time.Millisecond)
	loop := NewLoopContext("long task", 3, nil)
	h.setSessionLoopCtx(owner, loop)
	t.Cleanup(func() { loop.Cancel(); loop.Done() })

	select {
	case res := <-resultC:
		if !res.Handled || res.Response == nil || strings.TrimSpace(res.Response.Text) == "" {
			t.Fatalf("queued /stop result = %#v", res.Response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued /stop did not steer the published loop")
	}
	if !loop.IsCancelled() {
		t.Fatal("queued /stop did not cancel the published loop")
	}
}

// A done-but-still-published loopCtx is a stale publication, not a claimant.
// If the session lock is also leaked, the waiter must hit the orphan
// recovery path — the dead pointer cannot stand in for a live owner.
func TestIMMessageSerializationFailsOnOrphanedLockWithStaleLoopCtx(t *testing.T) {
	prev := ownerTurnLockTimeout
	ownerTurnLockTimeout = 120 * time.Millisecond
	defer func() { ownerTurnLockTimeout = prev }()

	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/stale-ctx"
	state := h.getSessionLoop(owner)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Publish a loop whose DoneC is already closed — the shape a leaked
	// unlock leaves behind when cleanup never cleared the pointer.
	loop := NewLoopContext("finished task", 3, nil)
	loop.Done()
	h.setSessionLoopCtx(owner, loop)
	t.Cleanup(func() { h.setSessionLoopCtx(owner, nil) })

	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	select {
	case res := <-resultC:
		if !res.Handled || res.Response == nil || !strings.Contains(res.Response.Text, "系统正在恢复中") {
			t.Fatalf("stale-ctx orphan lock result = %#v", res.Response)
		}
		if res.ActiveLoopOwnsNextTurn {
			t.Fatal("a dead loop must not own the next turn; the parked continuation must be restored")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale loopCtx kept the leaked lock claimed; wait never recovered")
	}
}

// ownerTurnSteerEligible is the gate for the single queued-scheduler retry.
// Background messages, empty text, cancelled boundaries, and every
// host-authored platform are rejected before the scheduler can read them.
func TestOwnerTurnSteerEligibleGuards(t *testing.T) {
	newHandler := func() *IMMessageHandler {
		h := &IMMessageHandler{memory: agent.NewConversationMemory(), interruptHandler: newIMInterruptHandler(nil)}
		h.interruptHandler.handler = h
		return h
	}
	owner := "desktop-user:C:/tasks/eligible"

	cases := []struct {
		name string
		msg  IMUserMessage
		want bool
	}{
		{"foreground user message", IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "继续"}, true},
		{"background message", IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "sync", IsBackground: true}, false},
		{"empty text", IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: ""}, false},
		{"goal continuation", IMUserMessage{UserID: owner, Platform: "goal-continuation", Text: "继续目标"}, false},
		{"unattended workflow", IMUserMessage{UserID: owner, Platform: unattendedWorkflowPlatform, Text: "继续"}, false},
		{"session round continuation", IMUserMessage{UserID: owner, Platform: sessionRoundContinuationPlatform, Text: "继续"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newHandler().ownerTurnSteerEligible(&tc.msg); got != tc.want {
				t.Fatalf("ownerTurnSteerEligible(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}

	// A nil interrupt handler makes every message ineligible.
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	if h.ownerTurnSteerEligible(&IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "继续"}) {
		t.Fatal("nil interruptHandler must make steering ineligible")
	}
}

// Host-authored continuation turns carry "不要…" engine instructions, not
// user speech. They must never be steered by the queued retry — they wait
// for the window like every serialized turn.
func TestIMMessageSerializationContinuationNeverSteers(t *testing.T) {
	for _, platform := range []string{"goal-continuation", unattendedWorkflowPlatform, sessionRoundContinuationPlatform} {
		t.Run(platform, func(t *testing.T) {
			h := &IMMessageHandler{memory: agent.NewConversationMemory(), interruptHandler: newIMInterruptHandler(nil)}
			h.interruptHandler.handler = h
			owner := "desktop-user:C:/tasks/cont-" + platform
			first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
			defer first.Unlock()

			resultC := make(chan imMessageSerializationResult, 1)
			go func() {
				resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: platform, Text: "停止当前任务"}, nil, nil, nil, explicitTaskSlotDecision{})
			}()

			time.Sleep(200 * time.Millisecond)
			loop := NewLoopContext("long task", 3, nil)
			h.setSessionLoopCtx(owner, loop)
			t.Cleanup(func() { loop.Cancel(); loop.Done() })

			// The continuation must still be queued — it must not have been
			// read as a user cancel/merge even though the loop is live.
			select {
			case res := <-resultC:
				res.Unlock()
				t.Fatalf("continuation message returned early: %#v", res.Response)
			case <-time.After(400 * time.Millisecond):
			}
			if loop.IsCancelled() {
				t.Fatalf("%s turn cancelled the owner loop", platform)
			}
			first.Unlock()
			select {
			case res := <-resultC:
				res.Unlock()
			case <-time.After(2 * time.Second):
				t.Fatal("continuation did not acquire after the owner window closed")
			}
		})
	}
}
