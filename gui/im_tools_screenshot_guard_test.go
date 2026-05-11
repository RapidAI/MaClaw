package main

import (
	"strings"
	"testing"
	"time"
)

func TestScreenshotLocalImagePathGuardMessage(t *testing.T) {
	msg := strings.Join([]string{
		"看这张图",
		filePathPromptPrefix,
		`C:\Users\me\Pictures\screen.png`,
	}, "\n")
	got := screenshotLocalImagePathGuardMessage(msg)
	if !strings.Contains(got, "不要调用 screenshot") {
		t.Fatalf("guard message = %q", got)
	}
	if got := screenshotLocalImagePathGuardMessage("no image path"); got != "" {
		t.Fatalf("guard message = %q, want empty", got)
	}
}

func TestScreenshotCooldownMessage(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	if got := screenshotCooldownMessage(time.Time{}, now); got != "" {
		t.Fatalf("zero cooldown message = %q, want empty", got)
	}
	if got := screenshotCooldownMessage(now.Add(-screenshotCooldown), now); got != "" {
		t.Fatalf("expired cooldown message = %q, want empty", got)
	}
	got := screenshotCooldownMessage(now.Add(-10*time.Second), now)
	if !strings.Contains(got, "21 秒") {
		t.Fatalf("cooldown message = %q, want 21 seconds", got)
	}
}
