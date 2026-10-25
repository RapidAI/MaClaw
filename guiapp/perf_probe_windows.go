//go:build windows

package guiapp

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WebView2 process-tree sampling.
//
// The earlier "WebView2 uses 148% CPU" measurement was wrong because it summed
// every msedgewebview2.exe on the machine, which also includes Windows Search
// and Widgets. WebView2 hosts are ordinary child processes of this executable,
// so the tree rooted at our own PID is the only set that belongs to MaClaw.
//
// Two numbers are produced per sample:
//   - cpu: total CPU seconds consumed by the tree, and the rate since the
//     previous sample (the useful signal for "is it busy while idle").
//   - mem: summed working set, which is what a low-memory machine runs out of.
//
// The walk uses Toolhelp for the PID/parent map (cheap, one syscall) and then
// opens each WebView2 child for CPU/memory counters. Failures are tolerated:
// a process may exit between the snapshot and OpenProcess, and losing one
// sample must never disturb the app.

const perfSampleInterval = 30 * time.Second

// perfWebviewProcessNames are the child process images that belong to the
// WebView2 runtime. Anything else parented to us (helper binaries, sidecars)
// is excluded so the figure stays comparable across builds.
var perfWebviewProcessNames = map[string]struct{}{
	"msedgewebview2.exe": {},
}

type perfProcSample struct {
	pid     uint32
	name    string
	cpuSec  float64
	workSet uint64
}

var (
	perfSamplerOnce sync.Once
	perfSamplerStop atomic.Bool

	perfSampleMu       sync.Mutex
	perfLastSampleAt   time.Time
	perfLastSampleCPU  float64
	perfLastWebviewCPU float64
	perfLastWebviewMem uint64
)

// startPerfSampler begins periodic sampling of this process and its WebView2
// children. It is safe to call once at startup; repeated calls are no-ops.
func startPerfSampler() {
	if !perfProbeEnabled() {
		return
	}
	perfSamplerOnce.Do(func() {
		go perfSampleLoop()
	})
}

func perfSampleLoop() {
	// First sample establishes the CPU baseline; the rate is only meaningful
	// from the second sample onward.
	perfTakeSample(true)
	ticker := time.NewTicker(perfSampleInterval)
	defer ticker.Stop()
	for range ticker.C {
		if perfSamplerStop.Load() {
			return
		}
		perfTakeSample(false)
	}
}

// perfTakeSample records one sample and logs the delta since the previous one.
func perfTakeSample(baseline bool) {
	rows, selfCPU, err := perfSampleProcessTree()
	if err != nil {
		if perfProbeVerboseEnabled() {
			log.Printf("[perf] sample_error=%v", err)
		}
		return
	}
	now := time.Now()

	var webviewCPU float64
	var webviewMem uint64
	for _, row := range rows {
		webviewCPU += row.cpuSec
		webviewMem += row.workSet
	}

	perfSampleMu.Lock()
	prevAt := perfLastSampleAt
	prevWebviewCPU := perfLastWebviewCPU
	perfLastSampleAt = now
	perfLastSampleCPU = selfCPU
	perfLastWebviewCPU = webviewCPU
	perfLastWebviewMem = webviewMem
	perfSampleMu.Unlock()

	if baseline || prevAt.IsZero() {
		log.Printf("[perf] sample baseline webview_procs=%d webview_cpu_s=%.2f webview_mem_mb=%d self_cpu_s=%.2f",
			len(rows), webviewCPU, webviewMem/1048576, selfCPU)
		return
	}

	wall := now.Sub(prevAt).Seconds()
	if wall <= 0 {
		return
	}
	// CPU percent is normalized to one core, matching how Task Manager's
	// "CPU" column reads for a single process.
	webviewPct := (webviewCPU - prevWebviewCPU) / wall * 100
	log.Printf("[perf] sample webview_procs=%d webview_cpu_pct=%.1f webview_mem_mb=%d self_cpu_s=%.2f window_s=%.0f",
		len(rows), webviewPct, webviewMem/1048576, selfCPU, wall)

	if perfProbeVerboseEnabled() {
		logPerfSampleDetail(rows)
	}
}

func logPerfSampleDetail(rows []perfProcSample) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].cpuSec > rows[j].cpuSec })
	for _, row := range rows {
		log.Printf("[perf] sample_proc pid=%d name=%s cpu_s=%.2f mem_mb=%d",
			row.pid, row.name, row.cpuSec, row.workSet/1048576)
	}
}

// perfSampleProcessTree returns WebView2 child samples plus this process's own
// cumulative CPU seconds.
func perfSampleProcessTree() ([]perfProcSample, float64, error) {
	parents, err := perfParentMap()
	if err != nil {
		return nil, 0, err
	}
	selfPID := uint32(windows.GetCurrentProcessId())
	selfCPU, err := perfProcessCPUSeconds(selfPID)
	if err != nil {
		// Not fatal: the WebView figure is the one being investigated.
		selfCPU = 0
	}

	// Collect descendants iteratively so a deep tree (WebView2 nests utility
	// processes under the browser process) is fully covered.
	children := make(map[uint32][]uint32, len(parents))
	for pid, ppid := range parents {
		children[ppid] = append(children[ppid], pid)
	}
	queue := append([]uint32(nil), children[selfPID]...)
	seen := make(map[uint32]struct{}, len(queue))
	rows := make([]perfProcSample, 0, len(queue))
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, dup := seen[pid]; dup {
			continue
		}
		seen[pid] = struct{}{}
		name := perfProcName(pid)
		if _, isWebview := perfWebviewProcessNames[strings.ToLower(name)]; isWebview {
			cpu, cpuErr := perfProcessCPUSeconds(pid)
			mem, memErr := perfProcessWorkingSet(pid)
			if cpuErr == nil || memErr == nil {
				rows = append(rows, perfProcSample{pid: pid, name: name, cpuSec: cpu, workSet: mem})
			}
		}
		queue = append(queue, children[pid]...)
	}
	return rows, selfCPU, nil
}

// perfProcTable maps PID to parent PID for one Toolhelp snapshot.
type perfProcTable map[uint32]uint32

func perfParentMap() (perfProcTable, error) {
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
	out := make(perfProcTable, 256)
	for {
		out[entry.ProcessID] = entry.ParentProcessID
		perfCacheProcessName(entry.ProcessID, windows.UTF16ToString(entry.ExeFile[:]))
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out, nil
}

var (
	perfProcNameMu    sync.RWMutex
	perfProcNameCache = make(map[uint32]string, 256)
)

func perfCacheProcessName(pid uint32, name string) {
	perfProcNameMu.Lock()
	perfProcNameCache[pid] = name
	perfProcNameMu.Unlock()
}

func perfProcName(pid uint32) string {
	perfProcNameMu.RLock()
	name := perfProcNameCache[pid]
	perfProcNameMu.RUnlock()
	return name
}

// perfProcessCPUSeconds returns cumulative CPU time for a process. Requires
// PROCESS_QUERY_LIMITED_INFORMATION, which works for same-user processes
// without elevation.
func perfProcessCPUSeconds(pid uint32) (float64, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	k := float64(kernel.HighDateTime)*4294967296 + float64(kernel.LowDateTime)
	u := float64(user.HighDateTime)*4294967296 + float64(user.LowDateTime)
	// FILETIME counts 100ns intervals.
	return (k + u) / 1e7, nil
}

// PROCESS_MEMORY_COUNTERS mirrors the Win32 struct used by
// GetProcessMemoryInfo. x/sys/windows does not expose it, so the layout is
// declared here; the field order and sizes match psapi.h exactly.
type perfProcessMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	perfPSAPIDLL               = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfoRW = perfPSAPIDLL.NewProc("GetProcessMemoryInfo")
)

func perfProcessWorkingSet(pid uint32) (uint64, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)
	var counters perfProcessMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))
	ret, _, callErr := procGetProcessMemoryInfoRW.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.cb),
	)
	if ret == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	return uint64(counters.WorkingSetSize), nil
}
