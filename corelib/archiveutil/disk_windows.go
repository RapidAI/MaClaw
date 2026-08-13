//go:build windows

package archiveutil

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func availableBytes(path string) (int64, error) {
	root := filepath.VolumeName(path)
	if root == "" {
		root = path
	}
	if len(root) == 2 && root[1] == ':' {
		root += `\`
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(root), &available, &total, &free); err != nil {
		return 0, err
	}
	if available > uint64(^uint(0)>>1) {
		return int64(^uint(0) >> 1), nil
	}
	return int64(available), nil
}
