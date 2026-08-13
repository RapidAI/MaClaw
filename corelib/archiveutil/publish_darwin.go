//go:build darwin

package archiveutil

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameNoReplace(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}

func renameReplace(source, destination string) error {
	return unix.Rename(source, destination)
}

func isNoReplaceCollision(err error) bool {
	return errors.Is(err, unix.EEXIST)
}
