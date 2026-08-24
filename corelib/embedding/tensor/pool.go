package tensor

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// Persistent worker pool for matmul parallel sections.
// Avoids spawning/joining NumCPU goroutines on every MatMul call
// (~70 layers × 4 matmuls × 16 spawns was a measurable cost).

type matmulRangeJob struct {
	start, end int
	fn         func(start, end int)
	run        func()
	task       matmulRangeTask
	asyncTask  AsyncTask
	wg         *sync.WaitGroup
}

type matmulRangeTask interface {
	runRange(start, end int)
}

// AsyncTask is one non-partitioned unit of work submitted to the matmul pool.
// It lets hot callers reuse a structured task instead of allocating a closure.
type AsyncTask interface {
	RunAsyncTask()
}

var (
	poolOnce     sync.Once
	jobQueue     chan matmulRangeJob
	poolSize     int32
	poolInited   int32
	jobEnqueueN  int64
)

// JobEnqueueCount is the number of jobs submitted to the process-wide matmul pool.
func JobEnqueueCount() int64 { return atomic.LoadInt64(&jobEnqueueN) }

// ResetJobEnqueueCount zeros the enqueue counter (tests).
func ResetJobEnqueueCount() { atomic.StoreInt64(&jobEnqueueN, 0) }

func ensureMatmulPool() {
	poolOnce.Do(func() {
		// Respect GOMAXPROCS: callers use it to cap actual CPU parallelism
		// (including reproducible single-core inference). Spawning NumCPU jobs
		// when GOMAXPROCS=1 only adds queueing/context-switch overhead.
		n := runtime.GOMAXPROCS(0)
		// Cap workers: too many thrash L3 on encoder mats; CTC (N≈25k) still
		// benefits up to ~12 on 16-thread CPUs.
		if n > 12 {
			n = 12
		}
		if n < 1 {
			n = 1
		}
		atomic.StoreInt32(&poolSize, int32(n))
		jobQueue = make(chan matmulRangeJob, n*4)
		for i := 0; i < n; i++ {
			go func() {
				for j := range jobQueue {
					if j.task != nil {
						j.task.runRange(j.start, j.end)
					} else if j.asyncTask != nil {
						j.asyncTask.RunAsyncTask()
					} else if j.run != nil {
						j.run()
					} else {
						j.fn(j.start, j.end)
					}
					j.wg.Done()
				}
			}()
		}
		atomic.StoreInt32(&poolInited, 1)
	})
}

func poolWorkers() int {
	ensureMatmulPool()
	n := int(atomic.LoadInt32(&matMulMaxParallel))
	if n > 0 {
		ps := int(atomic.LoadInt32(&poolSize))
		if n > ps {
			return ps
		}
		return n
	}
	return int(atomic.LoadInt32(&poolSize))
}

// matMulWorkersFor limits only small encoder projections. For these shapes the
// 12-way pool's dispatch and cache traffic can outweigh its extra compute lanes;
// SenseVoice command-length audio commonly reaches M=5..8 here. Very wide CTC
// argmax (N≈25k) retains the full pool even for short utterances.
func matMulWorkersFor(M, N, K int) int {
	nw := poolWorkers()
	if M >= 2 && M <= 8 && N >= 512 && N <= 4096 && K >= 256 && nw > 8 {
		return 8
	}
	return nw
}

// ParallelRanges splits [0, total) into up to nw contiguous ranges and runs
// fn(start,end) on the matmul worker pool. fn must be safe for concurrent
// disjoint ranges. Prefer this over spawning goroutines per call site
// (e.g. attention heads across ~70 SANM layers).
func ParallelRanges(total int, fn func(start, end int)) {
	parallelRanges(total, fn)
}

// RunAsyncTask submits a reusable structured task. The caller owns wg and must
// wait before returning its task to a pool or mutating its fields.
func RunAsyncTask(task AsyncTask, wg *sync.WaitGroup) {
	ensureMatmulPool()
	wg.Add(1)
	jobQueue <- matmulRangeJob{asyncTask: task, wg: wg}
}

// RunAsync submits fn to the matmul worker pool and returns a wait func.
// Prefer this over bare `go fn()` in the encoder (reuses pool goroutines;
// ~70 SANM layers × go would otherwise thrash the runtime).
func RunAsync(fn func()) (wait func()) {
	ensureMatmulPool()
	var wg sync.WaitGroup
	wg.Add(1)
	jobQueue <- matmulRangeJob{
		run: fn,
		wg:  &wg,
	}
	return wg.Wait
}

// parallelRanges splits [0, total) into up to nw contiguous ranges and runs
// fn(start,end) on the pool. fn must be safe for concurrent disjoint ranges.
//
// For large totals (matmul N-partition), prefer chunks ≥32 so workers are not
// starved of B columns. Small totals (heads=4) keep full parallel width.
func parallelRanges(total int, fn func(start, end int)) {
	parallelRangesWithWorkers(total, poolWorkers(), fn)
}

func parallelRangesForMatMul(M, N, K int, fn func(start, end int)) {
	parallelRangesWithWorkers(N, matMulWorkersFor(M, N, K), fn)
}

func parallelRangesWithWorkers(total, nw int, fn func(start, end int)) {
	nw = rangeWorkers(total, nw)
	if nw <= 0 {
		return
	}
	if nw == 1 {
		fn(0, total)
		return
	}
	ensureMatmulPool()
	var wg sync.WaitGroup
	chunk := (total + nw - 1) / nw
	for w := 0; w < nw; w++ {
		s := w * chunk
		e := s + chunk
		if e > total {
			e = total
		}
		if s >= e {
			break
		}
		wg.Add(1)
		atomic.AddInt64(&jobEnqueueN, 1)
		jobQueue <- matmulRangeJob{start: s, end: e, fn: fn, wg: &wg}
	}
	wg.Wait()
}

func rangeWorkers(total, nw int) int {
	if total <= 0 {
		return 0
	}
	if nw > total {
		nw = total
	}
	// Wide partitions only: avoid N=512 → 12×42-col slices (A reload thrash).
	// Keep minChunk=32: fatter floors (64/256) cut workers to ≤8 and regressed
	// e2e ~126ms → ~190ms+ on 8745HS (parallelism loss > L3 thrash savings).
	if total >= 384 {
		const minChunk = 32
		if maxUseful := (total + minChunk - 1) / minChunk; nw > maxUseful {
			nw = maxUseful
		}
	}
	if nw <= 1 {
		return 1
	}
	return nw
}

// flopThreshold: below this, serial is faster than pool dispatch.
// ~1e6 ≈ M=8×N=256×K=512 — small encoder tiles stay serial; CTC/FFN still parallel.
const flopThreshold int64 = 1_000_000

func shouldParallel(M, N, K int) bool {
	if poolWorkers() <= 1 {
		return false
	}
	if N <= 1 {
		return false
	}
	flops := int64(M) * int64(N) * int64(K)
	return flops >= flopThreshold
}
