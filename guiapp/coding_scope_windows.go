//go:build windows

package guiapp

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func init() {
	codingDirectoryFinalPathOS = codingDirectoryFinalPathWindows
}

// codingDirectoryFinalPathWindows opens path so Windows follows reparse
// points, then asks for the DOS path of that handle. Readlink only
// understands symlinks and mount points; this covers the other directory
// links Lstat does not call directories.
func codingDirectoryFinalPathWindows(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	h, err := windows.CreateFile(
		p16,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		// VOLUME_NAME_DOS is 0. This x/sys revision does not export the name.
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			if n > uint32(len(buf)) {
				buf = make([]uint16, n)
				continue
			}
			return "", false
		}
		if n == 0 {
			return "", false
		}
		if n < uint32(len(buf)) {
			buf = buf[:n]
			break
		}
		buf = make([]uint16, n+1)
	}
	final := codingStripExtendedPath(windows.UTF16ToString(buf))
	abs, err := filepath.Abs(final)
	if err != nil || strings.TrimSpace(abs) == "" {
		return "", false
	}
	return filepath.Clean(abs), true
}
