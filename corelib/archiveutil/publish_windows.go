//go:build windows

package archiveutil

import (
	"errors"

	"golang.org/x/sys/windows"
)

// MoveFile does not replace an existing destination. This closes the
// check-then-rename race for create_zip on Windows.
func renameNoReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFile(from, to)
}

// MoveFileEx with MOVEFILE_REPLACE_EXISTING gives merge callers a single
// operation that works when the target regular file already exists.
func renameReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING)
}

func isNoReplaceCollision(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}
