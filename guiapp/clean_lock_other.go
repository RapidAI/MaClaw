//go:build !darwin

package guiapp

// cleanStaleLock is a no-op on non-macOS platforms.
// Windows uses a named mutex (not a file lock) for SingleInstanceLock,
// and Linux uses a similar flock mechanism that auto-releases on exit.
func cleanStaleLock() {}
