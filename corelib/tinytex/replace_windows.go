//go:build windows

package tinytex

import "golang.org/x/sys/windows"

// RenameReplacing moves src onto dest in one call. A failed call leaves dest
// in place, so callers never delete the live file first.
func RenameReplacing(src, dest string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
