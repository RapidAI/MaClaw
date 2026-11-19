//go:build linux

package tool

import (
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// linuxClkTck is the process-stat tick rate. Auxv is read once; a process
// that cannot see the vector keeps the userspace ABI rate.
var linuxClkTck = sync.OnceValue(func() int64 {
	vec, err := unix.Auxv()
	if err != nil {
		return clockTicksFromAuxv(nil)
	}
	return clockTicksFromAuxv(vec)
})

// sampleCommandTreeCPUByPID reads user+kernel time for pid and its
// descendants. It follows /proc/<pid>/task/<tid>/children so a poll stats
// only the tree. A missing children file means this kernel has no such
// interface, and the walk falls back to a full /proc scan. Every thread is
// read: a child forked by a non-main thread is still part of the tree.
func sampleCommandTreeCPUByPID(pid int) (map[int]time.Duration, bool) {
	if pid <= 0 {
		return nil, false
	}
	perSec := linuxClkTck()
	if perSec <= 0 {
		return nil, false
	}
	usage := make(map[int]time.Duration)
	seen := map[int]struct{}{}
	if walkProcCPU(pid, perSec, usage, seen) {
		if len(usage) == 0 {
			return nil, false
		}
		return usage, true
	}
	return sampleProcCPUFull(pid, perSec)
}

func walkProcCPU(pid int, perSec int64, usage map[int]time.Duration, seen map[int]struct{}) bool {
	if pid <= 0 {
		return true
	}
	if _, ok := seen[pid]; ok {
		return true
	}
	seen[pid] = struct{}{}
	if cpu, ok := procPIDCPU(pid, perSec); ok {
		usage[pid] = cpu
	}
	children, supported := procChildPIDs(pid)
	if !supported {
		return false
	}
	for _, child := range children {
		if !walkProcCPU(child, perSec, usage, seen) {
			return false
		}
	}
	return true
}

// procChildPIDs lists direct children of every thread in pid.
// supported is false only when the children file itself is absent.
// A process that has already exited reports no children.
func procChildPIDs(pid int) (children []int, supported bool) {
	taskDir := "/proc/" + strconv.Itoa(pid) + "/task"
	tasks, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, true
	}
	seen := map[int]struct{}{}
	sawFile := false
	for _, task := range tasks {
		data, err := os.ReadFile(taskDir + "/" + task.Name() + "/children")
		if err != nil {
			if os.IsNotExist(err) && !sawFile {
				return nil, false
			}
			continue
		}
		sawFile = true
		for _, child := range parseProcChildren(string(data)) {
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			children = append(children, child)
		}
	}
	return children, true
}

func procPIDCPU(pid int, perSec int64) (time.Duration, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	_, utime, stime, ok := procStatUsage(string(data))
	if !ok {
		return 0, false
	}
	return time.Duration(utime+stime) * time.Second / time.Duration(perSec), true
}

func sampleProcCPUFull(root int, perSec int64) (map[int]time.Duration, bool) {
	parents, ticks, ok := readProcCPUTable()
	if !ok {
		return nil, false
	}
	usage := make(map[int]time.Duration)
	for _, child := range descendantPIDs(root, parents) {
		n, exists := ticks[child]
		if !exists {
			continue
		}
		usage[child] = time.Duration(n) * time.Second / time.Duration(perSec)
	}
	if len(usage) == 0 {
		return nil, false
	}
	return usage, true
}

func readProcCPUTable() (parents map[int]int, ticks map[int]int64, ok bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil, false
	}
	parents = make(map[int]int)
	ticks = make(map[int]int64)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		ppid, utime, stime, parsed := procStatUsage(string(data))
		if !parsed {
			continue
		}
		parents[pid] = ppid
		ticks[pid] = utime + stime
	}
	if len(ticks) == 0 {
		return nil, nil, false
	}
	return parents, ticks, true
}
