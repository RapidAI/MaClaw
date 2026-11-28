package perfcpus

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestPerformanceCount(t *testing.T) {
	cases := []struct {
		name string
		caps []int
		ncpu int
		want int
	}{
		{name: "empty", caps: nil, ncpu: 8, want: 0},
		{name: "homogeneous", caps: []int{1024, 1024, 1024, 1024, 1024, 1024, 1024, 1024}, ncpu: 8, want: 0},
		{name: "big little", caps: []int{1024, 1024, 1024, 1024, 300, 300, 300, 300}, ncpu: 8, want: 4},
		{name: "prime gold little", caps: []int{1024, 800, 800, 250, 250, 250, 250, 250}, ncpu: 8, want: 3},
		{name: "half kept", caps: []int{1024, 512, 512, 200}, ncpu: 4, want: 3},
		{name: "just under half", caps: []int{1024, 511, 511, 511}, ncpu: 4, want: 2},
		{name: "single prime", caps: []int{1024, 100, 100, 100, 100, 100, 100, 100}, ncpu: 8, want: 2},
		{name: "two core hetero", caps: []int{1024, 100}, ncpu: 2, want: 0},
		{name: "zeros", caps: []int{0, 0, 0, 0}, ncpu: 4, want: 0},
		{name: "len mismatch", caps: []int{1024, 100}, ncpu: 8, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PerformanceCount(tc.caps, tc.ncpu); got != tc.want {
				t.Fatalf("PerformanceCount=%d want %d", got, tc.want)
			}
		})
	}
}

func TestReadCapacities(t *testing.T) {
	dir := t.TempDir()
	writeCaps(t, dir, []int{1024, 400})
	got, ok := ReadCapacities(dir, 2)
	if !ok || len(got) != 2 || got[0] != 1024 || got[1] != 400 {
		t.Fatalf("got %v ok %v", got, ok)
	}
	if _, ok := ReadCapacities(dir, 3); ok {
		t.Fatal("missing cpu2 should fail closed")
	}
	if err := os.WriteFile(filepath.Join(dir, "cpu1", "cpu_capacity"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadCapacities(dir, 2); ok {
		t.Fatal("unparsable capacity should fail closed")
	}
}

func TestApplyRootCapsProcs(t *testing.T) {
	if runtime.NumCPU() < 4 {
		t.Skip("need at least 4 cpus")
	}
	prev := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	dir := t.TempDir()
	caps := make([]int, runtime.NumCPU())
	for i := range caps {
		caps[i] = 200
		if i < 2 {
			caps[i] = 1024
		}
	}
	writeCaps(t, dir, caps)
	procs, total, ok := applyRoot(dir, true)
	if !ok || procs != 2 || total != runtime.NumCPU() {
		t.Fatalf("apply procs=%d total=%d ok=%v", procs, total, ok)
	}
	if got := runtime.GOMAXPROCS(0); got != 2 {
		t.Fatalf("GOMAXPROCS=%d", got)
	}
	runtime.GOMAXPROCS(prev)
	if _, _, ok := applyRoot(dir, false); ok {
		t.Fatal("disabled apply should leave the runtime alone")
	}
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Fatalf("GOMAXPROCS changed to %d", got)
	}
	if _, _, ok := applyRoot(t.TempDir(), true); ok {
		t.Fatal("missing sysfs should leave the runtime alone")
	}
}

func writeCaps(t *testing.T, root string, caps []int) {
	t.Helper()
	for i, c := range caps {
		dir := filepath.Join(root, "cpu"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cpu_capacity"), []byte(strconv.Itoa(c)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
