//go:build windows

package browser

import (
	"encoding/binary"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processCommandLineInformation = 60

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	gwlExStyle       = ^uintptr(19) // -20
	wsExToolWindow   = 0x00000080
	wsExAppWindow    = 0x00040000
	swpNoSize        = 0x0001
	swpNoMove        = 0x0002
	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	swpFrameChanged  = 0x0020
	swShowNoActivate = 4
	swRestore        = 9
	gwHwndNext       = 2
)

var (
	user32Park                  = windows.NewLazySystemDLL("user32.dll")
	procEnumWindowsPark         = user32Park.NewProc("EnumWindows")
	procGetWindowThreadProcPark = user32Park.NewProc("GetWindowThreadProcessId")
	procGetClassNamePark        = user32Park.NewProc("GetClassNameW")
	procIsWindowVisiblePark     = user32Park.NewProc("IsWindowVisible")
	procIsIconicPark            = user32Park.NewProc("IsIconic")
	procIsZoomedPark            = user32Park.NewProc("IsZoomed")
	procGetWindowRectPark       = user32Park.NewProc("GetWindowRect")
	procSetWindowPosPark        = user32Park.NewProc("SetWindowPos")
	procGetWindowLongPtrPark    = user32Park.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrPark    = user32Park.NewProc("SetWindowLongPtrW")
	procGetWindowPlacementPark  = user32Park.NewProc("GetWindowPlacement")
	procSetWindowPlacementPark  = user32Park.NewProc("SetWindowPlacement")
	procGetForegroundPark       = user32Park.NewProc("GetForegroundWindow")
	procSetForegroundPark       = user32Park.NewProc("SetForegroundWindow")
	procGetSystemMetricsPark    = user32Park.NewProc("GetSystemMetrics")
	procIsWindowPark            = user32Park.NewProc("IsWindow")
	procGetWindowTextPark       = user32Park.NewProc("GetWindowTextW")
	procAttachThreadInputPark   = user32Park.NewProc("AttachThreadInput")
	procAllowSetForegroundPark  = user32Park.NewProc("AllowSetForegroundWindow")
	procGetWindowPark           = user32Park.NewProc("GetWindow")
	procShowWindowPark          = user32Park.NewProc("ShowWindow")
)

type parkRect struct {
	left, top, right, bottom int32
}

type parkPlacement struct {
	length  uint32
	flags   uint32
	showCmd uint32
	minPosX int32
	minPosY int32
	maxPosX int32
	maxPosY int32
	normal  parkRect
}

type parkedDir struct {
	path      string
	since     time.Time
	missSince time.Time
}

var (
	parkLoopMu      sync.Mutex
	parkLoopDirs    = map[string]parkedDir{}
	parkLoopStarted bool
	parkLoopWake    = make(chan struct{}, 1)
	parkLogged      = map[string]bool{}
	parkForeground  uintptr
)

// wakeParkLoop lets a new profile interrupt the idle sleep. The idle sleep
// does not snapshot processes, and it must not leave a window that just
// appeared sitting on the desktop until that sleep ends.
func wakeParkLoop() {
	select {
	case parkLoopWake <- struct{}{}:
	default:
	}
}

func parkLoopSleep(d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-parkLoopWake:
	case <-timer.C:
	}
}

func virtualScreenOrigin() (int, int) {
	x, _, _ := procGetSystemMetricsPark.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetricsPark.Call(smYVirtualScreen)
	return int(int32(x)), int(int32(y))
}

func parkedWindowPosition() (int, int) {
	x, y := virtualScreenOrigin()
	return parkedOriginForScreen(x, y)
}

func captureParkForeground() {
	fg, _, _ := procGetForegroundPark.Call()
	if !focusableForeground(fg, nil) {
		return
	}
	parkLoopMu.Lock()
	parkForeground = fg
	parkLoopMu.Unlock()
}

func startManagedBrowserParkLoop(userDataDir string) {
	key := managedBrowserKey(userDataDir)
	parkLoopMu.Lock()
	parkLoopDirs[key] = parkedDir{path: userDataDir, since: time.Now()}
	delete(parkSkipLogged, key)
	start := !parkLoopStarted
	parkLoopStarted = true
	parkLoopMu.Unlock()
	wakeParkLoop()
	if start {
		go managedBrowserParkLoop()
	}
}

func managedBrowserParkLoop() {
	// SetForegroundWindow only sticks on the thread that attached the input
	// queues. Keep that thread for the life of the loop.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	wasBusy := false
	for {
		dirs := parkedDirectories()
		if len(dirs) == 0 {
			wasBusy = false
			parkLoopSleep(parkPollWait(0, false, false))
			continue
		}
		now := time.Now()
		fresh := false
		for _, dir := range dirs {
			if now.Sub(dir.since) < parkFreshFor {
				fresh = true
				break
			}
		}
		age := parkPIDCacheAge(fresh, wasBusy)
		busy := false
		for _, dir := range dirs {
			found, still := parkProfile(dir.path, age)
			if !found {
				noteParkMiss(dir.path)
				continue
			}
			noteParkHit(dir.path)
			if still {
				busy = true
			}
		}
		wasBusy = busy
		parkLoopSleep(parkPollWait(len(dirs), fresh, busy))
	}
}

func parkedDirectories() []parkedDir {
	parkLoopMu.Lock()
	defer parkLoopMu.Unlock()
	out := make([]parkedDir, 0, len(parkLoopDirs))
	for _, dir := range parkLoopDirs {
		out = append(out, dir)
	}
	return out
}

// parkProfileWindows moves this profile's visible top-level windows off the
// virtual screen. It returns true when a window was still on a monitor.
// A process without the occlusion switch is left on the desktop: moving that
// one would mark the page hidden.
func parkProfileWindows(userDataDir string) bool {
	_, still := parkProfile(userDataDir, 0)
	return still
}

// parkProfile reports whether this profile still has a process, and whether
// one of its windows is still on a monitor. maxAge is how old a process-list
// snapshot may be; zero always reads the list again.
func parkProfile(userDataDir string, maxAge time.Duration) (found, still bool) {
	pids, safe := cachedProfileProcessIDs(userDataDir, maxAge)
	if len(pids) == 0 {
		return false, false
	}
	if !safe {
		noteParkSkip(userDataDir)
		return true, false
	}
	hwnds := chromeWindowsForPIDs(pids)
	if len(hwnds) == 0 {
		return true, false
	}
	rememberForeground(pids)
	still = false
	for _, hwnd := range hwnds {
		needs := windowNeedsPark(hwnd)
		if needs {
			placeWindowOffScreen(hwnd)
		}
		if !windowIsTool(hwnd) {
			makeToolWindow(hwnd)
		}
		if windowNeedsPark(hwnd) {
			still = true
		}
	}
	restoreForeground(pids)
	if !still {
		key := managedBrowserKey(userDataDir)
		parkLoopMu.Lock()
		already := parkLogged[key]
		if !already {
			parkLogged[key] = true
		}
		parkLoopMu.Unlock()
		if !already {
			log.Printf("[browser] parked managed browser window off the desktop (%s)", filepath.Base(userDataDir))
		}
	}
	return true, still
}

type profilePIDSnapshot struct {
	at   time.Time
	pids map[uint32]struct{}
	safe bool
}

var (
	profilePIDMu    sync.Mutex
	profilePIDCache = map[string]profilePIDSnapshot{}
	parkSkipLogged  = map[string]bool{}
)

func cachedProfileProcessIDs(userDataDir string, maxAge time.Duration) (map[uint32]struct{}, bool) {
	key := managedBrowserKey(userDataDir)
	if maxAge > 0 {
		profilePIDMu.Lock()
		if entry, ok := profilePIDCache[key]; ok && time.Since(entry.at) < maxAge {
			profilePIDMu.Unlock()
			return entry.pids, entry.safe
		}
		profilePIDMu.Unlock()
	}
	pids, safe := profileProcessIDs(userDataDir)
	profilePIDMu.Lock()
	profilePIDCache[key] = profilePIDSnapshot{at: time.Now(), pids: pids, safe: safe}
	profilePIDMu.Unlock()
	return pids, safe
}

func noteParkHit(userDataDir string) {
	key := managedBrowserKey(userDataDir)
	parkLoopMu.Lock()
	dir, ok := parkLoopDirs[key]
	if ok && !dir.missSince.IsZero() {
		dir.missSince = time.Time{}
		parkLoopDirs[key] = dir
	}
	parkLoopMu.Unlock()
}

func noteParkMiss(userDataDir string) {
	key := managedBrowserKey(userDataDir)
	parkLoopMu.Lock()
	dir, ok := parkLoopDirs[key]
	if !ok {
		parkLoopMu.Unlock()
		return
	}
	now := time.Now()
	if dir.missSince.IsZero() {
		dir.missSince = now
		parkLoopDirs[key] = dir
		parkLoopMu.Unlock()
		return
	}
	if !parkWatchExpired(dir.missSince, now) {
		parkLoopMu.Unlock()
		return
	}
	delete(parkLoopDirs, key)
	delete(parkLogged, key)
	delete(parkSkipLogged, key)
	parkLoopMu.Unlock()
	profilePIDMu.Lock()
	delete(profilePIDCache, key)
	profilePIDMu.Unlock()
	log.Printf("[browser] stopped watching managed browser window (%s)", filepath.Base(userDataDir))
}

func noteParkSkip(userDataDir string) {
	key := managedBrowserKey(userDataDir)
	parkLoopMu.Lock()
	already := parkSkipLogged[key]
	if !already {
		parkSkipLogged[key] = true
	}
	parkLoopMu.Unlock()
	if !already {
		log.Printf("[browser] managed window left on the desktop; the process is missing %s", nativeOcclusionFeature)
	}
}

func profileProcessIDs(userDataDir string) (map[uint32]struct{}, bool) {
	needle := filepath.Clean(userDataDir)
	out := map[uint32]struct{}{}
	safe := true
	sawBrowser := false
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, false
	}
	for {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if name == "chrome.exe" || name == "msedge.exe" {
			cmd, ok := queryProcessCommandLine(entry.ProcessID)
			if ok && commandLineHasProfile(cmd, needle) {
				out[entry.ProcessID] = struct{}{}
				// Renderers share the profile and often omit the switch.
				// Only the debugged browser process decides whether an
				// off-screen window stays visible to the page.
				if strings.Contains(strings.ToLower(cmd), "--remote-debugging-port") {
					sawBrowser = true
					if !commandLineDisablesNativeOcclusion(cmd) {
						safe = false
					}
				}
			}
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	if !sawBrowser {
		safe = false
	}
	return out, safe
}

func queryProcessCommandLine(pid uint32) (string, bool) {
	access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
	handle, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		handle, err = windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
		if err != nil {
			return "", false
		}
	}
	defer windows.CloseHandle(handle)
	buf := make([]byte, 2048)
	for i := 0; i < 5; i++ {
		var n uint32
		err = windows.NtQueryInformationProcess(handle, processCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)), &n)
		if err == nil {
			if n > 0 && int(n) < len(buf) {
				buf = buf[:n]
			}
			return unicodeStringFromProcessBuffer(buf)
		}
		if n > uint32(len(buf)) && n < 1<<20 {
			buf = make([]byte, n)
			continue
		}
		buf = make([]byte, len(buf)*2)
	}
	return "", false
}

func unicodeStringFromProcessBuffer(buf []byte) (string, bool) {
	if len(buf) < 16 {
		return "", false
	}
	length := int(binary.LittleEndian.Uint16(buf[0:2]))
	if length <= 0 || length%2 != 0 {
		return "", false
	}
	ptr := binary.LittleEndian.Uint64(buf[8:16])
	base := uint64(uintptr(unsafe.Pointer(&buf[0])))
	var raw []byte
	if ptr >= base && ptr+uint64(length) <= base+uint64(len(buf)) {
		off := int(ptr - base)
		raw = buf[off : off+length]
	} else if 16+length <= len(buf) {
		raw = buf[16 : 16+length]
	} else {
		return "", false
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	text := windows.UTF16ToString(units)
	if text == "" {
		return "", false
	}
	return text, true
}

var (
	enumChromeOnce sync.Once
	enumChromeCB   uintptr
	enumChromeMu   sync.Mutex
	enumChromePIDs map[uint32]struct{}
	enumChromeOut  []uintptr
	enumClassBuf   [64]uint16
)

func chromeWindowsForPIDs(pids map[uint32]struct{}) []uintptr {
	enumChromeOnce.Do(func() {
		enumChromeCB = windows.NewCallback(enumChromeWindow)
	})
	enumChromeMu.Lock()
	defer enumChromeMu.Unlock()
	enumChromePIDs = pids
	enumChromeOut = enumChromeOut[:0]
	procEnumWindowsPark.Call(enumChromeCB, 0)
	out := make([]uintptr, len(enumChromeOut))
	copy(out, enumChromeOut)
	return out
}

func enumChromeWindow(hwnd uintptr, _ uintptr) uintptr {
	var pid uint32
	procGetWindowThreadProcPark.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if _, ok := enumChromePIDs[pid]; !ok {
		return 1
	}
	vis, _, _ := procIsWindowVisiblePark.Call(hwnd)
	if vis == 0 {
		return 1
	}
	n, _, _ := procGetClassNamePark.Call(hwnd, uintptr(unsafe.Pointer(&enumClassBuf[0])), uintptr(len(enumClassBuf)))
	if n == 0 {
		return 1
	}
	if windows.UTF16ToString(enumClassBuf[:n]) == "Chrome_WidgetWin_1" {
		enumChromeOut = append(enumChromeOut, hwnd)
	}
	return 1
}

func windowNeedsPark(hwnd uintptr) bool {
	var r parkRect
	ok, _, _ := procGetWindowRectPark.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return false
	}
	vx, _, _ := procGetSystemMetricsPark.Call(smXVirtualScreen)
	vy, _, _ := procGetSystemMetricsPark.Call(smYVirtualScreen)
	vw, _, _ := procGetSystemMetricsPark.Call(smCXVirtualScreen)
	vh, _, _ := procGetSystemMetricsPark.Call(smCYVirtualScreen)
	iconic, _, _ := procIsIconicPark.Call(hwnd)
	zoomed, _, _ := procIsZoomedPark.Call(hwnd)
	if iconic != 0 || zoomed != 0 {
		return true
	}
	return rectIntersectsVirtualScreen(int(r.left), int(r.top), int(r.right), int(r.bottom), int(int32(vx)), int(int32(vy)), int(int32(vw)), int(int32(vh)))
}

func windowIsTool(hwnd uintptr) bool {
	ex, _, _ := procGetWindowLongPtrPark.Call(hwnd, gwlExStyle)
	return ex&wsExToolWindow != 0 && ex&wsExAppWindow == 0
}

func placeWindowOffScreen(hwnd uintptr) {
	virtualX, virtualY := virtualScreenOrigin()
	iconic, _, _ := procIsIconicPark.Call(hwnd)
	zoomed, _, _ := procIsZoomedPark.Call(hwnd)
	shown := 0
	if iconic != 0 || zoomed != 0 {
		var place parkPlacement
		place.length = uint32(unsafe.Sizeof(place))
		if ok, _, _ := procGetWindowPlacementPark.Call(hwnd, uintptr(unsafe.Pointer(&place))); ok != 0 {
			// The icon rect and the monitor rect are not the size that
			// comes back. Placement uses workspace coordinates, and Windows
			// pulls a rectangle that misses every monitor back onto one, so
			// it only leaves the minimized or maximized state. SetWindowPos
			// below is the call that stays off the screen.
			shown = shownParkWidth(0, int(place.normal.right-place.normal.left), true)
			h := int(place.normal.bottom - place.normal.top)
			if h < 300 {
				h = 800
			}
			x := int32(parkedWindowLeft(virtualX, shown))
			y := int32(virtualY)
			place.showCmd = swShowNoActivate
			place.flags &^= 0x0002 // WPF_RESTORETOMAXIMIZED
			place.normal = parkRect{left: x, top: y, right: x + int32(shown), bottom: y + int32(h)}
			procSetWindowPlacementPark.Call(hwnd, uintptr(unsafe.Pointer(&place)))
			if windowIconicOrZoomed(hwnd) {
				place.showCmd = swRestore
				procSetWindowPlacementPark.Call(hwnd, uintptr(unsafe.Pointer(&place)))
			}
		}
	}
	iconicNow := windowIconicOrZoomed(hwnd)
	if !iconicNow {
		var r parkRect
		if ok, _, _ := procGetWindowRectPark.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok != 0 && r.right > r.left {
			shown = int(r.right - r.left)
		}
	}
	// A window that is still minimized must not use its icon width. shown is
	// the normal placement width in that case.
	w := shownParkWidth(shown, 0, false)
	if iconicNow {
		w = shownParkWidth(0, shown, true)
	}
	x := int32(parkedWindowLeft(virtualX, w))
	y := int32(virtualY)
	procSetWindowPosPark.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func windowIconicOrZoomed(hwnd uintptr) bool {
	iconic, _, _ := procIsIconicPark.Call(hwnd)
	if iconic != 0 {
		return true
	}
	zoomed, _, _ := procIsZoomedPark.Call(hwnd)
	return zoomed != 0
}

func makeToolWindow(hwnd uintptr) {
	if windowIsTool(hwnd) {
		return
	}
	ex, _, _ := procGetWindowLongPtrPark.Call(hwnd, gwlExStyle)
	next := (ex | wsExToolWindow) &^ wsExAppWindow
	procSetWindowLongPtrPark.Call(hwnd, gwlExStyle, next)
	procSetWindowPosPark.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

func rememberForeground(pids map[uint32]struct{}) {
	fg, _, _ := procGetForegroundPark.Call()
	if !focusableForeground(fg, pids) {
		return
	}
	parkLoopMu.Lock()
	parkForeground = fg
	parkLoopMu.Unlock()
}

func restoreForeground(pids map[uint32]struct{}) {
	fg, _, _ := procGetForegroundPark.Call()
	if fg == 0 || !foregroundIsProfile(fg, pids) {
		return
	}
	parkLoopMu.Lock()
	rememberedHWND := parkForeground
	parkLoopMu.Unlock()
	remembered := uintptr(0)
	// The saved window can be minimized after it was captured. Activating
	// it then restores that window onto a monitor, so it is not a target
	// until it is a normal visible window again.
	if windowAlive(rememberedHWND) && focusableForeground(rememberedHWND, pids) {
		remembered = rememberedHWND
	}
	behind := uintptr(0)
	own := uintptr(0)
	if remembered == 0 {
		// Reuse does not capture a foreground window before Chrome is already
		// in front. The next real window in Z-order is the one that was there.
		behind = windowBehind(fg, pids)
		if behind == 0 {
			own = ownVisibleWindow()
		}
	}
	target := parkRestoreTarget(fg, remembered, behind, own, true)
	if target == 0 || !windowAlive(target) {
		return
	}
	forceForeground(target)
}

func windowBehind(foreground uintptr, pids map[uint32]struct{}) uintptr {
	hwnd := foreground
	for i := 0; i < 32; i++ {
		next, _, _ := procGetWindowPark.Call(hwnd, gwHwndNext)
		if next == 0 {
			return 0
		}
		hwnd = next
		ex, _, _ := procGetWindowLongPtrPark.Call(hwnd, gwlExStyle)
		vis, _, _ := procIsWindowVisiblePark.Call(hwnd)
		iconic, _, _ := procIsIconicPark.Call(hwnd)
		var r parkRect
		ok, _, _ := procGetWindowRectPark.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		hasArea := ok != 0 && r.right > r.left && r.bottom > r.top
		if parkFocusCandidate(ex, vis != 0, foregroundIsProfile(hwnd, pids), hasArea, iconic != 0) {
			return hwnd
		}
	}
	return 0
}

func windowAlive(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	ok, _, _ := procIsWindowPark.Call(hwnd)
	return ok != 0
}

func forceForeground(hwnd uintptr) {
	fg, _, _ := procGetForegroundPark.Call()
	var fgTID, targetTID uintptr
	if fg != 0 && fg != hwnd {
		fgTID, _, _ = procGetWindowThreadProcPark.Call(fg, 0)
	}
	targetTID, _, _ = procGetWindowThreadProcPark.Call(hwnd, 0)
	curTID := uintptr(windows.GetCurrentThreadId())
	if fgTID != 0 && fgTID != curTID {
		procAttachThreadInputPark.Call(curTID, fgTID, 1)
		defer procAttachThreadInputPark.Call(curTID, fgTID, 0)
	}
	if targetTID != 0 && targetTID != curTID && targetTID != fgTID {
		procAttachThreadInputPark.Call(curTID, targetTID, 1)
		defer procAttachThreadInputPark.Call(curTID, targetTID, 0)
	}
	procAllowSetForegroundPark.Call(^uintptr(0))
	// SetForegroundWindow can return without the window coming forward.
	// Raising while the threads are still attached is what makes it stick.
	// HWND_TOP does not leave the window topmost. SWP_SHOWWINDOW is not
	// used: it would restore a minimized window onto a monitor.
	procSetWindowPosPark.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize)
	procSetForegroundPark.Call(hwnd)
}

func focusableForeground(hwnd uintptr, pids map[uint32]struct{}) bool {
	if hwnd == 0 {
		return false
	}
	ex, _, _ := procGetWindowLongPtrPark.Call(hwnd, gwlExStyle)
	vis, _, _ := procIsWindowVisiblePark.Call(hwnd)
	iconic, _, _ := procIsIconicPark.Call(hwnd)
	var r parkRect
	ok, _, _ := procGetWindowRectPark.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	hasArea := ok != 0 && r.right > r.left && r.bottom > r.top
	return parkFocusCandidate(ex, vis != 0, foregroundIsProfile(hwnd, pids), hasArea, iconic != 0)
}

var (
	enumOwnOnce  sync.Once
	enumOwnCB    uintptr
	enumOwnMu    sync.Mutex
	enumOwnOut   uintptr
	enumOwnClass [64]uint16
	enumOwnTitle [256]uint16
)

func ownVisibleWindow() uintptr {
	enumOwnOnce.Do(func() {
		enumOwnCB = windows.NewCallback(enumOwnWindow)
	})
	enumOwnMu.Lock()
	defer enumOwnMu.Unlock()
	enumOwnOut = 0
	procEnumWindowsPark.Call(enumOwnCB, 0)
	return enumOwnOut
}

func enumOwnWindow(hwnd uintptr, _ uintptr) uintptr {
	if enumOwnOut != 0 {
		return 0
	}
	var pid uint32
	procGetWindowThreadProcPark.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != uint32(windows.GetCurrentProcessId()) {
		return 1
	}
	vis, _, _ := procIsWindowVisiblePark.Call(hwnd)
	iconic, _, _ := procIsIconicPark.Call(hwnd)
	ex, _, _ := procGetWindowLongPtrPark.Call(hwnd, gwlExStyle)
	var r parkRect
	okRect, _, _ := procGetWindowRectPark.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	hasArea := okRect != 0 && r.right > r.left && r.bottom > r.top
	if !parkFocusCandidate(ex, vis != 0, false, hasArea, iconic != 0) {
		return 1
	}
	n, _, _ := procGetClassNamePark.Call(hwnd, uintptr(unsafe.Pointer(&enumOwnClass[0])), uintptr(len(enumOwnClass)))
	if n == 0 || windows.UTF16ToString(enumOwnClass[:n]) == "Chrome_WidgetWin_1" {
		return 1
	}
	tn, _, _ := procGetWindowTextPark.Call(hwnd, uintptr(unsafe.Pointer(&enumOwnTitle[0])), uintptr(len(enumOwnTitle)))
	if tn == 0 {
		return 1
	}
	enumOwnOut = hwnd
	return 0
}

func foregroundIsProfile(hwnd uintptr, pids map[uint32]struct{}) bool {
	var pid uint32
	procGetWindowThreadProcPark.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	_, ok := pids[pid]
	return ok
}
