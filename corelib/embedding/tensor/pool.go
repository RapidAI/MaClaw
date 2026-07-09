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
	wg         *sync.WaitGroup
}

var (
	poolOnce   sync.Once
	jobQueue   chan matmulRangeJob
	poolSize   int32
	poolInited int32
)

func ensureMatmulPool() {
	poolOnce.Do(func() {
		n := runtime.NumCPU()
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
					j.fn(j.start, j.end)
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

// ParallelRanges splits [0, total) into up to nw contiguous ranges and runs
// fn(start,end) on the matmul worker pool. fn must be safe for concurrent
// disjoint ranges. Prefer this over spawning goroutines per call site
// (e.g. attention heads across ~70 SANM layers).
func ParallelRanges(total int, fn func(start, end int)) {
	parallelRanges(total, fn)
}

// parallelRanges splits [0, total) into up to nw contiguous ranges and runs
// fn(start,end) on the pool. fn must be safe for concurrent disjoint ranges.
func parallelRanges(total int, fn func(start, end int)) {
	if total <= 0 {
		return
	}
	nw := poolWorkers()
	if nw > total {
		nw = total
	}
	if nw <= 1 {
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
		jobQueue <- matmulRangeJob{start: s, end: e, fn: fn, wg: &wg}
	}
	wg.Wait()
}

// flopThreshold: below this, serial is faster than pool dispatch.
const flopThreshold int64 = 1_500_000

func shouldParallel(M, N, K int) bool {
	if poolWorkers() <= 1 {
		return false
	}
	flops := int64(M) * int64(N) * int64(K)
	return flops >= flopThreshold && N > 1
}
