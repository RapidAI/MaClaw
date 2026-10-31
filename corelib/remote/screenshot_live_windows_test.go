//go:build windows

package remote

import (
	"os"
	"testing"
)

// TestLiveNativeScreenshot checks the real display. GDI BitBlt is denied on
// a Remote Desktop WDDM session, so a passing run means the PrintWindow
// composite produced a non-blank frame.
func TestLiveNativeScreenshot(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_SHOT") == "" {
		t.Skip("set MACLAW_LIVE_SHOT=1")
	}
	b64, err := NativeScreenshot()
	if err != nil || b64 == "" || IsBlankImage(b64) {
		t.Fatalf("primary screenshot err=%v len=%d", err, len(b64))
	}
	b64v, err := NativeScreenshotVirtual()
	if err != nil || b64v == "" || IsBlankImage(b64v) {
		t.Fatalf("virtual screenshot err=%v len=%d", err, len(b64v))
	}
}
