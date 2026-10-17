//go:build !unix

package dbmaint

import "fmt"

// FreeBytes is unavailable on this OS. The conversion guard then refuses
// to VACUUM rather than guessing at free space.
func FreeBytes(path string) (int64, error) {
	return 0, fmt.Errorf("free disk check is not supported for %s", path)
}
