//go:build unix

package dbmaint

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

// FreeBytes is the filesystem space available to non-root users for the
// filesystem that holds path. VACUUM needs about the database size plus
// the configured headroom.
func FreeBytes(path string) (int64, error) {
	dir := path
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
		if dir == "" || dir == "." {
			dir = "."
		}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	if free > uint64(math.MaxInt64) {
		return math.MaxInt64, nil
	}
	return int64(free), nil
}
