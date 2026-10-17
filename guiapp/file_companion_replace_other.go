//go:build !windows

package guiapp

import "os"

// renameReplacing moves src onto dst. On this OS rename replaces an existing
// destination in one step.
func renameReplacing(src, dst string) error {
	return os.Rename(src, dst)
}
