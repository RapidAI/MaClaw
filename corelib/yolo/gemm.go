//go:build amd64

package yolo

import (
	"os"
	"runtime"
	"strconv"
	"sync"
)

const (
	// gemmPanelWidth is the N-panel width for cache blocking. With K ≤ 4608
	// a panel is at most ~2.3MB (L3-resident) and gets re-streamed from
	// L1/L2 across the m-block loop instead of from RAM.
	gemmPanelWidth = 128
	// gemmSuperM is the number of output rows per super-block. The B panel
	// (K × panelWidth) is streamed from L3/RAM once per super-block and the
	// [gemmSuperM × panelWidth] C tile is accumulated in a compact L1/L2
	// scratch — so B traffic drops from (M/6)·K·N to (M/gemmSuperM)·K·N.
	gemmSuperM = 192
	// gemmKChunk bounds a k-chunk so its B slab (gemmKChunk × panelWidth,
	// ≤128KB) stays L2-resident while the super-block's m-blocks re-read it.
	gemmKChunk = 512
	// Below this many MACs, goroutine setup dominates the GEMM itself.
	gemmSingleThreadMACs = 1 << 22
)

// Runtime-tunable GEMM parameters (env-overridable for benchmarking;
// the defaults are what production uses).
var (
	gemmWorkers = envOrInt("YOLO_GEMM_WORKERS", runtime.NumCPU())
	gemmSuper   = envOrInt("YOLO_GEMM_SUPER", gemmSuperM)
	gemmKChk    = envOrInt("YOLO_GEMM_KCHUNK", gemmKChunk)
)

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// matmulConvActiv computes out[M,N] = W[M,K] × col[K,N] + bias with operator
// fusion: when silu is requested and the AVX2 kernel is available, the SiLU
// activation is applied in the kernel epilogue (no separate activation pass
// over the output tensor). It reports whether the activation was applied.
// matmulConvActiv computes out[M,N] = W[M,K] × col[K,N] + bias with operator
// fusion (see gemmConvAVX2). ldb is the physical row stride of col in
// elements; pass N when col is densely packed. It reports whether the AVX2
// kernel handled the activation.
func matmulConvActiv(W, col, bias, out []float32, M, K, N, ldb int, silu bool) bool {
	if hasAVX2FMA && N%16 == 0 && N >= 16 && K >= 1 && M >= 1 {
		gemmConvAVX2(W, col, bias, out, M, K, N, ldb, silu)
		return true
	}
	matmulConvLegacy(W, col, bias, out, M, K, N)
	return false
}

// gemmConvAVX2 computes out[M,N] = W[M,K] × col[K,N] + bias with bias
// initialization and an optional fused SiLU epilogue (see gemm_amd64.s).
//
// Requirements: N % 16 == 0, N >= 16. Handles any M >= 1: output rows are
// processed in super-blocks accumulated k-chunk by k-chunk into a compact
// scratch tile (the first chunk initializes with bias via gemm6x16AVX2,
// later chunks accumulate via gemm6x16AccAVX2, SiLU is applied on the
// scratch before the copy-out), so the im2col matrix is streamed from
// L3/RAM only M/gemmSuperM times instead of M/6.
//
// Work is parallelized across N-panels (see gemmPanelWidth).
// gemmConvAVX2 computes out[M,N] = W[M,K] × col[K,N] + bias (+ fused SiLU).
// ldb is col's physical row stride (elements); ldb = N + 16 avoids the 4K
// store/load aliasing that halves throughput when N*4 is a multiple of 4096.
func gemmConvAVX2(W, col, bias, out []float32, M, K, N, ldb int, silu bool) {
	nPanels := (N + gemmPanelWidth - 1) / gemmPanelWidth
	nWorkers := gemmWorkers
	if nWorkers > nPanels {
		nWorkers = nPanels
	}
	if nWorkers <= 1 || int64(M)*int64(K)*int64(N) < gemmSingleThreadMACs {
		gemmRunPanels(W, col, bias, out, M, K, N, ldb, silu, 0, 1)
		return
	}
	var wg sync.WaitGroup
	for w := 0; w < nWorkers; w++ {
		wg.Add(1)
		go func(first int) {
			defer wg.Done()
			gemmRunPanels(W, col, bias, out, M, K, N, ldb, silu, first, nWorkers)
		}(w)
	}
	wg.Wait()
}

// gemmRunPanels processes work units (panel × super-block, plus one tail
// unit per panel) round-robin with its own scratch buffers (pooled; the
// A-tail pad rows stay zero across units). Unit-level distribution keeps
// all workers busy even for small N (few panels).
func gemmRunPanels(W, col, bias, out []float32, M, K, N, ldb int, silu bool, first, step int) {
	aScratch := getBuf(6 * K)
	defer putBuf(aScratch)
	biasScratch := make([]float32, 6)
	nPanels := (N + gemmPanelWidth - 1) / gemmPanelWidth
	mFull := M / 6 * 6
	nSupers := (mFull + gemmSuper - 1) / gemmSuper
	tailUnit := nSupers // per-panel tail rows unit index
	nUnits := nPanels * (nSupers + 1)
	for u := first; u < nUnits; u += step {
		p := u / (nSupers + 1)
		s := u % (nSupers + 1)
		if s == tailUnit {
			gemmPanelTail(W, col, bias, out, M, K, N, ldb, p, silu, aScratch, biasScratch)
			continue
		}
		gemmSuperUnit(W, col, bias, out, M, K, N, ldb, p, s, mFull, silu)
	}
}

// gemmSuperUnit computes one super-block [ms,me) of rows for one N-panel,
// accumulating k-chunk by k-chunk directly into out (the C tile's cache
// footprint is identical to a compact scratch at this size, and the
// direct store eliminates a full copy round-trip). The B panel is streamed
// from L3/RAM once per super-block.
func gemmSuperUnit(W, col, bias, out []float32, M, K, N, ldb, panel, super, mFull int, silu bool) {
	n0 := panel * gemmPanelWidth
	b := col[n0:]
	c := out[n0:]
	nTiles := gemmPanelWidth
	if n0+nTiles > N {
		nTiles = N - n0
	}
	nTiles /= 16
	w := nTiles * 16

	ms := super * gemmSuper
	me := ms + gemmSuper
	if me > mFull {
		me = mFull
	}
	single := K <= gemmKChk
	k0 := 0
	first := true
	for k0 < K {
		kc := K - k0
		if kc > gemmKChk {
			kc = gemmKChk
		}
		for m0 := ms; m0 < me; m0 += 6 {
			a := &W[m0*K+k0]
			for t := 0; t < nTiles; t++ {
				cp := &c[m0*N+t*16]
				bk := &b[k0*ldb+t*16]
				if first {
					// Single-chunk layers fuse SiLU right in the kernel
					// epilogue; multi-chunk layers take a separate pass.
					gemm6x16AVX2(a, bk, cp, &bias[m0], kc, K, ldb, N, single && silu)
				} else {
					gemm6x16AccAVX2(a, bk, cp, kc, K, ldb, N)
				}
			}
		}
		first = false
		k0 += kc
	}
	if silu && !single {
		for r := ms; r < me; r++ {
			siluSlice(c[r*N : r*N+w])
		}
	}
}

// gemmPanelTail computes the ragged tail rows (M % 6) of one N-panel:
// zero-padded A scratch, one full-K kernel per 16-column tile.
func gemmPanelTail(W, col, bias, out []float32, M, K, N, ldb, panel int, silu bool, aScratch, biasScratch []float32) {
	mFull := M / 6 * 6
	rem := M - mFull
	if rem == 0 {
		return
	}
	n0 := panel * gemmPanelWidth
	b := col[n0:]
	c := out[n0:]
	nTiles := gemmPanelWidth
	if n0+nTiles > N {
		nTiles = N - n0
	}
	nTiles /= 16
	copy(aScratch[:rem*K], W[mFull*K:M*K])
	copy(biasScratch, bias[mFull:M])
	var cTile [6 * 16]float32
	for t := 0; t < nTiles; t++ {
		gemm6x16AVX2(&aScratch[0], &b[t*16], &cTile[0], &biasScratch[0], K, K, ldb, 16, silu)
		for r := 0; r < rem; r++ {
			copy(c[(mFull+r)*N+t*16:(mFull+r)*N+t*16+16], cTile[r*16:r*16+16])
		}
	}
}
