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

// foregroundWindowTitle returns the frontmost window title via System Events
// ("" on failure, e.g. missing accessibility permission).
func foregroundWindowTitle() string {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first window of (first process whose frontmost is true)`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// windowTitleAtPoint is unsupported on macOS (no cheap point→window API);
// callers fall back to ForegroundWindowTitle.
func windowTitleAtPoint(x, y int) string {
	return ""
}

func foregroundWindowBounds() (WindowBounds, bool) {
	out, err := exec.Command("osascript", "-e", `
tell application "System Events"
  tell (first process whose frontmost is true)
    set p to position of first window
    set s to size of first window
    set t to name of first window as string
    return (item 1 of p as string) & "," & (item 2 of p as string) & "," & (item 1 of s as string) & "," & (item 2 of s as string) & "," & t
  end tell
end tell`).Output()
	if err != nil {
		return WindowBounds{}, false
	}
	return parseCSVWindowBounds(strings.TrimSpace(string(out)))
}

func namedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return WindowBounds{}, false
	}
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(titleSubstring)
	script := fmt.Sprintf(`
tell application "System Events"
  set procs to every process whose background only is false
  repeat with p in procs
    try
      repeat with w in windows of p
        set t to name of w as string
        if t contains "%s" then
          set pos to position of w
          set sz to size of w
          return (item 1 of pos as string) & "," & (item 2 of pos as string) & "," & (item 1 of sz as string) & "," & (item 2 of sz as string) & "," & t
        end if
      end repeat
    end try
  end repeat
end tell
error "not found"
`, esc)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return WindowBounds{}, false
	}
	return parseCSVWindowBounds(strings.TrimSpace(string(out)))
}
