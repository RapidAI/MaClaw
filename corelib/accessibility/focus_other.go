//go:build !windows && !darwin && !linux

package accessibility

import "fmt"

func focusWindow(titleSubstring string) error {
	return fmt.Errorf("FocusWindow not supported on this platform")
}
