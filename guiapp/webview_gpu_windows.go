//go:build windows

package guiapp

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// WebView2 GPU acceleration policy.
//
// GPU compositing is on by default. It is what makes scrolling, transitions and
// the assistant's streaming output smooth; with it disabled every frame is
// rasterized on the CPU, which is unusable on low-end hardware.
//
// It used to be disabled unconditionally as a workaround for a real failure
// mode: on some NVIDIA/high-DPI setups the WebView2 GPU compositor leaves a
// blank window that never requests assets. A blanket disable pays that
// workaround's cost on every other machine, so the failure is now detected and
// remembered instead:
//
//   - the paint watchdog (frontend_paint_watch.go) writes a marker when the
//     document still has not painted several seconds after launch;
//   - the next launch reads the marker and disables GPU for this user only.
//
// Operators can force either direction:
//
//	MACLAW_WEBVIEW_GPU=off  disable (same as the old behaviour)
//	MACLAW_WEBVIEW_GPU=on   enable and clear any recorded failure
//
// The marker lives beside the WebView profile so it is brand-isolated like the
// rest of the WebView state.

const webviewGPUPolicyEnvKey = "MACLAW_WEBVIEW_GPU"

// webviewGPUFailureMarkerName records that GPU compositing produced a blank
// window on this machine.
const webviewGPUFailureMarkerName = ".maclaw-webview-gpu-failed"

// webviewGPUDisabled reports whether WebView2 should start with --disable-gpu.
func webviewGPUDisabled(userDataPath string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(webviewGPUPolicyEnvKey))) {
	case "off", "0", "false", "no", "disable", "disabled":
		log.Printf("[webview-gpu] disabled by %s", webviewGPUPolicyEnvKey)
		return true
	case "on", "1", "true", "yes", "enable", "enabled":
		// An explicit enable is a request to retry: drop the recorded failure
		// so the next automatic decision starts clean.
		clearWebviewGPUFailure(userDataPath)
		log.Printf("[webview-gpu] enabled by %s", webviewGPUPolicyEnvKey)
		return false
	}

	if marker, err := webviewGPUFailureMarkerPath(userDataPath); err == nil {
		if _, statErr := os.Stat(marker); statErr == nil {
			log.Printf("[webview-gpu] disabled: previous launch recorded a blank-window failure marker=%q", marker)
			return true
		}
	}
	return false
}

// recordWebviewGPUFailure remembers that the WebView stayed blank long enough
// to need recovery, so the next launch starts with software rendering.
func recordWebviewGPUFailure(userDataPath, reason string) {
	marker, err := webviewGPUFailureMarkerPath(userDataPath)
	if err != nil {
		return
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		return
	}
	if mkErr := os.MkdirAll(filepath.Dir(marker), 0o755); mkErr != nil {
		return
	}
	payload := "reason=" + reason + "\n"
	if writeErr := os.WriteFile(marker, []byte(payload), 0o644); writeErr != nil {
		log.Printf("[webview-gpu] failed to record failure marker %q: %v", marker, writeErr)
		return
	}
	log.Printf("[webview-gpu] recorded blank-window failure reason=%s marker=%q; next launch will disable GPU", reason, marker)
}

func clearWebviewGPUFailure(userDataPath string) {
	marker, err := webviewGPUFailureMarkerPath(userDataPath)
	if err != nil {
		return
	}
	if removeErr := os.Remove(marker); removeErr != nil && !os.IsNotExist(removeErr) {
		log.Printf("[webview-gpu] failed to clear failure marker %q: %v", marker, removeErr)
	}
}

func webviewGPUFailureMarkerPath(userDataPath string) (string, error) {
	userDataPath = strings.TrimSpace(userDataPath)
	if userDataPath == "" {
		return "", os.ErrInvalid
	}
	return filepath.Join(userDataPath, webviewGPUFailureMarkerName), nil
}
