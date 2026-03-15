package main

import (
	"encoding/base64"
	"os"
	"runtime"
	"strings"
	"testing"
)

// --- BuildScreenshotCommand tests ---

func TestBuildScreenshotCommand_ReturnsNonEmpty(t *testing.T) {
	cmd := BuildScreenshotCommand()
	if cmd == "" {
		t.Fatal("BuildScreenshotCommand() returned empty string on supported platform")
	}
}

func TestBuildScreenshotCommand_PlatformKeywords(t *testing.T) {
	cmd := BuildScreenshotCommand()

	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(strings.ToLower(cmd), "powershell") {
			t.Fatalf("expected Windows command to contain 'powershell', got: %s", cmd)
		}
	case "darwin":
		if !strings.Contains(cmd, "screencapture") {
			t.Fatalf("expected macOS command to contain 'screencapture', got: %s", cmd)
		}
	case "linux":
		if !strings.Contains(cmd, "scrot") {
			t.Fatalf("expected Linux command to contain 'scrot', got: %s", cmd)
		}
	default:
		t.Skipf("unsupported platform: %s", runtime.GOOS)
	}
}

// --- ParseScreenshotOutput tests ---

func TestParseScreenshotOutput_EmptyInput(t *testing.T) {
	_, err := ParseScreenshotOutput("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "no output") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseScreenshotOutput_ValidPNG(t *testing.T) {
	// Build minimal data starting with PNG magic bytes
	pngData := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("fake-png-body")...)
	encoded := base64.StdEncoding.EncodeToString(pngData)

	result, err := ParseScreenshotOutput(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify round-trip: decode the result and check PNG magic bytes
	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("result is not valid base64: %v", err)
	}
	if len(decoded) < 8 {
		t.Fatal("decoded data too short")
	}
	for i, b := range []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a} {
		if decoded[i] != b {
			t.Fatalf("PNG magic byte mismatch at index %d: got %x, want %x", i, decoded[i], b)
		}
	}
	if string(decoded) != string(pngData) {
		t.Fatal("round-trip data mismatch")
	}
}

func TestParseScreenshotOutput_ValidPNGWithWhitespace(t *testing.T) {
	pngData := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("body-data")...)
	encoded := base64.StdEncoding.EncodeToString(pngData)
	// Inject whitespace and newlines
	withWhitespace := "  \n" + encoded + "\n  \t"

	result, err := ParseScreenshotOutput(withWhitespace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != encoded {
		t.Fatalf("expected cleaned base64 %q, got %q", encoded, result)
	}
}

func TestParseScreenshotOutput_InvalidBase64(t *testing.T) {
	_, err := ParseScreenshotOutput("!!!not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "invalid base64") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseScreenshotOutput_NotPNG(t *testing.T) {
	// Valid base64 but not PNG data
	nonPNG := base64.StdEncoding.EncodeToString([]byte("this is not a PNG file at all"))
	_, err := ParseScreenshotOutput(nonPNG)
	if err == nil {
		t.Fatal("expected error for non-PNG data")
	}
	if !strings.Contains(err.Error(), "not PNG") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// --- DetectDisplayServer tests ---

func TestDetectDisplayServer_ReasonableResult(t *testing.T) {
	available, reason := DetectDisplayServer()

	switch runtime.GOOS {
	case "windows", "darwin":
		// These platforms always return true
		if !available {
			t.Fatalf("expected available=true on %s, got false (reason: %s)", runtime.GOOS, reason)
		}
		if reason != "" {
			t.Fatalf("expected empty reason when available, got: %s", reason)
		}
	case "linux":
		// On Linux, result depends on DISPLAY/WAYLAND_DISPLAY env vars.
		// Just verify consistency: if not available, reason must be non-empty.
		if !available && reason == "" {
			t.Fatal("expected non-empty reason when display is not available on Linux")
		}
		if available && reason != "" {
			t.Fatalf("expected empty reason when display is available, got: %s", reason)
		}
	default:
		// Unsupported platform should return false with a reason
		if available {
			t.Fatalf("expected available=false on unsupported platform %s", runtime.GOOS)
		}
		if reason == "" {
			t.Fatal("expected non-empty reason on unsupported platform")
		}
	}
}

// --- CaptureScreenshot integration tests ---

func TestCaptureScreenshot_SessionNotFound(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	err := manager.CaptureScreenshot("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestCaptureScreenshot_RejectsPTYSession(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	session := &RemoteSession{
		ID:   "pty-session",
		Exec: newFakeExecutionHandle(1),
	}
	manager.mu.Lock()
	manager.sessions["pty-session"] = session
	manager.mu.Unlock()

	err := manager.CaptureScreenshot("pty-session")
	if err == nil {
		t.Fatal("expected error for PTY session")
	}
	if !strings.Contains(err.Error(), "SDK mode") {
		t.Fatalf("expected 'SDK mode' error, got: %v", err)
	}
}

func TestCaptureScreenshot_NoDisplayEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display detection test only applicable on Linux")
	}

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:   "sdk-no-display",
		Exec: sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions["sdk-no-display"] = session
	manager.mu.Unlock()

	err := manager.CaptureScreenshot("sdk-no-display")
	if err == nil {
		t.Fatal("expected error when no display is available")
	}
	if !strings.Contains(err.Error(), "graphical display") {
		t.Fatalf("expected display-related error, got: %v", err)
	}
}

func TestCaptureScreenshot_CommandFailure(t *testing.T) {
	// This test verifies that a command returning non-zero exit code
	// produces an appropriate error. We use an SDK session so it passes
	// the SDK mode check, and rely on the actual screenshot command
	// failing (which it will in CI/test environments without a display
	// on Linux, or if screenshot tools aren't installed).
	//
	// On Windows/macOS where DetectDisplayServer always returns true,
	// the screenshot command itself will likely fail in a headless test
	// environment, which is exactly what we want to test.

	if runtime.GOOS == "linux" {
		display := os.Getenv("DISPLAY")
		wayland := os.Getenv("WAYLAND_DISPLAY")
		if display == "" && wayland == "" {
			t.Skip("skipping command failure test on Linux without display (would fail at display detection)")
		}
	}

	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:   "sdk-cmd-fail",
		Exec: sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions["sdk-cmd-fail"] = session
	manager.mu.Unlock()

	// The screenshot command will fail in test environments (no actual screen).
	// We just verify it returns an error rather than panicking.
	err := manager.CaptureScreenshot("sdk-cmd-fail")
	if err == nil {
		// If it somehow succeeded (real desktop environment), that's fine too
		t.Log("CaptureScreenshot succeeded — running in a desktop environment")
		return
	}
	// Verify the error is from command execution or output parsing, not from
	// earlier checks (SDK mode, display detection).
	errMsg := err.Error()
	if strings.Contains(errMsg, "SDK mode") {
		t.Fatalf("should not fail on SDK mode check: %v", err)
	}
}

func TestCaptureScreenshot_LogPrefixInErrors(t *testing.T) {
	// Verify that error messages from CaptureScreenshot contain expected
	// content. The [screenshot] prefix is used in log messages (via app.log),
	// while returned errors have descriptive messages.

	app := &App{}
	manager := NewRemoteSessionManager(app)

	// Test 1: session not found error
	err := manager.CaptureScreenshot("missing-session")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("expected 'session not found' in error, got: %v", err)
	}

	// Test 2: PTY session error contains "SDK mode"
	session := &RemoteSession{
		ID:   "pty-log-test",
		Exec: newFakeExecutionHandle(1),
	}
	manager.mu.Lock()
	manager.sessions["pty-log-test"] = session
	manager.mu.Unlock()

	err = manager.CaptureScreenshot("pty-log-test")
	if err == nil {
		t.Fatal("expected error for PTY session")
	}
	if !strings.Contains(err.Error(), "SDK mode") {
		t.Fatalf("expected 'SDK mode' in error, got: %v", err)
	}
}

