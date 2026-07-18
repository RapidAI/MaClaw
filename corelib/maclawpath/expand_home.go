package maclawpath

import (
	"os"
	"path/filepath"
	"strings"
)

// IsHomePath reports whether path is a current-user home marker:
// "~", "~/…", or "~\…". "~user" (other-user homes) is not matched.
func IsHomePath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`)
}

// ExpandHomePath expands a leading "~" (or "~/…" / "~\…") to the real user
// home directory from os.UserHomeDir. Non-home paths are returned unchanged
// after TrimSpace.
//
// Chat / agent replies and portable config values often use ~/… paths.
// Windows does not interpret "~", so OS open/stat fail without expansion.
//
// Implementation note: never filepath.Join(home, path[1:]) when path is "~/x"
// because path[1:] starts with a separator and Join treats it as absolute,
// dropping home on every GOOS.
func ExpandHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return ExpandHomePathWithHome(path, home)
}

// ExpandHomePathWithHome is like ExpandHomePath but uses the provided home
// directory (e.g. App.testHomeDir / GetUserHomeDir) instead of os.UserHomeDir.
// Empty home leaves tilde paths unexpanded.
func ExpandHomePathWithHome(path, home string) string {
	path = strings.TrimSpace(path)
	if path == "" || !IsHomePath(path) {
		return path
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return path
	}
	if path == "~" {
		return filepath.Clean(home)
	}
	return filepath.Clean(filepath.Join(home, strings.TrimLeft(path[1:], `/\`)))
}
