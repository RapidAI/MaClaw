//go:build linux

package accessibility

import (
	"fmt"
	"os/exec"
	"strings"
)

func focusWindow(titleSubstring string) error {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return fmt.Errorf("window title required")
	}
	// Prefer wmctrl -a (substring activate), then xdotool.
	if err := exec.Command("wmctrl", "-a", titleSubstring).Run(); err == nil {
		return nil
	}
	out, err := exec.Command("xdotool", "search", "--name", titleSubstring).CombinedOutput()
	if err != nil {
		return fmt.Errorf("focus window %q: install wmctrl or xdotool: %w", titleSubstring, err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return fmt.Errorf("no window matching %q", titleSubstring)
	}
	if err := exec.Command("xdotool", "windowactivate", ids[0]).Run(); err != nil {
		return fmt.Errorf("xdotool windowactivate: %w", err)
	}
	return nil
}
