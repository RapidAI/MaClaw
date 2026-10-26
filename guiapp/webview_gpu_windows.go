//go:build windows

package guiapp

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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

// webviewGPUActive mirrors the decision made for this launch, so the paint
// watchdog only blames GPU compositing when it was actually in use.
var webviewGPUActive atomic.Bool

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

	// A Remote Desktop session has no hardware GPU to accelerate with. WebView2
	// falls back to a software D3D rasterizer (WARP), which is slower at
	// compositing this UI than the plain CPU path that --disable-gpu selects.
	// The blank-window workaround this policy replaces was also most often hit
	// on exactly these GPU-less configurations (VMs, RDP, high-DPI NVIDIA).
	if perfSessionIsRemote() {
		log.Printf("[webview-gpu] disabled: remote desktop session without hardware acceleration")
		return true
	}

	return false
}

// noteWebviewGPUActive records whether this launch actually enabled GPU
// compositing. Recording a failure only makes sense when acceleration was on:
// otherwise an unrelated slow paint on a software-rendered session would write a
// marker that disables GPU forever on the next local (non-RDP) launch.
func noteWebviewGPUActive(enabled bool) {
	webviewGPUActive.Store(enabled)
}

func webviewGPUIsActive() bool {
	return webviewGPUActive.Load()
}

// recordWebviewGPUFailure remembers that the WebView stayed blank long enough
// to need recovery, so the next launch starts with software rendering.
func recordWebviewGPUFailure(userDataPath, reason string) {
	if !webviewGPUIsActive() {
		// GPU was already off; a blank window here is a different bug and
		// must not be remembered as a GPU failure.
		return
	}
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

// SM_REMOTESESSION is the GetSystemMetrics index that reports whether the
// calling process is bound to a Remote Desktop session.
const smRemoteSession = 0x1000

var procGetSystemMetricsGPU = func() *syscall.LazyProc {
	user32 := syscall.NewLazyDLL("user32.dll")
	return user32.NewProc("GetSystemMetrics")
}()

// perfSessionIsRemote reports whether this process runs in a Remote Desktop
// session, where hardware GPU acceleration is unavailable.
func perfSessionIsRemote() bool {
	ret, _, _ := procGetSystemMetricsGPU.Call(uintptr(smRemoteSession))
	return ret != 0
}
