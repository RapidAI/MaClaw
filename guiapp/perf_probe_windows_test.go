//go:build windows

package guiapp

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestPerfSampleProcessTreeIncludesSelfChildren verifies the tree walk starts
// from this process and does not reach unrelated processes. The key regression
// this guards: an earlier measurement summed every msedgewebview2.exe on the
// machine, which includes Windows Search and Widgets and produced a figure
// several times too large.
func TestPerfSampleProcessTreeIncludesSelfChildren(t *testing.T) {
	rows, selfCPU, err := perfSampleProcessTree()
	if err != nil {
		t.Fatalf("perfSampleProcessTree: %v", err)
	}
	if selfCPU < 0 {
		t.Fatalf("selfCPU = %v, want >= 0", selfCPU)
	}
	// A Go test binary has no WebView2 children, so the expected result is an
	// empty set. A non-empty result means the walk escaped our own subtree.
	if len(rows) != 0 {
		for _, row := range rows {
			t.Logf("unexpected child: pid=%d name=%s", row.pid, row.name)
		}
		t.Fatalf("got %d WebView2 children, want 0 for a test process", len(rows))
	}
}

// TestPerfProcessCPUSecondsSelf checks the FILETIME conversion against a
// process we know exists: ourselves. The value must be positive and finite
// once the process has run for any measurable time.
func TestPerfProcessCPUSecondsSelf(t *testing.T) {
	pid := uint32(windows.GetCurrentProcessId())
	cpu, err := perfProcessCPUSeconds(pid)
	if err != nil {
		t.Fatalf("perfProcessCPUSeconds: %v", err)
	}
	if cpu < 0 {
		t.Fatalf("cpu = %v, want >= 0", cpu)
	}
	if cpu > 1e9 {
		t.Fatalf("cpu = %v, implausibly large; FILETIME conversion is wrong", cpu)
	}
}

// TestPerfProcessWorkingSetSelf verifies the psapi struct layout by reading our
// own working set. A wrong layout yields zero or a wildly wrong value.
func TestPerfProcessWorkingSetSelf(t *testing.T) {
	pid := uint32(windows.GetCurrentProcessId())
	mem, err := perfProcessWorkingSet(pid)
	if err != nil {
		t.Fatalf("perfProcessWorkingSet: %v", err)
	}
	// Any running Go test binary holds at least a few MB; anything below 1 MB
	// means the struct offsets do not match psapi.h.
	const oneMiB = 1 << 20
	if mem < oneMiB {
		t.Fatalf("working set = %d bytes, want >= 1MiB (struct layout mismatch)", mem)
	}
}

// TestPerfParentMapNonEmpty confirms the Toolhelp snapshot returns a usable
// process table on this host.
func TestPerfParentMapNonEmpty(t *testing.T) {
	snapshot, err := perfParentMap()
	if err != nil {
		t.Fatalf("perfParentMap: %v", err)
	}
	if len(snapshot.parents) < 2 {
		t.Fatalf("got %d processes, want at least 2", len(snapshot.parents))
	}
	selfPID := uint32(windows.GetCurrentProcessId())
	if _, ok := snapshot.parents[selfPID]; !ok {
		t.Fatalf("current process %d missing from snapshot", selfPID)
	}
	// The name map must cover the same PIDs; the sampler filters on it.
	if name := snapshot.names[selfPID]; name == "" {
		t.Fatalf("current process %d has no image name in snapshot", selfPID)
	}
}
