//go:build !windows && !linux && !darwin

package archiveutil

import (
	"errors"
	"os"
)

// Platforms without a portable no-replace rename primitive retain the guarded
// fallback. The public desktop targets use the atomic implementations above.
func renameNoReplace(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}

func renameReplace(source, destination string) error {
	return os.Rename(source, destination)
}

func isNoReplaceCollision(err error) bool {
	return errors.Is(err, os.ErrExist)
}
