//go:build windows

package guiapp

import "golang.org/x/sys/windows"

// sealSkillSubtreeListing holds dir open with no sharing. The parent stays
// listable, and a later listing of dir fails with a sharing error rather
// than a missing path. The returned function releases the handle.
func sealSkillSubtreeListing(dir string) (func(), error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
