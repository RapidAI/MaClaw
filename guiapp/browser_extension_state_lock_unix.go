//go:build !windows

package guiapp

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockBrowserExtensionState(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockBrowserExtensionState(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}