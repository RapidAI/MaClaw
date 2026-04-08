package main

import (
	"strings"
	"testing"
)

// Regression coverage for the uploaded-image flow:
// when the user asks about an already selected local screenshot/image,
// the backend must recognize the path block and avoid any new screenshot capture.

func TestHasSelectedLocalImagePath_UserSuppliedScreenshotPrompt(t *testing.T) {
	t.Parallel()

	msg := strings.Join([]string{
		"图上有什么？",
		"",
		"[用户选择的本地文件路径]",
		`C:\Users\ma139\Pictures\Screenshots\屏幕截图 2026-03-14 073217.png`,
		"这是用户已经提供的本地图片文件。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容后回答。",
	}, "\n")

	if !hasSelectedLocalImagePath(msg) {
		t.Fatalf("expected selected local image path to be detected")
	}
}

func TestToolScreenshotRejectsUserSuppliedImagePathPrompt(t *testing.T) {
	t.Parallel()

	h := &IMMessageHandler{
		lastUserText: strings.Join([]string{
			"图上有什么？",
			"",
			"[用户选择的本地文件路径]",
			`C:\Users\ma139\Pictures\Screenshots\屏幕截图 2026-03-14 073217.png`,
			"这是用户已经提供的本地图片文件。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容后回答。",
		}, "\n"),
	}

	got := h.toolScreenshot(nil)
	if !strings.Contains(got, "不要调用 screenshot") {
		t.Fatalf("expected screenshot guard message, got %q", got)
	}
	if strings.Contains(got, "冷却") {
		t.Fatalf("expected image-path guard to trigger before cooldown, got %q", got)
	}
}
