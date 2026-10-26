package embedding

import (
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// TestResolveEmbedThreads covers the CPU shapes the adaptive policy has to
// answer for: high-end desktop, low-end 2C/4T, SMT vs no-SMT, weak ISA, and
// the explicit override.
func TestResolveEmbedThreads(t *testing.T) {
	cases := []struct {
		name                    string
		env, isa                string
		phys, logical, maxprocs int
		want                    int
	}{
		// High-end desktop (the 8745HS the table in threads.go was measured
		// on): 8 physical cores, but int8 kernels saturate at 4.
		{"desktop avx512", "", "avx512", 8, 16, 16, 4},
		{"desktop avx2", "", "avx2", 8, 16, 16, 4},
		// Low-end dual core: below the knee, so physical cores win.
		{"2c/4t avx2", "", "avx2", 2, 4, 4, 2},
		{"4c/4t avx2", "", "avx2", 4, 4, 4, 4},
		// No SMT: logical == physical, still capped at the knee.
		{"4c/4t avx512", "", "avx512", 4, 4, 4, 4},
		// No int8 GEMM (old Atom/Celeron, or MACLAW_Q8P=0): float kernels keep
		// scaling, so the cap is 8.
		{"float path 8c", "", "off", 8, 16, 16, 8},
		{"float path 4c", "", "off", 4, 4, 4, 4},
		// GOMAXPROCS (container / cgroup limit) is a hard ceiling.
		{"gomaxprocs 2", "", "avx512", 8, 16, 2, 2},
		{"gomaxprocs 1", "", "avx512", 8, 16, 1, 1},
		// Platform cannot report physical cores: fall back to logical.
		{"unknown phys", "", "avx2", 0, 8, 16, 4},
		{"unknown phys float", "", "off", 0, 8, 16, 8},
		// Nothing known at all: never zero.
		{"all zero", "", "avx2", 0, 0, 0, 1},
		// Explicit override wins over every derived value.
		{"override", "6", "avx512", 8, 16, 16, 6},
		{"override low", "1", "off", 8, 16, 16, 1},
		{"override ignores cap", "12", "avx2", 8, 16, 16, 12},
		// Malformed / zero override falls through to adaptive.
		{"bad override", "abc", "avx512", 8, 16, 16, 4},
		{"zero override", "0", "avx512", 8, 16, 16, 4},
		{"negative override", "-3", "avx512", 8, 16, 16, 4},
	}
	for _, c := range cases {
		got := resolveEmbedThreads(c.env, c.isa, c.phys, c.logical, c.maxprocs)
		if got != c.want {
			t.Errorf("%s: resolveEmbedThreads(env=%q isa=%q phys=%d logical=%d maxprocs=%d) = %d, want %d",
				c.name, c.env, c.isa, c.phys, c.logical, c.maxprocs, got, c.want)
		}
	}
}

// TestEmbedThreadsSanity checks the live value on this machine is usable: it
// must never exceed the logical CPU count or GOMAXPROCS, and never be zero
// (zero would mean "no cap", the exact regression this policy exists to stop).
func TestEmbedThreadsSanity(t *testing.T) {
	n := EmbedThreads()
	if n < 1 {
		t.Fatalf("EmbedThreads() = %d, want >= 1", n)
	}
	if max := runtime.NumCPU(); n > max {
		t.Fatalf("EmbedThreads() = %d exceeds NumCPU %d", n, max)
	}
	if max := runtime.GOMAXPROCS(0); n > max {
		t.Fatalf("EmbedThreads() = %d exceeds GOMAXPROCS %d", n, max)
	}
	if cap := embedThreadCap(tensor.Q8PISAName()); n > cap {
		t.Fatalf("EmbedThreads() = %d exceeds ISA cap %d", n, cap)
	}
	t.Logf("EmbedThreads()=%d isa=%s phys=%d logical=%d gomaxprocs=%d",
		n, tensor.Q8PISAName(), physicalCores(), runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

// TestEmbedThreadsStableAcrossCalls pins the sync.Once cache.
func TestEmbedThreadsStableAcrossCalls(t *testing.T) {
	first := EmbedThreads()
	for i := 0; i < 5; i++ {
		if got := EmbedThreads(); got != first {
			t.Fatalf("EmbedThreads() changed between calls: %d then %d", first, got)
		}
	}
}

// TestPhysicalCoresSane verifies the platform probe does not return nonsense.
func TestPhysicalCoresSane(t *testing.T) {
	p := physicalCores()
	if p < 0 {
		t.Fatalf("physicalCores() = %d, want >= 0", p)
	}
	if max := runtime.NumCPU(); p > max {
		t.Fatalf("physicalCores() = %d exceeds NumCPU %d", p, max)
	}
	t.Logf("physicalCores()=%d NumCPU=%d", p, runtime.NumCPU())
}
