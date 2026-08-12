package onnxrt

import (
	"math"
	"runtime"
	"sync"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// scratchPool recycles temporary float32 buffers used by vectorized kernels.
var scratchPool = sync.Pool{
	New: func() interface{} { return make([]float32, 0, 1024) },
}

// Scratch reuse contract: callers fully overwrite the returned buffer before
// reading it (im2row writes explicit zeros for out-of-bounds taps, MatMul
// writes every output element, vek _Into ops write every element), so reused
// buffers are never zeroed.

// scratchBigMin is the element count above which buffers go to the
// GC-resilient free list instead of the sync.Pool.
const scratchBigMin = 1 << 16

// Caps on retained big scratch buffers: 48 buffers / 384 MB total. The det
// conv working set is ~12 workers x (16MB im2row panel + block output); the
// caps retain the hot panels without pinning unbounded memory.
const (
	scratchMaxBufs  = 48
	scratchMaxElems = 96 << 20
)

// scratchBig recycles large float32 buffers across GCs. The OCR pipeline
// allocates at GB/s rates, so sync.Pool entries are flushed between ops and
// every conv block would re-make (and re-zero) its multi-MB panels.
var scratchBig = struct {
	sync.Mutex
	bufs  [][]float32
	elems int
}{}

func getScratch(n int) []float32 {
	if n < scratchBigMin {
		b := scratchPool.Get().([]float32)
		if cap(b) < n {
			b = make([]float32, n)
		}
		return b[:n]
	}
	scratchBig.Lock()
	best := -1
	for i, b := range scratchBig.bufs {
		if cap(b) >= n && (best < 0 || cap(b) < cap(scratchBig.bufs[best])) {
			best = i
		}
	}
	if best >= 0 {
		b := scratchBig.bufs[best]
		scratchBig.bufs = append(scratchBig.bufs[:best], scratchBig.bufs[best+1:]...)
		scratchBig.elems -= cap(b)
		scratchBig.Unlock()
		return b[:n]
	}
	scratchBig.Unlock()
	return make([]float32, n)
}

func putScratch(b []float32) {
	if cap(b) < scratchBigMin {
		scratchPool.Put(b[:0])
		return
	}
	scratchBig.Lock()
	if len(scratchBig.bufs) < scratchMaxBufs && scratchBig.elems+cap(b) <= scratchMaxElems {
		scratchBig.bufs = append(scratchBig.bufs, b[:0])
		scratchBig.elems += cap(b)
	}
	scratchBig.Unlock()
}

// binOp identifies an elementwise binary operation.
type binOp int

const (
	bAdd binOp = iota
	bSub
	bMul
	bDiv
	bPow
)

// unOp identifies an elementwise unary operation.
type unOp int

const (
	uRelu unOp = iota
	uSigmoid
	uSqrt
	uErf
	uHardSigmoid
)

// parThreshold is the element count above which elementwise loops are split
// across the worker pool.
const parThreshold = 1 << 18

// parallelChunks splits data-length work over the pool when large.
func parallelChunks(n int, fn func(start, end int)) {
	if n < parThreshold {
		fn(0, n)
		return
	}
	xt.ParallelRanges(n, fn)
}

// parallelOuter splits [0, total) into contiguous ranges and runs them on
// plain goroutines rather than the shared matmul worker pool. Use this when
// fn itself submits nested work to that pool (MatMul, parallelChunks): a
// pool worker that submits nested jobs and then waits can deadlock the
// fixed-size pool once every worker is blocked (the nested jobs have no
// consumer left). Plain goroutines leave all pool workers free to drain the
// nested jobs.
func parallelOuter(total int, fn func(start, end int)) {
	// Match the runtime's active CPU budget. In fixed-core inference (and
	// GOMAXPROCS-constrained services), spawning one outer goroutine per
	// physical core only adds scheduling overhead around nested MatMul work.
	nw := runtime.GOMAXPROCS(0)
	if nw > 12 {
		nw = 12 // mirror the pool's worker cap
	}
	if nw > total {
		nw = total
	}
	if nw <= 1 {
		fn(0, total)
		return
	}
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
		go func(s, e int) {
			defer wg.Done()
			fn(s, e)
		}(s, e)
	}
	wg.Wait()
}

// softmaxRowVec computes dst = softmax(src) for one contiguous row using
// vectorized primitives. The exponent is clamped at -80 (exp(-80) ≈ 2e-35,
// far below float32 probability resolution after normalization) because
// vek32's Exp approximation breaks on subnormal outputs.
func softmaxRowVec(dst, src []float32) {
	n := len(src)
	if n == 0 {
		return
	}
	e := getScratch(n)
	vek32.SubNumber_Into(e, src, vek32.Max(src))
	vek32.MaximumNumber_Inplace(e, -80)
	vek32.Exp_Inplace(e)
	var sum float64
	for _, v := range e {
		sum += float64(v)
	}
	vek32.MulNumber_Into(dst, e, float32(1.0/sum))
	putScratch(e)
}

// binaryVec computes out[i] = a[i] <op> b[i] over equal-length slices.
func binaryVec(code binOp, out, a, b []float32) {
	switch code {
	case bAdd:
		parallelChunks(len(out), func(s, e int) { vek32.Add_Into(out[s:e], a[s:e], b[s:e]) })
	case bSub:
		parallelChunks(len(out), func(s, e int) { vek32.Sub_Into(out[s:e], a[s:e], b[s:e]) })
	case bMul:
		parallelChunks(len(out), func(s, e int) { vek32.Mul_Into(out[s:e], a[s:e], b[s:e]) })
	case bDiv:
		parallelChunks(len(out), func(s, e int) { vek32.Div_Into(out[s:e], a[s:e], b[s:e]) })
	case bPow:
		for i := range out {
			out[i] = float32(math.Pow(float64(a[i]), float64(b[i])))
		}
	}
}

// binaryScalar computes out[i] = a[i] <op> s (scalarLeft=false) or
// out[i] = s <op> a[i] (scalarLeft=true).
func binaryScalar(code binOp, out, a []float32, s float32, scalarLeft bool) {
	if !scalarLeft {
		switch code {
		case bAdd:
			if len(out) < parThreshold {
				vek32.AddNumber_Into(out, a, s)
				return
			}
			parallelChunks(len(out), func(st, e int) { vek32.AddNumber_Into(out[st:e], a[st:e], s) })
		case bSub:
			if len(out) < parThreshold {
				vek32.SubNumber_Into(out, a, s)
				return
			}
			parallelChunks(len(out), func(st, e int) { vek32.SubNumber_Into(out[st:e], a[st:e], s) })
		case bMul:
			if len(out) < parThreshold {
				vek32.MulNumber_Into(out, a, s)
				return
			}
			parallelChunks(len(out), func(st, e int) { vek32.MulNumber_Into(out[st:e], a[st:e], s) })
		case bDiv:
			if len(out) < parThreshold {
				vek32.DivNumber_Into(out, a, s)
				return
			}
			parallelChunks(len(out), func(st, e int) { vek32.DivNumber_Into(out[st:e], a[st:e], s) })
		case bPow:
			switch s {
			case 1:
				copy(out, a)
			case 2:
				parallelChunks(len(out), func(st, e int) { vek32.Mul_Into(out[st:e], a[st:e], a[st:e]) })
			case 0.5:
				parallelChunks(len(out), func(st, e int) { vek32.Sqrt_Into(out[st:e], a[st:e]) })
			case -1:
				parallelChunks(len(out), func(st, e int) { vek32.Inv_Into(out[st:e], a[st:e]) })
			default:
				for i := range out {
					out[i] = float32(math.Pow(float64(a[i]), float64(s)))
				}
			}
		}
		return
	}
	// scalar on the left
	switch code {
	case bAdd:
		binaryScalar(bAdd, out, a, s, false)
	case bMul:
		binaryScalar(bMul, out, a, s, false)
	case bSub: // s - a
		parallelChunks(len(out), func(st, e int) {
			vek32.Neg_Into(out[st:e], a[st:e])
			vek32.AddNumber_Inplace(out[st:e], s)
		})
	case bDiv: // s / a
		parallelChunks(len(out), func(st, e int) {
			vek32.Inv_Into(out[st:e], a[st:e])
			vek32.MulNumber_Inplace(out[st:e], s)
		})
	case bPow:
		for i := range out {
			out[i] = float32(math.Pow(float64(s), float64(a[i])))
		}
	}
}

// binaryOpTensor is the SIMD replacement for closure-based binaryFloat.
func binaryOpTensor(rc *runCtx, n *Node, a, b *Tensor, code binOp) (*Tensor, error) {
	outShape, err := broadcastShapes(a.Shape, b.Shape)
	if err != nil {
		return nil, err
	}
	af, err := a.Floats()
	if err != nil {
		return nil, err
	}
	bf, err := b.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, outShape...)
	switch {
	case shapeEqual(a.Shape, outShape) && shapeEqual(b.Shape, outShape):
		binaryVec(code, out.F32, af, bf)
	case shapeEqual(a.Shape, outShape) && b.NumElements() == 1:
		binaryScalar(code, out.F32, af, bf[0], false)
	case shapeEqual(b.Shape, outShape) && a.NumElements() == 1:
		binaryScalar(code, out.F32, bf, af[0], true)
	default:
		broadcastRows(code, out.F32, af, bf, a.Shape, b.Shape, outShape)
	}
	return out, nil
}

// broadcastRows handles general broadcasting by decomposing into
// contiguous last-dim rows, each processed with vector ops.
// Last-dim broadcast strides are always 0 or 1 for contiguous tensors.
func broadcastRows(code binOp, out, af, bf []float32, aShape, bShape, outShape []int) {
	nd := len(outShape)
	if nd == 0 {
		switch code {
		case bAdd:
			out[0] = af[0] + bf[0]
		case bSub:
			out[0] = af[0] - bf[0]
		case bMul:
			out[0] = af[0] * bf[0]
		case bDiv:
			out[0] = af[0] / bf[0]
		case bPow:
			out[0] = float32(math.Pow(float64(af[0]), float64(bf[0])))
		}
		return
	}
	sa := broadcastStrides(aShape, outShape)
	sb := broadcastStrides(bShape, outShape)
	last := nd - 1
	L := outShape[last]
	saL, sbL := sa[last], sb[last]
	if saL > 1 || sbL > 1 {
		// cannot happen for contiguous tensors; defensive fallback
		broadcastLoop(outShape, sa, sb, func(ai, bi, oi int) {
			switch code {
			case bAdd:
				out[oi] = af[ai] + bf[bi]
			case bSub:
				out[oi] = af[ai] - bf[bi]
			case bMul:
				out[oi] = af[ai] * bf[bi]
			case bDiv:
				out[oi] = af[ai] / bf[bi]
			case bPow:
				out[oi] = float32(math.Pow(float64(af[ai]), float64(bf[bi])))
			}
		})
		return
	}
	rows := numElements(outShape[:last])
	outerShape := outShape[:last]

	// rowWorker processes rows [r0, r1). Keep the common single-worker path
	// allocation-free: the former closure allocated an index slice for every
	// broadcast op, which is especially costly across PP-OCR's many affine
	// Mul/Add layers. Only the parallel path needs an arbitrary start index.
	rowWorker := func(r0, r1 int) {
		idx := make([]int, len(outerShape))
		// initialize multi-index and offsets for row r0
		aOff, bOff := 0, 0
		rem := r0
		for d := len(outerShape) - 1; d >= 0; d-- {
			idx[d] = rem % outerShape[d]
			rem /= outerShape[d]
		}
		for d := range outerShape {
			aOff += idx[d] * sa[d]
			bOff += idx[d] * sb[d]
		}
		broadcastRowsRange(code, out, af, bf, outerShape, sa, sb, L, r0, r1, idx, aOff, bOff)
	}
	if rows >= 4 && rows*L >= parThreshold {
		// rowWorker calls binaryVec/parallelChunks (pool): use plain
		// goroutines for the outer level to avoid nested pool submission.
		parallelOuter(rows, rowWorker)
		return
	}
	// With r0=0, all broadcast offsets start at zero. Avoid constructing the
	// per-dimension index vector entirely; advance offsets via a mixed-radix
	// counter represented by the row number instead.
	broadcastRowsSerial(code, out, af, bf, outerShape, sa, sb, L, rows)
}

func broadcastRowsRange(code binOp, out, af, bf []float32, outerShape, sa, sb []int, L, r0, r1 int, idx []int, aOff, bOff int) {
	for r := r0; r < r1; r++ {
		dst := out[r*L : (r+1)*L]
		switch {
		case sa[len(sa)-1] == 1 && sb[len(sb)-1] == 1:
			binaryVec(code, dst, af[aOff:aOff+L], bf[bOff:bOff+L])
		case sb[len(sb)-1] == 0:
			binaryScalar(code, dst, af[aOff:aOff+L], bf[bOff], false)
		default: // saL == 0
			binaryScalar(code, dst, bf[bOff:bOff+L], af[aOff], true)
		}
		// advance multi-index
		for d := len(outerShape) - 1; d >= 0; d-- {
			idx[d]++
			aOff += sa[d]
			bOff += sb[d]
			if idx[d] < outerShape[d] {
				break
			}
			idx[d] = 0
			aOff -= sa[d] * outerShape[d]
			bOff -= sb[d] * outerShape[d]
		}
	}
}

// broadcastRowsSerial is the allocation-free row walk used for normal
// single-core inference. PP-OCR's broadcast tensors have at most four outer
// dimensions; retain their mixed-radix index in a small stack array so each
// row advances with adds rather than recomputing coordinates via division.
func broadcastRowsSerial(code binOp, out, af, bf []float32, outerShape, sa, sb []int, L, rows int) {
	saL, sbL := sa[len(sa)-1], sb[len(sb)-1]
	if len(outerShape) > 8 {
		// Unusual high-rank tensors retain the general allocation-free math
		// fallback. Models in the OCR path take the fast counter route below.
		for r := 0; r < rows; r++ {
			aOff, bOff := 0, 0
			rem := r
			for d := len(outerShape) - 1; d >= 0; d-- {
				v := rem % outerShape[d]
				rem /= outerShape[d]
				aOff += v * sa[d]
				bOff += v * sb[d]
			}
			broadcastRow(code, out[r*L:(r+1)*L], af, bf, aOff, bOff, L, saL, sbL)
		}
		return
	}
	var idx [8]int
	aOff, bOff := 0, 0
	for r := 0; r < rows; r++ {
		broadcastRow(code, out[r*L:(r+1)*L], af, bf, aOff, bOff, L, saL, sbL)
		for d := len(outerShape) - 1; d >= 0; d-- {
			idx[d]++
			aOff += sa[d]
			bOff += sb[d]
			if idx[d] < outerShape[d] {
				break
			}
			idx[d] = 0
			aOff -= sa[d] * outerShape[d]
			bOff -= sb[d] * outerShape[d]
		}
	}
}

func broadcastRow(code binOp, dst, af, bf []float32, aOff, bOff, L, saL, sbL int) {
	switch {
	case saL == 1 && sbL == 1:
		binaryVec(code, dst, af[aOff:aOff+L], bf[bOff:bOff+L])
	case sbL == 0:
		binaryScalar(code, dst, af[aOff:aOff+L], bf[bOff], false)
	default:
		binaryScalar(code, dst, bf[bOff:bOff+L], af[aOff], true)
	}
}

// unaryOpTensor applies a SIMD unary op.
func unaryOpTensor(rc *runCtx, n *Node, t *Tensor, code unOp, p0, p1 float32) (*Tensor, error) {
	data, err := t.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, t.Shape...)
	dst := out.F32
	switch code {
	case uRelu:
		parallelChunks(len(dst), func(s, e int) { vek32.MaximumNumber_Into(dst[s:e], data[s:e], 0) })
	case uSigmoid:
		parallelChunks(len(dst), func(s, e int) {
			vek32.Neg_Into(dst[s:e], data[s:e])
			// clamp exp argument: sigmoid saturates to 0/1 far earlier;
			// vek32's Exp breaks on subnormal/overflowing outputs.
			vek32.MaximumNumber_Inplace(dst[s:e], -80)
			vek32.MinimumNumber_Inplace(dst[s:e], 80)
			vek32.Exp_Inplace(dst[s:e])
			vek32.AddNumber_Inplace(dst[s:e], 1)
			vek32.Inv_Inplace(dst[s:e])
		})
	case uSqrt:
		parallelChunks(len(dst), func(s, e int) { vek32.Sqrt_Into(dst[s:e], data[s:e]) })
	case uErf:
		parallelChunks(len(dst), func(s, e int) { erf32Into(dst[s:e], data[s:e]) })
	case uHardSigmoid:
		alpha, beta := p0, p1
		parallelChunks(len(dst), func(s, e int) {
			vek32.MulNumber_Into(dst[s:e], data[s:e], alpha)
			vek32.AddNumber_Inplace(dst[s:e], beta)
			vek32.MaximumNumber_Inplace(dst[s:e], 0)
			vek32.MinimumNumber_Inplace(dst[s:e], 1)
		})
	}
	return out, nil
}
