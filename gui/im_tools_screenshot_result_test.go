package main

import (
	"strings"
	"testing"
)

func TestFormatScreenshotBase64ResultAddsPrefix(t *testing.T) {
	got := formatScreenshotBase64Result("abc123")
	if got != "[screenshot_base64]abc123" {
		t.Fatalf("formatScreenshotBase64Result() = %q", got)
	}
}

func TestDownsizeScreenshotForIMKeepsSmallPayload(t *testing.T) {
	got := downsizeScreenshotForIM("small")
	if got != "small" {
		t.Fatalf("downsizeScreenshotForIM() = %q, want small", got)
	}
}

func TestDownsizeScreenshotForIMFallsBackWhenDownsizeFails(t *testing.T) {
	payload := strings.Repeat("not-image", screenshotIMInlineSizeLimit/len("not-image")+1)
	got := downsizeScreenshotForIM(payload)
	if got != payload {
		t.Fatal("expected invalid oversized screenshot payload to fall back to original")
	}
}
