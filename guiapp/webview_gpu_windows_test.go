//go:build windows

package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

// The GPU policy must be decided before the window is created, so it cannot
// depend on anything the running app reports later. These tests pin the
// precedence order: explicit env override > recorded failure > remote session >
// default on.
func TestWebviewGPUExplicitEnvOverrideWins(t *testing.T) {
	dir := t.TempDir()
	// A recorded failure marker must not beat an explicit enable.
	if err := os.WriteFile(filepath.Join(dir, webviewGPUFailureMarkerName), []byte("reason=test\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	t.Setenv(webviewGPUPolicyEnvKey, "on")
	if webviewGPUDisabled(dir) {
		t.Fatal("MACLAW_WEBVIEW_GPU=on must enable GPU even with a failure marker")
	}
	// The explicit enable also clears the marker so the next automatic run is clean.
	if _, err := os.Stat(filepath.Join(dir, webviewGPUFailureMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("failure marker should be cleared after explicit enable, stat err = %v", err)
	}

	t.Setenv(webviewGPUPolicyEnvKey, "off")
	if !webviewGPUDisabled(dir) {
		t.Fatal("MACLAW_WEBVIEW_GPU=off must disable GPU")
	}
}

// A recorded blank-window failure disables acceleration on the next launch.
func TestWebviewGPUFailureMarkerDisables(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(webviewGPUPolicyEnvKey, "")
	// The marker is only written when this launch actually enabled GPU; without
	// this the test would pass on a remote session for the session reason and
	// never exercise the marker at all.
	noteWebviewGPUActive(true)
	t.Cleanup(func() { noteWebviewGPUActive(false) })

	recordWebviewGPUFailure(dir, "test")
	if !webviewGPUDisabled(dir) {
		t.Fatal("GPU should be disabled after a recorded failure")
	}
	if _, err := os.Stat(filepath.Join(dir, webviewGPUFailureMarkerName)); err != nil {
		t.Fatalf("failure marker was not written: %v", err)
	}

	// Recording twice must be idempotent and keep the app running.
	recordWebviewGPUFailure(dir, "test-again")
	if !webviewGPUDisabled(dir) {
		t.Fatal("GPU should stay disabled")
	}
}

// When GPU was already off, a blank window is some other bug and must not be
// remembered as a GPU failure — otherwise an RDP user would have acceleration
// disabled permanently on their next local session.
func TestWebviewGPUFailureNotRecordedWhenGPUWasOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(webviewGPUPolicyEnvKey, "")
	noteWebviewGPUActive(false)
	t.Cleanup(func() { noteWebviewGPUActive(false) })

	recordWebviewGPUFailure(dir, "software-rendered-blank")
	if _, err := os.Stat(filepath.Join(dir, webviewGPUFailureMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("marker must not be written when GPU was not in use, stat err = %v", err)
	}
}

// A missing user-data path must not panic; it simply means the marker cannot
// be persisted.
func TestWebviewGPUFailureMarkerEmptyPath(t *testing.T) {
	t.Setenv(webviewGPUPolicyEnvKey, "")
	recordWebviewGPUFailure("", "no-path")
	clearWebviewGPUFailure("")
	if _, err := webviewGPUFailureMarkerPath(""); err == nil {
		t.Fatal("empty user-data path should not produce a marker path")
	}
}
