//go:build !windows

package main

import (
	"os"
	"syscall"
)

func launchCompanionGUI(gui string) int {
	// fds 0, 1, and 2 stay the browser's pipes. This process does not read or write them.
	err := syscall.Exec(gui, []string{gui, companionPaperHostArg}, os.Environ())
	if err != nil {
		return 1
	}
	return 0
}
