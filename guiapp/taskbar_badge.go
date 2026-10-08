package guiapp

// SetTaskbarBadgeCount mirrors the number of in-progress tasks onto the main
// window's taskbar button as an overlay badge (Windows ITaskbarList3, the
// same mechanism as the QQ unread-count badge). A count of zero clears the
// badge. Other platforms ignore the call.
//
// The count is computed by the frontend from the same live-running signals
// the sidebar "进行中" chip uses, so the badge always agrees with the task
// list the user sees.
func (a *App) SetTaskbarBadgeCount(count int) {
	notifyTaskbarBadgeCount(count)
}
