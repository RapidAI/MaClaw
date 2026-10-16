//go:build darwin

package accessibility

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func darwinWindowTitles() []string {
	out, err := exec.Command("osascript", "-e", `
tell application "System Events"
  set out to ""
  set procs to every process whose background only is false
  repeat with p in procs
    try
      repeat with w in windows of p
        set out to out & (name of w as string) & linefeed
      end repeat
    end try
  end repeat
  return out
end tell`).Output()
	if err != nil {
		return nil
	}
	var titles []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			titles = append(titles, line)
		}
	}
	return titles
}

// narrowDarwinWindowHint replaces hint with the best listed window title.
// When System Events cannot list windows, ok is false and the caller keeps
// the original contains search. When windows were listed and none score, miss
// is true so a loose contains search cannot focus the wrong app.
func narrowDarwinWindowHint(hint string) (resolved string, miss bool) {
	titles := darwinWindowTitles()
	if len(titles) == 0 {
		return hint, false
	}
	best, ok := computeruse.BestWindowTitle(hint, titles)
	if !ok {
		return "", true
	}
	return best, false
}

func focusWindow(titleSubstring string) error {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return fmt.Errorf("window title required")
	}
	if resolved, miss := narrowDarwinWindowHint(titleSubstring); miss {
		return fmt.Errorf("no visible window matching %q", titleSubstring)
	} else if resolved != "" {
		titleSubstring = resolved
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
	if resolved, miss := narrowDarwinWindowHint(titleSubstring); miss {
		return WindowBounds{}, false
	} else if resolved != "" {
		titleSubstring = resolved
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
