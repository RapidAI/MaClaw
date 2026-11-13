// Package testenv holds predicates shared by tests across the module.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// ModelTestsEnabled reports whether tests that load real AI model weights
// (embedding/ASR/OCR/YOLO/…) or call live model services should run.
//
// MACLAW_TEST_MODELS decides when set: "off"/"0"/"false"/"no"/"skip" disable,
// any other value enables. When unset, model tests run except under
// `go test -short`, matching the existing convention in corelib/intent.
//
// Model tests self-skip when the weights are absent, but on a dev machine the
// models are usually installed — a plain `go test ./...` would otherwise load
// several GB of weights alongside a running app and stall the host.
func ModelTestsEnabled() bool {
	if v, ok := os.LookupEnv("MACLAW_TEST_MODELS"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "off", "0", "false", "no", "skip", "disabled":
			return false
		}
		return true
	}
	return !testing.Short()
}

// SkipModelTests skips tb when model tests are disabled (see
// ModelTestsEnabled). Call it inside the helpers that locate real model
// files so every consumer inherits the gate.
func SkipModelTests(tb testing.TB) {
	tb.Helper()
	if !ModelTestsEnabled() {
		tb.Skip("model tests disabled (MACLAW_TEST_MODELS=off or -short)")
	}
}
