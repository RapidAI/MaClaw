//go:build !windows

package tinytex

import "os"

// RenameReplacing moves src onto dest. On Unix, rename replaces dest
// atomically and leaves it unchanged when the call fails.
func RenameReplacing(src, dest string) error {
	return os.Rename(src, dest)
}
