package tensor

import (
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Q8Tensor holds a reference to Q8_0 quantized data (typically mmap-backed).
// Block layout: [scale:f16(2 bytes)][d0..d31:int8(32 bytes)] = 34 bytes per 32 elements.
// The data slice is NOT owned — it points into the mmap region.
//
// Scales is an optional f32 scale cache (len = Rows*(Cols/32)), built by
// PrepareScales(). When present, dequant skips f16→f32 conversion on the hot path.
type Q8Tensor struct {
	Data   []byte    // raw Q8_0 blocks
	Packed []byte    // optional packed int8 qs, 32 bytes/block, row-major (no f16 hole)
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
	// Data is row-major Q8 blocks (34B each); scales sit at byte 0 of every block.
	// Prefer F16C bulk convert (AVX2); scalar 8-way unroll as fallback.
	data := t.Data
	if !prepareScalesBulk(scales, data) {
		off := 0
		i := 0
		for ; i+7 < need; i += 8 {
			scales[i] = float16to32Fast(uint16(data[off]) | uint16(data[off+1])<<8)
			scales[i+1] = float16to32Fast(uint16(data[off+q8BlockBytes]) | uint16(data[off+q8BlockBytes+1])<<8)
			scales[i+2] = float16to32Fast(uint16(data[off+2*q8BlockBytes]) | uint16(data[off+2*q8BlockBytes+1])<<8)
			scales[i+3] = float16to32Fast(uint16(data[off+3*q8BlockBytes]) | uint16(data[off+3*q8BlockBytes+1])<<8)
			scales[i+4] = float16to32Fast(uint16(data[off+4*q8BlockBytes]) | uint16(data[off+4*q8BlockBytes+1])<<8)
			scales[i+5] = float16to32Fast(uint16(data[off+5*q8BlockBytes]) | uint16(data[off+5*q8BlockBytes+1])<<8)
			scales[i+6] = float16to32Fast(uint16(data[off+6*q8BlockBytes]) | uint16(data[off+6*q8BlockBytes+1])<<8)
			scales[i+7] = float16to32Fast(uint16(data[off+7*q8BlockBytes]) | uint16(data[off+7*q8BlockBytes+1])<<8)
			off += 8 * q8BlockBytes
		}
		for ; i < need; i++ {
			scales[i] = float16to32Fast(uint16(data[off]) | uint16(data[off+1])<<8)
			off += q8BlockBytes
		}
	}
	t.Scales = scales
}

func (t *Q8Tensor) packedNeed() int {
	if t == nil || t.Cols < q8BlockSize || t.Rows <= 0 {
		return 0
	}
	return t.Rows * t.Cols
}

// PackQS copies Q8_0 payloads into a hole-free 32-byte-block layout so Dual3
// AVX-512 can use aligned VPMOVSXBD instead of 34-byte GGUF blocks.
func (t *Q8Tensor) PackQS() {
	need := t.packedNeed()
	if need == 0 || t.Packed != nil {
		return
	}
	buf := make([]byte, need+64)
	off := int(uintptr(unsafe.Pointer(&buf[0])) % 64)
	if off != 0 {
		buf = buf[64-off:]
	}
	t.PackQSFrom(buf)
}

// PackQSFrom packs into dst (must be at least packedNeed() bytes) and
// returns the unused tail. Gemma uses this to place every layer's qs in one
// arena so Dual3 N-split workers share a single TLB-friendly region.
func (t *Q8Tensor) PackQSFrom(dst []byte) []byte {
	need := t.packedNeed()
	if t == nil || t.Packed != nil || need == 0 {
		return dst
	}
	nBlocks := t.Cols / q8BlockSize
	if len(t.Data) < t.Rows*nBlocks*q8BlockBytes || len(dst) < need {
		return dst
	}
	src := t.Data
	di := 0
	si := 0
	for r := 0; r < t.Rows; r++ {
		for b := 0; b < nBlocks; b++ {
			copy(dst[di:di+q8BlockSize], src[si+2:si+2+q8BlockSize])
			di += q8BlockSize
			si += q8BlockBytes
		}
	}
	t.Packed = dst[:need]
	return dst[need:]
}

// FaultInPacked touches packed qs pages (separate from mmap Data).
func (t *Q8Tensor) FaultInPacked() {
	if t == nil {
		return
	}
	p := t.Packed
	n := len(p)
	if n == 0 {
		return
	}
	for i := 0; i < n; i += 4096 {
		_ = p[i]
	}
	_ = p[n-1]
}

// FaultIn touches every mmap page of Q8 payload so the first MatMul after
// Open does not pay demand paging. PrepareScales only reads the 2-byte scale
// at the start of each 34-byte block.
func (t *Q8Tensor) FaultIn() {
	if t == nil {
		return
	}
	data := t.Data
	n := len(data)
	if n == 0 {
		return
	}
	for i := 0; i < n; i += 4096 {
		_ = data[i]
	}
	_ = data[n-1]
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

// MatMulMaxParallelForTest returns the process-global cap (0 = default).
func MatMulMaxParallelForTest() int { return int(atomic.LoadInt32(&matMulMaxParallel)) }

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
	MatMulQ8N(out, a, b, M, N, K, 0)
}

// MatMulQ8N is MatMulQ8 with a per-call worker cap.
// maxWorkers==1 never enqueues the process-wide matmul jobQueue (serial kernels).
// maxWorkers==0 uses the existing shouldParallel / poolWorkers path.
func MatMulQ8N(out, a []float32, b *Q8Tensor, M, N, K, maxWorkers int) {
	MatMulQ8BiasN(out, a, b, nil, M, N, K, maxWorkers)
}

// argmaxPartials holds per-worker best buffers for MatMulQ8Argmax (pooled).
type argmaxPartials struct {
	v  [][]float32
	i  [][]int
	m  int
	wg sync.WaitGroup
}

var argmaxPartialPool = sync.Pool{New: func() any { return &argmaxPartials{} }}

// smallArgmaxPartials covers CTC command-length shapes (M<=8) without the
// slice-of-slices indirection used by the general partial buffer.
type smallArgmaxPartials struct {
	v  [12][8]float32
	i  [12][8]int
	wg sync.WaitGroup
}

var smallArgmaxPartialPool = sync.Pool{New: func() any { return new(smallArgmaxPartials) }}

type q8ArgmaxTask struct {
	bestV   []float32
	bestI   []int
	a, bias []float32
	b       *Q8Tensor
	M, N, K int
}

func (t *q8ArgmaxTask) runRange(ns, ne int) {
	matMulQ8ArgmaxNRange(t.bestV, t.bestI, t.a, t.b, t.bias, t.M, t.N, t.K, ns, ne)
}

var q8ArgmaxTaskPool = sync.Pool{New: func() any { return new(q8ArgmaxTask) }}

var fusedPadPool = sync.Pool{New: func() any { p := make([]float32, 4*1152); return &p }}

func getArgmaxPartials(nw, M int) *argmaxPartials {
	p := argmaxPartialPool.Get().(*argmaxPartials)
	if len(p.v) < nw || p.m < M {
		p.v = make([][]float32, nw)
		p.i = make([][]int, nw)
		for w := 0; w < nw; w++ {
			p.v[w] = make([]float32, M)
			p.i[w] = make([]int, M)
		}
		p.m = M
	} else {
		p.v = p.v[:nw]
		p.i = p.i[:nw]
	}
	neg := float32(-math.MaxFloat32)
	for w := 0; w < nw; w++ {
		for m := 0; m < M; m++ {
			p.v[w][m] = neg
			p.i[w][m] = 0
		}
	}
	return p
}

// MatMulQ8Argmax computes per-row argmax of A @ B^T + bias without materializing
// the full [M,N] matrix. Used for CTC greedy decode (N≈25k) to skip ~M*N stores
// and a second full scan.
//
// Parallelizes over N (like MatMulQ8Bias): each worker owns a column range, keeps
// per-row local bests, then a cheap merge. Avoids M-parallel which re-dequants B.
func MatMulQ8Argmax(outIDs []int, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	nw := matMulWorkersFor(M, N, K)
	if nw <= 1 || !shouldParallel(M, N, K) {
		p := getArgmaxPartials(1, M)
		matMulQ8ArgmaxNRange(p.v[0], p.i[0], a, b, bias, M, N, K, 0, N)
		copy(outIDs[:M], p.i[0])
		argmaxPartialPool.Put(p)
		return
	}
	// Per-worker partial argmax over its N slice, then merge.
	if nw > N {
		nw = N
	}
	if M <= 8 && nw <= 12 {
		matMulQ8ArgmaxSmall(outIDs, a, b, bias, M, N, K, nw)
		return
	}
	p := getArgmaxPartials(nw, M)
	ensureMatmulPool()
	var tasks [12]*q8ArgmaxTask
	chunk := (N + nw - 1) / nw
	for w := 0; w < nw; w++ {
		ns := w * chunk
		ne := ns + chunk
		if ne > N {
			ne = N
		}
		if ns >= ne {
			break
		}
		p.wg.Add(1)
		t := q8ArgmaxTaskPool.Get().(*q8ArgmaxTask)
		t.bestV, t.bestI, t.a, t.b, t.bias = p.v[w], p.i[w], a, b, bias
		t.M, t.N, t.K = M, N, K
		tasks[w] = t
		jobQueue <- matmulRangeJob{start: ns, end: ne, task: t, wg: &p.wg}
	}
	p.wg.Wait()
	for w := 0; w < nw; w++ {
		t := tasks[w]
		if t == nil {
			continue
		}
		t.bestV, t.bestI, t.a, t.b, t.bias = nil, nil, nil, nil, nil
		q8ArgmaxTaskPool.Put(t)
	}
	// Merge partials
	for m := 0; m < M; m++ {
		bestV := p.v[0][m]
		bestI := p.i[0][m]
		for w := 1; w < nw; w++ {
			if p.v[w][m] > bestV {
				bestV = p.v[w][m]
				bestI = p.i[w][m]
			}
		}
		outIDs[m] = bestI
	}
	argmaxPartialPool.Put(p)
}

func matMulQ8ArgmaxSmall(outIDs []int, a []float32, b *Q8Tensor, bias []float32, M, N, K, nw int) {
	p := smallArgmaxPartialPool.Get().(*smallArgmaxPartials)
	neg := float32(-math.MaxFloat32)
	for w := 0; w < nw; w++ {
		for m := 0; m < M; m++ {
			p.v[w][m], p.i[w][m] = neg, 0
		}
	}
	ensureMatmulPool()
	var tasks [12]*q8ArgmaxTask
	chunk := (N + nw - 1) / nw
	for w := 0; w < nw; w++ {
		ns, ne := w*chunk, (w+1)*chunk
		if ne > N {
			ne = N
		}
		p.wg.Add(1)
		t := q8ArgmaxTaskPool.Get().(*q8ArgmaxTask)
		t.bestV, t.bestI, t.a, t.b, t.bias = p.v[w][:M], p.i[w][:M], a, b, bias
		t.M, t.N, t.K = M, N, K
		tasks[w] = t
		jobQueue <- matmulRangeJob{start: ns, end: ne, task: t, wg: &p.wg}
	}
	p.wg.Wait()
	for w := 0; w < nw; w++ {
		t := tasks[w]
		t.bestV, t.bestI, t.a, t.b, t.bias = nil, nil, nil, nil, nil
		q8ArgmaxTaskPool.Put(t)
	}
	for m := 0; m < M; m++ {
		bestV, bestI := p.v[0][m], p.i[0][m]
		for w := 1; w < nw; w++ {
			if p.v[w][m] > bestV {
				bestV, bestI = p.v[w][m], p.i[w][m]
			}
		}
		outIDs[m] = bestI
	}
	smallArgmaxPartialPool.Put(p)
}

// matMulQ8ArgmaxNRange updates bestV/bestI for all M rows over B columns [ns,ne).
func matMulQ8ArgmaxNRange(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int) {
	_ = N
	nBlocks := K / q8BlockSize
	hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0
	hasBias := bias != nil
	useDeq := useQ8DequantOnce(M, ne-ns, K, hasScales)

	if useDeq {
		// N-tile outer: dequant each B panel ONCE, then multiDot all M (was M-outer
		// re-dequanting every A tile — costly for CTC N≈25k × M≈100).
		nTile := q8BPanelRows(K, ne-ns)
		panel, panelPool := getQ8DequantBuf(nTile * K)
		var dDual0, dDual1 [8]float32
		var d8 [8]float32
		mt := mTileForK(K)
		for n0 := ns; n0 < ne; n0 += nTile {
			nt := nTile
			if n0+nt > ne {
				nt = ne - n0
			}
			dequantQ8Panel(b, n0, nt, K, panel, K == 512)
			m := 0
			if mt >= 8 {
				for ; m+7 < M; m += 8 {
					aPanel := a[m*K : (m+8)*K]
					// Update running bests in-place (no tile copy).
					matMulPanelDualArgmax(aPanel, panel, bias, n0, K, nt, hasBias, &dDual0, &dDual1, &d8, 8, bestV[m:m+8], bestI[m:m+8])
				}
			}
			if mt >= 4 {
				for ; m+3 < M; m += 4 {
					aPanel := a[m*K : (m+4)*K]
					matMulPanelDualArgmax(aPanel, panel, bias, n0, K, nt, hasBias, &dDual0, &dDual1, &d8, 4, bestV[m:m+4], bestI[m:m+4])
				}
			}
			for ; m < M; m++ {
				aRow := a[m*K : m*K+K]
				bv, bi := bestV[m], bestI[m]
				for ti := 0; ti < nt; ti++ {
					n := n0 + ti
					s := Dot(aRow, panel[ti*K:(ti+1)*K])
					if hasBias {
						s += bias[n]
					}
					if s > bv {
						bv, bi = s, n
					}
				}
				bestV[m], bestI[m] = bv, bi
			}
		}
		putQ8DequantBuf(panel, panelPool)
		return
	}

	// Fused dual multiDot over [ns,ne) — update bests in-place.
	// CTC always has scales+bias; specialize that path (no per-col branches).
	if hasScales && hasBias {
		matMulQ8ArgmaxFusedScaledBias(bestV, bestI, a, b, bias, M, K, ns, ne, nBlocks)
		return
	}
	matMulQ8ArgmaxFusedGeneric(bestV, bestI, a, b, bias, M, K, ns, ne, nBlocks, hasScales, hasBias)
}

// matMulQ8ArgmaxFusedScaledBias: fused Q8 multiDot argmax with f32 scales + bias.
func matMulQ8ArgmaxFusedScaledBias(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, K, ns, ne, nBlocks int) {
	var dDual0, dDual1 [8]float32
	var d8 [8]float32
	var d4 [4]float32
	var d2 [4]float32
	m := 0
	for ; m+7 < M; m += 8 {
		aPanel := a[m*K : (m+8)*K]
		bv, bi := bestV[m:m+8], bestI[m:m+8]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot8T(&dDual0, &dDual1, aPanel, b, n, n+1, nBlocks, K)
			updateArgmaxDual4(bv, bi, 0, n, &dDual0, bias[n], bias[n+1])
			updateArgmaxDual4(bv, bi, 4, n, &dDual1, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			q8MultiDot8T(&d8, aPanel, b, n, nBlocks, K)
			updateArgmax8(bv, bi, n, &d8, bias[n])
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		bv, bi := bestV[m:m+4], bestI[m:m+4]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, K)
			updateArgmaxDual4(bv, bi, 0, n, &dDual0, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			bn := bias[n]
			q8MultiDot4T(&d4, aPanel, b, n, nBlocks, K)
			for r := 0; r < 4; r++ {
				v := d4[r] + bn
				if v > bv[r] {
					bv[r], bi[r] = v, n
				}
			}
		}
	}
	for ; m+1 < M; m += 2 {
		aPanel := a[m*K : (m+2)*K]
		bv0, bi0 := bestV[m], bestI[m]
		bv1, bi1 := bestV[m+1], bestI[m+1]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot2T(&d2, aPanel, b, n, n+1, nBlocks, K)
			v00, v10 := d2[0]+bias[n], d2[1]+bias[n]
			v01, v11 := d2[2]+bias[n+1], d2[3]+bias[n+1]
			if v00 > bv0 {
				bv0, bi0 = v00, n
			}
			if v01 > bv0 {
				bv0, bi0 = v01, n+1
			}
			if v10 > bv1 {
				bv1, bi1 = v10, n
			}
			if v11 > bv1 {
				bv1, bi1 = v11, n+1
			}
		}
		for ; n < ne; n++ {
			bn := bias[n]
			v0 := DotQ8RowScaled(aPanel[:K], b, n) + bn
			v1 := DotQ8RowScaled(aPanel[K:], b, n) + bn
			if v0 > bv0 {
				bv0, bi0 = v0, n
			}
			if v1 > bv1 {
				bv1, bi1 = v1, n
			}
		}
		bestV[m], bestI[m] = bv0, bi0
		bestV[m+1], bestI[m+1] = bv1, bi1
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		bv, bi := bestV[m], bestI[m]
		n := ns
		for ; n+1 < ne; n += 2 {
			s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
			s0 += bias[n]
			s1 += bias[n+1]
			if s0 > bv {
				bv, bi = s0, n
			}
			if s1 > bv {
				bv, bi = s1, n+1
			}
		}
		for ; n < ne; n++ {
			s := DotQ8RowScaled(aRow, b, n) + bias[n]
			if s > bv {
				bv, bi = s, n
			}
		}
		bestV[m], bestI[m] = bv, bi
	}
}

func matMulQ8ArgmaxFusedGeneric(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, K, ns, ne, nBlocks int, hasScales, hasBias bool) {
	var dDual0, dDual1 [8]float32
	var d8 [8]float32
	var d4 [4]float32
	m := 0
	for ; m+7 < M; m += 8 {
		aPanel := a[m*K : (m+8)*K]
		bv, bi := bestV[m:m+8], bestI[m:m+8]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if hasBias {
				bn0, bn1 = bias[n], bias[n+1]
			}
			if hasScales {
				q8DualMultiDot8T(&dDual0, &dDual1, aPanel, b, n, n+1, nBlocks, K)
			} else {
				q8DualMultiDot8(&dDual0, &dDual1, aPanel, b.Data, n, n+1, nBlocks, K)
			}
			updateArgmaxDual4(bv, bi, 0, n, &dDual0, bn0, bn1)
			updateArgmaxDual4(bv, bi, 4, n, &dDual1, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if hasBias {
				bn = bias[n]
			}
			if hasScales {
				q8MultiDot8T(&d8, aPanel, b, n, nBlocks, K)
			} else {
				q8MultiDot8(&d8, aPanel, b.Data, n, nBlocks, K)
			}
			updateArgmax8(bv, bi, n, &d8, bn)
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		bv, bi := bestV[m:m+4], bestI[m:m+4]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if hasBias {
				bn0, bn1 = bias[n], bias[n+1]
			}
			if hasScales {
				q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, K)
			} else {
				q8DualMultiDot4(&dDual0, aPanel, b.Data, n, n+1, nBlocks, K)
			}
			updateArgmaxDual4(bv, bi, 0, n, &dDual0, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if hasBias {
				bn = bias[n]
			}
			if hasScales {
				q8MultiDot4T(&d4, aPanel, b, n, nBlocks, K)
			} else {
				q8MultiDot4(&d4, aPanel, b.Data, n, nBlocks, K)
			}
			for r := 0; r < 4; r++ {
				v := d4[r] + bn
				if v > bv[r] {
					bv[r], bi[r] = v, n
				}
			}
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		bv, bi := bestV[m], bestI[m]
		n := ns
		for ; n+1 < ne; n += 2 {
			var s0, s1 float32
			if hasScales {
				s0, s1 = DotQ8RowDualScaled(aRow, b, n, n+1)
			} else {
				s0, s1 = DotQ8RowDual(aRow, b.Data, n, n+1, nBlocks)
			}
			if hasBias {
				s0 += bias[n]
				s1 += bias[n+1]
			}
			if s0 > bv {
				bv, bi = s0, n
			}
			if s1 > bv {
				bv, bi = s1, n+1
			}
		}
		for ; n < ne; n++ {
			var s float32
			if hasScales {
				s = DotQ8RowScaled(aRow, b, n)
			} else {
				s = DotQ8Row(aRow, b.Data, n, nBlocks)
			}
			if hasBias {
				s += bias[n]
			}
			if s > bv {
				bv, bi = s, n
			}
		}
		bestV[m], bestI[m] = bv, bi
	}
}

// matMulPanelDualArgmax: like matMulPanelDual but updates running argmax instead of stores.
// bestV/bestI are length rows (4 or 8) and updated in-place.
// hasBias specialized (CTC always has bias) to drop per-column branches.
func matMulPanelDualArgmax(aPanel, panel, bias []float32, n0, K, nt int, hasBias bool, dDual0, dDual1, d8 *[8]float32, rows int, bestV []float32, bestI []int) {
	if hasBias {
		matMulPanelDualArgmaxBias(aPanel, panel, bias, n0, K, nt, dDual0, dDual1, d8, rows, bestV, bestI)
		return
	}
	matMulPanelDualArgmaxNoBias(aPanel, panel, n0, K, nt, dDual0, dDual1, d8, rows, bestV, bestI)
}

func matMulPanelDualArgmaxBias(aPanel, panel, bias []float32, n0, K, nt int, dDual0, dDual1, d8 *[8]float32, rows int, bestV []float32, bestI []int) {
	t := 0
	var dTri0, dTri1 [12]float32
	useTriple := K == 512
	if rows >= 8 {
		if useTriple {
			// Triple/dual-triple: keep A hot across 3/2 multiDots (B-outer ReLU/plain).
			for ; t+8 < nt; t += 9 {
				n := n0 + t
				for s := 0; s < 9; s += 3 {
					nn := n + s
					tt := t + s
					b0 := panel[tt*K : (tt+1)*K]
					b1 := panel[(tt+1)*K : (tt+2)*K]
					b2 := panel[(tt+2)*K : (tt+3)*K]
					if !multiDot8TripleArgmax(bestV, bestI, aPanel, b0, b1, b2, nn, K, bias[nn], bias[nn+1], bias[nn+2]) {
						multiDot8TripleB(&dTri0, &dTri1, aPanel, b0, b1, b2, K)
						updateArgmaxTriple4(bestV, bestI, 0, nn, &dTri0, bias[nn], bias[nn+1], bias[nn+2])
						updateArgmaxTriple4(bestV, bestI, 4, nn, &dTri1, bias[nn], bias[nn+1], bias[nn+2])
					}
				}
			}
			for ; t+5 < nt; t += 6 {
				n := n0 + t
				for s := 0; s < 6; s += 3 {
					nn := n + s
					tt := t + s
					b0 := panel[tt*K : (tt+1)*K]
					b1 := panel[(tt+1)*K : (tt+2)*K]
					b2 := panel[(tt+2)*K : (tt+3)*K]
					if !multiDot8TripleArgmax(bestV, bestI, aPanel, b0, b1, b2, nn, K, bias[nn], bias[nn+1], bias[nn+2]) {
						multiDot8TripleB(&dTri0, &dTri1, aPanel, b0, b1, b2, K)
						updateArgmaxTriple4(bestV, bestI, 0, nn, &dTri0, bias[nn], bias[nn+1], bias[nn+2])
						updateArgmaxTriple4(bestV, bestI, 4, nn, &dTri1, bias[nn], bias[nn+1], bias[nn+2])
					}
				}
			}
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				b0 := panel[t*K : (t+1)*K]
				b1 := panel[(t+1)*K : (t+2)*K]
				b2 := panel[(t+2)*K : (t+3)*K]
				if multiDot8TripleArgmax(bestV, bestI, aPanel, b0, b1, b2, n, K, bias[n], bias[n+1], bias[n+2]) {
					continue
				}
				multiDot8TripleB(&dTri0, &dTri1, aPanel, b0, b1, b2, K)
				updateArgmaxTriple4(bestV, bestI, 0, n, &dTri0, bias[n], bias[n+1], bias[n+2])
				updateArgmaxTriple4(bestV, bestI, 4, n, &dTri1, bias[n], bias[n+1], bias[n+2])
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			b0 := panel[t*K : (t+1)*K]
			b1 := panel[(t+1)*K : (t+2)*K]
			if multiDot8DualArgmax(bestV, bestI, aPanel, b0, b1, n, K, bias[n], bias[n+1]) {
				continue
			}
			multiDot8DualB(dDual0, dDual1, aPanel, b0, b1, K)
			updateArgmaxDual4(bestV, bestI, 0, n, dDual0, bias[n], bias[n+1])
			updateArgmaxDual4(bestV, bestI, 4, n, dDual1, bias[n], bias[n+1])
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			updateArgmax8(bestV, bestI, n, d8, bias[n])
		}
		return
	}
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(&dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			updateArgmaxTriple4(bestV, bestI, 0, n, &dTri0, bias[n], bias[n+1], bias[n+2])
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		updateArgmaxDual4(bestV, bestI, 0, n, dDual0, bias[n], bias[n+1])
	}
	for ; t < nt; t++ {
		n := n0 + t
		bn := bias[n]
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		for r := 0; r < 4; r++ {
			v := d4[r] + bn
			if v > bestV[r] {
				bestV[r], bestI[r] = v, n
			}
		}
	}
}

func matMulPanelDualArgmaxNoBias(aPanel, panel []float32, n0, K, nt int, dDual0, dDual1, d8 *[8]float32, rows int, bestV []float32, bestI []int) {
	t := 0
	var dTri0, dTri1 [12]float32
	useTriple := K == 512
	if rows >= 8 {
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				multiDot8TripleB(&dTri0, &dTri1, aPanel,
					panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
				updateArgmaxTriple4(bestV, bestI, 0, n, &dTri0, 0, 0, 0)
				updateArgmaxTriple4(bestV, bestI, 4, n, &dTri1, 0, 0, 0)
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			updateArgmaxDual4(bestV, bestI, 0, n, dDual0, 0, 0)
			updateArgmaxDual4(bestV, bestI, 4, n, dDual1, 0, 0)
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			updateArgmax8(bestV, bestI, n, d8, 0)
		}
		return
	}
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(&dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			updateArgmaxTriple4(bestV, bestI, 0, n, &dTri0, 0, 0, 0)
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		updateArgmaxDual4(bestV, bestI, 0, n, dDual0, 0, 0)
	}
	for ; t < nt; t++ {
		n := n0 + t
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		for r := 0; r < 4; r++ {
			v := d4[r]
			if v > bestV[r] {
				bestV[r], bestI[r] = v, n
			}
		}
	}
}

// updateArgmaxDual4: d[0:4]=col n, d[4:8]=col n+1; row offset baseR in best arrays.
func updateArgmaxDual4(bestV []float32, bestI []int, baseR, n int, d *[8]float32, bn0, bn1 float32) {
	v0 := d[0] + bn0
	v1 := d[4] + bn1
	i0 := baseR
	if v0 > bestV[i0] {
		bestV[i0], bestI[i0] = v0, n
	}
	if v1 > bestV[i0] {
		bestV[i0], bestI[i0] = v1, n+1
	}
	v0 = d[1] + bn0
	v1 = d[5] + bn1
	i0 = baseR + 1
	if v0 > bestV[i0] {
		bestV[i0], bestI[i0] = v0, n
	}
	if v1 > bestV[i0] {
		bestV[i0], bestI[i0] = v1, n+1
	}
	v0 = d[2] + bn0
	v1 = d[6] + bn1
	i0 = baseR + 2
	if v0 > bestV[i0] {
		bestV[i0], bestI[i0] = v0, n
	}
	if v1 > bestV[i0] {
		bestV[i0], bestI[i0] = v1, n+1
	}
	v0 = d[3] + bn0
	v1 = d[7] + bn1
	i0 = baseR + 3
	if v0 > bestV[i0] {
		bestV[i0], bestI[i0] = v0, n
	}
	if v1 > bestV[i0] {
		bestV[i0], bestI[i0] = v1, n+1
	}
}

// updateArgmaxTriple4: d[0:4]=n, d[4:8]=n+1, d[8:12]=n+2. Fully unrolled.
func updateArgmaxTriple4(bestV []float32, bestI []int, baseR, n int, d *[12]float32, bn0, bn1, bn2 float32) {
	idx := baseR
	v0, v1, v2 := d[0]+bn0, d[4]+bn1, d[8]+bn2
	if v0 > bestV[idx] {
		bestV[idx], bestI[idx] = v0, n
	}
	if v1 > bestV[idx] {
		bestV[idx], bestI[idx] = v1, n+1
	}
	if v2 > bestV[idx] {
		bestV[idx], bestI[idx] = v2, n+2
	}
	idx = baseR + 1
	v0, v1, v2 = d[1]+bn0, d[5]+bn1, d[9]+bn2
	if v0 > bestV[idx] {
		bestV[idx], bestI[idx] = v0, n
	}
	if v1 > bestV[idx] {
		bestV[idx], bestI[idx] = v1, n+1
	}
	if v2 > bestV[idx] {
		bestV[idx], bestI[idx] = v2, n+2
	}
	idx = baseR + 2
	v0, v1, v2 = d[2]+bn0, d[6]+bn1, d[10]+bn2
	if v0 > bestV[idx] {
		bestV[idx], bestI[idx] = v0, n
	}
	if v1 > bestV[idx] {
		bestV[idx], bestI[idx] = v1, n+1
	}
	if v2 > bestV[idx] {
		bestV[idx], bestI[idx] = v2, n+2
	}
	idx = baseR + 3
	v0, v1, v2 = d[3]+bn0, d[7]+bn1, d[11]+bn2
	if v0 > bestV[idx] {
		bestV[idx], bestI[idx] = v0, n
	}
	if v1 > bestV[idx] {
		bestV[idx], bestI[idx] = v1, n+1
	}
	if v2 > bestV[idx] {
		bestV[idx], bestI[idx] = v2, n+2
	}
}

func updateArgmax8(bestV []float32, bestI []int, n int, d *[8]float32, bn float32) {
	v := d[0] + bn
	if v > bestV[0] {
		bestV[0], bestI[0] = v, n
	}
	v = d[1] + bn
	if v > bestV[1] {
		bestV[1], bestI[1] = v, n
	}
	v = d[2] + bn
	if v > bestV[2] {
		bestV[2], bestI[2] = v, n
	}
	v = d[3] + bn
	if v > bestV[3] {
		bestV[3], bestI[3] = v, n
	}
	v = d[4] + bn
	if v > bestV[4] {
		bestV[4], bestI[4] = v, n
	}
	v = d[5] + bn
	if v > bestV[5] {
		bestV[5], bestI[5] = v, n
	}
	v = d[6] + bn
	if v > bestV[6] {
		bestV[6], bestI[6] = v, n
	}
	v = d[7] + bn
	if v > bestV[7] {
		bestV[7], bestI[7] = v, n
	}
}

// argmaxQ8Row: argmax_n (dot(a, B[n]) + bias[n]) via dual-B Q8 dots.
func argmaxQ8Row(a []float32, b *Q8Tensor, bias []float32, N, K int) int {
	nBlocks := K / q8BlockSize
	hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0
	bestID := 0
	bestVal := float32(-math.MaxFloat32)
	n := 0
	if hasScales {
		for ; n+1 < N; n += 2 {
			s0, s1 := DotQ8RowDualScaled(a, b, n, n+1)
			if bias != nil {
				s0 += bias[n]
				s1 += bias[n+1]
			}
			if s0 > bestVal {
				bestVal, bestID = s0, n
			}
			if s1 > bestVal {
				bestVal, bestID = s1, n+1
			}
		}
		for ; n < N; n++ {
			s := DotQ8RowScaled(a, b, n)
			if bias != nil {
				s += bias[n]
			}
			if s > bestVal {
				bestVal, bestID = s, n
			}
		}
		return bestID
	}
	for ; n+1 < N; n += 2 {
		s0, s1 := DotQ8RowDual(a, b.Data, n, n+1, nBlocks)
		if bias != nil {
			s0 += bias[n]
			s1 += bias[n+1]
		}
		if s0 > bestVal {
			bestVal, bestID = s0, n
		}
		if s1 > bestVal {
			bestVal, bestID = s1, n+1
		}
	}
	for ; n < N; n++ {
		s := DotQ8Row(a, b.Data, n, nBlocks)
		if bias != nil {
			s += bias[n]
		}
		if s > bestVal {
			bestVal, bestID = s, n
		}
	}
	return bestID
}

// MatMulQ8Bias is MatMulQ8 with optional bias fused into the store:
// out[m,n] = dot(A[m], dequant(B[n])) + bias[n].
//
// Loop order for M>1: M-tile outer, N inner — keeps an A panel (8×K ≈ 16KB for
// K=512) hot in L1 while streaming all B rows. N-outer would reload A for every
// B row (A is ~200KB for M=100,K=512 — larger than L1).
func MatMulQ8Bias(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	MatMulQ8BiasN(out, a, b, bias, M, N, K, 0)
}

// MatMulQ8BiasN is MatMulQ8Bias with a per-call worker cap (see MatMulQ8N).
func MatMulQ8BiasN(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, maxWorkers int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if maxWorkers == 1 {
		if M == 1 {
			matMulQ8Serial_M1(out, a, b, bias, N, K)
			return
		}
		matMulQ8SerialM(out, a, b, bias, M, N, K)
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
	if shouldParallel(M, N, K) {
		matMulQ8ParallelN_MTile(out, a, b, bias, M, N, K)
		return
	}
	matMulQ8SerialM(out, a, b, bias, M, N, K)
}

// MatMulQ8BiasAdd is MatMulQ8Bias with residual accumulate:
// out[m,n] += dot(A[m], dequant(B[n])) + bias[n].
// Used for FFN down-projection residual (skips a temp buffer + Add pass).
func MatMulQ8BiasAdd(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if M == 1 {
		// Inline accumulate (no temp): dual-B Q8 dots.
		nBlocks := K / q8BlockSize
		hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0
		aRow := a[:K]
		n := 0
		for ; n+1 < N; n += 2 {
			var s0, s1 float32
			if hasScales {
				s0, s1 = DotQ8RowDualScaled(aRow, b, n, n+1)
			} else {
				s0, s1 = DotQ8RowDual(aRow, b.Data, n, n+1, nBlocks)
			}
			if bias != nil {
				s0 += bias[n]
				s1 += bias[n+1]
			}
			out[n] += s0
			out[n+1] += s1
		}
		for ; n < N; n++ {
			var s float32
			if hasScales {
				s = DotQ8RowScaled(aRow, b, n)
			} else {
				s = DotQ8Row(aRow, b.Data, n, nBlocks)
			}
			if bias != nil {
				s += bias[n]
			}
			out[n] += s
		}
		return
	}
	if shouldParallel(M, N, K) {
		matMulQ8ParallelN_MTileAct(out, a, b, bias, M, N, K, false, true)
		return
	}
	matMulQ8SerialMAct(out, a, b, bias, M, N, K, false, true)
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
		matMulQ8ParallelN_MTileAct(out, a, b, bias, M, N, K, true, false)
		return
	}
	matMulQ8SerialMAct(out, a, b, bias, M, N, K, true, false)
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
	matMulQ8SerialMAct(out, a, b, bias, M, N, K, false, false)
}

// matMulQ8ParallelN_MTile partitions N across pool workers; each worker uses
// M-tile-outer so its A panels stay hot for its B-column range.
func matMulQ8ParallelN_MTile(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int) {
	matMulQ8ParallelN_MTileAct(out, a, b, bias, M, N, K, false, false)
}

func storeDot4(out []float32, m, n, N int, d *[4]float32, bn float32, relu, accum bool) {
	if relu {
		for t := 0; t < 4; t++ {
			v := d[t] + bn
			if v < 0 {
				v = 0
			}
			if accum {
				out[(m+t)*N+n] += v
			} else {
				out[(m+t)*N+n] = v
			}
		}
		return
	}
	if accum {
		out[m*N+n] += d[0] + bn
		out[(m+1)*N+n] += d[1] + bn
		out[(m+2)*N+n] += d[2] + bn
		out[(m+3)*N+n] += d[3] + bn
		return
	}
	out[m*N+n] = d[0] + bn
	out[(m+1)*N+n] = d[1] + bn
	out[(m+2)*N+n] = d[2] + bn
	out[(m+3)*N+n] = d[3] + bn
}

func storeDot8(out []float32, m, n, N int, d *[8]float32, bn float32, relu, accum bool) {
	if relu {
		for t := 0; t < 8; t++ {
			v := d[t] + bn
			if v < 0 {
				v = 0
			}
			if accum {
				out[(m+t)*N+n] += v
			} else {
				out[(m+t)*N+n] = v
			}
		}
		return
	}
	if accum {
		out[m*N+n] += d[0] + bn
		out[(m+1)*N+n] += d[1] + bn
		out[(m+2)*N+n] += d[2] + bn
		out[(m+3)*N+n] += d[3] + bn
		out[(m+4)*N+n] += d[4] + bn
		out[(m+5)*N+n] += d[5] + bn
		out[(m+6)*N+n] += d[6] + bn
		out[(m+7)*N+n] += d[7] + bn
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

// q8BPanelRows picks N-tile width so the float B panel stays cache-friendly.
// For wide N ranges (CTC under parallel workers) keep the panel smaller.
func q8BPanelRows(K, nRange int) int {
	if K <= 0 {
		return 8
	}
	// Encoder/FFN K=512: ~72KB panel in L2 → nt=36 (multiple of 9 and 6) so
	// triple-triple / dual-triple B-outer amortizes A reloads. Default ~32KB.
	// nt=54 (108KB) measured neutral/slightly worse e2e under thermal noise.
	// CTC (wide N) uses a mid size so workers keep more tiles warm.
	budget := 32 * 1024
	if K == 512 {
		budget = 72 * 1024 // nt=36
	}
	if nRange > 4096 {
		if K == 512 {
			budget = 36 * 1024 // nt=18
		} else {
			budget = 24 * 1024
		}
	}
	nt := budget / (K * 4)
	if nt < 4 {
		nt = 4
	}
	if nt > 48 {
		nt = 48
	}
	// Prefer multiple of 18 so triple-triple (9), dual-triple (6), and dual-B (2)
	// all tile cleanly when the panel is large enough; else fall back to %6.
	if nt >= 18 {
		nt = nt - (nt % 18)
	} else if nt >= 6 {
		nt = nt - (nt % 6)
	} else if nt&1 != 0 {
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
		// Gemma K=768: fused N24 Q8 (AVX-512 dual) beats dequant-once+f32
		// by skipping the dequant store (profile: 17% dequantRowScaledDual).
		if K == 768 {
			return false
		}
		// K=512/560 encoder shapes; not K=2048 FFN down-proj.
		return M >= 8 && K <= 1024
	}
	return M >= 32 && K <= 768
}

// mTileForK picks M micro-tile for Q8 multiDot.
// Prefer 8 when dual-4×2 can keep dual B hot across two 4-row halves:
// each dual-4 only needs 4*K floats in-kernel (K=2048 → 32KB), so L1 holds one
// half at a time while the same Q8 B pair is reused immediately for the second half.
// (A full 8-row one-pass with stack B1 thrash is still avoided — see q8DualMultiDot8T.)
func mTileForK(K int) int {
	if K <= 0 {
		return 4
	}
	// SenseVoice FFN down K=2048 and encoder K=512: dual-4×2 path.
	if K <= 2048 {
		return 8
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

// matMulQ8RangeFusedAccumScaled: FFN down-proj hot path.
// hasScales + residual accum, no ReLU — no per-column scale/relu branches.
// Bias is specialized: SenseVoice FFN always has bias (hot); nil-bias is rare.
func matMulQ8RangeFusedAccumScaled(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne, nBlocks int) {
	if bias != nil {
		matMulQ8RangeFusedAccumScaledBias(out, a, b, bias, M, N, K, ns, ne, nBlocks)
		return
	}
	matMulQ8RangeFusedAccumScaledNoBias(out, a, b, M, N, K, ns, ne, nBlocks)
}

func matMulQ8RangeFusedAccumScaledBias(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne, nBlocks int) {
	// VNNI path: prequant A once per 8-row panel, int8×int8 for all N (FFN down N=512).
	if N == 512 && K == 2048 && nBlocks == 64 && tryFusedAccumVNNI(out, a, b, bias, M, ns, ne, nBlocks) {
		return
	}
	mt := mTileForK(K)
	var dDual0, dDual1 [8]float32
	var d4 [4]float32
	var d8 [8]float32
	// FFN down K=2048: dual-B; dual-4x2 microkernel; dual8-accum for N=512.
	// 16-row outer: two 8-row A panels share the same dual-B pair back-to-back
	// so Q8 B stays hot (was: full N sweep per 8 A rows).
	m := 0
	if mt >= 8 {
		for ; m+15 < M; m += 16 {
			a0 := a[m*K : (m+8)*K]
			a1 := a[(m+8)*K : (m+16)*K]
			n := ns
			for ; n+1 < ne; n += 2 {
				bn0, bn1 := bias[n], bias[n+1]
				if N == 512 {
					ok0 := q8TryDual8AccumN512(out, a0, b, m, n, nBlocks, K, bn0, bn1)
					ok1 := q8TryDual8AccumN512(out, a1, b, m+8, n, nBlocks, K, bn0, bn1)
					if ok0 && ok1 {
						continue
					}
					if !ok0 {
						q8DualMultiDot8T(&dDual0, &dDual1, a0, b, n, n+1, nBlocks, K)
						storeDual4Accum(out, m, n, N, &dDual0, bn0, bn1)
						storeDual4Accum(out, m+4, n, N, &dDual1, bn0, bn1)
					}
					if !ok1 {
						q8DualMultiDot8T(&dDual0, &dDual1, a1, b, n, n+1, nBlocks, K)
						storeDual4Accum(out, m+8, n, N, &dDual0, bn0, bn1)
						storeDual4Accum(out, m+12, n, N, &dDual1, bn0, bn1)
					}
					continue
				}
				q8DualMultiDot8T(&dDual0, &dDual1, a0, b, n, n+1, nBlocks, K)
				storeDual4Accum(out, m, n, N, &dDual0, bn0, bn1)
				storeDual4Accum(out, m+4, n, N, &dDual1, bn0, bn1)
				q8DualMultiDot8T(&dDual0, &dDual1, a1, b, n, n+1, nBlocks, K)
				storeDual4Accum(out, m+8, n, N, &dDual0, bn0, bn1)
				storeDual4Accum(out, m+12, n, N, &dDual1, bn0, bn1)
			}
			for ; n < ne; n++ {
				bn := bias[n]
				q8MultiDot8T(&d8, a0, b, n, nBlocks, K)
				storeDot8Accum(out, m, n, N, &d8, bn)
				q8MultiDot8T(&d8, a1, b, n, nBlocks, K)
				storeDot8Accum(out, m+8, n, N, &d8, bn)
			}
		}
		for ; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			n := ns
			for ; n+1 < ne; n += 2 {
				if N == 512 && q8TryDual8AccumN512(out, aPanel, b, m, n, nBlocks, K, bias[n], bias[n+1]) {
					continue
				}
				q8DualMultiDot8T(&dDual0, &dDual1, aPanel, b, n, n+1, nBlocks, K)
				storeDual4Accum(out, m, n, N, &dDual0, bias[n], bias[n+1])
				storeDual4Accum(out, m+4, n, N, &dDual1, bias[n], bias[n+1])
			}
			for ; n < ne; n++ {
				q8MultiDot8T(&d8, aPanel, b, n, nBlocks, K)
				storeDot8Accum(out, m, n, N, &d8, bias[n])
			}
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, K)
			storeDual4Accum(out, m, n, N, &dDual0, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			q8MultiDot4T(&d4, aPanel, b, n, nBlocks, K)
			storeDot4Accum(out, m, n, N, &d4, bias[n])
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		n := ns
		for ; n+1 < ne; n += 2 {
			s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
			out[m*N+n] += s0 + bias[n]
			out[m*N+n+1] += s1 + bias[n+1]
		}
		for ; n < ne; n++ {
			out[m*N+n] += DotQ8RowScaled(aRow, b, n) + bias[n]
		}
	}
}

func matMulQ8RangeFusedAccumScaledNoBias(out, a []float32, b *Q8Tensor, M, N, K, ns, ne, nBlocks int) {
	mt := mTileForK(K)
	var dDual0, dDual1 [8]float32
	var d4 [4]float32
	var d8 [8]float32
	m := 0
	if mt >= 8 {
		for ; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			n := ns
			for ; n+1 < ne; n += 2 {
				q8DualMultiDot8T(&dDual0, &dDual1, aPanel, b, n, n+1, nBlocks, K)
				storeDual4Accum(out, m, n, N, &dDual0, 0, 0)
				storeDual4Accum(out, m+4, n, N, &dDual1, 0, 0)
			}
			for ; n < ne; n++ {
				q8MultiDot8T(&d8, aPanel, b, n, nBlocks, K)
				storeDot8Accum(out, m, n, N, &d8, 0)
			}
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, K)
			storeDual4Accum(out, m, n, N, &dDual0, 0, 0)
		}
		for ; n < ne; n++ {
			q8MultiDot4T(&d4, aPanel, b, n, nBlocks, K)
			storeDot4Accum(out, m, n, N, &d4, 0)
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		n := ns
		for ; n+1 < ne; n += 2 {
			s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
			out[m*N+n] += s0
			out[m*N+n+1] += s1
		}
		for ; n < ne; n++ {
			out[m*N+n] += DotQ8RowScaled(aRow, b, n)
		}
	}
}

// matMulQ8RangeFusedGeneric: fused Q8 multiDot with optional ReLU/accum/scales.
func matMulQ8RangeFusedGeneric(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne, nBlocks int, hasScales, relu, accum bool, d4 *[4]float32, d8 *[8]float32) {
	mt := mTileForK(K)
	fastAccum := accum && !relu
	var dDual0, dDual1 [8]float32
	m := 0
	if mt >= 8 {
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
				if fastAccum {
					storeDual4Accum(out, m, n, N, &dDual0, bn0, bn1)
					storeDual4Accum(out, m+4, n, N, &dDual1, bn0, bn1)
				} else {
					storeDual4(out, m, n, N, &dDual0, bn0, bn1, relu, accum)
					storeDual4(out, m+4, n, N, &dDual1, bn0, bn1, relu, accum)
				}
			}
			for ; n < ne; n++ {
				bn := float32(0)
				if bias != nil {
					bn = bias[n]
				}
				if hasScales {
					q8MultiDot8T(d8, aPanel, b, n, nBlocks, K)
				} else {
					q8MultiDot8(d8, aPanel, b.Data, n, nBlocks, K)
				}
				if fastAccum {
					storeDot8Accum(out, m, n, N, d8, bn)
				} else {
					storeDot8(out, m, n, N, d8, bn, relu, accum)
				}
			}
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
			if fastAccum {
				storeDual4Accum(out, m, n, N, &dDual0, bn0, bn1)
			} else {
				storeDual4(out, m, n, N, &dDual0, bn0, bn1, relu, accum)
			}
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			if hasScales {
				q8MultiDot4T(d4, aPanel, b, n, nBlocks, K)
			} else {
				q8MultiDot4(d4, aPanel, b.Data, n, nBlocks, K)
			}
			if fastAccum {
				storeDot4Accum(out, m, n, N, d4, bn)
			} else {
				storeDot4(out, m, n, N, d4, bn, relu, accum)
			}
		}
	}
	// Pad leftover 1–3 rows after M=8/M=4 tiles, and whole M=1–2 tiles
	// (Medium last 8-row chunk is M=2). Short GEMM is M=3 at m=0 → DualDot2.
	if m < M && (m > 0 || M <= 2) && hasScales && !relu && !accum && bias == nil && (nBlocks == 24 || nBlocks == 36) {
		rows := M - m
		ap := fusedPadPool.Get().(*[]float32)
		aPad := *ap
		need := 4 * K
		if cap(aPad) < need {
			aPad = make([]float32, need)
			*ap = aPad
		} else {
			aPad = aPad[:need]
		}
		clear(aPad)
		copy(aPad, a[m*K:M*K])
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dDual0, aPad, b, n, n+1, nBlocks, K)
			for r := 0; r < rows; r++ {
				out[(m+r)*N+n] = dDual0[r]
				out[(m+r)*N+n+1] = dDual0[r+4]
			}
		}
		fusedPadPool.Put(ap)
		return
	}
	if hasScales && !relu && !accum && bias == nil && m < M {
		for n := ns; n+1 < ne; n += 2 {
			for r := m; r < M; r++ {
				v0, v1 := DotQ8RowDualScaled(a[r*K:r*K+K], b, n, n+1)
				out[r*N+n] = v0
				out[r*N+n+1] = v1
			}
		}
		if n := ns + (ne-ns)/2*2; n < ne {
			for r := m; r < M; r++ {
				out[r*N+n] = DotQ8RowScaled(a[r*K:r*K+K], b, n)
			}
		}
		return
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			var v0, v1 float32
			if hasScales {
				v0, v1 = DotQ8RowDualScaled(aRow, b, n, n+1)
				v0 += bn0
				v1 += bn1
			} else {
				v0, v1 = DotQ8RowDual(aRow, b.Data, n, n+1, nBlocks)
				v0 += bn0
				v1 += bn1
			}
			if relu {
				if v0 < 0 {
					v0 = 0
				}
				if v1 < 0 {
					v1 = 0
				}
			}
			if accum {
				out[m*N+n] += v0
				out[m*N+n+1] += v1
			} else {
				out[m*N+n] = v0
				out[m*N+n+1] = v1
			}
		}
		for ; n < ne; n++ {
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
			if accum {
				out[m*N+n] += v
			} else {
				out[m*N+n] = v
			}
		}
	}
}

// matMulQ8Range computes columns [ns,ne) for all M rows.
// accum: out += result (residual); otherwise out = result.
func matMulQ8Range(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool) {
	nBlocks := K / q8BlockSize
	var d4 [4]float32
	var d8 [8]float32
	hasScales := len(b.Scales) >= b.Rows*nBlocks && nBlocks > 0

	if !relu && !accum && bias == nil && tryGemmaFusedPlain(out, a, b, M, N, K, ns, ne) {
		return
	}

	if !useQ8DequantOnce(M, N, K, hasScales) {
		// Fused path: dual-B q8 multiDot; uses f32 Scales when available (K=2048 FFN).
		// FFN down-proj hot case: hasScales + accum residual — fully specialized.
		if hasScales && accum && !relu {
			matMulQ8RangeFusedAccumScaled(out, a, b, bias, M, N, K, ns, ne, nBlocks)
			return
		}
		matMulQ8RangeFusedGeneric(out, a, b, bias, M, N, K, ns, ne, nBlocks, hasScales, relu, accum, &d4, &d8)
		return
	}

	// Dequant each B row once into an L1-sized N-tile float panel, then multiDot.
	// FFN up-proj (ReLU) and encoder plain store are specialized (no relu/accum flags).
	if hasScales && relu && !accum {
		matMulQ8RangeDeqReLU(out, a, b, bias, M, N, K, ns, ne)
		return
	}
	if hasScales && !relu && !accum {
		matMulQ8RangeDeqPlain(out, a, b, bias, M, N, K, ns, ne)
		return
	}
	matMulQ8RangeDeqGeneric(out, a, b, bias, M, N, K, ns, ne, relu, accum, &d8)
}

// matMulQ8RangeDeqReLU: FFN up-proj — dequant panel + triple/dual multiDot + max(0,·).
func matMulQ8RangeDeqReLU(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int) {
	nTile := q8BPanelRows(K, ne-ns)
	panel, panelPool := getQ8DequantBuf(nTile * K)
	var dDual0, dDual1 [8]float32
	var d8 [8]float32
	var dTri0, dTri1 [12]float32
	hasBias := bias != nil
	mt := mTileForK(K)
	useTriple := K == 512
	for n0 := ns; n0 < ne; n0 += nTile {
		nt := nTile
		if n0+nt > ne {
			nt = ne - n0
		}
		dequantQ8Panel(b, n0, nt, K, panel, useTriple)
		if hasBias && useTriple && mt >= 8 && M >= 16 {
			matMulPanelBOuterReLU8(out, a, panel, bias, M, n0, N, K, nt, &dDual0, &dDual1, &d8, &dTri0, &dTri1)
		} else {
			m := 0
			if mt >= 8 {
				for ; m+7 < M; m += 8 {
					aPanel := a[m*K : (m+8)*K]
					matMulPanelDualReLU(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, useTriple, &dDual0, &dDual1, &d8, &dTri0, &dTri1, 8)
				}
			}
			if mt >= 4 {
				for ; m+3 < M; m += 4 {
					aPanel := a[m*K : (m+4)*K]
					matMulPanelDualReLU(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, useTriple, &dDual0, &dDual1, &d8, &dTri0, &dTri1, 4)
				}
			}
			for ; m < M; m++ {
				aRow := a[m*K : m*K+K]
				for ti := 0; ti < nt; ti++ {
					n := n0 + ti
					v := Dot(aRow, panel[ti*K:(ti+1)*K])
					if hasBias {
						v += bias[n]
					}
					if v < 0 {
						v = 0
					}
					out[m*N+n] = v
				}
			}
		}
	}
	putQ8DequantBuf(panel, panelPool)
}

// matMulQ8RangeDeqPlain: encoder GEMM — dequant + multiDot, pure store (no ReLU/accum).
func matMulQ8RangeDeqPlain(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int) {
	nTile := q8BPanelRows(K, ne-ns)
	panel, panelPool := getQ8DequantBuf(nTile * K)
	var dDual0, dDual1 [8]float32
	var d8 [8]float32
	var dTri0, dTri1 [12]float32
	hasBias := bias != nil
	mt := mTileForK(K)
	useTriple := K == 512
	for n0 := ns; n0 < ne; n0 += nTile {
		nt := nTile
		if n0+nt > ne {
			nt = ne - n0
		}
		dequantQ8Panel(b, n0, nt, K, panel, useTriple)
		if hasBias && useTriple && mt >= 8 && M >= 16 {
			matMulPanelBOuterPlain8(out, a, panel, bias, M, n0, N, K, nt, &dDual0, &dDual1, &d8, &dTri0, &dTri1)
		} else {
			m := 0
			if mt >= 8 {
				for ; m+7 < M; m += 8 {
					aPanel := a[m*K : (m+8)*K]
					matMulPanelDualPlain(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, useTriple, &dDual0, &dDual1, &d8, &dTri0, &dTri1, 8)
				}
			}
			if mt >= 4 {
				for ; m+3 < M; m += 4 {
					aPanel := a[m*K : (m+4)*K]
					matMulPanelDualPlain(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, useTriple, &dDual0, &dDual1, &d8, &dTri0, &dTri1, 4)
				}
			}
			for ; m < M; m++ {
				aRow := a[m*K : m*K+K]
				for ti := 0; ti < nt; ti++ {
					n := n0 + ti
					v := Dot(aRow, panel[ti*K:(ti+1)*K])
					if hasBias {
						v += bias[n]
					}
					out[m*N+n] = v
				}
			}
		}
	}
	putQ8DequantBuf(panel, panelPool)
}

// matMulPanelDualPlain: encoder — triple/dual multiDot, pure store + bias (no flags).
// hasBias is specialized (SenseVoice linears always bias) to drop per-column branches.

// matMulPanelBOuterPlain8: B-tile outer x all 8-row A tiles (encoder plain).
// Triple-triple (9 B cols) then dual-triple (6): m-outer s-inner shares each A
// panel across 3/2 multiDots. dual-m 16 thrash; B-triple-outer/s-outer reloads
// A 3× and measured worse e2e for typical M.
func matMulPanelBOuterPlain8(out, a, panel, bias []float32, M, n0, N, K, nt int, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32) {
	t := 0
	for ; t+8 < nt; t += 9 {
		n := n0 + t
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			for s := 0; s < 9; s += 3 {
				nn := n + s
				tt := t + s
				b0 := panel[tt*K : (tt+1)*K]
				b1 := panel[(tt+1)*K : (tt+2)*K]
				b2 := panel[(tt+2)*K : (tt+3)*K]
				bn0, bn1, bn2 := bias[nn], bias[nn+1], bias[nn+2]
				if !multiDot8TriplePlain(out, aPanel, b0, b1, b2, m, nn, N, K, bn0, bn1, bn2) {
					multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
					storeTriple4Plain(out, m, nn, N, dTri0, bn0, bn1, bn2)
					storeTriple4Plain(out, m+4, nn, N, dTri1, bn0, bn1, bn2)
				}
			}
		}
	}
	for ; t+5 < nt; t += 6 {
		n := n0 + t
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			for s := 0; s < 6; s += 3 {
				nn := n + s
				tt := t + s
				b0 := panel[tt*K : (tt+1)*K]
				b1 := panel[(tt+1)*K : (tt+2)*K]
				b2 := panel[(tt+2)*K : (tt+3)*K]
				bn0, bn1, bn2 := bias[nn], bias[nn+1], bias[nn+2]
				if !multiDot8TriplePlain(out, aPanel, b0, b1, b2, m, nn, N, K, bn0, bn1, bn2) {
					multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
					storeTriple4Plain(out, m, nn, N, dTri0, bn0, bn1, bn2)
					storeTriple4Plain(out, m+4, nn, N, dTri1, bn0, bn1, bn2)
				}
			}
		}
	}
	for ; t+2 < nt; t += 3 {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		b1 := panel[(t+1)*K : (t+2)*K]
		b2 := panel[(t+2)*K : (t+3)*K]
		bn0, bn1, bn2 := bias[n], bias[n+1], bias[n+2]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			if multiDot8TriplePlain(out, aPanel, b0, b1, b2, m, n, N, K, bn0, bn1, bn2) {
				continue
			}
			multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
			storeTriple4Plain(out, m, n, N, dTri0, bn0, bn1, bn2)
			storeTriple4Plain(out, m+4, n, N, dTri1, bn0, bn1, bn2)
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		b1 := panel[(t+1)*K : (t+2)*K]
		bn0, bn1 := bias[n], bias[n+1]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			if multiDot8DualPlain(out, aPanel, b0, b1, m, n, N, K, bn0, bn1) {
				continue
			}
			multiDot8DualB(dDual0, dDual1, aPanel, b0, b1, K)
			storeDual4Plain(out, m, n, N, dDual0, bn0, bn1)
			storeDual4Plain(out, m+4, n, N, dDual1, bn0, bn1)
		}
	}
	for ; t < nt; t++ {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		bn := bias[n]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			multiDot8(d8, aPanel, b0, K)
			storeDot8Plain(out, m, n, N, d8, bn)
		}
	}
	m0 := M - (M % 8)
	if m0 >= M {
		return
	}
	var d4 [4]float32
	m := m0
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		tt := 0
		for ; tt+2 < nt; tt += 3 {
			n := n0 + tt
			multiDot4TripleB(dTri0, aPanel,
				panel[tt*K:(tt+1)*K], panel[(tt+1)*K:(tt+2)*K], panel[(tt+2)*K:(tt+3)*K], K)
			storeTriple4Plain(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
		}
		for ; tt+1 < nt; tt += 2 {
			n := n0 + tt
			multiDot4DualB(dDual0, aPanel, panel[tt*K:(tt+1)*K], panel[(tt+1)*K:(tt+2)*K], K)
			storeDual4Plain(out, m, n, N, dDual0, bias[n], bias[n+1])
		}
		for ; tt < nt; tt++ {
			n := n0 + tt
			multiDot4(&d4, aPanel, panel[tt*K:(tt+1)*K], K)
			storeDot4Plain(out, m, n, N, &d4, bias[n])
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for ti := 0; ti < nt; ti++ {
			n := n0 + ti
			out[m*N+n] = Dot(aRow, panel[ti*K:(ti+1)*K]) + bias[n]
		}
	}
}

// matMulPanelBOuterReLU8: B-tile outer for FFN up (N=2048 ReLU store).
// Triple-triple (9) then dual-triple (6): m-outer s-inner (A reuse).
// dual-m 16 and B-triple-outer/s-outer both measured e2e regressions.
func matMulPanelBOuterReLU8(out, a, panel, bias []float32, M, n0, N, K, nt int, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32) {
	t := 0
	for ; t+8 < nt; t += 9 {
		n := n0 + t
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			for s := 0; s < 9; s += 3 {
				nn := n + s
				tt := t + s
				b0 := panel[tt*K : (tt+1)*K]
				b1 := panel[(tt+1)*K : (tt+2)*K]
				b2 := panel[(tt+2)*K : (tt+3)*K]
				bn0, bn1, bn2 := bias[nn], bias[nn+1], bias[nn+2]
				if !multiDot8TripleReLU(out, aPanel, b0, b1, b2, m, nn, N, K, bn0, bn1, bn2) {
					multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
					storeTriple4ReLU(out, m, nn, N, dTri0, bn0, bn1, bn2)
					storeTriple4ReLU(out, m+4, nn, N, dTri1, bn0, bn1, bn2)
				}
			}
		}
	}
	for ; t+5 < nt; t += 6 {
		n := n0 + t
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			for s := 0; s < 6; s += 3 {
				nn := n + s
				tt := t + s
				b0 := panel[tt*K : (tt+1)*K]
				b1 := panel[(tt+1)*K : (tt+2)*K]
				b2 := panel[(tt+2)*K : (tt+3)*K]
				bn0, bn1, bn2 := bias[nn], bias[nn+1], bias[nn+2]
				if !multiDot8TripleReLU(out, aPanel, b0, b1, b2, m, nn, N, K, bn0, bn1, bn2) {
					multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
					storeTriple4ReLU(out, m, nn, N, dTri0, bn0, bn1, bn2)
					storeTriple4ReLU(out, m+4, nn, N, dTri1, bn0, bn1, bn2)
				}
			}
		}
	}
	for ; t+2 < nt; t += 3 {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		b1 := panel[(t+1)*K : (t+2)*K]
		b2 := panel[(t+2)*K : (t+3)*K]
		bn0, bn1, bn2 := bias[n], bias[n+1], bias[n+2]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			if multiDot8TripleReLU(out, aPanel, b0, b1, b2, m, n, N, K, bn0, bn1, bn2) {
				continue
			}
			multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
			storeTriple4ReLU(out, m, n, N, dTri0, bn0, bn1, bn2)
			storeTriple4ReLU(out, m+4, n, N, dTri1, bn0, bn1, bn2)
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		b1 := panel[(t+1)*K : (t+2)*K]
		bn0, bn1 := bias[n], bias[n+1]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			if multiDot8DualReLU(out, aPanel, b0, b1, m, n, N, K, bn0, bn1) {
				continue
			}
			multiDot8DualB(dDual0, dDual1, aPanel, b0, b1, K)
			storeDual4ReLU(out, m, n, N, dDual0, bn0, bn1)
			storeDual4ReLU(out, m+4, n, N, dDual1, bn0, bn1)
		}
	}
	for ; t < nt; t++ {
		n := n0 + t
		b0 := panel[t*K : (t+1)*K]
		bn := bias[n]
		for m := 0; m+7 < M; m += 8 {
			aPanel := a[m*K : (m+8)*K]
			multiDot8(d8, aPanel, b0, K)
			storeDot8ReLU(out, m, n, N, d8, bn)
		}
	}
	m0 := M - (M % 8)
	if m0 >= M {
		return
	}
	var d4 [4]float32
	m := m0
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		tt := 0
		for ; tt+2 < nt; tt += 3 {
			n := n0 + tt
			multiDot4TripleB(dTri0, aPanel,
				panel[tt*K:(tt+1)*K], panel[(tt+1)*K:(tt+2)*K], panel[(tt+2)*K:(tt+3)*K], K)
			storeTriple4ReLU(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
		}
		for ; tt+1 < nt; tt += 2 {
			n := n0 + tt
			multiDot4DualB(dDual0, aPanel, panel[tt*K:(tt+1)*K], panel[(tt+1)*K:(tt+2)*K], K)
			storeDual4ReLU(out, m, n, N, dDual0, bias[n], bias[n+1])
		}
		for ; tt < nt; tt++ {
			n := n0 + tt
			multiDot4(&d4, aPanel, panel[tt*K:(tt+1)*K], K)
			storeDot4ReLU(out, m, n, N, &d4, bias[n])
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for ti := 0; ti < nt; ti++ {
			n := n0 + ti
			v := Dot(aRow, panel[ti*K:(ti+1)*K]) + bias[n]
			if v < 0 {
				v = 0
			}
			out[m*N+n] = v
		}
	}
}

func matMulPanelDualPlain(out, aPanel, panel, bias []float32, m, n0, N, K, nt int, hasBias, useTriple bool, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32, rows int) {
	if hasBias {
		matMulPanelDualPlainBias(out, aPanel, panel, bias, m, n0, N, K, nt, useTriple, dDual0, dDual1, d8, dTri0, dTri1, rows)
		return
	}
	matMulPanelDualPlainNoBias(out, aPanel, panel, m, n0, N, K, nt, useTriple, dDual0, dDual1, d8, dTri0, dTri1, rows)
}

func matMulPanelDualPlainBias(out, aPanel, panel, bias []float32, m, n0, N, K, nt int, useTriple bool, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32, rows int) {
	t := 0
	if rows >= 8 {
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				b0 := panel[t*K : (t+1)*K]
				b1 := panel[(t+1)*K : (t+2)*K]
				b2 := panel[(t+2)*K : (t+3)*K]
				if multiDot8TriplePlain(out, aPanel, b0, b1, b2, m, n, N, K, bias[n], bias[n+1], bias[n+2]) {
					continue
				}
				multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
				storeTriple4Plain(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
				storeTriple4Plain(out, m+4, n, N, dTri1, bias[n], bias[n+1], bias[n+2])
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			b0 := panel[t*K : (t+1)*K]
			b1 := panel[(t+1)*K : (t+2)*K]
			if multiDot8DualPlain(out, aPanel, b0, b1, m, n, N, K, bias[n], bias[n+1]) {
				continue
			}
			multiDot8DualB(dDual0, dDual1, aPanel, b0, b1, K)
			storeDual4Plain(out, m, n, N, dDual0, bias[n], bias[n+1])
			storeDual4Plain(out, m+4, n, N, dDual1, bias[n], bias[n+1])
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			storeDot8Plain(out, m, n, N, d8, bias[n])
		}
		return
	}
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			storeTriple4Plain(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		storeDual4Plain(out, m, n, N, dDual0, bias[n], bias[n+1])
	}
	for ; t < nt; t++ {
		n := n0 + t
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		storeDot4Plain(out, m, n, N, &d4, bias[n])
	}
}

func matMulPanelDualPlainNoBias(out, aPanel, panel []float32, m, n0, N, K, nt int, useTriple bool, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32, rows int) {
	t := 0
	if rows >= 8 {
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				multiDot8TripleB(dTri0, dTri1, aPanel,
					panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
				storeTriple4Plain(out, m, n, N, dTri0, 0, 0, 0)
				storeTriple4Plain(out, m+4, n, N, dTri1, 0, 0, 0)
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			storeDual4Plain(out, m, n, N, dDual0, 0, 0)
			storeDual4Plain(out, m+4, n, N, dDual1, 0, 0)
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			storeDot8Plain(out, m, n, N, d8, 0)
		}
		return
	}
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			storeTriple4Plain(out, m, n, N, dTri0, 0, 0, 0)
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		storeDual4Plain(out, m, n, N, dDual0, 0, 0)
	}
	for ; t < nt; t++ {
		n := n0 + t
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		storeDot4Plain(out, m, n, N, &d4, 0)
	}
}

func matMulQ8RangeDeqGeneric(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, d8 *[8]float32) {
	nTile := q8BPanelRows(K, ne-ns)
	panel, panelPool := getQ8DequantBuf(nTile * K)
	var dDual0, dDual1 [8]float32
	hasBias := bias != nil
	mt := mTileForK(K)
	for n0 := ns; n0 < ne; n0 += nTile {
		nt := nTile
		if n0+nt > ne {
			nt = ne - n0
		}
		dequantQ8Panel(b, n0, nt, K, panel, K == 512)
		m := 0
		if mt >= 8 {
			for ; m+7 < M; m += 8 {
				aPanel := a[m*K : (m+8)*K]
				matMulPanelDual(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, relu, accum, &dDual0, &dDual1, d8, 8)
			}
		}
		if mt >= 4 {
			for ; m+3 < M; m += 4 {
				aPanel := a[m*K : (m+4)*K]
				matMulPanelDual(out, aPanel, panel, bias, m, n0, N, K, nt, hasBias, relu, accum, &dDual0, &dDual1, d8, 4)
			}
		}
		for ; m < M; m++ {
			aRow := a[m*K : m*K+K]
			for ti := 0; ti < nt; ti++ {
				n := n0 + ti
				v := Dot(aRow, panel[ti*K:(ti+1)*K])
				if hasBias {
					v += bias[n]
				}
				if relu && v < 0 {
					v = 0
				}
				if accum {
					out[m*N+n] += v
				} else {
					out[m*N+n] = v
				}
			}
		}
	}
	putQ8DequantBuf(panel, panelPool)
}

// matMulPanelDualReLU: FFN up — triple then dual, always store max(0, d+bias).
// hasBias specialized (SenseVoice FFN always bias).
func matMulPanelDualReLU(out, aPanel, panel, bias []float32, m, n0, N, K, nt int, hasBias, useTriple bool, dDual0, dDual1, d8 *[8]float32, dTri0, dTri1 *[12]float32, rows int) {
	if !hasBias {
		// rare: zero bias via plain zero constants
		t := 0
		if rows >= 8 {
			if useTriple {
				for ; t+2 < nt; t += 3 {
					n := n0 + t
					multiDot8TripleB(dTri0, dTri1, aPanel,
						panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
					storeTriple4ReLU(out, m, n, N, dTri0, 0, 0, 0)
					storeTriple4ReLU(out, m+4, n, N, dTri1, 0, 0, 0)
				}
			}
			for ; t+1 < nt; t += 2 {
				n := n0 + t
				multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
				storeDual4ReLU(out, m, n, N, dDual0, 0, 0)
				storeDual4ReLU(out, m+4, n, N, dDual1, 0, 0)
			}
			for ; t < nt; t++ {
				n := n0 + t
				multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
				storeDot8ReLU(out, m, n, N, d8, 0)
			}
			return
		}
		var d4 [4]float32
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				multiDot4TripleB(dTri0, aPanel,
					panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
				storeTriple4ReLU(out, m, n, N, dTri0, 0, 0, 0)
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			storeDual4ReLU(out, m, n, N, dDual0, 0, 0)
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
			storeDot4ReLU(out, m, n, N, &d4, 0)
		}
		return
	}
	t := 0
	if rows >= 8 {
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				b0 := panel[t*K : (t+1)*K]
				b1 := panel[(t+1)*K : (t+2)*K]
				b2 := panel[(t+2)*K : (t+3)*K]
				if multiDot8TripleReLU(out, aPanel, b0, b1, b2, m, n, N, K, bias[n], bias[n+1], bias[n+2]) {
					continue
				}
				multiDot8TripleB(dTri0, dTri1, aPanel, b0, b1, b2, K)
				storeTriple4ReLU(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
				storeTriple4ReLU(out, m+4, n, N, dTri1, bias[n], bias[n+1], bias[n+2])
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			b0 := panel[t*K : (t+1)*K]
			b1 := panel[(t+1)*K : (t+2)*K]
			if multiDot8DualReLU(out, aPanel, b0, b1, m, n, N, K, bias[n], bias[n+1]) {
				continue
			}
			multiDot8DualB(dDual0, dDual1, aPanel, b0, b1, K)
			storeDual4ReLU(out, m, n, N, dDual0, bias[n], bias[n+1])
			storeDual4ReLU(out, m+4, n, N, dDual1, bias[n], bias[n+1])
		}
		for ; t < nt; t++ {
			n := n0 + t
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			storeDot8ReLU(out, m, n, N, d8, bias[n])
		}
		return
	}
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			storeTriple4ReLU(out, m, n, N, dTri0, bias[n], bias[n+1], bias[n+2])
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		storeDual4ReLU(out, m, n, N, dDual0, bias[n], bias[n+1])
	}
	for ; t < nt; t++ {
		n := n0 + t
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		storeDot4ReLU(out, m, n, N, &d4, bias[n])
	}
}

// matMulPanelDual runs multiDot for an A panel of 4 or 8 rows against
// a dequantized B panel of nt columns starting at n0.
// Prefers triple-B (3 cols) then dual-B then single for max A-load amortization.
func matMulPanelDual(out, aPanel, panel, bias []float32, m, n0, N, K, nt int, hasBias, relu, accum bool, dDual0, dDual1 *[8]float32, d8 *[8]float32, rows int) {
	// FFN up-proj: ReLU store without accum — branch-free max(0, ·).
	fastReLU := relu && !accum
	t := 0
	var dTri0, dTri1 [12]float32
	useTriple := K == 512 // specialized triple micro-kernel
	if rows >= 8 {
		if useTriple {
			for ; t+2 < nt; t += 3 {
				n := n0 + t
				multiDot8TripleB(&dTri0, &dTri1, aPanel,
					panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
				bn0, bn1, bn2 := float32(0), float32(0), float32(0)
				if hasBias {
					bn0, bn1, bn2 = bias[n], bias[n+1], bias[n+2]
				}
				if fastReLU {
					storeTriple4ReLU(out, m, n, N, &dTri0, bn0, bn1, bn2)
					storeTriple4ReLU(out, m+4, n, N, &dTri1, bn0, bn1, bn2)
				} else {
					storeTriple4(out, m, n, N, &dTri0, bn0, bn1, bn2, relu, accum)
					storeTriple4(out, m+4, n, N, &dTri1, bn0, bn1, bn2, relu, accum)
				}
			}
		}
		for ; t+1 < nt; t += 2 {
			n := n0 + t
			multiDot8DualB(dDual0, dDual1, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
			bn0, bn1 := float32(0), float32(0)
			if hasBias {
				bn0, bn1 = bias[n], bias[n+1]
			}
			if fastReLU {
				storeDual4ReLU(out, m, n, N, dDual0, bn0, bn1)
				storeDual4ReLU(out, m+4, n, N, dDual1, bn0, bn1)
			} else {
				storeDual4(out, m, n, N, dDual0, bn0, bn1, relu, accum)
				storeDual4(out, m+4, n, N, dDual1, bn0, bn1, relu, accum)
			}
		}
		for ; t < nt; t++ {
			n := n0 + t
			bn := float32(0)
			if hasBias {
				bn = bias[n]
			}
			multiDot8(d8, aPanel, panel[t*K:(t+1)*K], K)
			if fastReLU {
				storeDot8ReLU(out, m, n, N, d8, bn)
			} else {
				storeDot8(out, m, n, N, d8, bn, relu, accum)
			}
		}
		return
	}
	// 4-row panel
	var d4 [4]float32
	if useTriple {
		for ; t+2 < nt; t += 3 {
			n := n0 + t
			multiDot4TripleB(&dTri0, aPanel,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K], K)
			bn0, bn1, bn2 := float32(0), float32(0), float32(0)
			if hasBias {
				bn0, bn1, bn2 = bias[n], bias[n+1], bias[n+2]
			}
			if fastReLU {
				storeTriple4ReLU(out, m, n, N, &dTri0, bn0, bn1, bn2)
			} else {
				storeTriple4(out, m, n, N, &dTri0, bn0, bn1, bn2, relu, accum)
			}
		}
	}
	for ; t+1 < nt; t += 2 {
		n := n0 + t
		multiDot4DualB(dDual0, aPanel, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], K)
		bn0, bn1 := float32(0), float32(0)
		if hasBias {
			bn0, bn1 = bias[n], bias[n+1]
		}
		if fastReLU {
			storeDual4ReLU(out, m, n, N, dDual0, bn0, bn1)
		} else {
			storeDual4(out, m, n, N, dDual0, bn0, bn1, relu, accum)
		}
	}
	for ; t < nt; t++ {
		n := n0 + t
		bn := float32(0)
		if hasBias {
			bn = bias[n]
		}
		multiDot4(&d4, aPanel, panel[t*K:(t+1)*K], K)
		if fastReLU {
			storeDot4ReLU(out, m, n, N, &d4, bn)
		} else {
			storeDot4(out, m, n, N, &d4, bn, relu, accum)
		}
	}
}

// storeTriple4 writes 4 A rows × 3 B columns from multiDot4TripleB layout:
// d[0:4]=col n, d[4:8]=col n+1, d[8:12]=col n+2.
func storeTriple4(out []float32, m, n, N int, d *[12]float32, bn0, bn1, bn2 float32, relu, accum bool) {
	if relu {
		for t := 0; t < 4; t++ {
			v0 := d[t] + bn0
			v1 := d[4+t] + bn1
			v2 := d[8+t] + bn2
			if v0 < 0 {
				v0 = 0
			}
			if v1 < 0 {
				v1 = 0
			}
			if v2 < 0 {
				v2 = 0
			}
			if accum {
				out[(m+t)*N+n] += v0
				out[(m+t)*N+n+1] += v1
				out[(m+t)*N+n+2] += v2
			} else {
				out[(m+t)*N+n] = v0
				out[(m+t)*N+n+1] = v1
				out[(m+t)*N+n+2] = v2
			}
		}
		return
	}
	if accum {
		out[m*N+n] += d[0] + bn0
		out[(m+1)*N+n] += d[1] + bn0
		out[(m+2)*N+n] += d[2] + bn0
		out[(m+3)*N+n] += d[3] + bn0
		out[m*N+n+1] += d[4] + bn1
		out[(m+1)*N+n+1] += d[5] + bn1
		out[(m+2)*N+n+1] += d[6] + bn1
		out[(m+3)*N+n+1] += d[7] + bn1
		out[m*N+n+2] += d[8] + bn2
		out[(m+1)*N+n+2] += d[9] + bn2
		out[(m+2)*N+n+2] += d[10] + bn2
		out[(m+3)*N+n+2] += d[11] + bn2
		return
	}
	storeTriple4Plain(out, m, n, N, d, bn0, bn1, bn2)
}

// storeDual4 writes 4 A rows × 2 B columns from multiDot4DualB layout:
// d[0:4]=col n, d[4:8]=col n+1.
func storeDual4(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32, relu, accum bool) {
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
			if accum {
				out[(m+t)*N+n] += v0
				out[(m+t)*N+n+1] += v1
			} else {
				out[(m+t)*N+n] = v0
				out[(m+t)*N+n+1] = v1
			}
		}
		return
	}
	if accum {
		storeDual4Accum(out, m, n, N, d, bn0, bn1)
		return
	}
	storeDual4Plain(out, m, n, N, d, bn0, bn1)
}

// storeDual4Accum: residual accumulate (FFN down-proj), no ReLU branches.
// Pair (n, n+1) per row so the two stores are adjacent in row-major out.
// N=512 (SenseVoice hidden) uses constant row strides for better addressing.
func storeDual4Accum(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32) {
	if N == 512 {
		base := m*512 + n
		out[base] += d[0] + bn0
		out[base+1] += d[4] + bn1
		out[base+512] += d[1] + bn0
		out[base+513] += d[5] + bn1
		out[base+1024] += d[2] + bn0
		out[base+1025] += d[6] + bn1
		out[base+1536] += d[3] + bn0
		out[base+1537] += d[7] + bn1
		return
	}
	base := m * N
	out[base+n] += d[0] + bn0
	out[base+n+1] += d[4] + bn1
	base += N
	out[base+n] += d[1] + bn0
	out[base+n+1] += d[5] + bn1
	base += N
	out[base+n] += d[2] + bn0
	out[base+n+1] += d[6] + bn1
	base += N
	out[base+n] += d[3] + bn0
	out[base+n+1] += d[7] + bn1
}

func storeDot4Accum(out []float32, m, n, N int, d *[4]float32, bn float32) {
	if N == 512 {
		base := m*512 + n
		out[base] += d[0] + bn
		out[base+512] += d[1] + bn
		out[base+1024] += d[2] + bn
		out[base+1536] += d[3] + bn
		return
	}
	out[m*N+n] += d[0] + bn
	out[(m+1)*N+n] += d[1] + bn
	out[(m+2)*N+n] += d[2] + bn
	out[(m+3)*N+n] += d[3] + bn
}

func storeDot8Accum(out []float32, m, n, N int, d *[8]float32, bn float32) {
	if N == 512 {
		base := m*512 + n
		out[base] += d[0] + bn
		out[base+512] += d[1] + bn
		out[base+1024] += d[2] + bn
		out[base+1536] += d[3] + bn
		out[base+2048] += d[4] + bn
		out[base+2560] += d[5] + bn
		out[base+3072] += d[6] + bn
		out[base+3584] += d[7] + bn
		return
	}
	out[m*N+n] += d[0] + bn
	out[(m+1)*N+n] += d[1] + bn
	out[(m+2)*N+n] += d[2] + bn
	out[(m+3)*N+n] += d[3] + bn
	out[(m+4)*N+n] += d[4] + bn
	out[(m+5)*N+n] += d[5] + bn
	out[(m+6)*N+n] += d[6] + bn
	out[(m+7)*N+n] += d[7] + bn
}

// gemmaStoreTailCol writes one leftover B column for rows of A (odd N-split).
func gemmaStoreTailCol(out, a []float32, b *Q8Tensor, rows, N, K, n int) {
	if n < 0 || rows <= 0 || b == nil {
		return
	}
	for r := 0; r < rows; r++ {
		out[r*N+n] = DotQ8RowScaled(a[r*K:(r+1)*K], b, n)
	}
}

// gemmaStoreM3N4 writes 3 A rows × 4 B cols from quad-3 accums.
func gemmaStoreM3N4(out []float32, n, stride int, d *[12]float32) {
	out[n] = d[0]
	out[n+1] = d[3]
	out[n+2] = d[6]
	out[n+3] = d[9]
	out[stride+n] = d[1]
	out[stride+n+1] = d[4]
	out[stride+n+2] = d[7]
	out[stride+n+3] = d[10]
	out[2*stride+n] = d[2]
	out[2*stride+n+1] = d[5]
	out[2*stride+n+2] = d[8]
	out[2*stride+n+3] = d[11]
}

func storeDual3Plain(out []float32, m, n, N int, d *[8]float32) {
	base := m*N + n
	out[base] = d[0]
	out[base+1] = d[4]
	base += N
	out[base] = d[1]
	out[base+1] = d[5]
	base += N
	out[base] = d[2]
	out[base+1] = d[6]
}

// storeDual4Plain: encoder pure store (no ReLU/accum). Pair-adjacent columns.
func storeDual4Plain(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32) {
	if N == 512 {
		base := m*512 + n
		out[base] = d[0] + bn0
		out[base+1] = d[4] + bn1
		out[base+512] = d[1] + bn0
		out[base+513] = d[5] + bn1
		out[base+1024] = d[2] + bn0
		out[base+1025] = d[6] + bn1
		out[base+1536] = d[3] + bn0
		out[base+1537] = d[7] + bn1
		return
	}
	switch N {
	case 256, 768, 1152:
		base := m*N + n
		out[base] = d[0] + bn0
		out[base+1] = d[4] + bn1
		base += N
		out[base] = d[1] + bn0
		out[base+1] = d[5] + bn1
		base += N
		out[base] = d[2] + bn0
		out[base+1] = d[6] + bn1
		base += N
		out[base] = d[3] + bn0
		out[base+1] = d[7] + bn1
		return
	}
	base := m * N
	out[base+n] = d[0] + bn0
	out[base+n+1] = d[4] + bn1
	base += N
	out[base+n] = d[1] + bn0
	out[base+n+1] = d[5] + bn1
	base += N
	out[base+n] = d[2] + bn0
	out[base+n+1] = d[6] + bn1
	base += N
	out[base+n] = d[3] + bn0
	out[base+n+1] = d[7] + bn1
}

// storeTriple4Plain: encoder pure store, three adjacent columns per row.
func storeTriple4Plain(out []float32, m, n, N int, d *[12]float32, bn0, bn1, bn2 float32) {
	if N == 512 {
		base := m*512 + n
		out[base], out[base+1], out[base+2] = d[0]+bn0, d[4]+bn1, d[8]+bn2
		out[base+512], out[base+513], out[base+514] = d[1]+bn0, d[5]+bn1, d[9]+bn2
		out[base+1024], out[base+1025], out[base+1026] = d[2]+bn0, d[6]+bn1, d[10]+bn2
		out[base+1536], out[base+1537], out[base+1538] = d[3]+bn0, d[7]+bn1, d[11]+bn2
		return
	}
	base := m*N + n
	out[base], out[base+1], out[base+2] = d[0]+bn0, d[4]+bn1, d[8]+bn2
	base += N
	out[base], out[base+1], out[base+2] = d[1]+bn0, d[5]+bn1, d[9]+bn2
	base += N
	out[base], out[base+1], out[base+2] = d[2]+bn0, d[6]+bn1, d[10]+bn2
	base += N
	out[base], out[base+1], out[base+2] = d[3]+bn0, d[7]+bn1, d[11]+bn2
}

func storeDot4Plain(out []float32, m, n, N int, d *[4]float32, bn float32) {
	if N == 512 {
		base := m*512 + n
		out[base] = d[0] + bn
		out[base+512] = d[1] + bn
		out[base+1024] = d[2] + bn
		out[base+1536] = d[3] + bn
		return
	}
	out[m*N+n] = d[0] + bn
	out[(m+1)*N+n] = d[1] + bn
	out[(m+2)*N+n] = d[2] + bn
	out[(m+3)*N+n] = d[3] + bn
}

func storeDot8Plain(out []float32, m, n, N int, d *[8]float32, bn float32) {
	if N == 512 {
		base := m*512 + n
		out[base] = d[0] + bn
		out[base+512] = d[1] + bn
		out[base+1024] = d[2] + bn
		out[base+1536] = d[3] + bn
		out[base+2048] = d[4] + bn
		out[base+2560] = d[5] + bn
		out[base+3072] = d[6] + bn
		out[base+3584] = d[7] + bn
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

// storeDual4ReLU: FFN up-proj store max(0, d+bias), pair-adjacent columns.
// N=2048 (SenseVoice FFN intermediate) uses constant strides.
func storeDual4ReLU(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32) {
	if N == 2048 {
		base := m*2048 + n
		v0, v1 := d[0]+bn0, d[4]+bn1
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		out[base], out[base+1] = v0, v1
		v0, v1 = d[1]+bn0, d[5]+bn1
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		out[base+2048], out[base+2049] = v0, v1
		v0, v1 = d[2]+bn0, d[6]+bn1
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		out[base+4096], out[base+4097] = v0, v1
		v0, v1 = d[3]+bn0, d[7]+bn1
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		out[base+6144], out[base+6145] = v0, v1
		return
	}
	base := m * N
	v0 := d[0] + bn0
	v1 := d[4] + bn1
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	out[base+n], out[base+n+1] = v0, v1
	base += N
	v0 = d[1] + bn0
	v1 = d[5] + bn1
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	out[base+n], out[base+n+1] = v0, v1
	base += N
	v0 = d[2] + bn0
	v1 = d[6] + bn1
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	out[base+n], out[base+n+1] = v0, v1
	base += N
	v0 = d[3] + bn0
	v1 = d[7] + bn1
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	out[base+n], out[base+n+1] = v0, v1
}

func storeTriple4ReLU(out []float32, m, n, N int, d *[12]float32, bn0, bn1, bn2 float32) {
	if N == 2048 {
		base := m*2048 + n
		v0, v1, v2 := d[0]+bn0, d[4]+bn1, d[8]+bn2
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		if v2 < 0 {
			v2 = 0
		}
		out[base], out[base+1], out[base+2] = v0, v1, v2
		v0, v1, v2 = d[1]+bn0, d[5]+bn1, d[9]+bn2
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		if v2 < 0 {
			v2 = 0
		}
		out[base+2048], out[base+2049], out[base+2050] = v0, v1, v2
		v0, v1, v2 = d[2]+bn0, d[6]+bn1, d[10]+bn2
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		if v2 < 0 {
			v2 = 0
		}
		out[base+4096], out[base+4097], out[base+4098] = v0, v1, v2
		v0, v1, v2 = d[3]+bn0, d[7]+bn1, d[11]+bn2
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
		if v2 < 0 {
			v2 = 0
		}
		out[base+6144], out[base+6145], out[base+6146] = v0, v1, v2
		return
	}
	base := m*N + n
	v0, v1, v2 := d[0]+bn0, d[4]+bn1, d[8]+bn2
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	if v2 < 0 {
		v2 = 0
	}
	out[base], out[base+1], out[base+2] = v0, v1, v2
	base += N
	v0, v1, v2 = d[1]+bn0, d[5]+bn1, d[9]+bn2
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	if v2 < 0 {
		v2 = 0
	}
	out[base], out[base+1], out[base+2] = v0, v1, v2
	base += N
	v0, v1, v2 = d[2]+bn0, d[6]+bn1, d[10]+bn2
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	if v2 < 0 {
		v2 = 0
	}
	out[base], out[base+1], out[base+2] = v0, v1, v2
	base += N
	v0, v1, v2 = d[3]+bn0, d[7]+bn1, d[11]+bn2
	if v0 < 0 {
		v0 = 0
	}
	if v1 < 0 {
		v1 = 0
	}
	if v2 < 0 {
		v2 = 0
	}
	out[base], out[base+1], out[base+2] = v0, v1, v2
}

func storeDot4ReLU(out []float32, m, n, N int, d *[4]float32, bn float32) {
	if N == 2048 {
		base := m*2048 + n
		for t := 0; t < 4; t++ {
			v := d[t] + bn
			if v < 0 {
				v = 0
			}
			out[base] = v
			base += 2048
		}
		return
	}
	for t := 0; t < 4; t++ {
		v := d[t] + bn
		if v < 0 {
			v = 0
		}
		out[(m+t)*N+n] = v
	}
}

func storeDot8ReLU(out []float32, m, n, N int, d *[8]float32, bn float32) {
	for t := 0; t < 8; t++ {
		v := d[t] + bn
		if v < 0 {
			v = 0
		}
		out[(m+t)*N+n] = v
	}
}

func matMulQ8SerialMAct(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int, relu, accum bool) {
	matMulQ8Range(out, a, b, bias, M, N, K, 0, N, relu, accum)
}

type q8RangeTask struct {
	out, a, bias []float32
	b            *Q8Tensor
	M, N, K      int
	relu, accum  bool
	wg           sync.WaitGroup
}

func (t *q8RangeTask) runRange(ns, ne int) {
	matMulQ8Range(t.out, t.a, t.b, t.bias, t.M, t.N, t.K, ns, ne, t.relu, t.accum)
}

var q8RangeTaskPool = sync.Pool{New: func() any { return new(q8RangeTask) }}

func matMulQ8ParallelN_MTileAct(out, a []float32, b *Q8Tensor, bias []float32, M, N, K int, relu, accum bool) {
	nw := rangeWorkers(N, matMulWorkersFor(M, N, K))
	if nw <= 1 {
		matMulQ8Range(out, a, b, bias, M, N, K, 0, N, relu, accum)
		return
	}
	t := q8RangeTaskPool.Get().(*q8RangeTask)
	t.out, t.a, t.bias, t.b = out, a, bias, b
	t.M, t.N, t.K, t.relu, t.accum = M, N, K, relu, accum
	t.wg.Add(nw)
	chunk := (N + nw - 1) / nw
	ensureMatmulPool()
	for w := 0; w < nw; w++ {
		s := w * chunk
		e := s + chunk
		if e > N {
			e = N
		}
		if s >= e {
			t.wg.Done()
			continue
		}
		jobQueue <- matmulRangeJob{start: s, end: e, task: t, wg: &t.wg}
	}
	t.wg.Wait()
	t.out, t.a, t.bias, t.b = nil, nil, nil, nil
	q8RangeTaskPool.Put(t)
}

var q8DequantPool = sync.Pool{
	// Keep a pointer in the pool rather than a slice value. Passing a slice to
	// sync.Pool boxes its three-word header on every Put, which made the Q8
	// panel cache allocate in the SenseVoice inference hot path.
	New: func() interface{} { return new([]float32) },
}

func getQ8DequantBuf(n int) ([]float32, *[]float32) {
	pooled := q8DequantPool.Get().(*[]float32)
	buf := *pooled
	if cap(buf) < n {
		buf = make([]float32, n)
	}
	return buf[:n], pooled
}

func putQ8DequantBuf(buf []float32, pooled *[]float32) {
	*pooled = buf[:0]
	q8DequantPool.Put(pooled)
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

// dequantQ8RowDual dequantizes two B rows into dst0/dst1 (panel dual fill).
func dequantQ8RowDual(t *Q8Tensor, row0, row1 int, dst0, dst1 []float32) {
	nBlocks := t.Cols / q8BlockSize
	need := (row1 + 1) * nBlocks
	if len(t.Scales) >= need && nBlocks > 0 {
		dequantRowScaledDual(dst0, dst1, t.Data,
			&t.Scales[row0*nBlocks], &t.Scales[row1*nBlocks],
			row0*nBlocks*q8BlockBytes, row1*nBlocks*q8BlockBytes, nBlocks)
		return
	}
	dequantQ8Row(t, row0, dst0)
	dequantQ8Row(t, row1, dst1)
}

// dequantQ8RowTriple dequantizes three B rows for triple multiDot panels.
func dequantQ8RowTriple(t *Q8Tensor, row0, row1, row2 int, dst0, dst1, dst2 []float32) {
	nBlocks := t.Cols / q8BlockSize
	need := (row2 + 1) * nBlocks
	if len(t.Scales) >= need && nBlocks > 0 {
		dequantRowScaledTriple(dst0, dst1, dst2, t.Data,
			&t.Scales[row0*nBlocks], &t.Scales[row1*nBlocks], &t.Scales[row2*nBlocks],
			row0*nBlocks*q8BlockBytes, row1*nBlocks*q8BlockBytes, row2*nBlocks*q8BlockBytes, nBlocks)
		return
	}
	dequantQ8RowDual(t, row0, row1, dst0, dst1)
	dequantQ8Row(t, row2, dst2)
}

// dequantQ8Panel fills panel[0:nt*K] with dequant of B rows [n0, n0+nt).
// When preferTriple (K=512 multiDot), dequant 3 rows at a time for better bandwidth.
func dequantQ8Panel(b *Q8Tensor, n0, nt, K int, panel []float32, preferTriple bool) {
	t := 0
	if preferTriple {
		for ; t+2 < nt; t += 3 {
			dequantQ8RowTriple(b, n0+t, n0+t+1, n0+t+2,
				panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K], panel[(t+2)*K:(t+3)*K])
		}
	}
	for ; t+1 < nt; t += 2 {
		dequantQ8RowDual(b, n0+t, n0+t+1, panel[t*K:(t+1)*K], panel[(t+1)*K:(t+2)*K])
	}
	for ; t < nt; t++ {
		dequantQ8Row(b, n0+t, panel[t*K:(t+1)*K])
	}
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

// float16to32Fast is the common-case path for Q8 block scales (normals or ±0).
// Subnormals / Inf / NaN fall back to float16to32.
func float16to32Fast(h uint16) float32 {
	exp := (h >> 10) & 0x1f
	if exp == 0 || exp == 31 {
		return float16to32(h)
	}
	return math.Float32frombits(uint32(h&0x8000)<<16 | (uint32(exp)+112)<<23 | uint32(h&0x3ff)<<13)
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
