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
	if !strings.Contains(last["content"], "[用户补充/纠正]") {
		t.Fatalf("guide content missing steer prefix: %q", last["content"])
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
