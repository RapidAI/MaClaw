//go:build !windows

package guiapp

import "path/filepath"

// remoteControlListRoots reports the top-level entries a directory browser
// can jump to: the filesystem root on Unix-likes.
func remoteControlListRoots() []string {
	return []string{string(filepath.Separator)}
}
