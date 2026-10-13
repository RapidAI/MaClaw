package guiapp

// SetTaskbarBadgeCount paints the given count on the main window's taskbar
// button as an overlay badge (Windows ITaskbarList3, the same mechanism as
// the QQ unread-count badge). A count of zero clears the badge. Other
// platforms ignore the call.
//
// The frontend pushes how many tasks are still in progress: task-list rows
// in the 进行中 bucket, in-progress bot tasks, running background loops, and
// live AI sessions. A session already covered by one of those rows, and a
// loop whose remote session is already in the count, are not added again.
func (a *App) SetTaskbarBadgeCount(count int) {
	notifyTaskbarBadgeCount(count)
}
