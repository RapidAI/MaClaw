//go:build !windows

package guiapp

func allowCompanionForeground() {}

func revealHiddenCompanionWindow() bool { return false }
