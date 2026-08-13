//go:build linux

package archiveutil

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameNoReplace(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}

func renameReplace(source, destination string) error {
	return unix.Rename(source, destination)
}

func isNoReplaceCollision(err error) bool {
	return errors.Is(err, unix.EEXIST)
}
