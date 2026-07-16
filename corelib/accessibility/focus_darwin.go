//go:build darwin

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
	// Escape for AppleScript string.
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(titleSubstring)
	script := fmt.Sprintf(`
tell application "System Events"
  set procs to every process whose background only is false
  repeat with p in procs
    try
      repeat with w in windows of p
        set t to name of w as string
        if t contains "%s" then
          set frontmost of p to true
          try
            perform action "AXRaise" of w
          end try
          return t
        end if
      end repeat
    end try
  end repeat
end tell
error "not found"
`, esc)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("focus window %q: %w (%s)", titleSubstring, err, strings.TrimSpace(string(out)))
	}
	return nil
}
