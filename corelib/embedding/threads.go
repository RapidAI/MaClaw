package embedding

// threads.go — adaptive worker count for one Qwen3 forward pass.
//
// The embedding kernels are memory-bound once the Q8P int8 tiles are in play:
// adding threads past the memory-bandwidth knee buys almost no latency but
// costs a lot of total CPU. On low-end machines that shows up as a permanently
// busy CPU (fan spin, battery drain) for a latency gain no user can feel, so
// the default width is derived from the CPU rather than "all logical cores".

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// EmbedThreadsEnv overrides the adaptive worker count (0 or unset = adaptive).
const EmbedThreadsEnv = "MACLAW_EMBED_THREADS"

var (
	embedThreadsOnce sync.Once
	embedThreads     int
)

// EmbedThreads is the worker count one Qwen3 forward should use.
//
// Measured on Ryzen 7 8745HS (8C/16T, AVX-512) with Qwen3-Embedding-0.6B Q8_0,
// 6-8 token inputs, 3 interleaved rounds (p50 latency | total CPU per embed):
//
//	threads   AVX-512            AVX2                float (Q8P off)
//	1         51.4ms |  74.2ms    92.9ms | 118.4ms   160.4ms | 209.4ms
//	2         37.1ms |  87.5ms    56.3ms | 132.6ms   107.2ms | 227.7ms
//	4         29.7ms |  99.2ms    32.3ms | 133.8ms    51.8ms | 218.4ms
//	8         28.6ms | 114.5ms    28.1ms | 163.4ms    42.2ms | 231.6ms
//
// The int8 kernels flatten out at 4 threads: 4 -> 8 buys 4% latency for 15%
// (AVX-512) / 22% (AVX2) more total CPU. The float path still scales (roughly
// 3x the arithmetic per byte of weights), so it keeps a wider cap. Everything
// is additionally clamped to the physical core count: SMT siblings share the
// load/store units the kernels are already saturating, so they add CPU without
// throughput.
func EmbedThreads() int {
	embedThreadsOnce.Do(func() {
		embedThreads = resolveEmbedThreads(
			os.Getenv(EmbedThreadsEnv),
			tensor.Q8PISAName(),
			physicalCores(),
			runtime.NumCPU(),
			runtime.GOMAXPROCS(0),
		)
	})
	return embedThreads
}

// resolveEmbedThreads is the pure policy behind EmbedThreads (tests call it
// directly with synthetic CPU shapes).
func resolveEmbedThreads(env, isa string, phys, logical, maxprocs int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(env)); err == nil && n > 0 {
		return n
	}
	cap := embedThreadCap(isa)
	// Physical cores are the real compute width; fall back to logical when the
	// platform cannot report them (phys <= 0).
	width := phys
	if width <= 0 {
		width = logical
	}
	if width <= 0 {
		width = 1
	}
	if maxprocs > 0 && width > maxprocs {
		width = maxprocs
	}
	if width > cap {
		width = cap
	}
	if width < 1 {
		width = 1
	}
	return width
}

// embedThreadCap is the per-ISA latency/cost knee from the table above.
func embedThreadCap(isa string) int {
	switch isa {
	case "avx512", "avxvnni", "avx2", "i8mm", "sdot":
		return 4
	case "off":
		// No int8 GEMM: the float kernels are compute-bound and keep paying
		// off across more cores.
		return 8
	default:
		return 4
	}
}
