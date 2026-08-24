//go:build linux

package accessibility

import (
	"fmt"
	"os/exec"
	"strconv"
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

// foregroundWindowTitle returns the active window name via xdotool ("" when
// xdotool is unavailable).
func foregroundWindowTitle() string {
	out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// windowTitleAtPoint hit-tests wmctrl window geometries and returns the
// smallest containing window title (avoids matching the desktop).
func windowTitleAtPoint(x, y int) string {
	out, err := exec.Command("wmctrl", "-lG").Output()
	if err != nil {
		return ""
	}
	bestTitle := ""
	bestArea := int64(1 << 62)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		wx, errX := strconv.Atoi(fields[2])
		wy, errY := strconv.Atoi(fields[3])
		ww, errW := strconv.Atoi(fields[4])
		wh, errH := strconv.Atoi(fields[5])
		if errX != nil || errY != nil || errW != nil || errH != nil || ww <= 0 || wh <= 0 {
			continue
		}
		if x < wx || y < wy || x >= wx+ww || y >= wy+wh {
			continue
		}
		area := int64(ww) * int64(wh)
		if area < bestArea {
			bestArea = area
			bestTitle = strings.Join(fields[7:], " ")
		}
	}
	return bestTitle
}

func foregroundWindowBounds() (WindowBounds, bool) {
	idOut, err := exec.Command("xdotool", "getactivewindow").Output()
	if err != nil {
		return namedWindowBounds(foregroundWindowTitle())
	}
	id := strings.TrimSpace(string(idOut))
	if id == "" {
		return WindowBounds{}, false
	}
	geo, err := exec.Command("xdotool", "getwindowgeometry", "--shell", id).Output()
	if err != nil {
		return WindowBounds{}, false
	}
	b := WindowBounds{Title: foregroundWindowTitle()}
	for _, line := range strings.Split(string(geo), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		switch k {
		case "X":
			b.X = n
		case "Y":
			b.Y = n
		case "WIDTH":
			b.Width = n
		case "HEIGHT":
			b.Height = n
		}
	}
	if b.Width < 64 || b.Height < 64 {
		return WindowBounds{}, false
	}
	return b, true
}

func namedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	titleSubstring = strings.ToLower(strings.TrimSpace(titleSubstring))
	if titleSubstring == "" {
		return WindowBounds{}, false
	}
	out, err := exec.Command("wmctrl", "-lG").Output()
	if err != nil {
		return WindowBounds{}, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		title := strings.ToLower(strings.Join(fields[7:], " "))
		if !strings.Contains(title, titleSubstring) {
			continue
		}
		wx, errX := strconv.Atoi(fields[2])
		wy, errY := strconv.Atoi(fields[3])
		ww, errW := strconv.Atoi(fields[4])
		wh, errH := strconv.Atoi(fields[5])
		if errX != nil || errY != nil || errW != nil || errH != nil || ww < 64 || wh < 64 {
			continue
		}
		return WindowBounds{
			X: wx, Y: wy, Width: ww, Height: wh,
			Title: strings.Join(fields[7:], " "),
		}, true
	}
	return WindowBounds{}, false
}
