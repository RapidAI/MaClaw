package guiapp

import (
	"crypto/sha256"
	"fmt"
)

// LaunchFingerprint computes a deterministic hash of the LaunchSpec fields
// that affect session behavior. When the fingerprint changes between
// consecutive launches for the same project, it signals that the session
// parameters have materially changed (e.g. model switch, permission mode
// toggle) and the caller may want to warn the user or force a new session
// instead of reusing an existing one.
//
// Inspired by happy-coder's MessageQueue2 mode-hash mechanism which
// detects model/permission/systemPrompt changes and isolates messages
// into a new SDK conversation.
func LaunchFingerprint(spec LaunchSpec) string {
	h := sha256.New()
	fmt.Fprintf(h, "tool=%s\n", spec.Tool)
	fmt.Fprintf(h, "model=%s\n", spec.ModelID)
	fmt.Fprintf(h, "yolo=%v\n", spec.YoloMode)
	fmt.Fprintf(h, "admin=%v\n", spec.AdminMode)
	fmt.Fprintf(h, "proxy=%v\n", spec.UseProxy)
	fmt.Fprintf(h, "team=%v\n", spec.TeamMode)
	fmt.Fprintf(h, "python=%s\n", spec.PythonEnv)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ModeChanged returns true if two LaunchSpecs differ in any field that
// affects session behavior. This is a convenience wrapper around
// LaunchFingerprint for callers that just need a boolean check.
func ModeChanged(a, b LaunchSpec) bool {
	return LaunchFingerprint(a) != LaunchFingerprint(b)
}
