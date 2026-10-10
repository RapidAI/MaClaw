package guiapp

// SetTaskbarBadgeCount paints the given count on the main window's taskbar
// button as an overlay badge (Windows ITaskbarList3, the same mechanism as
// the QQ unread-count badge). A count of zero clears the badge. Other
// platforms ignore the call.
//
// The frontend pushes unread bot replies. When that count is zero the
// overlay is cleared. The left-rail 任务 badge keeps the running-task count.
func (a *App) SetTaskbarBadgeCount(count int) {
	notifyTaskbarBadgeCount(count)
}
