package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// CaptureScreenshot executes the full screenshot capture flow for the given
// session: detect display → build command → execute → parse output → send image.
// Only SDK-mode sessions are supported; PTY sessions return an error.
func (m *RemoteSessionManager) CaptureScreenshot(sessionID string) error {
	cmdStr := remote.BuildScreenshotCommandWithFallback().Command
	if cmdStr == "" {
		return fmt.Errorf("screenshot capture is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, "", cmdStr)
}

// CaptureWindowScreenshot captures a screenshot of a specific window by title
// and sends it through the image transfer pipeline. The windowTitle is matched
// as a substring against visible window titles.
func (m *RemoteSessionManager) CaptureWindowScreenshot(sessionID, windowTitle string) error {
	if strings.TrimSpace(windowTitle) == "" {
		return fmt.Errorf("window title must not be empty")
	}
	cmdStr := remote.BuildWindowScreenshotCommand(windowTitle)
	if cmdStr == "" {
		return fmt.Errorf("window screenshot is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, windowTitle, cmdStr)
}

// captureAndSend is the shared implementation for CaptureScreenshot and
// CaptureWindowScreenshot. It validates the session, executes the shell
// command, parses the base64 output, and sends the image via the hub.
func (m *RemoteSessionManager) captureAndSend(sessionID, label, cmdStr string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	// Screenshot capture works for SDK-mode and Gemini ACP sessions.
	// The capture runs outside the CLI process (platform-native commands),
	// so it doesn't depend on the CLI tool's own image support.
	switch s.Exec.(type) {
	case *SDKExecutionHandle, *GeminiACPExecutionHandle:
		// supported
	default:
		return fmt.Errorf("screenshot capture is only supported in SDK and ACP mode sessions")
	}

	// On macOS 10.15+, ensure screen recording permission is granted before
	// spawning child processes. This ties the TCC prompt to our bundle ID
	// so the user only sees it once, instead of repeatedly.
	if !EnsureScreenRecordingPermission() {
		return fmt.Errorf("screen recording permission not granted - please open System Settings > Privacy & Security > Screen Recording, grant permission to MaClaw, then restart the app")
	}

	available, reason := remote.DetectDisplayServer()
	if !available {
		return fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	logLabel := "fullscreen"
	if label != "" {
		logLabel = fmt.Sprintf("window %q", label)
	}

	// Ensure large image buffers are returned to the OS promptly after
	// the screenshot pipeline completes, regardless of exit path.
	defer remote.ReleaseScreenshotMemory()

	// On macOS, try native CGO capture first (avoids screencapture subprocess
	// which triggers its own TCC dialog on macOS 26+). Only for fullscreen
	// captures (label == "" means no specific window title).
	var base64Data string
	if runtime.GOOS == "darwin" && label == "" {
		m.app.log(fmt.Sprintf("[screenshot] trying native CGO capture for session=%s", sessionID))
		if b64, err := nativeCaptureScreenshot(); err == nil && b64 != "" && !remote.IsBlankImage(b64) {
			base64Data = b64
			m.app.log(fmt.Sprintf("[screenshot] native CGO capture succeeded for session=%s", sessionID))
		} else {
			if err != nil {
				m.app.log(fmt.Sprintf("[screenshot] native CGO capture failed, falling back to shell: %v", err))
			} else {
				m.app.log("[screenshot] native CGO capture returned blank image, falling back to shell")
			}
		}
	}

	// Fallback: shell command approach (all platforms, or when native failed).
	if base64Data == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		var shellName string
		var shellArgs []string
		if runtime.GOOS == "windows" {
			shellName = "powershell"
			shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
		} else {
			shellName = "bash"
			shellArgs = []string{"-c", cmdStr}
		}

		cmd := exec.CommandContext(ctx, shellName, shellArgs...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		hideCommandWindow(cmd)

		m.app.log(fmt.Sprintf("[screenshot] capturing %s for session=%s via shell", logLabel, sessionID))

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("screenshot command timed out after 45s")
			}
			m.app.log(fmt.Sprintf("[screenshot] capture failed for session=%s: %v, stderr: %s", sessionID, err, stderr.String()))
			return fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		}

		rawOut := stdout.String()
		// Release the stdout buffer immediately — rawOut holds its own copy.
		stdout.Reset()

		// ParseScreenshotOutputOpt does parse + blank-check in one pass,
		// avoiding a second base64-decode + PNG-decode round-trip.
		var blank bool
		var err error
		base64Data, blank, err = remote.ParseScreenshotOutputOpt(rawOut)
		rawOut = "" // allow GC
		if err != nil {
			m.app.log(fmt.Sprintf("[screenshot] failed to parse output for session=%s: %v (stderr=%q)",
				sessionID, err, strings.TrimSpace(stderr.String())))
			return fmt.Errorf("screenshot output parse error: %w", err)
		}
		if blank {
			m.app.log(fmt.Sprintf("[screenshot] captured image is blank/black for session=%s — session may be locked or display is off", sessionID))
			return fmt.Errorf("screenshot is blank (all black) — the session may be locked, the display may be off, or the remote desktop is disconnected")
		}
	} else {
		// Native capture path — blank check was already done in the condition
		// above (only non-blank images are assigned to base64Data), so no
		// additional IsBlankImage call is needed here.
	}

	// Quick size check without a full base64 decode.
	if ExceedsImageSizeLimit(base64Data, ImageOutputSizeLimit) {
		downsized, dsErr := remote.DownsizeScreenshotBase64(base64Data, ImageOutputSizeLimit)
		if dsErr != nil {
			m.app.log(fmt.Sprintf("[screenshot] downsize failed for session=%s: %v", sessionID, dsErr))
			return fmt.Errorf("image transfer: decoded size exceeds limit and downsize failed: %w", dsErr)
		}
		m.app.log(fmt.Sprintf("[screenshot] downsized image for session=%s", sessionID))
		base64Data = downsized
		// Verify after downsize.
		if ExceedsImageSizeLimit(base64Data, ImageOutputSizeLimit) {
			m.app.log(fmt.Sprintf("[screenshot] image still exceeds size limit after downsize for session=%s", sessionID))
			return fmt.Errorf("image transfer: decoded size exceeds limit after downsize")
		}
	}

	msg := NewImageTransferMessage(sessionID, "image/png", base64Data)

	if m.hubClient != nil {
		if err := m.hubClient.SendSessionImage(msg); err != nil {
			m.app.log(fmt.Sprintf("[screenshot] failed to send image for session=%s: %v", sessionID, err))
			return err
		}
	}

	m.app.log(fmt.Sprintf("[screenshot] successfully captured %s for session=%s", logLabel, sessionID))
	return nil
}

// CaptureScreenshotDirect captures a screenshot of the local display without
// requiring an active session. It returns the base64-encoded PNG data directly,
// suitable for embedding in IM responses via ImageKey.
//
// On Windows, it uses PowerShell with multiple fallback strategies (CopyFromScreen,
// BitBlt+CAPTUREBLT, tscon reconnect, PrintWindow composite) that work even when
// the session is locked. On other platforms, it uses command-line tools.
func (m *RemoteSessionManager) CaptureScreenshotDirect() (string, error) {
	// On macOS 10.15+, ensure screen recording permission is granted.
	if !EnsureScreenRecordingPermission() {
		return "", fmt.Errorf("screen recording permission not granted - please open System Settings > Privacy & Security > Screen Recording, grant permission to MaClaw, then restart the app")
	}

	available, reason := remote.DetectDisplayServer()
	if !available {
		return "", fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	defer remote.ReleaseScreenshotMemory()

	// On macOS, try native CGO capture first (avoids screencapture TCC dialog).
	if runtime.GOOS == "darwin" {
		m.app.log("[screenshot-direct] trying native CGO capture")
		if b64, err := nativeCaptureScreenshot(); err == nil && b64 != "" && !remote.IsBlankImage(b64) {
			m.app.log("[screenshot-direct] native CGO capture succeeded")
			return b64, nil
		} else if err != nil {
			m.app.log(fmt.Sprintf("[screenshot-direct] native CGO capture failed, falling back to shell: %v", err))
		} else {
			m.app.log("[screenshot-direct] native CGO capture returned blank, falling back to shell")
		}
	}

	// Fallback: shell command approach (multi-monitor: captures all displays).
	cmdStr := remote.BuildScreenshotCommandWithFallback().Command
	if cmdStr == "" {
		return "", fmt.Errorf("screenshot capture is not supported on %s", runtime.GOOS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

	m.app.log("[screenshot-direct] capturing fullscreen (all monitors) via command-line")

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("screenshot command timed out after 45s")
		}
		m.app.log(fmt.Sprintf("[screenshot-direct] capture failed: %v, stderr: %s", err, stderr.String()))
		return "", fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	rawOut := stdout.String()
	stdout.Reset()

	base64Data, blank, err := remote.ParseScreenshotOutputOpt(rawOut)
	rawOut = "" // allow GC
	if err != nil {
		m.app.log(fmt.Sprintf("[screenshot-direct] parse error: %v (stderr=%q)", err, strings.TrimSpace(stderr.String())))
		return "", fmt.Errorf("screenshot output parse error: %w", err)
	}

	if blank {
		m.app.log("[screenshot-direct] captured image is blank/black")
		return "", fmt.Errorf("screenshot is blank (all black) — the display may be off or locked")
	}

	m.app.log("[screenshot-direct] successfully captured fullscreen (all monitors) via command-line")
	return base64Data, nil
}

// CaptureScreenshotDirectForDisplay captures a single display by index.
// It uses BuildSingleMonitorScreenshotCommandSafe which includes BitBlt
// fallback for environments where CopyFromScreen returns blank images.
func (m *RemoteSessionManager) CaptureScreenshotDirectForDisplay(displayIndex int) (string, error) {
	if !EnsureScreenRecordingPermission() {
		return "", fmt.Errorf("screen recording permission not granted")
	}

	available, reason := remote.DetectDisplayServer()
	if !available {
		return "", fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	defer remote.ReleaseScreenshotMemory()

	result, err := remote.BuildSingleMonitorScreenshotCommandSafe(displayIndex)
	if err != nil {
		return "", err
	}
	cmdStr := result.Command
	if cmdStr == "" {
		return "", fmt.Errorf("single monitor screenshot not supported on this platform")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

	m.app.log(fmt.Sprintf("[screenshot-display] capturing display %d", displayIndex))

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("screenshot command timed out after 45s")
		}
		return "", fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	rawOut := stdout.String()
	stdout.Reset()

	base64Data, blank, err := remote.ParseScreenshotOutputOpt(rawOut)
	rawOut = ""
	if err != nil {
		return "", fmt.Errorf("screenshot output parse error: %w", err)
	}
	if blank {
		return "", fmt.Errorf("screenshot of display %d is blank (all black)", displayIndex)
	}

	m.app.log(fmt.Sprintf("[screenshot-display] successfully captured display %d", displayIndex))
	return base64Data, nil
}

// CaptureScreenshotToBase64 captures a screenshot (using the same command-line
// approach as CaptureScreenshot) but returns the base64-encoded PNG data
// directly instead of sending it via the WebSocket image channel.
// This is used by IM platforms (WeChat, QQ, etc.) that cannot receive
// session.image WebSocket pushes.
func (m *RemoteSessionManager) CaptureScreenshotToBase64(sessionID string) (string, error) {
	if sessionID == "" {
		return m.CaptureScreenshotDirect()
	}
	s, ok := m.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	switch s.Exec.(type) {
	case *SDKExecutionHandle, *GeminiACPExecutionHandle:
		// supported
	default:
		return "", fmt.Errorf("screenshot capture is only supported in SDK and ACP mode sessions")
	}

	if !EnsureScreenRecordingPermission() {
		return "", fmt.Errorf("screen recording permission not granted - please open System Settings > Privacy & Security > Screen Recording, grant permission to MaClaw, then restart the app")
	}
	available, reason := remote.DetectDisplayServer()
	if !available {
		return "", fmt.Errorf("screenshot requires a graphical display: %s", reason)
	}

	defer remote.ReleaseScreenshotMemory()

	// On macOS, try native CGO capture first (avoids screencapture TCC dialog).
	if runtime.GOOS == "darwin" {
		m.app.log(fmt.Sprintf("[screenshot-b64] trying native CGO capture for session=%s", sessionID))
		if b64, err := nativeCaptureScreenshot(); err == nil && b64 != "" && !remote.IsBlankImage(b64) {
			m.app.log(fmt.Sprintf("[screenshot-b64] native CGO capture succeeded for session=%s", sessionID))
			return b64, nil
		} else if err != nil {
			m.app.log(fmt.Sprintf("[screenshot-b64] native CGO capture failed, falling back to shell: %v", err))
		} else {
			m.app.log("[screenshot-b64] native CGO capture returned blank, falling back to shell")
		}
	}

	// Fallback: shell command approach (multi-monitor: captures all displays).
	cmdStr := remote.BuildScreenshotCommandWithFallback().Command
	if cmdStr == "" {
		return "", fmt.Errorf("screenshot not supported on %s", runtime.GOOS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

	m.app.log(fmt.Sprintf("[screenshot-b64] capturing for session=%s", sessionID))
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("screenshot command timed out after 45s")
		}
		return "", fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	rawOut := stdout.String()
	stdout.Reset()

	base64Data, blank, err := remote.ParseScreenshotOutputOpt(rawOut)
	rawOut = "" // allow GC
	if err != nil {
		return "", fmt.Errorf("screenshot output parse error: %w", err)
	}
	if blank {
		return "", fmt.Errorf("screenshot is blank — session may be locked or display is off")
	}

	m.app.log(fmt.Sprintf("[screenshot-b64] successfully captured for session=%s", sessionID))
	return base64Data, nil
}
