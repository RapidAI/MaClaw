package tensor

import (
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
)

// Q8Tensor holds a reference to Q8_0 quantized data (typically mmap-backed).
// Block layout: [scale:f16(2 bytes)][d0..d31:int8(32 bytes)] = 34 bytes per 32 elements.
// The data slice is NOT owned — it points into the mmap region.
//
// Scales is an optional f32 scale cache (len = Rows*(Cols/32)), built by
// PrepareScales(). When present, dequant skips f16→f32 conversion on the hot path.
type Q8Tensor struct {
	Data   []byte    // raw Q8_0 blocks
	Scales []float32 // optional preconverted f32 scales, row-major by block
	Rows   int       // number of rows (outer dimension)
	Cols   int       // number of columns (inner dimension, must be multiple of 32)
}

// PrepareScales extracts all f16 block scales into f32. Safe to call multiple times.
// Call after load for inference-critical weights (encoder / CTC).
func (t *Q8Tensor) PrepareScales() {
	if t == nil || t.Scales != nil || t.Cols < q8BlockSize || len(t.Data) == 0 {
		return
	}
	nBlocks := t.Cols / q8BlockSize
	need := t.Rows * nBlocks
	scales := make([]float32, need)
	for r := 0; r < t.Rows; r++ {
		base := r * nBlocks
		rowOff := base * q8BlockBytes
		for b := 0; b < nBlocks; b++ {
			off := rowOff + b*q8BlockBytes
			scales[base+b] = float16to32(binary.LittleEndian.Uint16(t.Data[off:]))
		}
	}
	t.Scales = scales
}

const (
	q8BlockSize  = 32
	q8BlockBytes = 2 + q8BlockSize // 34 bytes per block
)

// DequantRow dequantizes a single row into dst (must be len >= t.Cols).
// This is used for token embedding lookup — only one row at a time.
// Uses f32 scale cache when PrepareScales() has been called.
func (t *Q8Tensor) DequantRow(row int, dst []float32) {
	cols := t.Cols
	nBlocks := cols / q8BlockSize
	rowOff := row * nBlocks * q8BlockBytes
	end := rowOff + nBlocks*q8BlockBytes
	if end > len(t.Data) {
		return // out of bounds — caller should validate row index
	}
	dequantQ8Row(t, row, dst)
}

// matMulMaxParallel controls internal parallelism for MatMul operations.
// 0 = default (NumCPU). Set to 1 to force single-threaded (for batch-level parallelism).
var matMulMaxParallel int32

// SetMatMulMaxParallel sets the internal parallelism limit.
func SetMatMulMaxParallel(n int) { atomic.StoreInt32(&matMulMaxParallel, int32(n)) }

func getMatMulWorkers() int {
	// Prefer pool size (capped) so dispatch cost stays low.
	return poolWorkers()
}

// MatMulQ8 computes out = A @ B^T where A is [M, K] float32 and B is Q8_0 [N, K].
// Result out is [M, N].
//
// Each B row is dequantized ONCE then dotted against all M A rows (N-outer).
// Parallelizes across N with a work-size floor to avoid oversubscription.
func MatMulQ8(out, a []float32, b *Q8Tensor, M, N, K int) {
	MatMulQ8Bias(out, a, b, nil, M, N, K)
}

// MatMulQ8Bias is MatMulQ8 with optional bias fused into the store:
// out[m,n] = dot(A[m], dequant(B[n])) + bias[n].
//
// Loop order for M>1: M-tile outer, N inner — keeps an A panel (8×K ≈ 16KB for
// K=512) hot in L1 while streaming all B rows. N-outer would reload A for every
// B row (A is ~200KB for M=100,K=512 — larger than L1).
func MatMulQ8Bias(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if M == 1 {
		if shouldParallel(1, N, K) {
			matMulQ8ParallelN_M1(out, a, b, bias, N, K)
			return
		}
		matMulQ8Serial_M1(out, a, b, bias, N, K)
		return
	}
	// Encoder-style: parallelize over N (partition B), M-tile outer inside
	// each worker so the A panel stays hot for that worker's B slice.
	if shouldParallel(M, N, K) {
		matMulQ8ParallelN_MTile(out, a, b, bias, M, N, K)
		return
	}
	matMulQ8SerialM(out, a, b, bias, M, N, K)
}

// MatMulQ8BiasReLU is MatMulQ8Bias with ReLU fused at the store:
// out[m,n] = max(0, dot + bias[n]). Used for FFN up-projection.
func MatMulQ8BiasReLU(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if M == 1 {
		MatMulQ8Bias(out, a, b, bias, M, N, K)
		reluInplace(out[:N])
		return
	}
	if shouldParallel(M, N, K) {
		matMulQ8ParallelN_MTileAct(out, a, b, bias, M, N, K, true)
		return
	}
	matMulQ8SerialMAct(out, a, b, bias, M, N, K, true)
}

func reluInplace(x []float32) {
	if len(x) == 0 {
		return
	}
	// SIMD max(x, 0) via vek path in ops — call through local to avoid import cycle.
	// Implemented below in this file without vek to keep q8 self-contained.
	n := len(x)
	i := 0
	for ; i+7 < n; i += 8 {
		if x[i] < 0 {
			x[i] = 0
		}
		if x[i+1] < 0 {
			x[i+1] = 0
		}
		if x[i+2] < 0 {
			x[i+2] = 0
		}
		if x[i+3] < 0 {
			x[i+3] = 0
		}
		if x[i+4] < 0 {
			x[i+4] = 0
		}
		if x[i+5] < 0 {
			x[i+5] = 0
		}
		if x[i+6] < 0 {
			x[i+6] = 0
		}
		if x[i+7] < 0 {
			x[i+7] = 0
		}
	}
	for ; i < n; i++ {
		if x[i] < 0 {
			x[i] = 0
		}
	}
}

func matMulQ8Serial_M1(out, a []float32, b *Q8Tensor, bias []float32, N, K int) {
	matMulQ8Range_M1(out, a, b, bias, N, K, 0, N)
}

func matMulQ8ParallelN_M1(out, a []float32, b *Q8Tensor, bias []float32, N, K int) {
	parallelRanges(N, func(ns, ne int) {
		matMulQ8Range_M1(out, a, b, bias, N, K, ns, ne)
	})
}

// matMulQ8Range_M1: dual-B DotQ8 — loads A once per Q8 block for two B columns.
// Uses f32 scale cache when available.
func matMulQ8Range_M1(out, a []float32, b *Q8Tensor, bias []float32, N, K, ns, ne int) {
	nBlocks := K / q8BlockSize
	aRow := a[:K]
	hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0
	n := ns
	if hasScales {
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
			out[n] = s0 + bn0
			out[n+1] = s1 + bn1
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[n] = DotQ8RowScaled(aRow, b, n) + bn
		}
		return
	}
	for ; n+1 < ne; n += 2 {
		bn0, bn1 := float32(0), float32(0)
		if bias != nil {
			bn0, bn1 = bias[n], bias[n+1]
		}
		s0, s1 := DotQ8RowDual(aRow, b.Data, n, n+1, nBlocks)
		out[n] = s0 + bn0
		out[n+1] = s1 + bn1
	}
	for ; n < ne; n++ {
		bn := float32(0)
		if bias != nil {
			bn = bias[n]
		}
		out[n] = DotQ8Row(aRow, b.Data, n, nBlocks) + bn
	}
}

// matMulQ8SerialM: M-tile outer, N inner — A panel stays hot.
func matMulQ8SerialM(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	matMulQ8SerialMAct(out, a, b, bias, M, N, K, false)
}

// matMulQ8ParallelN_MTile partitions N across pool workers; each worker uses
// M-tile-outer so its A panels stay hot for its B-column range.
func matMulQ8ParallelN_MTile(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	matMulQ8ParallelN_MTileAct(out, a, b, bias, M, N, K, false)
}

func storeDot4(out []float32, m, n, N int, d *[4]float32, bn float32, relu bool) {
	if relu {
		for t := 0; t < 4; t++ {
			v := d[t] + bn
			if v < 0 {
				v = 0
			}
			out[(m+t)*N+n] = v
		}
		return
	}
	out[m*N+n] = d[0] + bn
	out[(m+1)*N+n] = d[1] + bn
	out[(m+2)*N+n] = d[2] + bn
	out[(m+3)*N+n] = d[3] + bn
}

func storeDot8(out []float32, m, n, N int, d *[8]float32, bn float32, relu bool) {
	if relu {
		for t := 0; t < 8; t++ {
			v := d[t] + bn
			if v < 0 {
				v = 0
			}
			out[(m+t)*N+n] = v
		}
		return
	}
	out[m*N+n] = d[0] + bn
	out[(m+1)*N+n] = d[1] + bn
	out[(m+2)*N+n] = d[2] + bn
	out[(m+3)*N+n] = d[3] + bn
	out[(m+4)*N+n] = d[4] + bn
	out[(m+5)*N+n] = d[5] + bn
	out[(m+6)*N+n] = d[6] + bn
	out[(m+7)*N+n] = d[7] + bn
}

// q8BPanelRows picks N-tile width so the float B panel stays ~L1-sized.
// For wide N ranges (CTC under parallel workers) keep the panel smaller.
func q8BPanelRows(K, nRange int) int {
	if K <= 0 {
		return 8
	}
	// Target ~32KB panel; shrink when the worker already owns a huge N slice.
	budget := 32 * 1024
	if nRange > 4096 {
		budget = 16 * 1024
	}
	nt := budget / (K * 4)
	if nt < 4 {
		nt = 4
	}
	if nt > 32 {
		nt = 32
	}
	// Dual-B wants even tile width.
	if nt&1 != 0 {
		nt--
	}
	if nt < 4 {
		nt = 4
	}
	return nt
}

// useQ8DequantOnce: prefer dequant-once + dual-B F32 multiDot when re-dequant
// would dominate. Cap K: for K=2048, float B rows are 4× Q8 size and fused
// multiDot streams less memory (measured faster than dequant-once).
func useQ8DequantOnce(M, N, K int, hasScales bool) bool {
	_ = N
	if M < 2 || K <= 0 {
		return false
	}
	if hasScales {
		// K=512/560/768 encoder shapes; not K=2048 FFN down-proj.
		return M >= 8 && K <= 1024
	}
	return M >= 32 && K <= 768
}

// mTileForK picks M micro-tile so A panel stays cache-friendly (~32KB).
// K=512 → 8 (16KB); K=2048 → 4 (32KB, fits L2; 8 would be 64KB and thrash L1).
func mTileForK(K int) int {
	if K <= 0 {
		return 4
	}
	// tile * K * 4 <= 32768 → tile <= 8192/K
	t := 8192 / K
	if t >= 8 {
		return 8
	}
	if t >= 4 {
		return 4
	}
	if t >= 2 {
		return 2
	}
	return 1
}

// matMulQ8Range computes columns [ns,ne) for all M rows.
func matMulQ8Range(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu bool) {
	nBlocks := K / q8BlockSize
	var d4 [4]float32
	var d8 [8]float32
	hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0

	if !useQ8DequantOnce(M, N, K, hasScales) {
		// Fused path: dual-B q8 multiDot; uses f32 Scales when available (K=2048 FFN).
		var dDual0, dDual1 [8]float32
		m := 0
		for ; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			n := ns
			for ; n+1 < ne; n += 2 {
				bn0, bn1 := float32(0), float32(0)
				if bias != nil {
					bn0, bn1 = bias[n], bias[n+1]
				}
				if hasScales {
					q8DualMultiDot8T(&dDual0, &dDual1, aPanel, b, n, n+1, nBlocks, K)
				} else {
					q8DualMultiDot8(&dDual0, &dDual1, aPanel, b.Data, n, n+1, nBlocks, K)
				}
				storeDual4(out, m, n, N, &dDual0, bn0, bn1, relu)
				storeDual4(out, m+4, n, N, &dDual1, bn0, bn1, relu)
			}
			for ; n < ne; n++ {
				bn := float32(0)
				if bias != nil {
					bn = bias[n]
				}
				if hasScales {
					q8MultiDot8T(&d8, aPanel, b, n, nBlocks, K)
				} else {
					q8MultiDot8(&d8, aPanel, b.Data, n, nBlocks, K)
				}
				storeDot8(out, m, n, N, &d8, bn, relu)
			}
		}
		for ; m+3 < M; m += 4 {
			aPanel := a[m*K : (m+4)*K]
			n := ns
			for ; n+1 < ne; n += 2 {
				bn0, bn1 := float32(0), float32(0)
				if bias != nil {
					bn0, bn1 = bias[n], bias[n+1]
				}
				if hasScales {
					q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, K)
				} else {
					q8DualMultiDot4(&dDual0, aPanel, b.Data, n, n+1, nBlocks, K)
				}
				storeDual4(out, m, n, N, &dDual0, bn0, bn1, relu)
			}
			for ; n < ne; n++ {
				bn := float32(0)
				if bias != nil {
					bn = bias[n]
				}
				if hasScales {
					q8MultiDot4T(&d4, aPanel, b, n, nBlocks, K)
				} else {
					q8MultiDot4(&d4, aPanel, b.Data, n, nBlocks, K)
				}
				storeDot4(out, m, n, N, &d4, bn, relu)
			}
		}
		for ; m < M; m++ {
			aRow := a[m*K : m*K+K]
			for n := ns; n < ne; n++ {
				bn := float32(0)
				if bias != nil {
					bn = bias[n]
				}
				var v float32
				if hasScales {
					v = DotQ8RowScaled(aRow, b, n) + bn
				} else {
					v = DotQ8Row(aRow, b.Data, n, nBlocks) + bn
				}
				if relu && v < 0 {
					v = 0
				}
				out[m*N+n] = v
			}
		}
		return
	}

	// Dequant each B row once into an L1-sized N-tile float panel, then multiDot.
	// M-tile adapts to K so A panel fits L1 (critical for FFN K=2048).
	nTile := q8BPanelRows(K, ne-ns)
	panel := getQ8DequantBuf(nTile * K)
	var dDual0, dDual1 [8]float32
	hasBias := bias != nil
	mt := mTileForK(K)
	for n0 := ns; n0 < ne; n0 += nTile {
		nt := nTile
		if n0+nt > ne {
			nt = ne - n0
		}
		for t := 0; t < nt; t++ {
			dequantQ8Row(b, n0+t, panel[t*K:(t+1)*K])
		}
		m := 0
		if mt >= 8 {
			for ; m+7 < M; m += 8 {
				aPanel := a[m*K : (m+8)*K]
				matMulPanelDual(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, relu, &dDual0, &dDual1, &d8, 8)
			}
		}
		if mt >= 4 {
			for ; m+3 < M; m += 4 {
				aPanel := a[m*K : (m+4)*K]
				matMulPanelDual(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, relu, &dDual0, &dDual1, &d8, 4)
			}
		}
		// mt==2 or remainder: dual-row multiDot4 is overkill; use multiDot4 on pairs via dual with 2 rows... use 1-row Dot.
		for ; m+1 < M; m += 2 {
			// Two A rows × dual B via multiDot4DualB needs 4 A rows packing.
			// Simple: two rows as multiDot4 with zero pad is wasteful — just Dot.
			a0 := a[m*K : m*K+K]
			a1 := a[(m+1)*K : (m+1)*K+K]
			for t := 0; t < nt; t++ {
				n := n0 + t
				bn := float32(0)
				if hasBias {
					bn = bias[n]
				}
				bp := panel[t*K : (t+1)*K]
				v0 := Dot(a0, bp) + bn
				v1 := Dot(a1, bp) + bn
				if relu {
					if v0 < 0 {
						v0 = 0
					}
					if v1 < 0 {
						v1 = 0
					}
				}
				out[m*N+n] = v0
				out[(m+1)*N+n] = v1
			}
		}
		for ; m < M; m++ {
			aRow := a[m*K : m*K+K]
			for t := 0; t < nt; t++ {
				n := n0 + t
				v := Dot(aRow, panel[t*K:(t+1)*K])
				if hasBias {
					v += bias[n]
				}
				if relu && v < 0 {
					v = 0
				}
				out[m*N+n] = v
			}
		}
	}
	putQ8DequantBuf(panel)
}

// matMulPanelDual runs dual-B multiDot for an A panel of 4 or 8 rows against
// a dequantized B panel of nt columns starting at n0.
func matMulPanelDual(out, aPanel, panel, bias []float32, m, n0, N, K, nt int, hasBias, relu bool, dDual0, dDual1 *[8]float32, d8 *[8]float32, rows int) {
	t := 0
	if rows >= 8 {
		if hasBias {
			for ; t+1 < nt; t += 2 {
				n := n0 + t
				multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
				storeDual4(out, m, n, N, dDual0, bias[n], bias[n+1], relu)
				storeDual4(out, m+4, n, N, dDual1, bias[n], bias[n+1], relu)
			}
			for ; t < nt; t++ {
				n := n0 + t
				multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
				storeDot8(out, m, n, N, d8, bias[n], relu)
			}
		} else {
			for ; t+1 < nt; t += 2 {
				n := n0 + t
				multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
				storeDual4(out, m, n, N, dDual0, 0, 0, relu)
				storeDual4(out, m+4, n, N, dDual1, 0, 0, relu)
			}
			for ; t < nt; t++ {
				n := n0 + t
				multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
				storeDot8(out, m, n, N, d8, 0, relu)
			}
		}
		return
	}
	// 4-row panel
	var d4 [4]float32
	if hasBias {
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			storeDual4(out, m, n, N, dDual0, bias[n], bias[n+1], relu)
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
			storeDot4(out, m, n, N, &d4, bias[n], relu)
		}
	} else {
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			storeDual4(out, m, n, N, dDual0, 0, 0, relu)
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
			storeDot4(out, m, n, N, &d4, 0, relu)
		}
	}
}

// storeDual4 writes 4 A rows × 2 B columns from multiDot4DualB layout:
// d[0:4]=col n, d[4:8]=col n+1.
func storeDual4(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32, relu bool) {
	if relu {
		for t := 0; t < 4; t++ {
			v0 := d[t] + bn0
			v1 := d[4+t] + bn1
			if v0 < 0 {
				v0 = 0
			}
			if v1 < 0 {
				v1 = 0
			}
			out[(m+t)*N+n] = v0
			out[(m+t)*N+n+1] = v1
		}
		return
	}
	out[m*N+n] = d[0] + bn0
	out[(m+1)*N+n] = d[1] + bn0
	out[(m+2)*N+n] = d[2] + bn0
	out[(m+3)*N+n] = d[3] + bn0
	out[m*N+n+1] = d[4] + bn1
	out[(m+1)*N+n+1] = d[5] + bn1
	out[(m+2)*N+n+1] = d[6] + bn1
	out[(m+3)*N+n+1] = d[7] + bn1
}

func matMulQ8SerialMAct(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int, relu bool) {
	matMulQ8Range(out, a, b, bias, M, N, K, 0, N, relu)
}

func matMulQ8ParallelN_MTileAct(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int, relu bool) {
	parallelRanges(N, func(ns, ne int) {
		matMulQ8Range(out, a, b, bias, M, N, K, ns, ne, relu)
	})
}

var q8DequantPool = sync.Pool{
	New: func() interface{} { return make([]float32, 0, 1024) },
}

func getQ8DequantBuf(n int) []float32 {
	buf := q8DequantPool.Get().([]float32)
	if cap(buf) < n {
		buf = make([]float32, n)
	}
	return buf[:n]
}

func putQ8DequantBuf(buf []float32) {
	q8DequantPool.Put(buf[:0])
}

// dequantRowInto dequantizes a Q8_0 row into dst (len >= nBlocks*32).
// Prefer dequantQ8Row when a Q8Tensor (with optional Scales) is available.
func dequantRowInto(data []byte, row int, nBlocks int, dst []float32) {
	rowOff := row * nBlocks * q8BlockBytes
	dequantRowIntoASM(dst, data, rowOff, nBlocks)
}

// dequantQ8Row dequantizes one row of t into dst, using f32 scale cache when ready.
func dequantQ8Row(t *Q8Tensor, row int, dst []float32) {
	nBlocks := t.Cols / q8BlockSize
	if len(t.Scales) >= (row+1)*nBlocks {
		dequantRowScaled(dst, t.Data, &t.Scales[row*nBlocks], row*nBlocks*q8BlockBytes, nBlocks)
		return
	}
	dequantRowInto(t.Data, row, nBlocks, dst)
}

// dequantRowScaled dequantizes using preconverted f32 scales[0:nBlocks].
// rowOff is the byte offset of the row in data.
func dequantRowScaled(dst []float32, data []byte, scales *float32, rowOff, nBlocks int) {
	dequantRowScaledASM(dst, data, scales, rowOff, nBlocks)
}

// dequantRowScaledScalar is the pure-Go scaled dequant (no f16 convert).
func dequantRowScaledScalar(dst []float32, data []byte, scales []float32, rowOff, nBlocks int) {
	for b := 0; b < nBlocks; b++ {
		scale := scales[b]
		base := b * q8BlockSize
		qOff := rowOff + b*q8BlockBytes + 2
		for i := 0; i < q8BlockSize; i += 8 {
			dst[base+i] = scale * float32(int8(data[qOff+i]))
			dst[base+i+1] = scale * float32(int8(data[qOff+i+1]))
			dst[base+i+2] = scale * float32(int8(data[qOff+i+2]))
			dst[base+i+3] = scale * float32(int8(data[qOff+i+3]))
			dst[base+i+4] = scale * float32(int8(data[qOff+i+4]))
			dst[base+i+5] = scale * float32(int8(data[qOff+i+5]))
			dst[base+i+6] = scale * float32(int8(data[qOff+i+6]))
			dst[base+i+7] = scale * float32(int8(data[qOff+i+7]))
		}
	}
}

// dequantRowIntoScalar is the pure Go fallback implementation.
// Uses 8x unrolled loop to help the compiler auto-vectorize.
func dequantRowIntoScalar(dst []float32, data []byte, rowOff int, nBlocks int) {
	for b := 0; b < nBlocks; b++ {
		off := rowOff + b*q8BlockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		base := b * q8BlockSize
		qOff := off + 2
		for i := 0; i < q8BlockSize; i += 8 {
			dst[base+i] = scale * float32(int8(data[qOff+i]))
			dst[base+i+1] = scale * float32(int8(data[qOff+i+1]))
			dst[base+i+2] = scale * float32(int8(data[qOff+i+2]))
			dst[base+i+3] = scale * float32(int8(data[qOff+i+3]))
			dst[base+i+4] = scale * float32(int8(data[qOff+i+4]))
			dst[base+i+5] = scale * float32(int8(data[qOff+i+5]))
			dst[base+i+6] = scale * float32(int8(data[qOff+i+6]))
			dst[base+i+7] = scale * float32(int8(data[qOff+i+7]))
		}
	}
}

// DotQ8Row computes the dot product of a float32 vector a[0:cols] with a
// single Q8_0 row, without materializing the full dequantized row.
// On amd64, uses AVX2 SIMD acceleration via dotQ8RowASM.
func DotQ8Row(a []float32, data []byte, row, nBlocks int) float32 {
	rowOff := row * nBlocks * q8BlockBytes
	return dotQ8RowASM(a, data, rowOff, nBlocks)
}

// DotQ8RowScaled is DotQ8Row using t.Scales (must be prepared).
func DotQ8RowScaled(a []float32, t *Q8Tensor, row int) float32 {
	nBlocks := t.Cols / q8BlockSize
	if len(t.Scales) < (row+1)*nBlocks {
		return DotQ8Row(a, t.Data, row, nBlocks)
	}
	return dotQ8RowScaled(a, t.Data, &t.Scales[row*nBlocks], row*nBlocks*q8BlockBytes, nBlocks)
}

// DotQ8RowDual dots one A vector against two Q8 B rows, loading A once per block.
func DotQ8RowDual(a []float32, data []byte, row0, row1, nBlocks int) (float32, float32) {
	return dotQ8RowDual(a, data, row0, row1, nBlocks)
}

// DotQ8RowDualScaled is DotQ8RowDual using t.Scales.
func DotQ8RowDualScaled(a []float32, t *Q8Tensor, row0, row1 int) (float32, float32) {
	nBlocks := t.Cols / q8BlockSize
	need := (row1 + 1) * nBlocks
	if len(t.Scales) < need {
		return DotQ8RowDual(a, t.Data, row0, row1, nBlocks)
	}
	return dotQ8RowDualScaled(a, t.Data, t.Scales, row0, row1, nBlocks)
}

// dotQ8RowScalar is the pure Go fallback implementation.
// Fuses dequantization and dot-product into one pass.
func dotQ8RowScalar(a []float32, data []byte, rowOff, nBlocks int) float32 {
	var sum float32
	for b := 0; b < nBlocks; b++ {
		off := rowOff + b*q8BlockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		base := b * q8BlockSize
		qOff := off + 2
		var blockSum float32
		for i := 0; i < q8BlockSize; i += 8 {
			blockSum += float32(int8(data[qOff+i])) * a[base+i]
			blockSum += float32(int8(data[qOff+i+1])) * a[base+i+1]
			blockSum += float32(int8(data[qOff+i+2])) * a[base+i+2]
			blockSum += float32(int8(data[qOff+i+3])) * a[base+i+3]
			blockSum += float32(int8(data[qOff+i+4])) * a[base+i+4]
			blockSum += float32(int8(data[qOff+i+5])) * a[base+i+5]
			blockSum += float32(int8(data[qOff+i+6])) * a[base+i+6]
			blockSum += float32(int8(data[qOff+i+7])) * a[base+i+7]
		}
		sum += scale * blockSum
	}
	return sum
}

// MatMulQ8Fused computes out = A @ B^T where A is [M, K] float32 and B is Q8_0 [N, K].
// Uses fused dequant-dot (DotQ8Row) instead of dequant-then-SIMD-dot.
// For small N*K (< fusedThreshold), the fused path avoids buffer allocation and
// memory bandwidth overhead. For large matrices, falls back to the SIMD path
// which benefits from AVX2/NEON vectorized dot products.
const fusedThreshold = 32768 // N*K threshold: below this, fused is faster

func MatMulQ8Fused(out, a []float32, b *Q8Tensor, M, N, K int) {
	// For large matrices, the SIMD dequant+dot path wins due to vectorization.
	if N*K >= fusedThreshold {
		MatMulQ8(out, a, b, M, N, K)
		return
	}
	// Small shapes: fused DotQ8Row. Still N-outer so each B row is touched once
	// per A-set (DotQ8Row re-reads int8, but avoids dequant buffer).
	nBlocks := K / q8BlockSize
	nCPU := getMatMulWorkers()
	if nCPU > 1 && N > 8 {
		nWorkers := nCPU
		if nWorkers > N {
			nWorkers = N
		}
		var wg sync.WaitGroup
		colsPerWorker := (N + nWorkers - 1) / nWorkers
		for w := 0; w < nWorkers; w++ {
			ns := w * colsPerWorker
			ne := ns + colsPerWorker
			if ne > N {
				ne = N
			}
			if ns >= ne {
				break
			}
			wg.Add(1)
			go func(ns, ne int) {
				defer wg.Done()
				for n := ns; n < ne; n++ {
					for m := 0; m < M; m++ {
						out[m*N+n] = DotQ8Row(a[m*K:m*K+K], b.Data, n, nBlocks)
					}
				}
			}(ns, ne)
		}
		wg.Wait()
		return
	}
	for n := 0; n < N; n++ {
		for m := 0; m < M; m++ {
			out[m*N+n] = DotQ8Row(a[m*K:m*K+K], b.Data, n, nBlocks)
		}
	}
}

// float16to32 converts IEEE 754 half-precision to float32.
func float16to32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		for mant&0x400 == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x3ff
		fallthrough
	case exp < 31:
		return math.Float32frombits(sign | (exp+112)<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
}

// QuantizeToQ8 quantizes a float32 weight matrix [rows, cols] to Q8_0 format.
// cols must be a multiple of 32. Returns a Q8Tensor suitable for MatMulQ8.
// This enables runtime quantization of F32 weights for reduced memory and
// bandwidth during inference.
func QuantizeToQ8(data []float32, rows, cols int) *Q8Tensor {
	nBlocks := cols / q8BlockSize
	totalBytes := rows * nBlocks * q8BlockBytes
	raw := make([]byte, totalBytes)

	for r := 0; r < rows; r++ {
		rowData := data[r*cols : (r+1)*cols]
		rowOff := r * nBlocks * q8BlockBytes
		for b := 0; b < nBlocks; b++ {
			blockData := rowData[b*q8BlockSize : (b+1)*q8BlockSize]
			// Find absmax for scale
			var amax float32
			for _, v := range blockData {
				av := float32(math.Abs(float64(v)))
				if av > amax {
					amax = av
				}
			}
			scale := amax / 127.0
			off := rowOff + b*q8BlockBytes
			// Store scale as float16
			binary.LittleEndian.PutUint16(raw[off:], float32to16(scale))
			// Quantize values
			if scale > 0 {
				invScale := 127.0 / amax
				for i := 0; i < q8BlockSize; i++ {
					q := int(math.Round(float64(blockData[i] * invScale)))
					if q > 127 {
						q = 127
					} else if q < -128 {
						q = -128
					}
					raw[off+2+i] = byte(int8(q))
				}
			} else {
				// All zeros
				for i := 0; i < q8BlockSize; i++ {
					raw[off+2+i] = 0
				}
			}
		}
	}
	t := &Q8Tensor{Data: raw, Rows: rows, Cols: cols}
	t.PrepareScales()
	return t
}

// float32to16 converts float32 to IEEE 754 half-precision.
func float32to16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xff) - 127
	mant := bits & 0x7fffff

	switch {
	case exp > 15:
		return sign | 0x7c00 // infinity
	case exp < -14:
		return sign // zero (flush subnormals)
	default:
		return sign | uint16(exp+15)<<10 | uint16(mant>>13)
	}
}
