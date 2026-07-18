package corelib

import "github.com/RapidAI/CodeClaw/corelib/maclawpath"

// ExpandHomePath expands a leading "~" (or "~/…" / "~\…") to the real user
// home directory. See maclawpath.ExpandHomePath for semantics.
//
// Kept on package corelib as the public entry point used by GUI/TUI and
// config helpers; implementation lives in maclawpath so leaf packages
// (tool, remote, …) can share it without import cycles.
func ExpandHomePath(path string) string {
	return maclawpath.ExpandHomePath(path)
}

// ExpandHomePathWithHome expands tilde paths using an explicit home directory
// (useful when callers override home, e.g. tests or custom data roots).
func ExpandHomePathWithHome(path, home string) string {
	return maclawpath.ExpandHomePathWithHome(path, home)
}

// IsHomePath reports whether path is "~", "~/…", or "~\…".
func IsHomePath(path string) bool {
	return maclawpath.IsHomePath(path)
}
