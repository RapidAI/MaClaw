//go:build windows

package guiapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func companionAppPath(name string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+name, registry.QUERY_VALUE|registry.WOW64_64KEY)
		if err != nil {
			continue
		}
		value, _, readErr := key.GetStringValue("")
		_ = key.Close()
		if readErr != nil {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if companionPathIsFile(value) {
			return value
		}
	}
	return ""
}

func browserProcessRunningOS(image string) bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+image, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(image))
}

func writeHKCUString(view, path, name, value string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{View: view, Path: path, Name: name, Value: value})
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|companionRegView(view))
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, value)
}

func deleteHKCUKey(view, path string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{Delete: true, View: view, Path: path})
	}
	parent := path
	child := ""
	if i := strings.LastIndex(path, `\`); i >= 0 {
		parent = path[:i]
		child = path[i+1:]
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, parent, registry.ALL_ACCESS|companionRegView(view))
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	if err := registry.DeleteKey(key, child); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func companionRegView(view string) uint32 {
	if view == "32" {
		return registry.WOW64_32KEY
	}
	return registry.WOW64_64KEY
}

var (
	procOpenProcessExt             = recoverKernel32.NewProc("OpenProcess")
	procCloseHandleExt             = recoverKernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = recoverKernel32.NewProc("QueryFullProcessImageNameW")
	procSetForegroundWindowExt     = recoverUser32.NewProc("SetForegroundWindow")

	companionExtEnumOnce sync.Once
	companionExtEnumCB   uintptr
	companionExtEnumGate sync.Mutex
	companionExtEnumMu   sync.Mutex
	companionExtImage    string
	companionExtHWNDS    []uintptr
)

// openCompanionExtensionsPageOS opens page in a new browser window.
// Chromium drops chrome:// and edge:// command-line URLs that come from
// another program, so the window would stay on an ordinary tab. about:blank
// is allowed. The address bar of that new window is then set to page.
func openCompanionExtensionsPageOS(exe, page string) error {
	image := strings.ToLower(filepath.Base(exe))
	before := companionBrowserWindowSet(image)
	cmd := exec.Command(exe, "--new-window", "about:blank")
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	hwnd := waitNewCompanionBrowserWindow(image, before, 8*time.Second)
	if hwnd == 0 {
		return fmt.Errorf("browser extensions page did not open")
	}
	procShowWindowRW.Call(hwnd, 9) // SW_RESTORE
	allowCompanionForeground()
	procSetForegroundWindowExt.Call(hwnd)
	deadline := time.Now().Add(8 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := setCompanionOmnibox(hwnd, page); err != nil {
			last = err
			time.Sleep(300 * time.Millisecond)
			continue
		}
		time.Sleep(1500 * time.Millisecond)
		if companionExtensionsWindowTitle(companionWindowTitle(hwnd)) {
			return nil
		}
		last = fmt.Errorf("browser extensions page did not open")
		time.Sleep(300 * time.Millisecond)
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("browser extensions page did not open")
}

func companionExtensionsWindowTitle(title string) bool {
	if strings.Contains(strings.ToLower(title), "about:blank") {
		return false
	}
	lower := strings.ToLower(title)
	return strings.Contains(lower, "extension") || strings.Contains(title, "扩展") || strings.Contains(title, "擴充")
}

func companionWindowTitle(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextWRW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func waitNewCompanionBrowserWindow(image string, before map[uintptr]struct{}, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, hwnd := range companionBrowserWindows(image) {
			if _, ok := before[hwnd]; !ok {
				return hwnd
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

func companionBrowserWindowSet(image string) map[uintptr]struct{} {
	set := map[uintptr]struct{}{}
	for _, hwnd := range companionBrowserWindows(image) {
		set[hwnd] = struct{}{}
	}
	return set
}

func companionBrowserWindows(image string) []uintptr {
	companionExtEnumOnce.Do(func() {
		companionExtEnumCB = syscall.NewCallback(companionExtEnumProc)
	})
	companionExtEnumGate.Lock()
	defer companionExtEnumGate.Unlock()
	companionExtEnumMu.Lock()
	companionExtImage = image
	companionExtHWNDS = nil
	companionExtEnumMu.Unlock()
	procEnumWindowsRW.Call(companionExtEnumCB, 0)
	companionExtEnumMu.Lock()
	out := append([]uintptr(nil), companionExtHWNDS...)
	companionExtEnumMu.Unlock()
	return out
}

func companionExtEnumProc(hwnd, _ uintptr) uintptr {
	visible, _, _ := procIsWindowVisibleRW.Call(hwnd)
	if visible == 0 {
		return 1
	}
	if companionWindowClass(hwnd) != "Chrome_WidgetWin_1" {
		return 1
	}
	companionExtEnumMu.Lock()
	want := companionExtImage
	companionExtEnumMu.Unlock()
	if companionWindowImageBase(hwnd) != want {
		return 1
	}
	companionExtEnumMu.Lock()
	companionExtHWNDS = append(companionExtHWNDS, hwnd)
	companionExtEnumMu.Unlock()
	return 1
}

func companionWindowClass(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameWRW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func companionWindowImageBase(hwnd uintptr) string {
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	const processQueryLimitedInformation = 0x1000
	h, _, _ := procOpenProcessExt.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandleExt.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf)))
}

func setCompanionOmnibox(hwnd uintptr, page string) error {
	script := `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Windows.Forms
$el = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]::new([long]$env:MACLAW_EXT_HWND))
if (-not $el) { exit 2 }
$cond = New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'OmniboxViewViews')
$edit = $el.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $edit) { exit 3 }
$vp = $edit.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
$vp.SetValue($env:MACLAW_EXT_URL)
[void]$edit.SetFocus()
[System.Windows.Forms.SendKeys]::SendWait('{Enter}')
exit 0
`
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = `C:\Windows`
	}
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"MACLAW_EXT_HWND="+strconv.FormatUint(uint64(hwnd), 10),
		"MACLAW_EXT_URL="+page,
	)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s", msg)
}
