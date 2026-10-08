//go:build !windows

package guiapp

// notifyTaskbarBadgeCount is a no-op off Windows; only the Windows taskbar
// supports the ITaskbarList3 overlay badge.
func notifyTaskbarBadgeCount(count int) {
	_ = count
}
