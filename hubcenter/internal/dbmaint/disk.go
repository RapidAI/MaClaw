package dbmaint

import "os"

// FileSize returns the length of path. A missing file is size 0.
func FileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}
