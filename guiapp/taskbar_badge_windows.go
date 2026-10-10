//go:build windows

package guiapp

import (
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Windows taskbar badge: render the given count into a small red disc and
// set it as the ITaskbarList3 overlay icon of the main window's taskbar
// button — the same mechanism behind the QQ unread-count badge. The frontend
// pushes unread bot replies through App.SetTaskbarBadgeCount whenever that
// count changes; this file owns rendering and the win32 plumbing.
//
// All COM calls run on one dedicated locked OS thread (the worker goroutine)
// because ITaskbarList3 requires an initialized COM apartment.
//
// Known limitation: an explorer.exe restart drops per-window overlays. The
// badge self-heals on the next count change, since the frontend re-pushes
// when the unread count changes.

const (
	taskbarBadgeCoInitApartmentThreaded = 0x2
	taskbarBadgeClsCtxInProcServer      = 0x1

	taskbarBadgeSmCxIcon = 11

	// Vtable layout: 3 IUnknown methods, then ITaskbarList (HrInit, AddTab,
	// DeleteTab, ActivateTab, SetActiveAlt), then ITaskbarList2
	// (MarkFullscreenWindow) — SetOverlayIcon therefore lands at index 11.
	taskbarBadgeVtableRelease        = 2
	taskbarBadgeVtableHrInit         = 3
	taskbarBadgeVtableSetOverlayIcon = 11

	taskbarBadgeBkModeTransparent = 1
	taskbarBadgeFontWeightBold    = 700
	taskbarBadgeQualityAntialiase = 4

	taskbarBadgeDtCenter     = 0x1
	taskbarBadgeDtVcenter    = 0x4
	taskbarBadgeDtSingleLine = 0x20

	// Bounded retry while the main HWND is still missing (the Wails window
	// can lag the first pushes right after launch).
	taskbarBadgeHWNDTries     = 20
	taskbarBadgeHWNDRetryWait = 500 * time.Millisecond

	// taskbarBadgeColorRef is the disc fill, COLORREF 0x00BBGGRR for
	// RGB(230, 68, 68) — the QQ-style notification red.
	taskbarBadgeColorRef = 0x004444E6
)

type taskbarBadgeGUID struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

var (
	clsidTaskbarList = taskbarBadgeGUID{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList3 = taskbarBadgeGUID{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
)

var (
	taskbarBadgeOle32       = syscall.NewLazyDLL("ole32.dll")
	procBadgeCoInitialize   = taskbarBadgeOle32.NewProc("CoInitializeEx")
	procBadgeCoCreateInst   = taskbarBadgeOle32.NewProc("CoCreateInstance")
	procBadgeCoUninitialize = taskbarBadgeOle32.NewProc("CoUninitialize")

	taskbarBadgeUser32          = syscall.NewLazyDLL("user32.dll")
	procBadgeSystemMetrics      = taskbarBadgeUser32.NewProc("GetSystemMetrics")
	procBadgeIsWindow           = taskbarBadgeUser32.NewProc("IsWindow")
	procBadgeDrawText           = taskbarBadgeUser32.NewProc("DrawTextW")
	procBadgeCreateIconIndirect = taskbarBadgeUser32.NewProc("CreateIconIndirect")
	procBadgeDestroyIcon        = taskbarBadgeUser32.NewProc("DestroyIcon")

	taskbarBadgeGdi32           = syscall.NewLazyDLL("gdi32.dll")
	procBadgeSetBkMode          = taskbarBadgeGdi32.NewProc("SetBkMode")
	procBadgeSetTextColor       = taskbarBadgeGdi32.NewProc("SetTextColor")
	procBadgeCreateCompatibleDC = taskbarBadgeGdi32.NewProc("CreateCompatibleDC")
	procBadgeDeleteDC           = taskbarBadgeGdi32.NewProc("DeleteDC")
	procBadgeCreateDIBSection   = taskbarBadgeGdi32.NewProc("CreateDIBSection")
	procBadgeSelectObject       = taskbarBadgeGdi32.NewProc("SelectObject")
	procBadgeDeleteObject       = taskbarBadgeGdi32.NewProc("DeleteObject")
	procBadgeCreateSolidBrush   = taskbarBadgeGdi32.NewProc("CreateSolidBrush")
	procBadgeCreatePen          = taskbarBadgeGdi32.NewProc("CreatePen")
	procBadgeEllipse            = taskbarBadgeGdi32.NewProc("Ellipse")
	procBadgeCreateFont         = taskbarBadgeGdi32.NewProc("CreateFontW")
	procBadgeCreateBitmap       = taskbarBadgeGdi32.NewProc("CreateBitmap")
)

var (
	taskbarBadgeStartOnce sync.Once
	taskbarBadgeRequests  chan int

	// Serializes the coalescing send below so two concurrent producers can
	// not drain each other's queued request and lose the latest count.
	taskbarBadgeMu sync.Mutex

	taskbarBadgeHWNDCacheMu sync.Mutex
	taskbarBadgeHWNDCache   uintptr
)

// notifyTaskbarBadgeCount wakes the worker with the desired count.
// Requests are coalesced so a burst of count changes costs one overlay set.
func notifyTaskbarBadgeCount(count int) {
	taskbarBadgeStartOnce.Do(func() {
		taskbarBadgeRequests = make(chan int, 1)
		go taskbarBadgeWorker(taskbarBadgeRequests)
	})
	taskbarBadgeMu.Lock()
	defer taskbarBadgeMu.Unlock()
	select {
	case taskbarBadgeRequests <- count:
		return
	default:
	}
	// The queued request may carry a stale count; replace it.
	select {
	case <-taskbarBadgeRequests:
	default:
	}
	select {
	case taskbarBadgeRequests <- count:
	default:
	}
}

func taskbarBadgeWorker(requests <-chan int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The badge thread owns its COM apartment for the process lifetime.
	hr, _, _ := procBadgeCoInitialize.Call(0, taskbarBadgeCoInitApartmentThreaded)
	if hr == 0 || hr == 1 { // S_OK / S_FALSE
		defer procBadgeCoUninitialize.Call()
	}

	taskbar, err := taskbarBadgeCreateTaskbarList3()
	if err != nil {
		log.Printf("[taskbar-badge] overlay unsupported: %v", err)
		for range requests {
		}
		return
	}
	defer taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableRelease)

	var liveIcon uintptr
	for count := range requests {
		liveIcon = taskbarBadgeServeCount(taskbar, count, liveIcon)
	}
}

// taskbarBadgeServeCount renders and applies a single badge request on the
// worker thread and returns the icon that must stay alive afterwards. The
// badge is purely decorative: any failure here is logged and dropped, never
// allowed to take the whole app down.
func taskbarBadgeServeCount(taskbar unsafe.Pointer, count int, liveIcon uintptr) (nextLive uintptr) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[taskbar-badge] panic suppressed: %v", r)
			nextLive = liveIcon
		}
	}()
	hwnd := taskbarBadgeHWND()
	for attempt := 1; hwnd == 0 && attempt < taskbarBadgeHWNDTries; attempt++ {
		time.Sleep(taskbarBadgeHWNDRetryWait)
		hwnd = taskbarBadgeHWND()
	}
	if hwnd == 0 {
		// No window yet after the bounded retry; the next count change
		// starts a fresh attempt.
		return liveIcon
	}
	icon := taskbarBadgeRenderIcon(count)
	if icon == 0 && count > 0 {
		// Render failed; keep the overlay that is already in place
		// rather than silently clearing a live count.
		return liveIcon
	}
	if hr := taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableSetOverlayIcon, hwnd, icon, 0); hr != 0 {
		log.Printf("[taskbar-badge] SetOverlayIcon failed: hr=0x%08x", uint32(hr))
		if icon != 0 {
			procBadgeDestroyIcon.Call(icon)
		}
		return liveIcon
	}
	if liveIcon != 0 {
		procBadgeDestroyIcon.Call(liveIcon)
	}
	return icon
}

// taskbarBadgeCreateTaskbarList3 creates and initializes the shell taskbar
// list object on the calling (COM-owning) thread.
func taskbarBadgeCreateTaskbarList3() (unsafe.Pointer, error) {
	var taskbar unsafe.Pointer
	hr, _, _ := procBadgeCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		taskbarBadgeClsCtxInProcServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)),
		uintptr(unsafe.Pointer(&taskbar)),
	)
	if hr != 0 || taskbar == nil {
		return nil, fmt.Errorf("CoCreateInstance(ITaskbarList3) hr=0x%08x", uint32(hr))
	}
	if hr := taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableHrInit); hr != 0 {
		taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableRelease)
		return nil, fmt.Errorf("ITaskbarList3::HrInit hr=0x%08x", uint32(hr))
	}
	return taskbar, nil
}

// taskbarBadgeVtableProc resolves a COM interface method address: the
// interface pointer's first field is the vtable pointer, whose entries are
// plain function pointers.
func taskbarBadgeVtableProc(itf unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(itf)
	return uintptr(*(*unsafe.Pointer)(unsafe.Add(vtable, uintptr(index)*unsafe.Sizeof(uintptr(0)))))
}

// taskbarBadgeVtableCall invokes a COM interface method through its vtable.
func taskbarBadgeVtableCall(itf unsafe.Pointer, index int, args ...uintptr) uintptr {
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(itf))
	callArgs = append(callArgs, args...)
	hr, _, _ := syscall.SyscallN(taskbarBadgeVtableProc(itf, index), callArgs...)
	return hr
}

// taskbarBadgeHWND resolves the main window handle. findMainWindowHWND only
// matches visible windows, so it falls back to the Wails window-class scan:
// tasks keep running while the window sits hidden in the tray or minimized,
// and the overlay must still track their count.
func taskbarBadgeHWND() uintptr {
	taskbarBadgeHWNDCacheMu.Lock()
	hwnd := taskbarBadgeHWNDCache
	taskbarBadgeHWNDCacheMu.Unlock()
	if hwnd != 0 {
		if alive, _, _ := procBadgeIsWindow.Call(hwnd); alive != 0 {
			return hwnd
		}
	}
	hwnd = findMainWindowHWND()
	if hwnd == 0 {
		hwnd = findWailsWindowHWND()
	}
	if hwnd == 0 {
		return 0
	}
	taskbarBadgeHWNDCacheMu.Lock()
	taskbarBadgeHWNDCache = hwnd
	taskbarBadgeHWNDCacheMu.Unlock()
	return hwnd
}

type taskbarBadgeBitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type taskbarBadgeBitmapInfo struct {
	header taskbarBadgeBitmapInfoHeader
	colors [1]uint32
}

type taskbarBadgeRect struct {
	left, top, right, bottom int32
}

type taskbarBadgeIconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// taskbarBadgeRenderIcon draws the count as a red disc with a bold white
// number and returns an ARGB HICON, or 0 for "no badge" (count <= 0).
func taskbarBadgeRenderIcon(count int) uintptr {
	if count <= 0 {
		return 0
	}
	label := strconv.Itoa(count)
	if count > 99 {
		label = "99+"
	}
	size := int(taskbarBadgeSystemMetrics(taskbarBadgeSmCxIcon))
	if size <= 0 || size > 128 {
		size = 32
	}

	var bits *byte
	bmi := taskbarBadgeBitmapInfo{header: taskbarBadgeBitmapInfoHeader{
		size:     uint32(unsafe.Sizeof(taskbarBadgeBitmapInfoHeader{})),
		width:    int32(size),
		height:   -int32(size), // negative: top-down rows, pixel 0 = top-left
		planes:   1,
		bitCount: 32,
	}}

	hdc, _, _ := procBadgeCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return 0
	}
	defer procBadgeDeleteDC.Call(hdc)

	hbm, _, _ := procBadgeCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbm == 0 || bits == nil {
		if hbm != 0 {
			procBadgeDeleteObject.Call(hbm)
		}
		return 0
	}
	oldBmp, _, _ := procBadgeSelectObject.Call(hdc, hbm)

	brush, _, _ := procBadgeCreateSolidBrush.Call(taskbarBadgeColorRef)
	pen, _, _ := procBadgeCreatePen.Call(0 /*PS_SOLID*/, 1, taskbarBadgeColorRef)
	oldBrush, _, _ := procBadgeSelectObject.Call(hdc, brush)
	oldPen, _, _ := procBadgeSelectObject.Call(hdc, pen)
	procBadgeEllipse.Call(hdc, 0, 0, uintptr(size), uintptr(size))
	procBadgeSelectObject.Call(hdc, oldBrush)
	procBadgeSelectObject.Call(hdc, oldPen)
	procBadgeDeleteObject.Call(brush)
	procBadgeDeleteObject.Call(pen)

	fontHeight := size * 11 / 16
	switch {
	case len(label) >= 3:
		fontHeight = size * 7 / 16
	case len(label) == 2:
		fontHeight = size * 9 / 16
	}
	face, _ := syscall.UTF16FromString("Segoe UI")
	font, _, _ := procBadgeCreateFont.Call(
		uintptr(uint32(int32(-fontHeight))),
		0, 0, 0,
		taskbarBadgeFontWeightBold,
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0,
		taskbarBadgeQualityAntialiase,
		0,
		uintptr(unsafe.Pointer(&face[0])),
	)
	oldFont, _, _ := procBadgeSelectObject.Call(hdc, font)
	procBadgeSetBkMode.Call(hdc, taskbarBadgeBkModeTransparent)
	procBadgeSetTextColor.Call(hdc, 0xFFFFFF)
	labelUTF16, _ := syscall.UTF16FromString(label)
	rect := taskbarBadgeRect{0, 0, int32(size), int32(size)}
	procBadgeDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(&labelUTF16[0])),
		uintptr(len(labelUTF16)-1),
		uintptr(unsafe.Pointer(&rect)),
		taskbarBadgeDtCenter|taskbarBadgeDtVcenter|taskbarBadgeDtSingleLine,
	)
	procBadgeSelectObject.Call(hdc, oldFont)
	procBadgeDeleteObject.Call(font)

	// GDI writes color but leaves the DIB's alpha byte at 0; mark every
	// drawn pixel opaque while untouched corners stay fully transparent.
	for i := 0; i < size*size; i++ {
		pixel := (*uint32)(unsafe.Add(unsafe.Pointer(bits), uintptr(i)*4))
		if *pixel&0xFFFFFF != 0 {
			*pixel |= 0xFF000000
		}
	}

	procBadgeSelectObject.Call(hdc, oldBmp)

	// An all-zero mask keeps every pixel alpha-driven; the color bitmap's
	// alpha channel alone decides transparency. CreateBitmap documents NULL
	// bits as *uninitialized*, so pass explicit zeroes instead of trusting
	// the allocator to hand back a clean page.
	maskBits := make([]byte, ((size+15)/16)*2*size) // WORD-aligned monochrome rows
	mask, _, _ := procBadgeCreateBitmap.Call(
		uintptr(size), uintptr(size), 1, 1,
		uintptr(unsafe.Pointer(&maskBits[0])),
	)
	defer procBadgeDeleteObject.Call(mask)
	iconInfo := taskbarBadgeIconInfo{fIcon: 1, hbmMask: mask, hbmColor: hbm}
	icon, _, _ := procBadgeCreateIconIndirect.Call(uintptr(unsafe.Pointer(&iconInfo)))
	procBadgeDeleteObject.Call(hbm)
	return icon
}

func taskbarBadgeSystemMetrics(index int) int32 {
	v, _, _ := procBadgeSystemMetrics.Call(uintptr(index))
	return int32(v)
}
