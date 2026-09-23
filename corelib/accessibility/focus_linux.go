//go:build linux

package accessibility

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

type wmWindow struct {
	id    string
	title string
	x, y  int
	w, h  int
}

func listWmctrlWindows() []wmWindow {
	out, err := exec.Command("wmctrl", "-lG").Output()
	if err != nil {
		return nil
	}
	var hits []wmWindow
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		wx, errX := strconv.Atoi(fields[2])
		wy, errY := strconv.Atoi(fields[3])
		ww, errW := strconv.Atoi(fields[4])
		wh, errH := strconv.Atoi(fields[5])
		if errX != nil || errY != nil || errW != nil || errH != nil {
			continue
		}
		hits = append(hits, wmWindow{
			id:    fields[0],
			title: strings.Join(fields[7:], " "),
			x:     wx, y: wy, w: ww, h: wh,
		})
	}
	return hits
}

func bestWmctrlWindow(hint string) (wmWindow, bool) {
	return pickWmctrlWindow(listWmctrlWindows(), hint)
}

func pickWmctrlWindow(hits []wmWindow, hint string) (wmWindow, bool) {
	if len(hits) == 0 {
		return wmWindow{}, false
	}
	titles := make([]string, len(hits))
	for i, hit := range hits {
		titles[i] = hit.title
	}
	best, ok := computeruse.BestWindowTitle(hint, titles)
	if !ok {
		return wmWindow{}, false
	}
	want := computeruse.NormalizeWindowTitle(best)
	for _, hit := range hits {
		if computeruse.NormalizeWindowTitle(hit.title) == want && hit.w >= 64 && hit.h >= 64 {
			return hit, true
		}
	}
	return wmWindow{}, false
}

func focusWindow(titleSubstring string) error {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return fmt.Errorf("window title required")
	}
	if hits := listWmctrlWindows(); len(hits) > 0 {
		hit, ok := pickWmctrlWindow(hits, titleSubstring)
		if !ok {
			return fmt.Errorf("no visible window matching %q", titleSubstring)
		}
		if err := exec.Command("wmctrl", "-i", "-a", hit.id).Run(); err != nil {
			return fmt.Errorf("focus window %q: %w", hit.title, err)
		}
		return nil
	}
	// wmctrl listing unavailable: substring activate, then xdotool.
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
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return WindowBounds{}, false
	}
	hit, ok := bestWmctrlWindow(titleSubstring)
	if !ok {
		return WindowBounds{}, false
	}
	return WindowBounds{
		X: hit.x, Y: hit.y, Width: hit.w, Height: hit.h, Title: hit.title,
	}, true
}
