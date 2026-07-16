//go:build windows

package guiautomation

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// windowsInputSimulator drives mouse/keyboard via user32 (no PowerShell per action).
type windowsInputSimulator struct{}

func NewInputSimulator() InputSimulator { return &windowsInputSimulator{} }

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procMouseEvent       = user32.NewProc("mouse_event")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSendInput        = user32.NewProc("SendInput")
)

const (
	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventWheel      = 0x0800
	mouseEventHWheel     = 0x01000
	inputKeyboard        = 1
	keyeventfKeyUp       = 0x0002
	keyeventfUnicode     = 0x0004
	keyeventfScancode    = 0x0008
	smCXScreen           = 0
	smCYScreen           = 1
)

// winInput matches the Windows INPUT structure (amd64 size 40).
// Layout: DWORD type + 4-byte pad + 32-byte union (KEYBDINPUT padded).
type winInput struct {
	Type uint32
	_    uint32
	Ki   winKeybdInput
}

type winKeybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
	// Pad KEYBDINPUT (20/24) up to MOUSEINPUT union size (32 on amd64).
	_ [8]byte
}

func clampXY(x, y int) (int, int) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	if w > 0 {
		if x < 0 {
			x = 0
		}
		if x >= int(w) {
			x = int(w) - 1
		}
	}
	if h > 0 {
		if y < 0 {
			y = 0
		}
		if y >= int(h) {
			y = int(h) - 1
		}
	}
	return x, y
}

func setCursor(x, y int) error {
	x, y = clampXY(x, y)
	r, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if r == 0 {
		return fmt.Errorf("SetCursorPos: %v", err)
	}
	return nil
}

func mouseFlags(flags uint32, data int32) {
	procMouseEvent.Call(uintptr(flags), 0, 0, uintptr(data), 0)
}

func (w *windowsInputSimulator) Click(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	mouseFlags(mouseEventLeftDown, 0)
	mouseFlags(mouseEventLeftUp, 0)
	return nil
}

func (w *windowsInputSimulator) RightClick(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	mouseFlags(mouseEventRightDown, 0)
	mouseFlags(mouseEventRightUp, 0)
	return nil
}

func (w *windowsInputSimulator) DoubleClick(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	mouseFlags(mouseEventLeftDown, 0)
	mouseFlags(mouseEventLeftUp, 0)
	time.Sleep(40 * time.Millisecond)
	mouseFlags(mouseEventLeftDown, 0)
	mouseFlags(mouseEventLeftUp, 0)
	return nil
}

func sendInputs(inputs []winInput) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(inputs[0])),
	)
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput: sent %d/%d: %v", n, len(inputs), err)
	}
	return nil
}

func (w *windowsInputSimulator) Type(text string) error {
	if text == "" {
		return nil
	}
	// KEYEVENTF_UNICODE wants UTF-16 code units.
	units := utf16.Encode([]rune(text))
	inputs := make([]winInput, 0, len(units)*2)
	for _, u := range units {
		inputs = append(inputs, winInput{
			Type: inputKeyboard,
			Ki:   winKeybdInput{Scan: u, Flags: keyeventfUnicode},
		})
		inputs = append(inputs, winInput{
			Type: inputKeyboard,
			Ki:   winKeybdInput{Scan: u, Flags: keyeventfUnicode | keyeventfKeyUp},
		})
	}
	// Chunk to avoid huge stacks / driver limits.
	const chunk = 64
	for i := 0; i < len(inputs); i += chunk {
		j := i + chunk
		if j > len(inputs) {
			j = len(inputs)
		}
		if err := sendInputs(inputs[i:j]); err != nil {
			return err
		}
	}
	return nil
}

var vkMap = map[string]byte{
	"ctrl": 0x11, "control": 0x11, "alt": 0x12, "shift": 0x10, "win": 0x5B, "tab": 0x09,
	"enter": 0x0D, "return": 0x0D, "esc": 0x1B, "escape": 0x1B, "backspace": 0x08,
	"delete": 0x2E, "del": 0x2E, "space": 0x20, "insert": 0x2D,
	"home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22, "printscreen": 0x2C,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
	"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
}

func resolveVK(key string) (byte, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	if vk, ok := vkMap[k]; ok {
		return vk, nil
	}
	if len(k) == 1 {
		c := k[0]
		if c >= 'a' && c <= 'z' {
			return c - 32, nil
		}
		if c >= '0' && c <= '9' {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown key: %q", key)
}

func (w *windowsInputSimulator) KeyCombo(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	vks := make([]byte, 0, len(keys))
	for _, k := range keys {
		vk, err := resolveVK(k)
		if err != nil {
			return err
		}
		vks = append(vks, vk)
	}
	inputs := make([]winInput, 0, len(vks)*2)
	for _, vk := range vks {
		inputs = append(inputs, winInput{
			Type: inputKeyboard,
			Ki:   winKeybdInput{Vk: uint16(vk)},
		})
	}
	for i := len(vks) - 1; i >= 0; i-- {
		inputs = append(inputs, winInput{
			Type: inputKeyboard,
			Ki:   winKeybdInput{Vk: uint16(vks[i]), Flags: keyeventfKeyUp},
		})
	}
	return sendInputs(inputs)
}

func (w *windowsInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	if deltaY != 0 {
		// WHEEL_DELTA = 120 per notch
		mouseFlags(mouseEventWheel, int32(deltaY*120))
	}
	if deltaX != 0 {
		mouseFlags(mouseEventHWheel, int32(deltaX*120))
	}
	return nil
}

func (w *windowsInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	if err := setCursor(fromX, fromY); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	mouseFlags(mouseEventLeftDown, 0)
	time.Sleep(30 * time.Millisecond)
	if err := setCursor(toX, toY); err != nil {
		mouseFlags(mouseEventLeftUp, 0)
		return err
	}
	time.Sleep(30 * time.Millisecond)
	mouseFlags(mouseEventLeftUp, 0)
	return nil
}
