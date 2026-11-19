//go:build windows

package guiapp

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"golang.org/x/sys/windows"
)

func platformDesktopAcceptanceSession() (desktopAcceptanceSession, string) {
	if testing.Testing() {
		return nil, acceptanceReasonTestNoDesktop
	}
	return &realDesktopSession{}, ""
}

func platformAcceptanceListens(project string) []acceptanceListen {
	if testing.Testing() {
		return nil
	}
	rows, err := acceptanceLoopbackListeners()
	if err != nil {
		return nil
	}
	var out []acceptanceListen
	seen := map[string]bool{}
	for _, row := range rows {
		if row.pid == 0 || int(row.pid) == os.Getpid() {
			continue
		}
		image, ok := acceptanceProcessImage(row.pid)
		if !ok || image == "" || acceptanceImageIsMaClaw(image) || !pathWithin(project, image) {
			continue
		}
		key := acceptancePathKey(image) + ":" + strconv.Itoa(row.port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, acceptanceListen{Host: row.host, Port: row.port, Image: image})
	}
	return out
}

type realDesktopSession struct {
	exe     string
	cleanup func()
}

func (s *realDesktopSession) Start(ctx context.Context, exe, dir string) (int, func(), error) {
	if testing.Testing() {
		return 0, nil, errors.New("refusing to launch a window under test")
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return 0, nil, errors.New("empty executable")
	}
	cmdLine := acceptanceQuoteWindowsArg(exe)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	err := windows.CreateProcess(
		windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr(cmdLine),
		nil,
		nil,
		false,
		windows.CREATE_NEW_PROCESS_GROUP,
		nil,
		windows.StringToUTF16Ptr(dir),
		&si,
		&pi,
	)
	if err != nil {
		return 0, nil, err
	}
	_ = windows.CloseHandle(pi.Thread)
	proc := pi.Process
	pid := int(pi.ProcessId)
	if pid == 0 || pid == os.Getpid() {
		_ = windows.CloseHandle(proc)
		return 0, nil, errors.New("refusing process id")
	}
	s.exe = exe
	s.cleanup = registerAcceptanceComputerUse(exe)
	var once sync.Once
	kill := func() {
		once.Do(func() {
			if s.cleanup != nil {
				s.cleanup()
			}
			cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
			_ = cmd.Run()
			_ = windows.CloseHandle(proc)
		})
	}
	deadline := acceptanceNow().Add(acceptanceAttachWindow)
	for {
		obs, ok := acceptanceObservePID(pid)
		if ok && acceptanceSameImage(obs.Image, exe) {
			return pid, kill, nil
		}
		if ctx.Err() != nil {
			kill()
			return 0, nil, ctx.Err()
		}
		if !acceptanceNow().Before(deadline) {
			kill()
			return 0, nil, errors.New("没有等到该进程的可见窗口")
		}
		acceptanceWait(50 * time.Millisecond)
	}
}

func (s *realDesktopSession) Observe(ctx context.Context, pid int) (desktopObservation, error) {
	if err := ctx.Err(); err != nil {
		return desktopObservation{}, err
	}
	_ = pid
	obs, ok := acceptanceForegroundObservation()
	if !ok {
		return desktopObservation{}, errors.New("没有可见窗口")
	}
	return obs, nil
}

func (s *realDesktopSession) Click(ctx context.Context, pid, x, y int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = pid
	// One foreground read authorizes the point. A later read could be a
	// different window whose rectangle still contains the coordinate.
	obs, ok := s.foreground()
	if err := acceptanceDesktopAuthorize(obs, ok, s.exe, ""); err != nil {
		return err
	}
	if why := acceptanceClickOutside(obs, x, y); why != "" {
		return &acceptanceDesktopDriveFault{detail: "click_outside", reason: why}
	}
	return acceptanceSendDesktopClick(x, y)
}

func (s *realDesktopSession) foreground() (desktopObservation, bool) {
	if acceptanceReadForeground != nil {
		return acceptanceReadForeground()
	}
	return acceptanceForegroundObservation()
}

func acceptanceSendDesktopClick(x, y int) error {
	if acceptanceDesktopClickHook != nil {
		return acceptanceDesktopClickHook(x, y)
	}
	if testing.Testing() {
		return errors.New("refusing to send input under test")
	}
	return guiautomation.NewInputSimulator().Click(x, y)
}

func (s *realDesktopSession) Key(ctx context.Context, pid int, key string) error {
	if err := s.allowInput(ctx, pid, key); err != nil {
		return err
	}
	return acceptanceSendDesktopKey(key)
}

func acceptanceSendDesktopKey(key string) error {
	if testing.Testing() {
		return errors.New("refusing to send input under test")
	}
	return guiautomation.NewInputSimulator().KeyCombo(key)
}

func (s *realDesktopSession) allowInput(ctx context.Context, pid int, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = pid
	obs, ok := s.foreground()
	return acceptanceDesktopAuthorize(obs, ok, s.exe, key)
}

func registerAcceptanceComputerUse(exe string) func() {
	owner := "coding-acceptance:" + acceptancePathKey(exe)
	cfg := computeruse.DefaultConfig()
	cfg.TargetApps = nil
	cfg.MaxSteps = computeruse.DefaultConfig().MaxSteps
	sess := computeruse.NewSession(cfg)
	globalComputerUse.mu.Lock()
	if globalComputerUse.sessions == nil {
		globalComputerUse.sessions = map[string]*computeruse.Session{}
	}
	globalComputerUse.sessions[owner] = sess
	globalComputerUse.mu.Unlock()
	return func() {
		globalComputerUse.mu.Lock()
		delete(globalComputerUse.sessions, owner)
		globalComputerUse.mu.Unlock()
	}
}

type acceptanceListenRow struct {
	host string
	port int
	pid  uint32
}

func acceptanceLoopbackListeners() ([]acceptanceListenRow, error) {
	raw, err := acceptanceTCPTable(2) // AF_INET
	if err != nil {
		return nil, err
	}
	rows := parseAcceptanceTCP4(raw)
	if raw6, err6 := acceptanceTCPTable(23); err6 == nil {
		rows = append(rows, parseAcceptanceTCP6(raw6)...)
	}
	return rows, nil
}

func acceptanceTCPTable(family uintptr) ([]byte, error) {
	iphlp := windows.NewLazySystemDLL("iphlpapi.dll")
	proc := iphlp.NewProc("GetExtendedTcpTable")
	var size uint32
	// TCP_TABLE_OWNER_PID_LISTENER = 3
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&size)), 1, family, 3, 0)
	if r != 122 && r != 0 { // ERROR_INSUFFICIENT_BUFFER
		return nil, errors.New("GetExtendedTcpTable size")
	}
	if size == 0 || size > 8<<20 {
		return nil, errors.New("GetExtendedTcpTable size")
	}
	buf := make([]byte, size)
	r, _, _ = proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 1, family, 3, 0)
	if r != 0 {
		return nil, errors.New("GetExtendedTcpTable")
	}
	if int(size) < len(buf) {
		buf = buf[:size]
	}
	return buf, nil
}

func parseAcceptanceTCP4(buf []byte) []acceptanceListenRow {
	if len(buf) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	const row = 24
	var out []acceptanceListenRow
	for i := 0; i < n; i++ {
		off := 4 + i*row
		if off+row > len(buf) {
			break
		}
		addr := binary.LittleEndian.Uint32(buf[off+4 : off+8])
		port := ntohs(binary.LittleEndian.Uint32(buf[off+8 : off+12]))
		pid := binary.LittleEndian.Uint32(buf[off+20 : off+24])
		if addr != 0x0100007f || port <= 0 {
			continue
		}
		out = append(out, acceptanceListenRow{host: "127.0.0.1", port: port, pid: pid})
	}
	return out
}

func parseAcceptanceTCP6(buf []byte) []acceptanceListenRow {
	if len(buf) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	// MIB_TCP6ROW_OWNER_PID: 16+4+4+16+4+4+4+4 = 56
	const row = 56
	var out []acceptanceListenRow
	for i := 0; i < n; i++ {
		off := 4 + i*row
		if off+row > len(buf) {
			break
		}
		addr := buf[off : off+16]
		if !acceptanceIsIPv6Loopback(addr) {
			continue
		}
		port := ntohs(binary.LittleEndian.Uint32(buf[off+20 : off+24]))
		pid := binary.LittleEndian.Uint32(buf[off+52 : off+56])
		if port <= 0 {
			continue
		}
		out = append(out, acceptanceListenRow{host: "::1", port: port, pid: pid})
	}
	return out
}

func acceptanceIsIPv6Loopback(addr []byte) bool {
	if len(addr) != 16 {
		return false
	}
	for i := 0; i < 15; i++ {
		if addr[i] != 0 {
			return false
		}
	}
	return addr[15] == 1
}

func ntohs(v uint32) int {
	return int((v&0xff)<<8 | (v>>8)&0xff)
}

func acceptanceProcessImage(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var buf [32768]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

var (
	acceptanceUser32              = windows.NewLazySystemDLL("user32.dll")
	acceptanceEnumWindows         = acceptanceUser32.NewProc("EnumWindows")
	acceptanceGetWindowThreadPID  = acceptanceUser32.NewProc("GetWindowThreadProcessId")
	acceptanceIsWindowVisible     = acceptanceUser32.NewProc("IsWindowVisible")
	acceptanceGetWindowText       = acceptanceUser32.NewProc("GetWindowTextW")
	acceptanceGetClassName        = acceptanceUser32.NewProc("GetClassNameW")
	acceptanceGetWindowRect       = acceptanceUser32.NewProc("GetWindowRect")
	acceptanceGetSystemMetrics    = acceptanceUser32.NewProc("GetSystemMetrics")
	acceptanceGetForegroundWindow = acceptanceUser32.NewProc("GetForegroundWindow")
	acceptanceEnumChildWindows    = acceptanceUser32.NewProc("EnumChildWindows")
	acceptanceGetGUIThreadInfo    = acceptanceUser32.NewProc("GetGUIThreadInfo")
	acceptanceGetWindowLong       = acceptanceUser32.NewProc("GetWindowLongW")
	acceptanceWindowEnumMu        sync.Mutex
	acceptanceChildLabelsSlot     []string
	acceptanceSawPasswordEdit     bool
	acceptanceChildEnumCB         = windows.NewCallback(acceptanceCollectChildLabel)
)

const acceptanceESPassword = 0x0020

type acceptanceWinScan struct {
	pid   int
	obs   desktopObservation
	found bool
}

func acceptanceObservePID(pid int) (desktopObservation, bool) {
	acceptanceWindowEnumMu.Lock()
	defer acceptanceWindowEnumMu.Unlock()
	scan := &acceptanceWinScan{pid: pid}
	cb := windows.NewCallback(func(hwnd, lparam uintptr) uintptr {
		target := (*acceptanceWinScan)(unsafe.Pointer(lparam))
		var winPID uint32
		acceptanceGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
		if int(winPID) != target.pid {
			return 1
		}
		vis, _, _ := acceptanceIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		title := acceptanceWindowText(acceptanceGetWindowText, hwnd, 512)
		class := acceptanceWindowText(acceptanceGetClassName, hwnd, 256)
		target.obs = desktopObservation{
			Title:  title,
			Class:  class,
			Covers: acceptanceWindowCovers(hwnd),
		}
		if left, top, right, bottom, okRect := acceptanceWindowRect(hwnd); okRect {
			target.obs.Left, target.obs.Top, target.obs.Right, target.obs.Bottom = left, top, right, bottom
			target.obs.HasBounds = right > left && bottom > top
		}
		target.found = true
		return 0
	})
	acceptanceEnumWindows.Call(cb, uintptr(unsafe.Pointer(scan)))
	if !scan.found {
		return desktopObservation{}, false
	}
	if image, ok := acceptanceProcessImage(uint32(pid)); ok {
		scan.obs.Image = image
	}
	return scan.obs, true
}

func acceptanceWindowText(proc *windows.LazyProc, hwnd uintptr, n int) string {
	buf := make([]uint16, n)
	got, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	if got == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
}

func acceptanceWindowRect(hwnd uintptr) (left, top, right, bottom int, ok bool) {
	var rect [4]int32
	r, _, _ := acceptanceGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect[0])))
	if r == 0 {
		return 0, 0, 0, 0, false
	}
	return int(rect[0]), int(rect[1]), int(rect[2]), int(rect[3]), true
}

func acceptanceWindowCovers(hwnd uintptr) bool {
	left, top, right, bottom, ok := acceptanceWindowRect(hwnd)
	if !ok {
		return false
	}
	width := right - left
	height := bottom - top
	cx, _, _ := acceptanceGetSystemMetrics.Call(0)
	cy, _, _ := acceptanceGetSystemMetrics.Call(1)
	if cx == 0 || cy == 0 {
		return false
	}
	return width*height*10 >= int(cx)*int(cy)*9
}

type acceptanceGUIThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcLeft        int32
	RcTop         int32
	RcRight       int32
	RcBottom      int32
}

// acceptanceForegroundObservation reads the foreground window. Clicks and
// keys are refused unless this image is the executable just built.
func acceptanceForegroundObservation() (desktopObservation, bool) {
	acceptanceWindowEnumMu.Lock()
	defer acceptanceWindowEnumMu.Unlock()
	hwnd, _, _ := acceptanceGetForegroundWindow.Call()
	if hwnd == 0 {
		return desktopObservation{}, false
	}
	var winPID uint32
	tid, _, _ := acceptanceGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
	title := acceptanceWindowText(acceptanceGetWindowText, hwnd, 512)
	class := acceptanceWindowText(acceptanceGetClassName, hwnd, 256)
	labels, passwordEdit := acceptanceChildLabels(hwnd)
	focused, focusPassword := acceptanceFocusedText(tid)
	obs := desktopObservation{
		Title:        title,
		Class:        class,
		Covers:       acceptanceWindowCovers(hwnd),
		Labels:       labels,
		Focused:      focused,
		PasswordEdit: passwordEdit || focusPassword,
	}
	if left, top, right, bottom, okRect := acceptanceWindowRect(hwnd); okRect {
		obs.Left, obs.Top, obs.Right, obs.Bottom = left, top, right, bottom
		obs.HasBounds = right > left && bottom > top
	}
	if image, ok := acceptanceProcessImage(winPID); ok {
		obs.Image = image
	}
	if obs.Image == "" && obs.Title == "" && obs.Class == "" {
		return desktopObservation{}, false
	}
	return obs, true
}

func acceptanceCollectChildLabel(hwnd, _ uintptr) uintptr {
	if acceptanceHWNDPasswordEdit(hwnd) {
		acceptanceSawPasswordEdit = true
		return 1
	}
	if len(acceptanceChildLabelsSlot) >= 40 {
		return 1
	}
	text := strings.TrimSpace(acceptanceWindowText(acceptanceGetWindowText, hwnd, 128))
	if text == "" || len([]rune(text)) > 80 {
		return 1
	}
	acceptanceChildLabelsSlot = append(acceptanceChildLabelsSlot, text)
	return 1
}

func acceptanceChildLabels(hwnd uintptr) ([]string, bool) {
	acceptanceChildLabelsSlot = nil
	acceptanceSawPasswordEdit = false
	acceptanceEnumChildWindows.Call(hwnd, acceptanceChildEnumCB, 0)
	out := append([]string(nil), acceptanceChildLabelsSlot...)
	saw := acceptanceSawPasswordEdit
	acceptanceChildLabelsSlot = nil
	acceptanceSawPasswordEdit = false
	return out, saw
}

func acceptanceFocusedText(thread uintptr) (string, bool) {
	if thread == 0 {
		return "", false
	}
	info := acceptanceGUIThreadInfo{CbSize: uint32(unsafe.Sizeof(acceptanceGUIThreadInfo{}))}
	r, _, _ := acceptanceGetGUIThreadInfo.Call(thread, uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.HwndFocus == 0 {
		return "", false
	}
	if acceptanceHWNDPasswordEdit(info.HwndFocus) {
		return "", true
	}
	return strings.TrimSpace(acceptanceWindowText(acceptanceGetWindowText, info.HwndFocus, 128)), false
}

// acceptancePasswordEditClass reports an edit control whose style is the
// password style. The text of that control is not an observation.
func acceptancePasswordEditClass(class string, style uintptr) bool {
	c := strings.ToLower(strings.TrimSpace(class))
	if c != "edit" && !strings.Contains(c, "richedit") {
		return false
	}
	return style&acceptanceESPassword != 0
}

func acceptanceHWNDPasswordEdit(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	class := acceptanceWindowText(acceptanceGetClassName, hwnd, 64)
	style, _, _ := acceptanceGetWindowLong.Call(hwnd, ^uintptr(15))
	return acceptancePasswordEditClass(class, style)
}
