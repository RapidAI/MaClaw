//go:build windows

package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformAcceptanceSession() (consoleAcceptanceSession, string) {
	return realConsoleSession{}, ""
}

type realConsoleSession struct{}

func (realConsoleSession) Start(ctx context.Context, exe, dir string) (int, func(time.Duration) bool, func(), error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, err
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return 0, nil, nil, errors.New("empty executable")
	}
	cmdLine := acceptanceQuoteWindowsArg(exe)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	// The game needs its own console. Do not set CREATE_NO_WINDOW or
	// DETACHED_PROCESS, and do not inherit std handles: that would hide the
	// buffer ReadConsoleOutput has to see.
	err := windows.CreateProcess(
		windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr(cmdLine),
		nil,
		nil,
		false,
		windows.CREATE_NEW_CONSOLE|windows.CREATE_NEW_PROCESS_GROUP,
		nil,
		windows.StringToUTF16Ptr(dir),
		&si,
		&pi,
	)
	if err != nil {
		return 0, nil, nil, err
	}
	_ = windows.CloseHandle(pi.Thread)
	proc := pi.Process
	pid := int(pi.ProcessId)
	if pid == 0 || pid == os.Getpid() {
		_ = windows.CloseHandle(proc)
		return 0, nil, nil, errors.New("refusing process id")
	}
	var once sync.Once
	kill := func() {
		once.Do(func() {
			cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
			_ = cmd.Run()
			_ = windows.CloseHandle(proc)
		})
	}
	exitedWithin := func(d time.Duration) bool {
		ms := uint32(d / time.Millisecond)
		if ms == 0 {
			ms = 1
		}
		ev, waitErr := windows.WaitForSingleObject(proc, ms)
		return waitErr == nil && ev == windows.WAIT_OBJECT_0
	}
	return pid, exitedWithin, kill, nil
}

func (realConsoleSession) Probe(ctx context.Context, pid int) (consoleHelperResult, error) {
	return callAcceptanceHelper(ctx, pid, "probe", "")
}

func (realConsoleSession) Read(ctx context.Context, pid int) (consoleGrid, error) {
	res, err := callAcceptanceHelper(ctx, pid, "read", "")
	if err != nil {
		return consoleGrid{}, err
	}
	return gridFromText(res.Text), nil
}

func (realConsoleSession) Write(ctx context.Context, pid int, key string) error {
	_, err := callAcceptanceHelper(ctx, pid, "write", key)
	return err
}

func callAcceptanceHelper(ctx context.Context, pid int, op, key string) (consoleHelperResult, error) {
	var res consoleHelperResult
	if pid <= 0 || pid == os.Getpid() {
		res.Reason = "console_pid_mismatch"
		return res, errors.New(res.Reason)
	}
	exe, err := os.Executable()
	if err != nil {
		return res, err
	}
	args := []string{"console-buffer", "--pid", strconv.Itoa(pid), "--host", strconv.Itoa(os.Getpid()), op}
	if op == "write" {
		args = append(args, key)
	}
	cctx, cancel := context.WithTimeout(ctx, acceptanceHelperTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if jsonErr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &res); jsonErr != nil {
		if runErr != nil {
			return res, fmt.Errorf("%w: %s", runErr, strings.TrimSpace(stderr.String()))
		}
		return res, jsonErr
	}
	if !res.OK {
		if res.Reason == "" {
			res.Reason = "attach_console_failed"
		}
		return res, errors.New(res.Reason)
	}
	return res, nil
}

func runConsoleBufferCommand(args []string) int {
	pid, host, op, key, err := parseConsoleBufferArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		writeAcceptanceHelperJSON(consoleHelperResult{Reason: "bad_args"})
		return 2
	}
	if pid == os.Getpid() {
		fmt.Fprintf(os.Stderr, "refusing helper pid\n")
		writeAcceptanceHelperJSON(consoleHelperResult{Reason: "console_pid_mismatch"})
		return 2
	}
	r, _, callErr := procAttachConsole.Call(uintptr(pid))
	if r == 0 {
		res := consoleHelperResult{Reason: "attach_console_failed", Win32: acceptanceWin32(callErr), ConsoleHWND: acceptanceConsoleHWND()}
		fmt.Fprintf(os.Stderr, "AttachConsole(%d) failed: %v\n", pid, callErr)
		writeAcceptanceHelperJSON(res)
		return 1
	}
	defer procFreeConsole.Call()
	if ok, reason, code := acceptanceConsoleListAllows(pid, host); !ok {
		res := consoleHelperResult{Reason: reason, Win32: code, ConsoleHWND: acceptanceConsoleHWND()}
		fmt.Fprintf(os.Stderr, "%s\n", reason)
		writeAcceptanceHelperJSON(res)
		return 1
	}
	switch op {
	case "probe":
		writeAcceptanceHelperJSON(consoleHelperResult{OK: true, ConsoleHWND: acceptanceConsoleHWND()})
		return 0
	case "read":
		if ok, reason, code := acceptanceConsoleListAllows(pid, host); !ok {
			writeAcceptanceHelperJSON(consoleHelperResult{Reason: reason, Win32: code, ConsoleHWND: acceptanceConsoleHWND()})
			return 1
		}
		text, cols, rows, readErr := readAcceptanceConsoleWindow()
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read: %v\n", readErr)
			writeAcceptanceHelperJSON(consoleHelperResult{Reason: "attach_console_failed", Win32: acceptanceWin32(readErr), ConsoleHWND: acceptanceConsoleHWND()})
			return 1
		}
		writeAcceptanceHelperJSON(consoleHelperResult{OK: true, ConsoleHWND: acceptanceConsoleHWND(), Cols: cols, Rows: rows, Text: text})
		return 0
	case "write":
		if ok, reason, code := acceptanceConsoleListAllows(pid, host); !ok {
			writeAcceptanceHelperJSON(consoleHelperResult{Reason: reason, Win32: code, ConsoleHWND: acceptanceConsoleHWND()})
			return 1
		}
		if writeErr := writeAcceptanceConsoleKey(key); writeErr != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", writeErr)
			writeAcceptanceHelperJSON(consoleHelperResult{Reason: "attach_console_failed", Win32: acceptanceWin32(writeErr), ConsoleHWND: acceptanceConsoleHWND()})
			return 1
		}
		writeAcceptanceHelperJSON(consoleHelperResult{OK: true, ConsoleHWND: acceptanceConsoleHWND()})
		return 0
	default:
		writeAcceptanceHelperJSON(consoleHelperResult{Reason: "bad_args"})
		return 2
	}
}

func writeAcceptanceHelperJSON(res consoleHelperResult) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(res)
}

func acceptanceConsoleListAllows(client, host int) (bool, string, int) {
	pids, err := acceptanceConsoleProcessList()
	if err != nil {
		return false, "attach_console_failed", acceptanceWin32(err)
	}
	hasClient := false
	for _, pid := range pids {
		if int(pid) == host {
			return false, "attached_host_console", 0
		}
		if int(pid) == client {
			hasClient = true
		}
	}
	if !hasClient {
		return false, "console_pid_mismatch", 0
	}
	return true, "", 0
}

func acceptanceConsoleHWND() uint64 {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return uint64(hwnd)
}

func acceptanceWin32(err error) int {
	if err == nil {
		return 0
	}
	if errno, ok := err.(syscall.Errno); ok && errno != 0 {
		return int(errno)
	}
	return 0
}

var (
	kernel32Console                = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole              = kernel32Console.NewProc("AttachConsole")
	procFreeConsole                = kernel32Console.NewProc("FreeConsole")
	procGetConsoleProcessList      = kernel32Console.NewProc("GetConsoleProcessList")
	procGetConsoleScreenBufferInfo = kernel32Console.NewProc("GetConsoleScreenBufferInfo")
	procReadConsoleOutputW         = kernel32Console.NewProc("ReadConsoleOutputW")
	procWriteConsoleInputW         = kernel32Console.NewProc("WriteConsoleInputW")
	procGetConsoleWindow           = kernel32Console.NewProc("GetConsoleWindow")
)

type acceptanceCoord struct {
	X int16
	Y int16
}

type acceptanceSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type acceptanceConsoleScreenBufferInfo struct {
	Size       acceptanceCoord
	Cursor     acceptanceCoord
	Attributes uint16
	Window     acceptanceSmallRect
	Maximum    acceptanceCoord
}

type acceptanceCharInfo struct {
	Char uint16
	Attr uint16
}

// acceptanceInputRecord is a KEY_EVENT INPUT_RECORD: WORD EventType, 2 bytes
// of padding, then the 16-byte KEY_EVENT_RECORD. Arrow keys set ENHANCED_KEY
// (0x0100) on dwControlKeyState. That is not SendInput's extended-key flag.
type acceptanceInputRecord struct {
	EventType uint16
	_         uint16
	KeyDown   int32
	Repeat    uint16
	VK        uint16
	Scan      uint16
	Rune      uint16
	Control   uint32
}

func acceptanceConsoleProcessList() ([]uint32, error) {
	buf := make([]uint32, 16)
	for {
		n, _, callErr := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			if callErr != nil && acceptanceWin32(callErr) != 0 {
				return nil, callErr
			}
			return nil, errors.New("GetConsoleProcessList failed")
		}
		if int(n) <= len(buf) {
			out := append([]uint32(nil), buf[:n]...)
			return out, nil
		}
		buf = make([]uint32, n)
	}
}

func readAcceptanceConsoleWindow() (string, int, int, error) {
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return "", 0, 0, err
	}
	var info acceptanceConsoleScreenBufferInfo
	r, _, callErr := procGetConsoleScreenBufferInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return "", 0, 0, callErr
	}
	width := int(info.Window.Right - info.Window.Left + 1)
	height := int(info.Window.Bottom - info.Window.Top + 1)
	if width < 1 || height < 1 {
		return "", 0, 0, errors.New("empty console window")
	}
	if width > acceptanceGridCols {
		width = acceptanceGridCols
	}
	if height > acceptanceGridRows {
		height = acceptanceGridRows
	}
	buf := make([]acceptanceCharInfo, width*height)
	region := acceptanceSmallRect{
		Left:   info.Window.Left,
		Top:    info.Window.Top,
		Right:  info.Window.Left + int16(width) - 1,
		Bottom: info.Window.Top + int16(height) - 1,
	}
	r, _, callErr = procReadConsoleOutputW.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&buf[0])),
		acceptancePackCoord(int16(width), int16(height)),
		acceptancePackCoord(0, 0),
		uintptr(unsafe.Pointer(&region)),
	)
	if r == 0 {
		return "", 0, 0, callErr
	}
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		chars := make([]uint16, width)
		for col := 0; col < width; col++ {
			chars[col] = buf[row*width+col].Char
		}
		lines[row] = strings.TrimRight(string(utf16.Decode(chars)), " \x00")
	}
	return strings.Join(lines, "\n"), width, height, nil
}

func writeAcceptanceConsoleKey(key string) error {
	vk, scan, char, enhanced, ok := acceptanceConsoleKey(key)
	if !ok {
		return errors.New("key rejected")
	}
	handle, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return err
	}
	var control uint32
	if enhanced {
		control = 0x0100
	}
	recs := [2]acceptanceInputRecord{
		{EventType: 0x0001, KeyDown: 1, Repeat: 1, VK: vk, Scan: scan, Rune: char, Control: control},
		{EventType: 0x0001, KeyDown: 0, Repeat: 1, VK: vk, Scan: scan, Rune: char, Control: control},
	}
	var written uint32
	r, _, callErr := procWriteConsoleInputW.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&recs[0])),
		2,
		uintptr(unsafe.Pointer(&written)),
	)
	if r == 0 {
		return callErr
	}
	if written != 2 {
		return fmt.Errorf("wrote %d console input records", written)
	}
	return nil
}

func acceptanceConsoleKey(key string) (vk, scan, char uint16, enhanced, ok bool) {
	switch strings.ToLower(key) {
	case "left":
		return 0x25, 0x4B, 0, true, true
	case "up":
		return 0x26, 0x48, 0, true, true
	case "right":
		return 0x27, 0x4D, 0, true, true
	case "down":
		return 0x28, 0x50, 0, true, true
	case "space":
		return 0x20, 0x39, ' ', false, true
	case "enter":
		return 0x0D, 0x1C, '\r', false, true
	case "z":
		return 0x5A, 0x2C, 'z', false, true
	case "x":
		return 0x58, 0x2D, 'x', false, true
	case "c":
		return 0x43, 0x2E, 'c', false, true
	default:
		return 0, 0, 0, false, false
	}
}

func acceptancePackCoord(x, y int16) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

func acceptanceQuoteWindowsArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			slashes++
		case '"':
			for n := 0; n < slashes*2+1; n++ {
				b.WriteByte('\\')
			}
			b.WriteByte('"')
			slashes = 0
		default:
			for n := 0; n < slashes; n++ {
				b.WriteByte('\\')
			}
			slashes = 0
			b.WriteByte(s[i])
		}
	}
	for n := 0; n < slashes*2; n++ {
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}
