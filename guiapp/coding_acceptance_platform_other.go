//go:build !windows

package guiapp

func platformDesktopAcceptanceSession() (desktopAcceptanceSession, string) {
	return nil, "当前系统还没有窗口程序验收"
}

func platformAcceptanceListens(project string) []acceptanceListen {
	return nil
}
