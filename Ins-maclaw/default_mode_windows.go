//go:build windows

package main

import (
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

func defaultRunMode() string {
	if launchedFromExplorer() || !isTerminal() {
		return "gui"
	}
	return "tui"
}

func launchedFromExplorer() bool {
	parent, ok := parentProcessName(os.Getpid())
	return ok && strings.EqualFold(parent, "explorer.exe")
}

type processEntry32 struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	CntThreads        uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [260]uint16
}

func parentProcessName(pid int) (string, bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	createSnapshot := kernel32.NewProc("CreateToolhelp32Snapshot")
	processFirst := kernel32.NewProc("Process32FirstW")
	processNext := kernel32.NewProc("Process32NextW")
	closeHandle := kernel32.NewProc("CloseHandle")

	const th32csSnapProcess = 0x00000002
	h, _, _ := createSnapshot.Call(th32csSnapProcess, 0)
	if h == 0 || h == ^uintptr(0) {
		return "", false
	}
	defer closeHandle.Call(h)

	var entry processEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	ret, _, _ := processFirst.Call(h, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return "", false
	}
	parentPID := uint32(0)
	for {
		if entry.ProcessID == uint32(pid) {
			parentPID = entry.ParentProcessID
			break
		}
		ret, _, _ = processNext.Call(h, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			return "", false
		}
	}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ret, _, _ = processFirst.Call(h, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return "", false
	}
	for {
		if entry.ProcessID == parentPID {
			return syscall.UTF16ToString(entry.ExeFile[:]), true
		}
		ret, _, _ = processNext.Call(h, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}
	runtime.KeepAlive(entry)
	return "", false
}
