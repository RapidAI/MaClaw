package main

import (
	"strings"
	"testing"
	"time"
)

// Regression coverage for the uploaded-image flow:
// when the user asks about an already selected local screenshot/image,
// the backend must recognize the path block and avoid any new screenshot capture.

func TestHasSelectedLocalImagePath_UserSuppliedScreenshotPrompt(t *testing.T) {
	t.Parallel()

	msg := strings.Join([]string{
		"what is in this image?",
		"",
		filePathPromptPrefix,
		`C:\Users\ma139\Pictures\Screenshots\screen-2026-03-14.png`,
		"selected local image path; do not call screenshot again",
	}, "\n")

	if !hasSelectedLocalImagePath(msg) {
		t.Fatalf("expected selected local image path to be detected")
	}
}

func TestToolScreenshotUsesRuntimeOwnerTaskText(t *testing.T) {
	t.Parallel()

	desktopImagePrompt := strings.Join([]string{
		"look at this image",
		filePathPromptPrefix,
		`C:\Users\ma139\Pictures\screen.png`,
	}, "\n")
	h := &IMMessageHandler{lastUserText: desktopImagePrompt}
	remoteUserID := "weixin:user-1"
	remoteState := h.getSessionLoop(remoteUserID)
	remoteState.stateMu.Lock()
	remoteState.loopCtx = NewLoopContext("weixin", 3, nil)
	remoteState.userText = "please take a fresh screenshot"
	remoteState.stateMu.Unlock()

	got := h.toolScreenshot(map[string]interface{}{registeredToolPolicyOwnerIDField: remoteUserID})
	if strings.Contains(got, "screenshot") && strings.Contains(got, "不要调用") {
		t.Fatalf("runtime-owned screenshot should not inherit desktop image-path guard, got %q", got)
	}
}

func TestToolScreenshotEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	t.Parallel()

	h := &IMMessageHandler{lastUserText: "please take screenshot"}
	got := h.toolScreenshot(map[string]interface{}{registeredToolPolicyOwnerIDField: ""})
	if !strings.Contains(got, "runtime owner is missing") {
		t.Fatalf("empty runtime owner should fail closed, got %q", got)
	}
}

func TestToolScreenshotOwnerlessCurrentRuntimeDoesNotUseLegacyTaskText(t *testing.T) {
	desktopImagePrompt := strings.Join([]string{
		"look at this image",
		filePathPromptPrefix,
		`C:\Users\ma139\Pictures\screen.png`,
	}, "\n")
	h := &IMMessageHandler{
		lastUserText:   desktopImagePrompt,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-owner"}},
	}

	got := h.toolScreenshot(map[string]interface{}{})
	if strings.Contains(got, "不要调用 screenshot") || strings.Contains(got, "涓嶈璋冪敤 screenshot") {
		t.Fatalf("ownerless current runtime should not inherit legacy image-path guard, got %q", got)
	}
}

func TestScreenshotCooldownIsScopedByRuntimeOwner(t *testing.T) {
	now := time.Now()
	h := &IMMessageHandler{lastScreenshotAt: now}

	ownerKey, scoped := h.screenshotCooldownScope("weixin:user-1", true)
	if !scoped {
		t.Fatal("runtime owner screenshot should use scoped cooldown")
	}
	if got := screenshotCooldownMessage(h.lastScreenshotAtForScope(ownerKey, scoped), now.Add(time.Second)); got != "" {
		t.Fatalf("owner scoped cooldown inherited legacy screenshot time: %q", got)
	}
	h.recordScreenshotAtForScope(ownerKey, scoped, now)
	if got := screenshotCooldownMessage(h.lastScreenshotAtForScope(ownerKey, scoped), now.Add(time.Second)); got == "" {
		t.Fatal("owner scoped cooldown was not recorded")
	}
	if !h.lastScreenshotAt.Equal(now) {
		t.Fatalf("scoped cooldown should not mutate legacy screenshot time, got %v", h.lastScreenshotAt)
	}
}

func TestScreenshotCooldownOwnerlessDoesNotUseCurrentRuntimeScope(t *testing.T) {
	now := time.Now()
	h := &IMMessageHandler{
		lastScreenshotAt: now,
		currentLoopCtx:   &LoopContext{Runtime: RuntimeContext{RequestID: "req-ownerless-screenshot"}},
	}

	key, scoped := h.screenshotCooldownScope("", false)
	if scoped || key != "" {
		t.Fatalf("ownerless screenshot cooldown should not inherit current runtime, key=%q scoped=%v", key, scoped)
	}
	if got := screenshotCooldownMessage(h.lastScreenshotAtForScope(key, scoped), now.Add(time.Second)); got == "" {
		t.Fatal("ownerless screenshot cooldown should use legacy unscoped cooldown only")
	}
}

func TestToolScreenshotRejectsUserSuppliedImagePathPrompt(t *testing.T) {
	t.Parallel()

	ownerID := "weixin:user-image"
	h := &IMMessageHandler{}
	ctx := NewLoopContext("weixin", 1, nil)
	h.setSessionLoopCtx(ownerID, ctx)
	state := h.getSessionLoop(ownerID)
	state.stateMu.Lock()
	state.userText = strings.Join([]string{
		"what is in this image?",
		"",
		filePathPromptPrefix,
		`C:\Users\ma139\Pictures\Screenshots\screen-2026-03-14.png`,
		"selected local image path; do not call screenshot again",
	}, "\n")
	state.stateMu.Unlock()

	got := h.toolScreenshot(map[string]interface{}{registeredToolPolicyOwnerIDField: ownerID})
	if !strings.Contains(got, "不要调用 screenshot") {
		t.Fatalf("expected screenshot guard message, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "cooldown") {
		t.Fatalf("expected image-path guard to trigger before cooldown, got %q", got)
	}
}
