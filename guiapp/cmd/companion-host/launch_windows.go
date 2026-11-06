//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Link the helper with -H windowsgui so Chrome does not open a console.
// The installer still points the native host at the GUI executable until this
// binary is copied beside it. Do not retarget that manifest from this package.

func launchCompanionGUI(gui string) int {
	argv := windows.StringToUTF16Ptr(quoteWindowsArg(gui) + " " + quoteWindowsArg(companionPaperHostArg))
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = windows.Handle(os.Stdin.Fd())
	si.StdOutput = windows.Handle(os.Stdout.Fd())
	si.StdErr = windows.Handle(os.Stderr.Fd())
	var pi windows.ProcessInformation
	err := windows.CreateProcess(
		windows.StringToUTF16Ptr(gui),
		argv,
		nil,
		nil,
		true,
		0,
		nil,
		windows.StringToUTF16Ptr(filepath.Dir(gui)),
		&si,
		&pi,
	)
	if err != nil {
		return 1
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	if _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); err != nil {
		return 1
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return 1
	}
	return int(code)
}

func quoteWindowsArg(s string) string {
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
