//go:build windows

package guiapp

import (
	"runtime"
	"syscall"
	"unsafe"
)

// The hover sentence is a system tooltip. It is not a status panel: it is
// shown only while the pointer is over the pet, and it goes away on leave,
// drag, hide, and when the settle pose expires.
//
// Mouse capture cancels TrackMouseEvent. A WM_MOUSELEAVE delivered for that
// cancellation is not the cursor leaving, and the request has to be made
// again after the button comes up. One failed tooltip create is remembered
// for the rest of that visit and tried again on the next one.

const (
	ttfIDIsHWND      = 0x0001
	ttfTrack         = 0x0020
	ttfAbsolute      = 0x0080
	ttfTransparent   = 0x0100
	ttsAlwaysTip     = 0x01
	ttsNoPrefix      = 0x02
	ttsNoAnimate     = 0x10
	ttsNoFade        = 0x20
	wmUser           = 0x0400
	ttmAddTool       = wmUser + 50
	ttmTrackActivate = wmUser + 17
	ttmTrackPosition = wmUser + 18
	ttmSetMaxWidth   = wmUser + 24
	ttmGetBubble     = wmUser + 30
	ttmUpdateText    = wmUser + 57
	wsExNoActivate   = 0x08000000
	iccWin95Classes  = 0x000000FF
	tmeLeave         = 0x00000002
)

type toolInfoW struct {
	CbSize     uint32
	UFlags     uint32
	Hwnd       uintptr
	UId        uintptr
	Rect       rectW
	Hinst      uintptr
	LpszText   *uint16
	LParam     uintptr
	LpReserved uintptr
}

type trackMouseEventW struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type initCommonControlsEx struct {
	DwSize uint32
	DwICC  uint32
}

var (
	procInitCommonControlsEx = syscall.NewLazyDLL("comctl32.dll").NewProc("InitCommonControlsEx")
	petPresenceControlsOnce  bool
)

func (w *windowsFloatingWindow) SetPetPresenceLine(line string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.dutyLine = line
	w.refreshPresenceLocked()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmPetPresence, 0, 0)
	}
}

// petPresenceOnLeave is how a WM_MOUSELEAVE affects the visit. Capture
// cancels leave tracking and Windows may post the leave for that
// cancellation while the cursor is still on the pet. That notice only
// forgets the dead request. A leave without capture ends the visit.
func petPresenceOnLeave(captured bool) (endVisit bool) {
	return !captured
}

// petPresenceTrackNeeded is false when the tooltip is already showing this
// sentence at this point. Sending TTM_TRACKPOSITION again restarts the bubble.
func petPresenceTrackNeeded(active bool, shown, line string, tracked bool, lastX, lastY, x, y int) bool {
	if !active || shown != line || !tracked {
		return true
	}
	return lastX != x || lastY != y
}

// petPointInRect reports whether a screen point lies in a window rectangle.
// The right and bottom edges are exclusive, matching PtInRect.
func petPointInRect(x, y, left, top, right, bottom int) bool {
	return x >= left && y >= top && x < right && y < bottom
}

func (w *windowsFloatingWindow) armPetPointerLeave(hwnd uintptr) {
	if w == nil {
		return
	}
	w.mu.Lock()
	// A new visit may try the tooltip again. A create that failed earlier
	// in this same visit stays failed until the pointer leaves and returns,
	// so a mouse-move storm does not call CreateWindow on every pixel.
	if !w.petPointerOver {
		w.presenceBroken = false
	}
	w.petPointerOver = true
	armed := w.petLeaveArmed
	if !armed && hwnd != 0 {
		w.petLeaveArmed = true
	}
	w.mu.Unlock()
	if armed || hwnd == 0 {
		return
	}
	track := trackMouseEventW{
		CbSize:    uint32(unsafe.Sizeof(trackMouseEventW{})),
		DwFlags:   tmeLeave,
		HwndTrack: hwnd,
	}
	r, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&track)))
	if r == 0 {
		w.mu.Lock()
		w.petLeaveArmed = false
		w.mu.Unlock()
	}
}

func (w *windowsFloatingWindow) dismissPetPresence() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.petPointerOver = false
	// The drag is not a hover. Leave tracking was already cancelled by the
	// capture that started the press; forget it so the next visit asks again.
	w.petLeaveArmed = false
	w.mu.Unlock()
	w.syncPetPresence()
}

// settlePetPointer runs when the pet stops holding the pointer. Capture
// cancelled leave tracking, so the visit is a fresh hit test: still on her
// means ask for leave again, anywhere else hides the sentence.
func (w *windowsFloatingWindow) settlePetPointer(hwnd uintptr) {
	if w == nil {
		return
	}
	if petCursorOverWindow(hwnd) {
		w.armPetPointerLeave(hwnd)
		w.syncPetPresence()
		return
	}
	w.mu.Lock()
	w.petPointerOver = false
	w.petLeaveArmed = false
	w.mu.Unlock()
	w.syncPetPresence()
}

func petCursorOverWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var pt point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return false
	}
	var rc rectW
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return false
	}
	return petPointInRect(int(pt.X), int(pt.Y), int(rc.Left), int(rc.Top), int(rc.Right), int(rc.Bottom))
}

func (w *windowsFloatingWindow) destroyPetPresence() {
	if w == nil {
		return
	}
	w.mu.Lock()
	tip := w.presenceTip
	w.presenceTip = 0
	w.presenceAdded = false
	w.presenceActive = false
	w.presenceShown = ""
	w.presenceTracked = false
	w.mu.Unlock()
	if tip != 0 {
		procDestroyWindowProc.Call(tip)
	}
}

// syncPetPresence runs on the pet's window thread. Other threads post
// wmPetPresence instead of calling it.
func (w *windowsFloatingWindow) syncPetPresence() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.destroying {
		w.mu.Unlock()
		return
	}
	line := w.presenceLine
	over := w.petPointerOver
	hwnd := w.hwnd
	tip := w.presenceTip
	added := w.presenceAdded
	active := w.presenceActive
	shown := w.presenceShown
	broken := w.presenceBroken
	tracked := w.presenceTracked
	trackX := w.presenceTrackX
	trackY := w.presenceTrackY
	bubbleW := w.presenceBubbleW
	bubbleH := w.presenceBubbleH
	x := w.windowStartX
	y := w.windowStartY
	sz := w.size
	w.mu.Unlock()
	if hwnd == 0 || line == "" || !over {
		w.deactivatePetPresence(tip, hwnd, active)
		return
	}
	if tip == 0 {
		if broken {
			return
		}
		tip = createPetPresenceTip(hwnd)
		if tip == 0 {
			w.mu.Lock()
			w.presenceBroken = true
			w.mu.Unlock()
			return
		}
		w.mu.Lock()
		if w.destroying || w.hwnd != hwnd {
			w.mu.Unlock()
			procDestroyWindowProc.Call(tip)
			return
		}
		w.presenceTip = tip
		w.mu.Unlock()
		procSendMessageW.Call(tip, ttmSetMaxWidth, 0, ^uintptr(0))
	}
	text, textPtr := w.presenceText(line)
	info := w.presenceTool(hwnd, textPtr)
	if !added {
		r, _, _ := procSendMessageW.Call(tip, ttmAddTool, 0, uintptr(unsafe.Pointer(&info)))
		runtime.KeepAlive(text)
		runtime.KeepAlive(&info)
		if r == 0 {
			w.mu.Lock()
			w.presenceBroken = true
			w.mu.Unlock()
			w.destroyPetPresence()
			return
		}
		w.mu.Lock()
		w.presenceAdded = true
		w.mu.Unlock()
	} else if shown != line {
		procSendMessageW.Call(tip, ttmUpdateText, 0, uintptr(unsafe.Pointer(&info)))
		runtime.KeepAlive(text)
		runtime.KeepAlive(&info)
	}
	if shown != line {
		w.mu.Lock()
		w.presenceShown = line
		w.mu.Unlock()
	}
	var rc rectW
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	workLeft, workTop, workRight, workBottom := 0, 0, 0, 0
	if area, ok := petMonitorWorkArea(x, y, sz); ok {
		workLeft, workTop, workRight, workBottom = area.Left, area.Top, area.Right, area.Bottom
	}
	bw, bh := bubbleW, bubbleH
	if shown != line || bw < 1 || bh < 1 {
		bw, bh = petPresenceBubble(tip, &info)
		w.mu.Lock()
		w.presenceBubbleW = bw
		w.presenceBubbleH = bh
		w.mu.Unlock()
	}
	runtime.KeepAlive(text)
	px, py, ok := petPresenceTipPoint(int(rc.Left), int(rc.Top), int(rc.Right), int(rc.Bottom), bw, bh, workLeft, workTop, workRight, workBottom)
	if !ok {
		w.deactivatePetPresence(tip, hwnd, true)
		return
	}
	if !petPresenceTrackNeeded(active, shown, line, tracked, trackX, trackY, px, py) {
		return
	}
	procSendMessageW.Call(tip, ttmTrackPosition, 0, presenceLParam(px, py))
	if !active {
		info = w.presenceTool(hwnd, textPtr)
		procSendMessageW.Call(tip, ttmTrackActivate, 1, uintptr(unsafe.Pointer(&info)))
		runtime.KeepAlive(text)
		runtime.KeepAlive(&info)
		w.mu.Lock()
		w.presenceActive = true
		w.mu.Unlock()
	}
	w.mu.Lock()
	if w.presenceTip == tip {
		w.presenceTracked = true
		w.presenceTrackX = px
		w.presenceTrackY = py
	}
	w.mu.Unlock()
}

func (w *windowsFloatingWindow) deactivatePetPresence(tip, hwnd uintptr, active bool) {
	if w == nil || tip == 0 || !active {
		return
	}
	w.mu.Lock()
	var textPtr *uint16
	if len(w.presenceUTF16) > 0 {
		textPtr = &w.presenceUTF16[0]
	}
	w.mu.Unlock()
	info := w.presenceTool(hwnd, textPtr)
	procSendMessageW.Call(tip, ttmTrackActivate, 0, uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(w.presenceUTF16)
	w.mu.Lock()
	if w.presenceTip == tip {
		w.presenceActive = false
		w.presenceTracked = false
	}
	w.mu.Unlock()
}

func (w *windowsFloatingWindow) presenceText(line string) ([]uint16, *uint16) {
	text, err := syscall.UTF16FromString(line)
	if err != nil || len(text) == 0 {
		text = []uint16{0}
	}
	w.mu.Lock()
	w.presenceUTF16 = text
	w.mu.Unlock()
	return text, &text[0]
}

func (w *windowsFloatingWindow) presenceTool(hwnd uintptr, text *uint16) toolInfoW {
	return toolInfoW{
		CbSize:   uint32(unsafe.Sizeof(toolInfoW{})),
		UFlags:   ttfIDIsHWND | ttfTrack | ttfAbsolute | ttfTransparent,
		Hwnd:     hwnd,
		UId:      hwnd,
		LpszText: text,
	}
}

func petPresenceBubble(tip uintptr, info *toolInfoW) (int, int) {
	r, _, _ := procSendMessageW.Call(tip, ttmGetBubble, 0, uintptr(unsafe.Pointer(info)))
	bw := int(uint16(r))
	bh := int(uint16(r >> 16))
	if bw < 1 {
		bw = 160
	}
	if bh < 1 {
		bh = 22
	}
	return bw, bh
}

func presenceLParam(x, y int) uintptr {
	return uintptr(uint16(int16(x))) | uintptr(uint16(int16(y)))<<16
}

func createPetPresenceTip(owner uintptr) uintptr {
	if !petPresenceControlsOnce {
		icc := initCommonControlsEx{DwSize: uint32(unsafe.Sizeof(initCommonControlsEx{})), DwICC: iccWin95Classes}
		procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
		petPresenceControlsOnce = true
	}
	className, err := syscall.UTF16PtrFromString("tooltips_class32")
	if err != nil {
		return 0
	}
	hinst, _, _ := procGetModuleHandleW.Call(0)
	ex := uintptr(wsExTopmost | wsExToolwindow | wsExNoActivate)
	style := uintptr(wsPopup | ttsAlwaysTip | ttsNoPrefix | ttsNoAnimate | ttsNoFade)
	tip, _, _ := procCreateWindowExW.Call(
		ex,
		uintptr(unsafe.Pointer(className)),
		0,
		style,
		0, 0, 0, 0,
		owner,
		0,
		hinst,
		0,
	)
	runtime.KeepAlive(className)
	return tip
}
