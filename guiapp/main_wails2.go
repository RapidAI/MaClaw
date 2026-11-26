//go:build !wails3

package guiapp

// Default (v2) build: Main3 forwards to the v2 Main in main.go so the
// cmd/maclaw-gui entry can dispatch on wails3Enabled uniformly.

// wails3Enabled is the build-tag dispatch flag read by cmd/maclaw-gui.
const wails3Enabled = false

// Main3 forwards to the v2 Main under the default build.
func Main3(v string) { Main(v) }

// Wails3Enabled reports whether this binary was built with the wails3 tag.
func Wails3Enabled() bool { return wails3Enabled }
