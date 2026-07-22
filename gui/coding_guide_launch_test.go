package main

import (
	"strings"
	"testing"
	"time"
)

func TestAppendPendingSteerInjections_GuideLaunchAsUserRole(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/remote-demo"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "coding system"},
		map[string]string{"role": "user", "content": "写一个硬件检查工具"},
	}
	out, injected := h.appendPendingSteerInjections(userID, conversation, 1)
	if injected != "使用 c++ 开发" {
		t.Fatalf("injected text = %q, want user-facing guide only", injected)
	}
	if len(out) != 3 {
		t.Fatalf("conversation len = %d, want 3 after guide append", len(out))
	}
	last, ok := out[2].(map[string]string)
	if !ok {
		t.Fatalf("last message type = %T, want map[string]string", out[2])
	}
	if last["role"] != "user" {
		t.Fatalf("guide role = %q, want user", last["role"])
	}
	if !strings.Contains(last["content"], "使用 c++ 开发") {
		t.Fatalf("guide content missing user text: %q", last["content"])
	}
	if !strings.Contains(last["content"], "The user spoke while you were working") {
		t.Fatalf("guide content missing conversational steer context: %q", last["content"])
	}
	if !strings.Contains(last["content"], "Do not emit a canned receipt") {
		t.Fatalf("guide content missing natural-response instruction: %q", last["content"])
	}
	// Pending must be consumed once.
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("pendingInjection should be drained after append")
	}
}

func TestAppendPendingSteerInjections_PreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/local-demo"
	h.accumulatePreLoopGuide(userID, "改用 TypeScript")

	out, injected := h.appendPendingSteerInjections(userID, nil, 0)
	if injected == "" || !strings.Contains(injected, "改用 TypeScript") {
		t.Fatalf("pre-loop guide not applied: %q", injected)
	}
	if len(out) != 1 {
		t.Fatalf("conversation len = %d, want 1", len(out))
	}
	msg := out[0].(map[string]string)
	if msg["role"] != "user" || !strings.Contains(msg["content"], "改用 TypeScript") {
		t.Fatalf("unexpected pre-loop message: %#v", msg)
	}
	if !strings.Contains(msg["content"], "Do not emit a canned receipt") {
		t.Fatalf("pre-loop guide should use conversational steer semantics: %#v", msg)
	}
}

func TestAppendPendingSteerInjections_DiscardsStalePreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/stale"
	h.pendingPreLoopGuide.Store(userID, &preLoopGuideEntry{
		Text:      "too old",
		CreatedAt: time.Now().Add(-preLoopGuideMaxAge - time.Second),
	})
	out, injected := h.appendPendingSteerInjections(userID, nil, 0)
	if injected != "" {
		t.Fatalf("stale pre-loop guide should be discarded, got %q", injected)
	}
	if len(out) != 0 {
		t.Fatalf("stale guide should not append messages, got %#v", out)
	}
}

func TestCodingSubAgentHooks_TransformConversationInjectsGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/hook-demo"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))

	hooks := &codingSubAgentHooks{handler: h, userID: userID}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "hardware tool"},
	}
	next := hooks.TransformConversation(conversation)
	if next == nil {
		t.Fatal("TransformConversation returned nil, want guide-injected conversation")
	}
	if len(next) != 2 {
		t.Fatalf("len = %d, want 2", len(next))
	}
	// Second call with empty pending should not force a change (compaction nil).
	if again := hooks.TransformConversation(next); again != nil {
		t.Fatalf("second transform should be no-op without pending inject, got %#v", again)
	}
}

func TestInjectGuideReference_ActivePureCodingUsesPendingInjection(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/remote-coding"
	ctx := NewLoopContext("chat", 10, nil)
	// Simulate pure coding registration (beginPureCodingRuntime).
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop hardware tool")
	defer cleanup()

	if !h.InjectGuideReference(userID, "使用 c++ 开发") {
		t.Fatal("active pure-coding session should accept guide reference")
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("active loop must not store guide as pre-loop")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("active loop should store guide in pendingInjection")
	}
	text, _ := raw.(string)
	if !isGuideLaunchReferenceInjection(text) {
		t.Fatalf("pending injection is not a guide launch: %q", text)
	}
	if !strings.Contains(stripInjectionPrefix(text), "使用 c++ 开发") {
		t.Fatalf("pending guide missing user text: %q", text)
	}
}

func TestInjectGuideReferenceRejectsReplacementTurn(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/replacement-turn"
	ctx := NewLoopContext("chat", 10, nil)
	ctx.Runtime.RequestID = "request-new"
	cleanup := h.beginPureCodingRuntime(ctx, userID, "replacement task")
	defer cleanup()

	if h.InjectGuideReferenceWithID(userID, "belongs to old turn", "buf-delayed", "request-old") {
		t.Fatal("delayed guide must not attach to a replacement turn")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("request-mismatched guide must remain outside the active loop")
	}
	if !h.InjectGuideReferenceWithID(userID, "belongs to current turn", "buf-current", "request-new") {
		t.Fatal("guide bound to the published request should be accepted")
	}
}

func TestInjectGuideReferenceRejectsAfterFinalResponseSealed(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/final-sealed"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	if !ctx.TrySealReplans(ctx.ReplanRevision()) {
		t.Fatal("failed to seal clean finalization boundary")
	}
	if h.InjectGuideReference(userID, "too late for this turn") {
		t.Fatal("sealed loop must reject guide so GUI retains it for next turn")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("rejected guide must not be stranded in pending injection")
	}
}

func TestLoopContextCancelAndGuideAcceptanceAreAtomic(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx := NewLoopContext("cancel-steer-race", 10, nil)
		start := make(chan struct{})
		accepted := make(chan bool, 1)
		go func() {
			<-start
			_, ok := ctx.TryRequestReplan(nil)
			accepted <- ok
		}()
		cancelled := make(chan struct{})
		go func() {
			<-start
			ctx.Cancel()
			close(cancelled)
		}()
		close(start)
		ok := <-accepted
		<-cancelled
		if !ok && ctx.ReplanRevision() != 0 {
			t.Fatalf("rejected steer advanced revision on iteration %d", i)
		}
		if ctx.AcceptingReplans() {
			t.Fatalf("cancelled loop still advertises steer acceptance on iteration %d", i)
		}
	}
}

func TestInjectSupplementaryRejectsAfterFinalResponseSealedWithoutQueueing(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/supplement-final-sealed"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	if !ctx.TrySealReplans(ctx.ReplanRevision()) {
		t.Fatal("failed to seal clean finalization boundary")
	}
	if h.InjectSupplementary(userID, "too late for this turn") {
		t.Fatal("sealed loop must reject supplementary text")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("rejected supplementary text must not be queued without a consumer")
	}
}

func TestInjectSupplementaryAtomicallyQueuesAndRequestsReplan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/supplement-replan"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	revision := ctx.ReplanRevision()
	if !h.InjectSupplementary(userID, "keep backwards compatibility") {
		t.Fatal("active loop should accept supplementary text")
	}
	if !ctx.ReplanRequestedSince(revision) {
		t.Fatal("accepted supplementary text did not request replan")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok || !strings.Contains(raw.(string), "keep backwards compatibility") {
		t.Fatalf("accepted supplementary text missing from queue: %#v", raw)
	}
}

func TestBeginPureCodingRuntime_NilSafe(t *testing.T) {
	h := &IMMessageHandler{}
	cleanup := h.beginPureCodingRuntime(nil, "u", "task")
	cleanup() // must not panic
	var nilHandler *IMMessageHandler
	cleanup = nilHandler.beginPureCodingRuntime(NewLoopContext("chat", 1, nil), "u", "task")
	cleanup()
}

func TestBeginPureCodingRuntime_PromotesPreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/promote-guide"
	// Simulate guide fired after previous turn ended (pre-loop bag).
	h.accumulatePreLoopGuide(userID, "使用 rust 实现")
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "continue coding")
	defer cleanup()

	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("pre-loop guide should be promoted away")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("expected pendingInjection after promote")
	}
	text, _ := raw.(string)
	if !isGuideLaunchReferenceInjection(text) {
		t.Fatalf("promoted injection is not guide format: %q", text)
	}
	if !strings.Contains(stripInjectionPrefix(text), "使用 rust 实现") {
		t.Fatalf("promoted guide missing user text: %q", text)
	}
}

func TestGuideRejectsUnpublishedPreLoopWindowAndAcceptsAfterPublication(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/start-window-handoff"
	state := h.getSessionLoop(userID)
	state.mu.Lock()
	if h.InjectGuideReference(userID, "arrived during preflight") {
		state.mu.Unlock()
		t.Fatal("unpublished pre-loop work must not claim a guide consumer")
	}
	state.mu.Unlock()
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("rejected pre-loop guide must remain solely in the durable GUI queue")
	}

	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginAgentLoopRuntime(ctx, userID, "task", "desktop")
	defer cleanup()
	if !h.InjectGuideReference(userID, "arrived after publication") {
		t.Fatal("published active loop should accept guide")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok || !strings.Contains(raw.(string), "arrived after publication") {
		t.Fatalf("accepted published guide did not reach active loop: %#v", raw)
	}
}

func TestBeginPureCodingRuntime_DropsVeryStalePreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/stale-promote"
	h.pendingPreLoopGuide.Store(userID, &preLoopGuideEntry{
		Text:      "ancient",
		CreatedAt: time.Now().Add(-3 * time.Minute),
	})
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "task")
	defer cleanup()
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("very stale pre-loop guide must not be promoted")
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("stale pre-loop guide should be deleted on promote attempt")
	}
}

func TestAppendPendingSteerInjections_ReturnsUserFacingGuideText(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/guide-text"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))
	_, injected := h.appendPendingSteerInjections(userID, nil, 0)
	if injected != "使用 c++ 开发" {
		t.Fatalf("injected text = %q, want user-facing guide only", injected)
	}
}

func TestCodingSubAgentHooks_IgnoresEmptyGuideWrapper(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/empty-guide"
	// Marker + instruction with no user text after strip yields no conversation growth.
	h.accumulateInjection(userID, guideLaunchReferenceMarker+"\n"+guideLaunchReferenceInstruction+"\n")
	hooks := &codingSubAgentHooks{handler: h, userID: userID}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "task"},
	}
	if next := hooks.TransformConversation(conversation); next != nil {
		t.Fatalf("empty guide wrapper should not rewrite conversation, got %#v", next)
	}
}

func TestAppendPendingSteerInjections_PreservesMixedInjectionRolesAndOrder(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/mixed-steer"
	h.accumulateInjection(userID, "[用户补充] keep the public API")
	h.accumulateInjection(userID, buildGuideLaunchInjection("switch storage to SQLite"))
	h.accumulateInjection(userID, "[用户补充] retain migration support")

	out, injected := h.appendPendingSteerInjections(userID, nil, 2)
	if len(out) != 3 {
		t.Fatalf("conversation len = %d, want 3 distinct injections: %#v", len(out), out)
	}
	wantRoles := []string{"system", "user", "system"}
	for i, want := range wantRoles {
		msg, ok := out[i].(map[string]string)
		if !ok || msg["role"] != want {
			t.Fatalf("message %d = %#v, want role %q", i, out[i], want)
		}
	}
	if !strings.Contains(out[1].(map[string]string)["content"], "switch storage to SQLite") {
		t.Fatalf("guide content missing: %#v", out[1])
	}
	if strings.Contains(out[1].(map[string]string)["content"], "retain migration support") {
		t.Fatalf("adjacent system supplement leaked into user-role guide: %#v", out[1])
	}
	if !strings.Contains(injected, "keep the public API") ||
		!strings.Contains(injected, "switch storage to SQLite") ||
		!strings.Contains(injected, "retain migration support") {
		t.Fatalf("combined injected text lost an item: %q", injected)
	}
}

func TestPendingGuideLaunchUserText_ExcludesAdjacentSupplementaryInjection(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/mixed-guide-entry"
	h.accumulateInjection(userID, buildGuideLaunchInjection("use Rust"))
	h.accumulateInjection(userID, "[用户补充] internal system supplement")
	if got := h.pendingGuideLaunchUserText(userID); got != "use Rust" {
		t.Fatalf("pending guide text = %q, want only guide payload", got)
	}
}

func TestClearNonGuidePendingInjection_PreservesEveryGuideBoundary(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/cleanup-mixed-guides"
	h.accumulateInjection(userID, "[用户补充] stale ordinary supplement")
	h.accumulateInjection(userID, buildGuideLaunchInjection("first guide"))
	h.accumulateInjection(userID, "[用户补充] another ordinary supplement")
	h.accumulateInjection(userID, buildGuideLaunchInjection("second guide"))

	h.clearNonGuidePendingInjection(userID)
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("cleanup removed queued guides")
	}
	text, _ := raw.(string)
	parts := splitPendingInjections(text)
	if len(parts) != 2 {
		t.Fatalf("guide count after cleanup = %d, want 2: %q", len(parts), text)
	}
	if got := stripInjectionPrefix(parts[0]); got != "first guide" {
		t.Fatalf("first guide = %q", got)
	}
	if got := stripInjectionPrefix(parts[1]); got != "second guide" {
		t.Fatalf("second guide = %q", got)
	}
	if strings.Contains(text, "ordinary supplement") {
		t.Fatalf("cleanup retained ordinary supplement: %q", text)
	}
}
