//go:build linux

package guiautomation

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// linuxInputSimulator implements InputSimulator on Linux via xdotool.
type linuxInputSimulator struct{}

// NewInputSimulator creates a Linux InputSimulator backed by xdotool.
func NewInputSimulator() InputSimulator { return &linuxInputSimulator{} }

// runXdotool executes an xdotool command with the given arguments.
func runXdotool(args ...string) error {
	if _, err := exec.LookPath("xdotool"); err == nil {
		cmd := exec.Command("xdotool", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("xdotool %s failed: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("ydotool"); err == nil {
			return runYdotool(args)
		}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return fmt.Errorf("no Linux input backend: install xdotool (X11) or ydotool (Wayland)")
	}
	return fmt.Errorf("xdotool not found: install xdotool to simulate input on Linux")
}

func runYdotool(xdotoolArgs []string) error {
	ydArgs, err := mapXdotoolToYdotool(xdotoolArgs)
	if err != nil {
		return err
	}
	cmd := exec.Command("ydotool", ydArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ydotool %s failed: %w (output: %s)", strings.Join(ydArgs, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func mapXdotoolToYdotool(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty ydotool mapping")
	}
	switch args[0] {
	case "mousemove":
		if len(args) < 3 {
			return nil, fmt.Errorf("mousemove needs x y")
		}
		return []string{"mousemove", "--absolute", args[1], args[2]}, nil
	case "click":
		btn := "0xC0"
		repeat := 1
		for i := 1; i < len(args); i++ {
			if args[i] == "--repeat" && i+1 < len(args) {
				n, _ := strconv.Atoi(args[i+1])
				if n > 0 {
					repeat = n
				}
				i++
				continue
			}
			if args[i] == "--delay" {
				i++
				continue
			}
			switch args[i] {
			case "1":
				btn = "0xC0"
			case "3":
				btn = "0xC1"
			case "4":
				btn = "0xC3"
			case "5":
				btn = "0xC4"
			}
		}
		out := []string{"click"}
		for i := 0; i < repeat; i++ {
			out = append(out, btn)
		}
		return out, nil
	case "type":
		text := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--clearmodifiers" {
				continue
			}
			text = args[i]
		}
		return []string{"type", "--", text}, nil
	case "key":
		if len(args) < 2 {
			return nil, fmt.Errorf("key combo required")
		}
		return []string{"key", args[1]}, nil
	case "mousedown":
		return []string{"click", "0x40"}, nil
	case "mouseup":
		return []string{"click", "0x80"}, nil
	default:
		return nil, fmt.Errorf("ydotool cannot map xdotool %q", args[0])
	}
}

func (l *linuxInputSimulator) Click(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "1")
}

func (l *linuxInputSimulator) RightClick(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "3")
}

func (l *linuxInputSimulator) DoubleClick(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "--repeat", "2", "--delay", "50", "1")
}

func (l *linuxInputSimulator) Type(text string) error {
	return runXdotool("type", "--clearmodifiers", text)
}

// xdotoolKeyMap maps common key names to xdotool key identifiers.
var xdotoolKeyMap = map[string]string{
	"ctrl": "ctrl", "control": "ctrl",
	"alt": "alt", "shift": "shift",
	"win": "super", "super": "super", "meta": "super",
	"enter": "Return", "return": "Return",
	"tab": "Tab", "space": "space",
	"backspace": "BackSpace", "delete": "Delete", "del": "Delete",
	"escape": "Escape", "esc": "Escape",
	"insert": "Insert",
	"home":   "Home", "end": "End",
	"pageup": "Prior", "pagedown": "Next",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"printscreen": "Print",
	"capslock":    "Caps_Lock",
	"f1":          "F1", "f2": "F2", "f3": "F3", "f4": "F4",
	"f5": "F5", "f6": "F6", "f7": "F7", "f8": "F8",
	"f9": "F9", "f10": "F10", "f11": "F11", "f12": "F12",
}

// resolveXdotoolKey maps a key name to its xdotool equivalent.
func resolveXdotoolKey(key string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	if mapped, ok := xdotoolKeyMap[k]; ok {
		return mapped, nil
	}
	// Single character keys (letters and digits) are passed as-is.
	if len(k) == 1 {
		c := k[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown key: %q", key)
}

func (l *linuxInputSimulator) KeyCombo(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	mapped := make([]string, 0, len(keys))
	for _, k := range keys {
		mk, err := resolveXdotoolKey(k)
		if err != nil {
			return err
		}
		mapped = append(mapped, mk)
	}
	// xdotool key expects "key1+key2+..." format.
	return runXdotool("key", strings.Join(mapped, "+"))
}

func (l *linuxInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	if deltaY == 0 {
		return nil
	}
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	// xdotool: button 4 = scroll up, button 5 = scroll down.
	// computer_scroll: positive deltaY means scroll down (content moves up).
	button := "4" // up
	if deltaY > 0 {
		button = "5" // down
	}
	clicks := int(math.Abs(float64(deltaY)))
	if clicks == 0 {
		clicks = 1
	}
	return runXdotool("click", "--repeat", strconv.Itoa(clicks), button)
}

func (l *linuxInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	if err := runXdotool("mousemove", strconv.Itoa(fromX), strconv.Itoa(fromY)); err != nil {
		return err
	}
	if err := runXdotool("mousedown", "1"); err != nil {
		return err
	}
	if err := runXdotool("mousemove", strconv.Itoa(toX), strconv.Itoa(toY)); err != nil {
		return err
	}
	return runXdotool("mouseup", "1")
}
