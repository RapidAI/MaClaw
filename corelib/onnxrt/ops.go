package onnxrt

import (
	"fmt"
	"math"
	"sort"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// transpose2D transposes a [rows, cols] row-major matrix.
func transpose2D(src []float32, rows, cols int) []float32 {
	out := make([]float32, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			out[c*rows+r] = src[r*cols+c]
		}
	}
	return out
}

// batchOffset maps a linear batch index to a source batch offset using
// broadcast strides (strides are in elements of the batch-index space).
func batchOffset(bi int, batchShape, strides []int) int {
	nd := len(batchShape)
	if nd == 0 {
		return 0
	}
	off := 0
	for d := nd - 1; d >= 0; d-- {
		idx := bi % batchShape[d]
		bi /= batchShape[d]
		off += idx * strides[d]
	}
	return off
}

// opFunc computes the output tensors of one node. args has one entry per
// node input; omitted optional inputs are nil. The runCtx carries per-Run
// state (output arena); it may be nil in direct kernel calls (tests, load
// time constant folding), in which case outputs come from the GC heap.
type opFunc func(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error)

// opRegistry maps ONNX op_type to its kernel.
var opRegistry = map[string]opFunc{
	"Identity":           opIdentity,
	"Add":                opAdd,
	"Sub":                opSub,
	"Mul":                opMul,
	"Div":                opDiv,
	"Pow":                opPow,
	"Relu":               opRelu,
	"Sigmoid":            opSigmoid,
	"Sqrt":               opSqrt,
	"Erf":                opErf,
	"HardSigmoid":        opHardSigmoid,
	"Shape":              opShape,
	"Slice":              opSlice,
	"Squeeze":            opSqueeze,
	"Unsqueeze":          opUnsqueeze,
	"Reshape":            opReshape,
	"Transpose":          opTranspose,
	"Concat":             opConcat,
	"MatMul":             opMatMul,
	"Softmax":            opSoftmax,
	"BatchNormalization": opBatchNorm,
	"ReduceMean":         opReduceMean,
	"Conv":               opConv,
	"ConvTranspose":      opConvTranspose,
	"MaxPool":            opMaxPool,
	"AveragePool":        opAveragePool,
	"GlobalAveragePool":  opGlobalAveragePool,
	"Resize":             opResize,
}

// --- attribute helpers ---

func attrInt(n *Node, name string, def int64) int64 {
	if a, ok := n.Attrs[name]; ok {
		return a.Int()
	}
	return def
}

func attrInts(n *Node, name string) []int64 {
	if a, ok := n.Attrs[name]; ok {
		return a.Ints()
	}
	return nil
}

func attrFloat(n *Node, name string, def float32) float32 {
	if a, ok := n.Attrs[name]; ok {
		return a.Float()
	}
	return def
}

func attrStr(n *Node, name, def string) string {
	if a, ok := n.Attrs[name]; ok {
		return a.Str()
	}
	return def
}

// intsOrDefault copies attr ints or returns def.
func intsOrDefault(v []int64, def ...int64) []int64 {
	if v == nil {
		return def
	}
	return v
}

// normalizeAxis converts a possibly-negative axis to [0, rank).
func normalizeAxis(axis int64, rank int) (int, error) {
	a := int(axis)
	if a < 0 {
		a += rank
	}
	if a < 0 || a >= rank {
		return 0, fmt.Errorf("axis %d out of range for rank %d", axis, rank)
	}
	return a, nil
}

// tensorIntsArg reads an int64/int32/float tensor argument as []int64.
func tensorIntsArg(t *Tensor) ([]int64, error) {
	if t == nil {
		return nil, fmt.Errorf("missing tensor argument")
	}
	return t.Ints()
}

func requireArgs(n *Node, args []*Tensor, min int) error {
	got := 0
	for _, a := range args {
		if a != nil {
			got++
		}
	}
	if got < min {
		return fmt.Errorf("expected at least %d inputs, got %d", min, got)
	}
	return nil
}

// --- Identity / elementwise ---

func opIdentity(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	return []*Tensor{args[0]}, nil // alias is safe: tensors are immutable after production
}

// binaryDispatch routes to int64 arithmetic when both operands are int64,
// otherwise promotes to float32 and uses the SIMD kernels.
func binaryDispatch(rc *runCtx, n *Node, args []*Tensor, code binOp, iop func(x, y int64) int64) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	if a.DType == DInt64 && b.DType == DInt64 && iop != nil {
		out, err := binaryInt(a, b, iop)
		if err != nil {
			return nil, err
		}
		return []*Tensor{out}, nil
	}
	out, err := binaryOpTensor(rc, n, a, b, code)
	if err != nil {
		return nil, err
	}
	return []*Tensor{out}, nil
}

func opAdd(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return binaryDispatch(rc, n, args, bAdd, func(x, y int64) int64 { return x + y })
}

func opSub(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return binaryDispatch(rc, n, args, bSub, func(x, y int64) int64 { return x - y })
}

func opMul(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return binaryDispatch(rc, n, args, bMul, func(x, y int64) int64 { return x * y })
}

func opDiv(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return binaryDispatch(rc, n, args, bDiv, func(x, y int64) int64 {
		if y == 0 {
			return 0
		}
		return x / y
	})
}

func opPow(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return binaryDispatch(rc, n, args, bPow, func(x, y int64) int64 {
		return int64(math.Pow(float64(x), float64(y)))
	})
}

// unaryDispatch wraps unaryOpTensor for the registry signature.
func unaryDispatch(rc *runCtx, n *Node, args []*Tensor, code unOp, p0, p1 float32) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	out, err := unaryOpTensor(rc, n, args[0], code, p0, p1)
	if err != nil {
		return nil, err
	}
	return []*Tensor{out}, nil
}

func opRelu(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return unaryDispatch(rc, n, args, uRelu, 0, 0)
}

func opSigmoid(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return unaryDispatch(rc, n, args, uSigmoid, 0, 0)
}

func opSqrt(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return unaryDispatch(rc, n, args, uSqrt, 0, 0)
}

func opErf(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return unaryDispatch(rc, n, args, uErf, 0, 0)
}

func opHardSigmoid(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	alpha := attrFloat(n, "alpha", 0.2)
	beta := attrFloat(n, "beta", 0.5)
	return unaryDispatch(rc, n, args, uHardSigmoid, alpha, beta)
}

// --- shape manipulation ---

func opShape(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	out := NewInt(len(args[0].Shape))
	for i, d := range args[0].Shape {
		out.I64[i] = int64(d)
	}
	return []*Tensor{out}, nil
}

func opSlice(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 3); err != nil {
		return nil, err
	}
	data := args[0]
	starts, err := tensorIntsArg(args[1])
	if err != nil {
		return nil, fmt.Errorf("starts: %w", err)
	}
	ends, err := tensorIntsArg(args[2])
	if err != nil {
		return nil, fmt.Errorf("ends: %w", err)
	}
	if len(starts) != len(ends) {
		return nil, fmt.Errorf("starts len %d != ends len %d", len(starts), len(ends))
	}
	rank := data.Rank()
	axes := make([]int, len(starts))
	if len(args) > 3 && args[3] != nil {
		ax, err := tensorIntsArg(args[3])
		if err != nil {
			return nil, fmt.Errorf("axes: %w", err)
		}
		if len(ax) != len(starts) {
			return nil, fmt.Errorf("axes len %d != starts len %d", len(ax), len(starts))
		}
		for i, a := range ax {
			na, err := normalizeAxis(a, rank)
			if err != nil {
				return nil, err
			}
			axes[i] = na
		}
	} else {
		for i := range axes {
			axes[i] = i
		}
	}
	steps := make([]int64, len(starts))
	for i := range steps {
		steps[i] = 1
	}
	if len(args) > 4 && args[4] != nil {
		st, err := tensorIntsArg(args[4])
		if err != nil {
			return nil, fmt.Errorf("steps: %w", err)
		}
		if len(st) != len(starts) {
			return nil, fmt.Errorf("steps len %d != starts len %d", len(st), len(starts))
		}
		copy(steps, st)
	}

	// Start with a full-range selection on every axis.
	selStart := make([]int, rank)
	selStep := make([]int, rank)
	selLen := make([]int, rank)
	for d := 0; d < rank; d++ {
		selStep[d] = 1
		selLen[d] = data.Shape[d]
	}
	for i := range starts {
		ax := axes[i]
		dim := data.Shape[ax]
		step := steps[i]
		if step == 0 {
			return nil, fmt.Errorf("slice step 0")
		}
		s, e := starts[i], ends[i]
		if step > 0 {
			if s < 0 {
				s += int64(dim)
			}
			if s < 0 {
				s = 0
			}
			if s > int64(dim) {
				s = int64(dim)
			}
			if e < 0 {
				e += int64(dim)
			}
			if e < 0 {
				e = 0
			}
			if e > int64(dim) {
				e = int64(dim)
			}
			selStart[ax] = int(s)
			selStep[ax] = int(step)
			selLen[ax] = 0
			if e > s {
				selLen[ax] = int((e - s + step - 1) / step)
			}
		} else {
			// negative step: default start is dim-1, default end is -dim-1
			if s >= int64(dim) {
				s = int64(dim) - 1
			}
			if s < 0 {
				s += int64(dim)
			}
			if s < 0 {
				s = -1 // empty
			}
			if e >= int64(dim) {
				e = int64(dim) - 1
			}
			if e < 0 {
				e += int64(dim)
			}
			if e < -1 {
				e = -1
			}
			selStart[ax] = int(s)
			selStep[ax] = int(step)
			selLen[ax] = 0
			if s > e && s >= 0 {
				selLen[ax] = int((s - e - step - 1) / (-step))
			}
		}
	}

	outShape := make([]int, rank)
	copy(outShape, selLen)
	var out *Tensor
	if data.DType == DInt64 {
		out = NewInt(outShape...)
	} else {
		out = rc.newFloat(n, 0, outShape...)
	}
	strides := make([]int, rank)
	acc := 1
	for d := rank - 1; d >= 0; d-- {
		strides[d] = acc
		acc *= data.Shape[d]
	}
	idx := make([]int, rank)
	srcOff := make([]int, rank)
	for d := 0; d < rank; d++ {
		srcOff[d] = selStart[d] * strides[d]
	}
	nOut := out.NumElements()
	for oi := 0; oi < nOut; oi++ {
		off := 0
		for d := 0; d < rank; d++ {
			off += srcOff[d]
		}
		if data.DType == DInt64 {
			out.I64[oi] = data.I64[off]
		} else {
			out.F32[oi] = data.F32[off]
		}
		for d := rank - 1; d >= 0; d-- {
			idx[d]++
			srcOff[d] += selStep[d] * strides[d]
			if idx[d] < selLen[d] {
				break
			}
			idx[d] = 0
			srcOff[d] -= selLen[d] * selStep[d] * strides[d]
		}
	}
	return []*Tensor{out}, nil
}

// squeezeAxes reads axes from attr (opset <= 12) or second input (opset 13+).
func squeezeAxes(rc *runCtx, n *Node, args []*Tensor) ([]int64, bool, error) {
	if ax := attrInts(n, "axes"); ax != nil {
		return ax, true, nil
	}
	if len(args) > 1 && args[1] != nil {
		ax, err := tensorIntsArg(args[1])
		return ax, true, err
	}
	return nil, false, nil
}

func opSqueeze(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	data := args[0]
	axes, hasAxes, err := squeezeAxes(rc, n, args)
	if err != nil {
		return nil, err
	}
	rank := data.Rank()
	drop := make([]bool, rank)
	if hasAxes {
		for _, a := range axes {
			na, err := normalizeAxis(a, rank)
			if err != nil {
				return nil, err
			}
			if data.Shape[na] != 1 {
				return nil, fmt.Errorf("squeeze: axis %d has size %d", na, data.Shape[na])
			}
			drop[na] = true
		}
	} else {
		for d := 0; d < rank; d++ {
			if data.Shape[d] == 1 {
				drop[d] = true
			}
		}
	}
	var outShape []int
	for d := 0; d < rank; d++ {
		if !drop[d] {
			outShape = append(outShape, data.Shape[d])
		}
	}
	return []*Tensor{data.Reshape(outShape...)}, nil
}

func opUnsqueeze(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	data := args[0]
	axes, hasAxes, err := squeezeAxes(rc, n, args)
	if err != nil {
		return nil, err
	}
	if !hasAxes {
		return nil, fmt.Errorf("unsqueeze: no axes")
	}
	rank := data.Rank()
	outRank := rank + len(axes)
	norm := make([]int, len(axes))
	for i, a := range axes {
		na := int(a)
		if na < 0 {
			na += outRank
		}
		if na < 0 || na >= outRank {
			return nil, fmt.Errorf("unsqueeze: axis %d out of range", a)
		}
		norm[i] = na
	}
	sort.Ints(norm)
	outShape := make([]int, 0, outRank)
	src := 0
	for d := 0; d < outRank; d++ {
		if len(norm) > 0 && norm[0] == d {
			outShape = append(outShape, 1)
			norm = norm[1:]
		} else {
			outShape = append(outShape, data.Shape[src])
			src++
		}
	}
	return []*Tensor{data.Reshape(outShape...)}, nil
}

func opReshape(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data := args[0]
	shapeVals, err := tensorIntsArg(args[1])
	if err != nil {
		return nil, fmt.Errorf("shape: %w", err)
	}
	allowZero := attrInt(n, "allowzero", 0) != 0
	outShape := make([]int, len(shapeVals))
	infer := -1
	known := 1
	for i, v := range shapeVals {
		switch {
		case v == -1:
			if infer >= 0 {
				return nil, fmt.Errorf("reshape: multiple -1 dims")
			}
			infer = i
			outShape[i] = 1
		case v == 0 && !allowZero:
			if i >= data.Rank() {
				return nil, fmt.Errorf("reshape: 0 dim %d out of input rank %d", i, data.Rank())
			}
			outShape[i] = data.Shape[i]
			known *= outShape[i]
		case v < 0:
			return nil, fmt.Errorf("reshape: invalid dim %d", v)
		default:
			outShape[i] = int(v)
			known *= outShape[i]
		}
	}
	total := data.NumElements()
	if infer >= 0 {
		if known == 0 || total%known != 0 {
			return nil, fmt.Errorf("reshape: cannot infer dim, total %d known %d", total, known)
		}
		outShape[infer] = total / known
	} else if known != total {
		return nil, fmt.Errorf("reshape: shape %v holds %d elements, input has %d", outShape, known, total)
	}
	return []*Tensor{data.Reshape(outShape...)}, nil
}

func opTranspose(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	data := args[0]
	rank := data.Rank()
	perm := attrInts(n, "perm")
	if perm == nil {
		perm = make([]int64, rank)
		for i := range perm {
			perm[i] = int64(rank - 1 - i)
		}
	}
	if len(perm) != rank {
		return nil, fmt.Errorf("transpose: perm len %d != rank %d", len(perm), rank)
	}
	p := make([]int, rank)
	seen := make([]bool, rank)
	for i, v := range perm {
		a, err := normalizeAxis(v, rank)
		if err != nil {
			return nil, err
		}
		if seen[a] {
			return nil, fmt.Errorf("transpose: duplicate perm axis %d", a)
		}
		seen[a] = true
		p[i] = a
	}
	outShape := make([]int, rank)
	for i := range outShape {
		outShape[i] = data.Shape[p[i]]
	}
	srcStrides := make([]int, rank)
	acc := 1
	for d := rank - 1; d >= 0; d-- {
		srcStrides[d] = acc
		acc *= data.Shape[d]
	}
	// out index d maps to src dim p[d]
	outStridesToSrc := make([]int, rank)
	for d := 0; d < rank; d++ {
		outStridesToSrc[d] = srcStrides[p[d]]
	}
	var out *Tensor
	if data.DType == DInt64 {
		out = NewInt(outShape...)
	} else {
		out = rc.newFloat(n, 0, outShape...)
	}
	nOut := out.NumElements()
	idx := make([]int, rank)
	off := 0
	for oi := 0; oi < nOut; oi++ {
		if data.DType == DInt64 {
			out.I64[oi] = data.I64[off]
		} else {
			out.F32[oi] = data.F32[off]
		}
		for d := rank - 1; d >= 0; d-- {
			idx[d]++
			off += outStridesToSrc[d]
			if idx[d] < outShape[d] {
				break
			}
			idx[d] = 0
			off -= outStridesToSrc[d] * outShape[d]
		}
	}
	return []*Tensor{out}, nil
}

func opConcat(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	var ins []*Tensor
	for _, a := range args {
		if a != nil {
			ins = append(ins, a)
		}
	}
	rank := ins[0].Rank()
	axis, err := normalizeAxis(attrInt(n, "axis", 0), rank)
	if err != nil {
		return nil, err
	}
	dt := ins[0].DType
	outShape := cloneInts(ins[0].Shape)
	for i, t := range ins {
		if t.DType != dt {
			return nil, fmt.Errorf("concat: mixed dtypes %v and %v", dt, t.DType)
		}
		if t.Rank() != rank {
			return nil, fmt.Errorf("concat: rank mismatch")
		}
		for d := 0; d < rank; d++ {
			if d == axis {
				if i > 0 {
					outShape[axis] += t.Shape[axis]
				}
			} else if t.Shape[d] != ins[0].Shape[d] {
				return nil, fmt.Errorf("concat: dim %d mismatch (%d vs %d)", d, t.Shape[d], ins[0].Shape[d])
			}
		}
	}
	// generic copy via outer/inner decomposition
	outer := numElements(ins[0].Shape[:axis])
	inner := numElements(ins[0].Shape[axis+1:])
	var out *Tensor
	if dt == DInt64 {
		out = NewInt(outShape...)
	} else {
		out = rc.newFloat(n, 0, outShape...)
	}
	for o := 0; o < outer; o++ {
		dstOff := o * outShape[axis] * inner
		for _, t := range ins {
			axisN := t.Shape[axis]
			srcOff := o * axisN * inner
			cnt := axisN * inner
			if dt == DInt64 {
				copy(out.I64[dstOff:dstOff+cnt], t.I64[srcOff:srcOff+cnt])
			} else {
				copy(out.F32[dstOff:dstOff+cnt], t.F32[srcOff:srcOff+cnt])
			}
			dstOff += cnt
		}
	}
	return []*Tensor{out}, nil
}

func opMatMul(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	af, err := a.Floats()
	if err != nil {
		return nil, err
	}
	bf, err := b.Floats()
	if err != nil {
		return nil, err
	}
	if a.Rank() == 1 && b.Rank() == 1 {
		if a.Shape[0] != b.Shape[0] {
			return nil, errShapeMismatch
		}
		var s float32
		for i := range af {
			s += af[i] * bf[i]
		}
		return []*Tensor{FloatFrom([]float32{s})}, nil
	}
	// Promote 1D: a [K] -> [1,K]; b [K] -> [K,1]; dims removed afterwards.
	aShape, bShape := a.Shape, b.Shape
	dropA, dropB := false, false
	if len(aShape) == 1 {
		aShape = []int{1, aShape[0]}
		dropA = true
	}
	if len(bShape) == 1 {
		bShape = []int{bShape[0], 1}
		dropB = true
	}
	M, K := aShape[len(aShape)-2], aShape[len(aShape)-1]
	K2, N := bShape[len(bShape)-2], bShape[len(bShape)-1]
	if K != K2 {
		return nil, fmt.Errorf("matmul: inner dims %d vs %d", K, K2)
	}
	aBatch := aShape[:len(aShape)-2]
	bBatch := bShape[:len(bShape)-2]
	batch, err := broadcastShapes(aBatch, bBatch)
	if err != nil {
		return nil, err
	}
	aStrides := broadcastStrides(aBatch, batch)
	bStrides := broadcastStrides(bBatch, batch)
	outShape := append(cloneInts(batch), M, N)
	out := rc.newFloat(n, 0, outShape...)
	nBatch := numElements(batch)
	singleB := numElements(bBatch) == 1
	var bT []float32
	if singleB {
		// Constant B (initializer) was pre-transposed at graph load.
		if g := rc.graf(); g != nil && b.Rank() == 2 && g.initializers[n.Inputs[1]] == b {
			bT = g.matMulBT[n.Inputs[1]]
		}
		if bT == nil {
			bT = transpose2D(bf, K, N)
		}
	}
	for bi := 0; bi < nBatch; bi++ {
		aOff := batchOffset(bi, batch, aStrides) * M * K
		var bts []float32
		if singleB {
			bts = bT
		} else {
			bOff := batchOffset(bi, batch, bStrides) * K * N
			bts = transpose2D(bf[bOff:bOff+K*N], K, N)
		}
		xt.MatMul(out.F32[bi*M*N:(bi+1)*M*N], af[aOff:aOff+M*K], bts, M, N, K)
	}
	if dropA {
		outShape = append(cloneInts(batch), N)
		out = out.Reshape(outShape...)
	} else if dropB {
		outShape = append(cloneInts(batch), M)
		out = out.Reshape(outShape...)
	}
	return []*Tensor{out}, nil
}

// opSoftmax implements opset-11 semantics: the input is coerced to 2D
// [prod(shape[:axis]), prod(shape[axis:])], softmax applied per row.
// For axis == rank-1 this equals the opset-13 last-axis behavior.
func opSoftmax(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	// Default axis: 1 for opset <= 12, -1 for opset 13+ (spec change).
	g := rc.graf()
	defAxis := int64(1)
	if g != nil && g.Opset() >= 13 {
		defAxis = -1
	}
	axis := attrInt(n, "axis", defAxis)
	ax, err := normalizeAxis(axis, x.Rank())
	if err != nil {
		return nil, err
	}
	src, err := x.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, x.Shape...)
	if g != nil && g.Opset() >= 13 {
		// Opset 13+: softmax along the single axis, independently for every
		// (outer, inner) slab.
		dim := x.Shape[ax]
		inner := numElements(x.Shape[ax+1:])
		outer := numElements(x.Shape[:ax])
		if inner == 1 {
			// Contiguous slabs (e.g. the CTC vocab softmax): vectorized path.
			slabFn := func(o0, o1 int) {
				for o := o0; o < o1; o++ {
					softmaxRowVec(out.F32[o*dim:(o+1)*dim], src[o*dim:(o+1)*dim])
				}
			}
			if outer >= 4 && outer*dim >= parThreshold {
				xt.ParallelRanges(outer, slabFn)
			} else {
				slabFn(0, outer)
			}
			return []*Tensor{out}, nil
		}
		for o := 0; o < outer; o++ {
			for i := 0; i < inner; i++ {
				base := o*dim*inner + i
				m := src[base]
				for d := 1; d < dim; d++ {
					if v := src[base+d*inner]; v > m {
						m = v
					}
				}
				var sum float64
				for d := 0; d < dim; d++ {
					e := math.Exp(float64(src[base+d*inner] - m))
					out.F32[base+d*inner] = float32(e)
					sum += e
				}
				inv := float32(1.0 / sum)
				for d := 0; d < dim; d++ {
					out.F32[base+d*inner] *= inv
				}
			}
		}
		return []*Tensor{out}, nil
	}
	// Opset <= 12: flatten into [prod(shape[:axis]), prod(shape[axis:])] and
	// softmax each row. Rows are contiguous: use the vectorized row kernel
	// (parallel over rows when large).
	rows := numElements(x.Shape[:ax])
	cols := numElements(x.Shape[ax:])
	rowFn := func(r0, r1 int) {
		for r := r0; r < r1; r++ {
			softmaxRowVec(out.F32[r*cols:(r+1)*cols], src[r*cols:(r+1)*cols])
		}
	}
	if rows >= 4 && rows*cols >= parThreshold {
		xt.ParallelRanges(rows, rowFn)
	} else {
		rowFn(0, rows)
	}
	return []*Tensor{out}, nil
}

func opBatchNorm(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 5); err != nil {
		return nil, err
	}
	x, scale, bias, mean, varT := args[0], args[1], args[2], args[3], args[4]
	eps := float64(attrFloat(n, "epsilon", 1e-5))
	if x.Rank() < 2 {
		return nil, fmt.Errorf("batchnorm: rank %d < 2", x.Rank())
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	sf, err := scale.Floats()
	if err != nil {
		return nil, err
	}
	bf, err := bias.Floats()
	if err != nil {
		return nil, err
	}
	mf, err := mean.Floats()
	if err != nil {
		return nil, err
	}
	vf, err := varT.Floats()
	if err != nil {
		return nil, err
	}
	C := x.Shape[1]
	if len(sf) != C || len(bf) != C || len(mf) != C || len(vf) != C {
		return nil, fmt.Errorf("batchnorm: param length mismatch with C=%d", C)
	}
	out := rc.newFloat(n, 0, x.Shape...)
	N := x.Shape[0]
	spatial := numElements(x.Shape[2:])
	// precompute per-channel factors
	factor := make([]float32, C)
	for c := 0; c < C; c++ {
		factor[c] = sf[c] / float32(math.Sqrt(float64(vf[c])+eps))
	}
	for nI := 0; nI < N; nI++ {
		for c := 0; c < C; c++ {
			base := (nI*C + c) * spatial
			f := factor[c]
			off := bf[c] - mf[c]*f
			for s := 0; s < spatial; s++ {
				out.F32[base+s] = xf[base+s]*f + off
			}
		}
	}
	// Only the first output (Y) is meaningful at inference time.
	return []*Tensor{out}, nil
}
