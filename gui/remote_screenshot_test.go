package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// --- BuildScreenshotCommand tests ---

func TestBuildScreenshotCommand_ReturnsNonEmpty(t *testing.T) {
	cmd := remote.BuildScreenshotCommand()
	if cmd == "" {
		t.Fatal("BuildScreenshotCommand() returned empty string on supported platform")
	}
}

func TestBuildScreenshotCommand_PlatformKeywords(t *testing.T) {
	cmd := remote.BuildScreenshotCommand()

	switch runtime.GOOS {
	case "windows":
		// Windows commands are pure PowerShell script blocks (no powershell.exe prefix)
		if !strings.Contains(cmd, "System.Drawing") {
			t.Fatalf("expected Windows command to contain 'System.Drawing', got: %s", cmd)
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
	_, err := remote.ParseScreenshotOutput("")
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

	result, err := remote.ParseScreenshotOutput(encoded)
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

	result, err := remote.ParseScreenshotOutput(withWhitespace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != encoded {
		t.Fatalf("expected cleaned base64 %q, got %q", encoded, result)
	}
}

func TestParseScreenshotOutput_InvalidBase64(t *testing.T) {
	// After stripping non-base64 characters, "!!!not-valid-base64!!!" becomes
	// "notvalidbase64" which is decodable but not PNG data.
	_, err := remote.ParseScreenshotOutput("!!!not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	// The error should indicate either invalid base64 or not PNG.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "invalid base64") && !strings.Contains(errMsg, "not PNG") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseScreenshotOutput_NotPNG(t *testing.T) {
	// Valid base64 but not PNG data
	nonPNG := base64.StdEncoding.EncodeToString([]byte("this is not a PNG file at all"))
	_, err := remote.ParseScreenshotOutput(nonPNG)
	if err == nil {
		t.Fatal("expected error for non-PNG data")
	}
	if !strings.Contains(err.Error(), "not PNG") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// --- DetectDisplayServer tests ---

func TestDetectDisplayServer_ReasonableResult(t *testing.T) {
	available, reason := remote.DetectDisplayServer()

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
	if !strings.Contains(err.Error(), "SDK") {
		t.Fatalf("expected 'SDK' error, got: %v", err)
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

// --- sanitizeWindowTitle tests ---

func TestSanitizeWindowTitle_AllowsSafeCharacters(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"Notepad", "Notepad"},
		{"My App - v1.0", "My App - v1.0"},
		{"文件管理器", "文件管理器"},
		{"test_app (debug)", "test_app (debug)"},
	}
	for _, tc := range cases {
		got := remote.SanitizeWindowTitle(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeWindowTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizeWindowTitle_StripsDangerousCharacters(t *testing.T) {
	cases := []struct {
		input string
	}{
		{`'; rm -rf /; echo '`},
		{"`whoami`"},
		{"$(calc.exe)"},
		{"test\"; exit 1; \""},
		{"hello$world"},
		{"a&b|c"},
	}
	for _, tc := range cases {
		got := remote.SanitizeWindowTitle(tc.input)
		// Should not contain any of: ' " ` $ ; & | \
		for _, bad := range []string{"'", "\"", "`", "$", ";", "&", "|", "\\"} {
			if strings.Contains(got, bad) {
				t.Errorf("SanitizeWindowTitle(%q) = %q, still contains %q", tc.input, got, bad)
			}
		}
	}
}

func TestSanitizeWindowTitle_EmptyAfterSanitize(t *testing.T) {
	got := remote.SanitizeWindowTitle("$$")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBuildWindowScreenshotCommand_EmptyAfterSanitize(t *testing.T) {
	cmd := remote.BuildWindowScreenshotCommand("$$")
	if cmd != "" {
		t.Errorf("expected empty command for fully-sanitized title, got: %s", cmd)
	}
}

func TestBuildWindowScreenshotCommand_ValidTitle(t *testing.T) {
	cmd := remote.BuildWindowScreenshotCommand("Notepad")
	if cmd == "" {
		t.Fatal("expected non-empty command for valid title")
	}
	if !strings.Contains(cmd, "Notepad") {
		t.Fatalf("expected command to contain window title, got: %s", cmd)
	}
}

func TestParseScreenshotOutput_BOMPrefix(t *testing.T) {
	pngData := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("bom-test")...)
	encoded := base64.StdEncoding.EncodeToString(pngData)
	// Prepend UTF-8 BOM
	withBOM := "\xEF\xBB\xBF" + encoded

	result, err := remote.ParseScreenshotOutput(withBOM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != encoded {
		t.Fatalf("expected %q, got %q", encoded, result)
	}
}

func TestParseScreenshotOutput_RawBase64NoPadding(t *testing.T) {
	pngData := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("no-pad")...)
	// Encode without padding
	encoded := base64.RawStdEncoding.EncodeToString(pngData)

	result, err := remote.ParseScreenshotOutput(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result should be re-encoded with standard padding
	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("result is not valid standard base64: %v", err)
	}
	if string(decoded) != string(pngData) {
		t.Fatal("round-trip data mismatch")
	}
}

func TestParseScreenshotOutput_NullBytesStripped(t *testing.T) {
	pngData := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("null-test")...)
	encoded := base64.StdEncoding.EncodeToString(pngData)
	// Inject null bytes
	withNulls := encoded[:4] + "\x00" + encoded[4:]

	result, err := remote.ParseScreenshotOutput(withNulls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != encoded {
		t.Fatalf("expected %q, got %q", encoded, result)
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
	if !strings.Contains(err.Error(), "SDK") {
		t.Fatalf("expected 'SDK' in error, got: %v", err)
	}
}

// --- IsBlankImage tests ---

func TestIsBlankImage_AllBlackPNG(t *testing.T) {
	// Create a small all-black PNG image.
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	// All pixels default to (0,0,0,0) — effectively black.
	// Set alpha to 255 so they're opaque black.
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	if !remote.IsBlankImage(b64) {
		t.Fatal("expected all-black image to be detected as blank")
	}
}

func TestIsBlankImage_ColorfulPNG(t *testing.T) {
	// Create a small image with non-black pixels.
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{100, 150, 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	if remote.IsBlankImage(b64) {
		t.Fatal("expected colorful image to NOT be detected as blank")
	}
}

func TestIsBlankImage_NearBlackPNG(t *testing.T) {
	// Create an image with very dark but not pure-black pixels (brightness ~2).
	// This should still be detected as blank since it's below the threshold.
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{2, 2, 3, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	if !remote.IsBlankImage(b64) {
		t.Fatal("expected near-black image to be detected as blank")
	}
}

func TestIsBlankImage_MixedWithBrightPixel(t *testing.T) {
	// Mostly black but with enough bright pixels to push average above threshold.
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	// Set ~30% of pixels to white — average should be well above threshold.
	for y := 0; y < 3; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	if remote.IsBlankImage(b64) {
		t.Fatal("expected image with bright pixels to NOT be detected as blank")
	}
}

func TestIsBlankImage_InvalidBase64(t *testing.T) {
	// Invalid base64 should return false (not blank) — don't discard
	// potentially valid data due to decode errors.
	if remote.IsBlankImage("not-valid-base64!!!") {
		t.Fatal("expected invalid base64 to return false (not blank)")
	}
}

func TestIsBlankImage_EmptyString(t *testing.T) {
	if remote.IsBlankImage("") {
		t.Fatal("expected empty string to return false (not blank)")
	}
}

func TestBuildScreenshotCommand_BlankDetectionKeywords(t *testing.T) {
	cmd := remote.BuildScreenshotCommand()
	switch runtime.GOOS {
	case "windows":
		// Windows command should include blank detection logic.
		if !strings.Contains(cmd, "Test-BlankBitmap") {
			t.Fatal("expected Windows command to include blank image detection")
		}
		if !strings.Contains(cmd, "0x40000000") || !strings.Contains(cmd, "BitBlt") {
			t.Fatal("expected Windows command to include BitBlt fallback with CAPTUREBLT flag")
		}
		if !strings.Contains(cmd, "tscon") {
			t.Fatal("expected Windows command to include tscon reconnect attempt")
		}
		if !strings.Contains(cmd, "PrintWindow") {
			t.Fatal("expected Windows command to include PrintWindow composite fallback")
		}
		if !strings.Contains(cmd, "EnumWindows") {
			t.Fatal("expected Windows command to include window enumeration for composite")
		}
	case "darwin":
		if !strings.Contains(cmd, "is_blank") {
			t.Fatal("expected macOS command to include blank image detection")
		}
	case "linux":
		if !strings.Contains(cmd, "is_blank") {
			t.Fatal("expected Linux command to include blank image detection")
		}
	}
}

func TestBuildWindowsWindowScreenshotCommand_PrintWindowFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	cmd := remote.BuildWindowScreenshotCommand("Notepad")
	if !strings.Contains(cmd, "PrintWindow") {
		t.Fatal("expected Windows window command to use PrintWindow API")
	}
	if !strings.Contains(cmd, "Test-BlankBitmap") {
		t.Fatal("expected Windows window command to include blank detection")
	}
}
