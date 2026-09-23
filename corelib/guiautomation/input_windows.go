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
	user32                    = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos          = user32.NewProc("SetCursorPos")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSendInput             = user32.NewProc("SendInput")
	procOpenClipboard         = user32.NewProc("OpenClipboard")
	procCloseClipboard        = user32.NewProc("CloseClipboard")
	procEmptyClipboard        = user32.NewProc("EmptyClipboard")
	procSetClipboardData      = user32.NewProc("SetClipboardData")
	procGetClipboardData      = user32.NewProc("GetClipboardData")
	procCountClipboardFormats = user32.NewProc("CountClipboardFormats")
	kernel32Clip              = syscall.NewLazyDLL("kernel32.dll")
	procGlobalAlloc           = kernel32Clip.NewProc("GlobalAlloc")
	procGlobalLock            = kernel32Clip.NewProc("GlobalLock")
	procGlobalUnlock          = kernel32Clip.NewProc("GlobalUnlock")
	procGlobalFree            = kernel32Clip.NewProc("GlobalFree")
)

const (
	mouseEventLeftDown  = 0x0002
	mouseEventLeftUp    = 0x0004
	mouseEventRightDown = 0x0008
	mouseEventRightUp   = 0x0010
	mouseEventWheel     = 0x0800
	mouseEventHWheel    = 0x01000
	inputMouse          = 0
	inputKeyboard       = 1
	keyeventfKeyUp      = 0x0002
	keyeventfUnicode    = 0x0004
	keyeventfScancode   = 0x0008
	smCXScreen          = 0
	smCYScreen          = 1
	smXVirtualScreen    = 76
	smYVirtualScreen    = 77
	smCXVirtualScreen   = 78
	smCYVirtualScreen   = 79
	cfUnicodeText       = 13
	gmemMoveable        = 0x0002
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

// winMouseInput is MOUSEINPUT. Go inserts 4 bytes before ExtraInfo so the
// struct is 32 bytes, matching the INPUT union on amd64.
type winMouseInput struct {
	Dx        int32
	Dy        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winInputMouse struct {
	Type uint32
	_    uint32
	Mi   winMouseInput
}

func systemMetric(idx uintptr) int {
	r, _, _ := procGetSystemMetrics.Call(idx)
	return int(int32(r))
}

func clampXY(x, y int) (int, int) {
	vx := systemMetric(smXVirtualScreen)
	vy := systemMetric(smYVirtualScreen)
	vw := systemMetric(smCXVirtualScreen)
	vh := systemMetric(smCYVirtualScreen)
	if vw <= 0 || vh <= 0 {
		vx, vy = 0, 0
		vw = systemMetric(smCXScreen)
		vh = systemMetric(smCYScreen)
	}
	return clampToVirtual(x, y, vx, vy, vw, vh)
}

func setCursor(x, y int) error {
	x, y = clampXY(x, y)
	r, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if r == 0 {
		return fmt.Errorf("SetCursorPos: %v", err)
	}
	return nil
}

func sendMouse(flags uint32, data int32) error {
	in := winInputMouse{
		Type: inputMouse,
		Mi: winMouseInput{
			Flags:     flags,
			MouseData: uint32(data),
		},
	}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if int(n) != 1 {
		return fmt.Errorf("SendInput mouse: %v", err)
	}
	return nil
}

func clickButtons(down, up uint32) error {
	if err := sendMouse(down, 0); err != nil {
		return err
	}
	// A short gap lets Qt, WeChat, and Office observe the press.
	time.Sleep(15 * time.Millisecond)
	return sendMouse(up, 0)
}

func (w *windowsInputSimulator) Click(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	return clickButtons(mouseEventLeftDown, mouseEventLeftUp)
}

func (w *windowsInputSimulator) RightClick(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	return clickButtons(mouseEventRightDown, mouseEventRightUp)
}

func (w *windowsInputSimulator) DoubleClick(x, y int) error {
	if err := setCursor(x, y); err != nil {
		return err
	}
	if err := clickButtons(mouseEventLeftDown, mouseEventLeftUp); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return clickButtons(mouseEventLeftDown, mouseEventLeftUp)
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
	if typeUsesClipboard(text) {
		if err := typeViaClipboard(w, text); err == nil {
			return nil
		}
		// Clipboard busy or holding a file/image: fall back to keystrokes.
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
		if err := sendMouse(mouseEventWheel, int32(deltaY*120)); err != nil {
			return err
		}
	}
	if deltaX != 0 {
		if err := sendMouse(mouseEventHWheel, int32(deltaX*120)); err != nil {
			return err
		}
	}
	return nil
}

func (w *windowsInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	if err := setCursor(fromX, fromY); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := sendMouse(mouseEventLeftDown, 0); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := setCursor(toX, toY); err != nil {
		_ = sendMouse(mouseEventLeftUp, 0)
		return err
	}
	time.Sleep(30 * time.Millisecond)
	return sendMouse(mouseEventLeftUp, 0)
}

func openClipboard() bool {
	for i := 0; i < 8; i++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func closeClipboard() {
	procCloseClipboard.Call()
}

func clipboardReadText() (string, bool) {
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	var units []uint16
	for i := 0; i < 1<<20; i++ {
		u := *(*uint16)(unsafe.Pointer(ptr + uintptr(i*2)))
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units)), true
}

func clipboardSetText(text string) error {
	units := utf16.Encode([]rune(text))
	n := (len(units) + 1) * 2
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(n))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock: %v", err)
	}
	for i, u := range units {
		*(*uint16)(unsafe.Pointer(ptr + uintptr(i*2))) = u
	}
	*(*uint16)(unsafe.Pointer(ptr + uintptr(len(units)*2))) = 0
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	return nil
}

// typeViaClipboard pastes text and restores a previous text clipboard.
// A non-text clipboard (files, images) is left alone so the caller can fall
// back to Unicode keystrokes.
func typeViaClipboard(w *windowsInputSimulator, text string) error {
	if !openClipboard() {
		return fmt.Errorf("clipboard busy")
	}
	formats, _, _ := procCountClipboardFormats.Call()
	prev, hadText := clipboardReadText()
	if formats > 0 && !hadText {
		closeClipboard()
		return fmt.Errorf("clipboard holds non-text data")
	}
	procEmptyClipboard.Call()
	if err := clipboardSetText(text); err != nil {
		closeClipboard()
		return err
	}
	closeClipboard()

	err := w.KeyCombo("ctrl", "v")
	time.Sleep(60 * time.Millisecond)
	if openClipboard() {
		procEmptyClipboard.Call()
		if hadText {
			_ = clipboardSetText(prev)
		}
		closeClipboard()
	}
	return err
}
