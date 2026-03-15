package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// pngMagicBytes is the 8-byte PNG file header signature.
var pngMagicBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// ParseScreenshotOutput extracts and validates base64-encoded PNG data from
// the screenshot command's stdout output. It trims whitespace and newlines,
// validates the base64 encoding, and confirms the decoded data starts with
// PNG magic bytes.
func ParseScreenshotOutput(stdout string) (string, error) {
	cleaned := strings.TrimSpace(stdout)
	cleaned = strings.Join(strings.Fields(cleaned), "")

	if cleaned == "" {
		return "", fmt.Errorf("screenshot command produced no output")
	}

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid base64 encoding")
	}

	if len(decoded) < len(pngMagicBytes) || !bytes.Equal(decoded[:len(pngMagicBytes)], pngMagicBytes) {
		return "", fmt.Errorf("output is not PNG")
	}

	return cleaned, nil
}



// BuildScreenshotCommand returns a platform-specific shell command string that
// captures a screenshot and outputs the result as raw base64-encoded PNG data
// to stdout. Temporary files are cleaned up on macOS and Linux regardless of
// success or failure.
func BuildScreenshotCommand() string {
	switch runtime.GOOS {
	case "windows":
		return buildWindowsScreenshotCommand()
	case "darwin":
		return buildDarwinScreenshotCommand()
	case "linux":
		return buildLinuxScreenshotCommand()
	default:
		return ""
	}
}

func buildWindowsScreenshotCommand() string {
	return `powershell -NoProfile -NonInteractive -Command "` +
		`Add-Type -AssemblyName System.Drawing; ` +
		`Add-Type -AssemblyName System.Windows.Forms; ` +
		`$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; ` +
		`$bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`$g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size); ` +
		`$g.Dispose(); ` +
		`$ms = New-Object System.IO.MemoryStream; ` +
		`$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); ` +
		`$bmp.Dispose(); ` +
		`[Convert]::ToBase64String($ms.ToArray()); ` +
		`$ms.Dispose()"`
}

func buildDarwinScreenshotCommand() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`screencapture -x "$tmpfile" && ` +
		`base64 < "$tmpfile"`
}

func buildLinuxScreenshotCommand() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`if command -v scrot >/dev/null 2>&1; then ` +
		`scrot "$tmpfile"; ` +
		`elif command -v import >/dev/null 2>&1; then ` +
		`import -window root "$tmpfile"; ` +
		`else ` +
		`echo "no screenshot tool found (scrot or import required)" >&2; exit 1; ` +
		`fi && ` +
		`base64 < "$tmpfile"`
}

// DetectDisplayServer checks whether a graphical display environment is
// available on the current platform.
// Returns (available, reason) where reason is non-empty when available is false.
//   - Windows: always returns true (desktop app necessarily has display)
//   - macOS: always returns true (Quartz display server is available for desktop apps)
//   - Linux: checks DISPLAY or WAYLAND_DISPLAY environment variables
func DetectDisplayServer() (bool, string) {
	switch runtime.GOOS {
	case "windows":
		return true, ""
	case "darwin":
		return true, ""
	case "linux":
		if display := os.Getenv("DISPLAY"); display != "" {
			return true, ""
		}
		if waylandDisplay := os.Getenv("WAYLAND_DISPLAY"); waylandDisplay != "" {
			return true, ""
		}
		return false, "no display server detected: neither DISPLAY nor WAYLAND_DISPLAY environment variable is set"
	default:
		return false, fmt.Sprintf("unsupported platform for display detection: %s", runtime.GOOS)
	}
}


// CaptureScreenshot executes the full screenshot capture flow for the given
// session: detect display → build command → execute → parse output → send image.
// Only SDK-mode sessions are supported; PTY sessions return an error.
func (m *RemoteSessionManager) CaptureScreenshot(sessionID string) error {
	// Look up session and verify it exists.
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Verify session is SDK mode.
	if _, isSDK := s.Exec.(*SDKExecutionHandle); !isSDK {
		return fmt.Errorf("screenshot capture is only supported in SDK mode sessions")
	}

	// Detect display environment.
	available, reason := DetectDisplayServer()
	if !available {
		m.app.log(fmt.Sprintf("[screenshot] display server not available: %s", reason))
		return fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	// Build the platform-specific screenshot command.
	cmdStr := BuildScreenshotCommand()
	if cmdStr == "" {
		m.app.log(fmt.Sprintf("[screenshot] unsupported platform: %s", runtime.GOOS))
		return fmt.Errorf("screenshot capture is not supported on %s", runtime.GOOS)
	}

	// Execute the command with a 10-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var shellName, shellFlag string
	if runtime.GOOS == "windows" {
		shellName = "cmd"
		shellFlag = "/c"
	} else {
		shellName = "bash"
		shellFlag = "-c"
	}

	cmd := exec.CommandContext(ctx, shellName, shellFlag, cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	m.app.log(fmt.Sprintf("[screenshot] executing capture command for session=%s", sessionID))

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			m.app.log(fmt.Sprintf("[screenshot] command timed out after 10s for session=%s", sessionID))
			return fmt.Errorf("screenshot command timed out after 10s")
		}
		m.app.log(fmt.Sprintf("[screenshot] command failed for session=%s: %v, stderr: %s", sessionID, err, stderr.String()))
		return fmt.Errorf("screenshot command failed: %w", err)
	}

	// Parse the base64 output.
	base64Data, err := ParseScreenshotOutput(stdout.String())
	if err != nil {
		m.app.log(fmt.Sprintf("[screenshot] failed to parse output for session=%s: %v", sessionID, err))
		return fmt.Errorf("screenshot output parse error: %w", err)
	}

	// Construct the image transfer message.
	msg := NewImageTransferMessage(sessionID, "image/png", base64Data)

	// Validate the message against the output size limit.
	if err := ValidateImageTransferMessage(msg, ImageOutputSizeLimit); err != nil {
		m.app.log(fmt.Sprintf("[screenshot] image exceeds size limit for session=%s: %v", sessionID, err))
		return err
	}

	// Send the screenshot through the image transfer pipeline.
	if m.hubClient != nil {
		if err := m.hubClient.SendSessionImage(msg); err != nil {
			m.app.log(fmt.Sprintf("[screenshot] failed to send image for session=%s: %v", sessionID, err))
			return err
		}
	}

	m.app.log(fmt.Sprintf("[screenshot] successfully captured and sent screenshot for session=%s", sessionID))
	return nil
}
