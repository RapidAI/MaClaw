//go:build windows

package guiapp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/text/encoding/simplifiedchinese"
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
	procIsWindowExt                = recoverUser32.NewProc("IsWindow")
	procKeybdEventExt              = recoverUser32.NewProc("keybd_event")
	procSendMessageTimeoutWExt     = recoverUser32.NewProc("SendMessageTimeoutW")

	companionExtEnumOnce sync.Once
	companionExtEnumCB   uintptr
	companionExtEnumGate sync.Mutex
	companionExtEnumMu   sync.Mutex
	companionExtImage    string
	companionExtHWNDS    []uintptr
)

const companionElevatedBrowserError = "the browser is running as administrator, so the extensions page cannot be opened automatically"

const companionFocusBrowserError = "could not focus the new browser window"

// companionExtensionsMarker is the helper page's <title>: it makes the new
// browser window unambiguous. It must not contain "extension", "扩展" or
// "擴充" — companionExtensionsWindowTitle would take the marker page for a
// successful navigation. Keep it in sync with companionExtensionsPageHTML.
const companionExtensionsMarker = "MaClaw companion page"

// companionExtensionsPageHTML is the local page the new window opens. Its
// title is the marker; the body tells the user what to do when automatic
// navigation never happens.
const companionExtensionsPageHTML = `<!doctype html>
<meta charset="utf-8">
<title>MaClaw companion page</title>
<style>body{font-family:system-ui,sans-serif;margin:3em;color:#444;line-height:1.7}</style>
<p>正在打开扩展程序页面…</p>
<p>如果没有自动跳转，请在地址栏输入 <b>chrome://extensions/</b>（Edge 为 <b>edge://extensions/</b>）后回车，开启「开发人员模式」并选择「加载已解压的扩展程序」。</p>
`

// companionExtensionsStartPage returns the marker that identifies the new
// browser window and the URL it opens. A file: URL is honored on the command
// line — chrome:// and edge:// are dropped — and its title keeps a
// session-restored window from being mistaken for ours. about:blank with no
// marker is the fallback when the state directory is unavailable.
func companionExtensionsStartPage() (marker, startURL string) {
	dir, err := browserExtensionStateDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return "", "about:blank"
	}
	path := filepath.Join(dir, "extensions-page.html")
	if err := writeCompanionFile(path, []byte(companionExtensionsPageHTML)); err != nil {
		return "", "about:blank"
	}
	return companionExtensionsMarker, (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

// openCompanionExtensionsPageOS opens page in a new browser window.
// Chromium drops chrome:// and edge:// command-line URLs that come from
// another program, so the window opens a local helper page instead. The
// address bar of that window is then set to page.
func openCompanionExtensionsPageOS(exe, page string) error {
	image := strings.ToLower(filepath.Base(exe))
	marker, startURL := companionExtensionsStartPage()
	before := companionBrowserWindowSet(image)
	cmd := exec.Command(exe, "--new-window", startURL)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	hwnd := waitNewCompanionBrowserWindow(image, before, marker, 8*time.Second)
	if hwnd == 0 {
		return fmt.Errorf("browser extensions page did not open")
	}
	if companionBrowserElevated(hwnd) {
		// UIPI blocks UI Automation and keystrokes into an elevated browser.
		return fmt.Errorf(companionElevatedBrowserError)
	}
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		// The hwnd captured above can belong to a transient popup that is
		// already gone. UI Automation and keystrokes both fail on a dead
		// window, so replace it with another browser window first.
		if !companionWindowAlive(hwnd) {
			before[hwnd] = struct{}{}
			next := waitNewCompanionBrowserWindow(image, before, marker, 2*time.Second)
			if next == 0 {
				return fmt.Errorf("browser extensions page did not open")
			}
			hwnd = next
			if companionBrowserElevated(hwnd) {
				return fmt.Errorf(companionElevatedBrowserError)
			}
		}
		// UI Automation sets the omnibox text without needing focus. When it
		// is blocked the URL still has to reach the page: focus the window,
		// send Ctrl+L and type it as keystrokes. Enter is sent from here and
		// only after the window is the real foreground window, so a stray
		// Enter can never land in an unrelated application.
		uiaErr := setCompanionOmnibox(hwnd, page)
		if uiaErr != nil {
			last = uiaErr
		}
		if err := sendCompanionOmniboxKeys(hwnd, page, uiaErr != nil); err != nil {
			if last == nil {
				last = err
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		time.Sleep(1200 * time.Millisecond)
		if companionExtensionsWindowTitle(companionWindowTitle(hwnd)) {
			return nil
		}
		last = fmt.Errorf("browser extensions page did not open")
		time.Sleep(300 * time.Millisecond)
	}
	if companionBrowserElevated(hwnd) {
		return fmt.Errorf(companionElevatedBrowserError)
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("browser extensions page did not open")
}

// companionBrowserElevated reports whether the browser window runs at a
// higher integrity level than this process.
func companionBrowserElevated(hwnd uintptr) bool {
	pid := companionWindowPID(hwnd)
	return pid != 0 && companionProcessElevated(pid) && !companionSelfElevated()
}

func companionWindowAlive(hwnd uintptr) bool {
	alive, _, _ := procIsWindowExt.Call(hwnd)
	return alive != 0
}

func companionWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func companionProcessElevated(pid uint32) bool {
	const processQueryLimitedInformation = 0x1000
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var token windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

func companionSelfElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

func companionExtensionsWindowTitle(title string) bool {
	if strings.Contains(strings.ToLower(title), "about:blank") {
		return false
	}
	lower := strings.ToLower(title)
	return strings.Contains(lower, "extension") || strings.Contains(title, "扩展") || strings.Contains(title, "擴充")
}

// companionWindowTitle reads the window title through SendMessageTimeoutW.
// GetWindowTextW sends WM_GETTEXT and waits for the window's thread — a hung
// browser would stall the open attempt forever, so the send is bounded.
func companionWindowTitle(hwnd uintptr) string {
	const (
		wmGetText       = 0x000D
		smtoAbortIfHung = 0x0002
	)
	buf := make([]uint16, 512)
	var result uintptr
	r, _, _ := procSendMessageTimeoutWExt.Call(hwnd, wmGetText, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), smtoAbortIfHung, 500, uintptr(unsafe.Pointer(&result)))
	if r == 0 || result == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:result+1])
}

func waitNewCompanionBrowserWindow(image string, before map[uintptr]struct{}, marker string, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, hwnd := range companionBrowserWindows(image) {
			if _, ok := before[hwnd]; ok {
				continue
			}
			if marker != "" && !companionNewWindowTitle(companionWindowTitle(hwnd), marker) {
				continue
			}
			return hwnd
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

// companionNewWindowTitle tells the window just opened apart from windows a
// session restore may raise at the same time. The helper page title is the
// marker; the blank variants cover a browser that dropped the file: URL and
// stayed on its new-tab page.
func companionNewWindowTitle(title, marker string) bool {
	if strings.Contains(title, marker) {
		return true
	}
	lower := strings.ToLower(title)
	return strings.Contains(lower, "about:blank") ||
		strings.Contains(lower, "untitled") ||
		strings.Contains(lower, "new tab") ||
		strings.Contains(title, "新标签页") ||
		strings.Contains(title, "新分頁") ||
		strings.Contains(title, "无标题") ||
		strings.Contains(title, "無標題")
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
	pid := companionWindowPID(hwnd)
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

const (
	companionKeyUp      = 0x0002
	companionKeyUnicode = 0x0004
	vkControl           = 0x11
	vkL                 = 0x4C
	vkReturn            = 0x0D
)

// sendCompanionOmniboxKeys delivers the page through plain keystrokes.
// Ctrl+L focuses the omnibox and selects its text; typeURL then replaces the
// selection with the URL (needed when UI Automation could not set it);
// Enter navigates. Typing uses KEYEVENTF_UNICODE, so an IME or a non-US
// keyboard layout cannot rewrite the characters.
func sendCompanionOmniboxKeys(hwnd uintptr, page string, typeURL bool) error {
	procShowWindowRW.Call(hwnd, 9) // SW_RESTORE
	allowCompanionForeground()
	procSetForegroundWindowExt.Call(hwnd)
	time.Sleep(300 * time.Millisecond)
	fg, _, _ := procGetForegroundWndIW.Call()
	if fg != hwnd {
		return fmt.Errorf(companionFocusBrowserError)
	}
	keybd := func(vk, scan, flags uintptr) {
		procKeybdEventExt.Call(vk, scan, flags, 0)
	}
	keybd(vkControl, 0, 0)
	keybd(vkL, 0, 0)
	keybd(vkL, 0, companionKeyUp)
	keybd(vkControl, 0, companionKeyUp)
	time.Sleep(150 * time.Millisecond)
	if typeURL {
		for _, r := range page {
			if r < 0x20 || r > 0xFF {
				continue
			}
			keybd(0, uintptr(r), companionKeyUnicode)
			keybd(0, uintptr(r), companionKeyUnicode|companionKeyUp)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// A user click or an unrelated popup could steal the foreground while
	// the keys above were sent. Do not press Enter into that window.
	fg, _, _ = procGetForegroundWndIW.Call()
	if fg != hwnd {
		return fmt.Errorf(companionFocusBrowserError)
	}
	keybd(vkReturn, 0, 0)
	keybd(vkReturn, 0, companionKeyUp)
	return nil
}

// setCompanionOmnibox writes page into the omnibox through UI Automation.
// It deliberately does not send Enter: Enter must reach the window that owns
// the keyboard focus, which is verified on the Go side instead of letting
// SendKeys fire into whatever application happens to be foreground.
// FromHandle can throw ElementNotAvailableException — for a window that is
// being replaced, or when a policy or integrity level blocks UIA — and the
// caller then falls back to typing the URL itself.
func setCompanionOmnibox(hwnd uintptr, page string) error {
	script := `
$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$el = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]::new([long]$env:MACLAW_EXT_HWND))
if (-not $el) { Write-Output 'browser window not found by UI Automation'; exit 2 }
$cond = New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'OmniboxViewViews')
$edit = $el.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $edit) { Write-Output 'browser address bar not found'; exit 3 }
$vp = $edit.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
$vp.SetValue($env:MACLAW_EXT_URL)
exit 0
`
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = `C:\Windows`
	}
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"MACLAW_EXT_HWND="+strconv.FormatUint(uint64(hwnd), 10),
		"MACLAW_EXT_URL="+page,
	)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := companionCommandOutputText(out)
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s", msg)
}

// companionCommandOutputText keeps localized PowerShell errors readable.
// The script asks for UTF-8, but a console that still answers in the OEM
// code page (GBK on zh-CN) would otherwise surface as mojibake.
func companionCommandOutputText(out []byte) string {
	msg := strings.TrimSpace(string(out))
	if utf8.ValidString(msg) {
		return msg
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().String(msg)
	if err != nil {
		return msg
	}
	return decoded
}
