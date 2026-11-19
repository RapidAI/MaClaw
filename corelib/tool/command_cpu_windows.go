//go:build windows

package tool

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func sampleCommandTreeCPUByPID(pid int) (map[int]time.Duration, bool) {
	if pid <= 0 {
		return nil, false
	}
	parents, err := processParentMap()
	if err != nil {
		return nil, false
	}
	usage := make(map[int]time.Duration)
	for _, child := range descendantPIDs(pid, parents) {
		cpu, ok := processCPUTime(uint32(child))
		if !ok {
			continue
		}
		usage[child] = cpu
	}
	if len(usage) == 0 {
		return nil, false
	}
	return usage, true
}

func processParentMap() (map[int]int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	parents := make(map[int]int, 256)
	for {
		parents[int(entry.ProcessID)] = int(entry.ParentProcessID)
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return parents, nil
}

func processCPUTime(pid uint32) (time.Duration, bool) {
	if pid == 0 {
		return 0, false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return filetimeDuration(kernel) + filetimeDuration(user), true
}

// filetimeDuration reads a process-time FILETIME as a duration. Those values
// count 100ns units of CPU time, not an absolute timestamp, so the epoch
// conversion in Filetime.Nanoseconds must not be used.
func filetimeDuration(ft windows.Filetime) time.Duration {
	ticks := uint64(uint32(ft.HighDateTime))<<32 | uint64(ft.LowDateTime)
	return time.Duration(ticks * 100)
}
