package onnxrt

import (
	"fmt"
	"math"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Sequence, compare, and padding kernels used by encoder-decoder ONNX graphs
// (FormulaNet and the same opset-18 subset). Bool results are int64 0/1:
// there is no bool dtype, and a non-zero value is true. Metal tryOp ignores
// any non-float32 argument, so these int64 nodes stay on the CPU.

func opConstant(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	a, ok := n.Attrs["value"]
	if !ok || a.Tensor() == nil {
		return nil, fmt.Errorf("Constant: missing value")
	}
	t, err := tensorFromProto(&a.Tensor().TensorProto)
	if err != nil {
		return nil, fmt.Errorf("Constant: %w", err)
	}
	return []*Tensor{t}, nil
}

func opConstantOfShape(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	if args[0].Rank() != 1 {
		return nil, fmt.Errorf("ConstantOfShape: shape must be 1-D, got rank %d", args[0].Rank())
	}
	dims, err := tensorIntsArg(args[0])
	if err != nil {
		return nil, fmt.Errorf("ConstantOfShape: %w", err)
	}
	shape, _, err := checkedShape(dims)
	if err != nil {
		return nil, err
	}
	fill, err := constantOfShapeFill(n)
	if err != nil {
		return nil, err
	}
	if fill.DType == DInt64 {
		out := NewInt(shape...)
		if v := fill.I64[0]; v != 0 {
			for i := range out.I64 {
				out.I64[i] = v
			}
		}
		return []*Tensor{out}, nil
	}
	out := rc.newFloat(n, 0, shape...)
	v := fill.F32[0]
	if v == 0 {
		clear(out.F32)
	} else {
		for i := range out.F32 {
			out.F32[i] = v
		}
	}
	return []*Tensor{out}, nil
}

// constantOfShapeFill returns the scalar fill. A missing value attribute is
// float32 0, matching the ONNX default.
func constantOfShapeFill(n *Node) (*Tensor, error) {
	a, ok := n.Attrs["value"]
	if !ok || a.Tensor() == nil {
		return NewFloat(), nil
	}
	t, err := tensorFromProto(&a.Tensor().TensorProto)
	if err != nil {
		return nil, fmt.Errorf("ConstantOfShape: %w", err)
	}
	if t.NumElements() < 1 {
		return nil, fmt.Errorf("ConstantOfShape: empty value")
	}
	return t, nil
}

func opCast(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	if _, ok := n.Attrs["to"]; !ok {
		return nil, fmt.Errorf("Cast: missing to")
	}
	to := TensorDataType(attrInt(n, "to", 0))
	x := args[0]
	switch to {
	case TypeFloat, TypeFloat16, TypeDouble:
		out := rc.newFloat(n, 0, x.Shape...)
		switch x.DType {
		case DFloat32:
			copy(out.F32, x.F32)
		case DInt64:
			for i, v := range x.I64 {
				out.F32[i] = float32(v)
			}
		default:
			return nil, fmt.Errorf("Cast: unsupported source %v", x.DType)
		}
		return []*Tensor{out}, nil
	case TypeInt32, TypeInt64:
		out := NewInt(x.Shape...)
		switch x.DType {
		case DInt64:
			copy(out.I64, x.I64)
		case DFloat32:
			for i, v := range x.F32 {
				out.I64[i] = floatToInt64(v)
			}
		default:
			return nil, fmt.Errorf("Cast: unsupported source %v", x.DType)
		}
		return []*Tensor{out}, nil
	case TypeBool:
		// Stored as int64 0/1. NaN is true, matching numpy/ORT.
		out := NewInt(x.Shape...)
		switch x.DType {
		case DInt64:
			for i, v := range x.I64 {
				if v != 0 {
					out.I64[i] = 1
				}
			}
		case DFloat32:
			for i, v := range x.F32 {
				if v != 0 {
					out.I64[i] = 1
				}
			}
		default:
			return nil, fmt.Errorf("Cast: unsupported source %v", x.DType)
		}
		return []*Tensor{out}, nil
	default:
		return nil, fmt.Errorf("Cast: unsupported to %s", to)
	}
}

func floatToInt64(v float32) int64 {
	if math.IsNaN(float64(v)) {
		return 0
	}
	d := float64(v)
	if d >= 9223372036854775807 {
		return math.MaxInt64
	}
	if d <= -9223372036854775808 {
		return math.MinInt64
	}
	return int64(d) // truncates toward zero
}

func opGather(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data, indices := args[0], args[1]
	if data.Rank() == 0 {
		return nil, fmt.Errorf("Gather: scalar data")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", 0), data.Rank())
	if err != nil {
		return nil, fmt.Errorf("Gather: %w", err)
	}
	raw, err := tensorIntsArg(indices)
	if err != nil {
		return nil, fmt.Errorf("Gather: %w", err)
	}
	// Copy: Ints() aliases an int64 tensor, and negative indices wrap.
	idx := make([]int64, len(raw))
	axisSize := data.Shape[axis]
	for i, v := range raw {
		if v < 0 {
			v += int64(axisSize)
		}
		if v < 0 || v >= int64(axisSize) {
			return nil, fmt.Errorf("Gather: index %d out of range for axis %d size %d", raw[i], axis, axisSize)
		}
		idx[i] = v
	}
	outShape := make([]int, 0, data.Rank()-1+indices.Rank())
	outShape = append(outShape, data.Shape[:axis]...)
	outShape = append(outShape, indices.Shape...)
	outShape = append(outShape, data.Shape[axis+1:]...)
	if _, _, err := checkedShape(intsToInt64(outShape)); err != nil {
		return nil, err
	}
	outer := numElements(data.Shape[:axis])
	inner := numElements(data.Shape[axis+1:])
	nIdx := len(idx)
	if data.DType == DInt64 {
		out := NewInt(outShape...)
		for o := 0; o < outer; o++ {
			for g := 0; g < nIdx; g++ {
				src := (o*axisSize + int(idx[g])) * inner
				dst := (o*nIdx + g) * inner
				copy(out.I64[dst:dst+inner], data.I64[src:src+inner])
			}
		}
		return []*Tensor{out}, nil
	}
	if data.DType != DFloat32 {
		return nil, fmt.Errorf("Gather: unsupported dtype %v", data.DType)
	}
	out := rc.newFloat(n, 0, outShape...)
	for o := 0; o < outer; o++ {
		for g := 0; g < nIdx; g++ {
			src := (o*axisSize + int(idx[g])) * inner
			dst := (o*nIdx + g) * inner
			copy(out.F32[dst:dst+inner], data.F32[src:src+inner])
		}
	}
	return []*Tensor{out}, nil
}

func opLayerNormalization(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 3); err != nil {
		return nil, err
	}
	x, scale, bias := args[0], args[1], args[2]
	if x.DType != DFloat32 {
		return nil, fmt.Errorf("LayerNormalization: expected float input, got %v", x.DType)
	}
	if x.Rank() == 0 {
		return nil, fmt.Errorf("LayerNormalization: scalar input")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", -1), x.Rank())
	if err != nil {
		return nil, fmt.Errorf("LayerNormalization: %w", err)
	}
	eps := float64(attrFloat(n, "epsilon", 1e-5))
	normShape := x.Shape[axis:]
	if err := broadcastExact(scale.Shape, normShape); err != nil {
		return nil, fmt.Errorf("LayerNormalization: scale: %w", err)
	}
	if err := broadcastExact(bias.Shape, normShape); err != nil {
		return nil, fmt.Errorf("LayerNormalization: bias: %w", err)
	}
	scaleF, err := scale.Floats()
	if err != nil {
		return nil, err
	}
	biasF, err := bias.Floats()
	if err != nil {
		return nil, err
	}
	outer := numElements(x.Shape[:axis])
	inner := numElements(normShape)
	y := rc.newFloat(n, 0, x.Shape...)
	wantMean := len(n.Outputs) > 1 && n.Outputs[1] != ""
	wantInv := len(n.Outputs) > 2 && n.Outputs[2] != ""
	var meanT, invT *Tensor
	if wantMean || wantInv {
		meanShape := append([]int(nil), x.Shape...)
		for i := axis; i < len(meanShape); i++ {
			meanShape[i] = 1
		}
		if wantMean {
			meanT = rc.newFloat(n, 1, meanShape...)
		}
		if wantInv {
			invT = rc.newFloat(n, 2, meanShape...)
		}
	}
	if inner == 0 {
		clear(y.F32)
		return layerNormOuts(n, y, meanT, invT), nil
	}
	dStr := rowStrides(normShape)
	sStr := broadcastStrides(scale.Shape, normShape)
	bStr := broadcastStrides(bias.Shape, normShape)
	scaleAt := func(i int) float64 { return float64(scaleF[i]) }
	biasAt := func(i int) float64 { return float64(biasF[i]) }
	if !shapeEqual(scale.Shape, normShape) {
		scaleAt = func(i int) float64 { return float64(scaleF[flatToBroadcast(i, dStr, sStr)]) }
	}
	if !shapeEqual(bias.Shape, normShape) {
		biasAt = func(i int) float64 { return float64(biasF[flatToBroadcast(i, dStr, bStr)]) }
	}
	for o := 0; o < outer; o++ {
		base := o * inner
		sum := 0.0
		for i := 0; i < inner; i++ {
			sum += float64(x.F32[base+i])
		}
		mean := sum / float64(inner)
		varsum := 0.0
		for i := 0; i < inner; i++ {
			d := float64(x.F32[base+i]) - mean
			varsum += d * d
		}
		denom := varsum/float64(inner) + eps
		if denom < 0 || math.IsNaN(denom) || math.IsInf(denom, 0) {
			return nil, fmt.Errorf("LayerNormalization: non-finite variance")
		}
		inv := 1.0 / math.Sqrt(denom)
		if wantMean {
			meanT.F32[o] = float32(mean)
		}
		if wantInv {
			invT.F32[o] = float32(inv)
		}
		for i := 0; i < inner; i++ {
			y.F32[base+i] = float32((float64(x.F32[base+i])-mean)*inv*scaleAt(i) + biasAt(i))
		}
	}
	return layerNormOuts(n, y, meanT, invT), nil
}

func layerNormOuts(n *Node, y, meanT, invT *Tensor) []*Tensor {
	outs := []*Tensor{y}
	if len(n.Outputs) > 1 {
		outs = append(outs, meanT)
	}
	if len(n.Outputs) > 2 {
		outs = append(outs, invT)
	}
	return outs
}

// broadcastExact reports whether src broadcasts onto dst and the result is dst.
func broadcastExact(src, dst []int) error {
	got, err := broadcastShapes(src, dst)
	if err != nil {
		return err
	}
	if !shapeEqual(got, dst) {
		return fmt.Errorf("shape %v does not broadcast to %v", src, dst)
	}
	return nil
}

func opEqual(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opCompare(n, args, func(a, b float32) bool { return a == b }, func(a, b int64) bool { return a == b })
}

func opLess(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opCompare(n, args, func(a, b float32) bool { return a < b }, func(a, b int64) bool { return a < b })
}

func opCompare(n *Node, args []*Tensor, fcmp func(float32, float32) bool, icmp func(int64, int64) bool) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	if a.DType == DInt64 && b.DType == DInt64 {
		out, err := binaryInt(a, b, func(x, y int64) int64 {
			if icmp(x, y) {
				return 1
			}
			return 0
		})
		if err != nil {
			return nil, err
		}
		return []*Tensor{out}, nil
	}
	fa, err := a.Floats()
	if err != nil {
		return nil, err
	}
	fb, err := b.Floats()
	if err != nil {
		return nil, err
	}
	outShape, err := broadcastShapes(a.Shape, b.Shape)
	if err != nil {
		return nil, err
	}
	out := NewInt(outShape...)
	sa := broadcastStrides(a.Shape, outShape)
	sb := broadcastStrides(b.Shape, outShape)
	// Last-dim peel: elemOffset once per row, then a stride step. Same-shape
	// rows are contiguous (step 1); a scalar operand has step 0.
	walkRows(outShape, func(row, inner int) {
		aBase, aStep := rowStep(row, outShape, sa)
		bBase, bStep := rowStep(row, outShape, sb)
		dst := out.I64[row : row+inner]
		for j := 0; j < inner; j++ {
			if fcmp(fa[aBase+j*aStep], fb[bBase+j*bStep]) {
				dst[j] = 1
			} else {
				dst[j] = 0
			}
		}
	})
	return []*Tensor{out}, nil
}

func opWhere(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 3); err != nil {
		return nil, err
	}
	cond, x, y := args[0], args[1], args[2]
	outShape, err := broadcast3(cond.Shape, x.Shape, y.Shape)
	if err != nil {
		return nil, err
	}
	cs := broadcastStrides(cond.Shape, outShape)
	xs := broadcastStrides(x.Shape, outShape)
	ys := broadcastStrides(y.Shape, outShape)
	// Both branches are stored. Where's float output may be a dirty arena
	// buffer, and a broadcast condition (masked fill) has a zero stride.
	condAt := func(off int) bool {
		if cond.DType == DInt64 {
			return cond.I64[off] != 0
		}
		return cond.F32[off] != 0
	}
	if x.DType == DInt64 && y.DType == DInt64 {
		out := NewInt(outShape...)
		walkRows(outShape, func(row, inner int) {
			cBase, cStep := rowStep(row, outShape, cs)
			xBase, xStep := rowStep(row, outShape, xs)
			yBase, yStep := rowStep(row, outShape, ys)
			dst := out.I64[row : row+inner]
			for j := 0; j < inner; j++ {
				if condAt(cBase + j*cStep) {
					dst[j] = x.I64[xBase+j*xStep]
				} else {
					dst[j] = y.I64[yBase+j*yStep]
				}
			}
		})
		return []*Tensor{out}, nil
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	yf, err := y.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, outShape...)
	walkRows(outShape, func(row, inner int) {
		cBase, cStep := rowStep(row, outShape, cs)
		xBase, xStep := rowStep(row, outShape, xs)
		yBase, yStep := rowStep(row, outShape, ys)
		dst := out.F32[row : row+inner]
		for j := 0; j < inner; j++ {
			if condAt(cBase + j*cStep) {
				dst[j] = xf[xBase+j*xStep]
			} else {
				dst[j] = yf[yBase+j*yStep]
			}
		}
	})
	return []*Tensor{out}, nil
}

func opExpand(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data := args[0]
	if args[1].Rank() != 1 {
		return nil, fmt.Errorf("Expand: shape must be 1-D, got rank %d", args[1].Rank())
	}
	dims, err := tensorIntsArg(args[1])
	if err != nil {
		return nil, fmt.Errorf("Expand: %w", err)
	}
	target := make([]int, len(dims))
	for i, d := range dims {
		if d < 0 {
			return nil, fmt.Errorf("Expand: negative dim %d", d)
		}
		target[i] = int(d)
	}
	// Output shape is the numpy broadcast of data with the requested shape.
	// A requested 1 does not shrink a larger data dim (ORT Expand).
	outShape, err := broadcastShapes(data.Shape, target)
	if err != nil {
		return nil, fmt.Errorf("Expand: %w", err)
	}
	if _, _, err := checkedShape(intsToInt64(outShape)); err != nil {
		return nil, err
	}
	strides := broadcastStrides(data.Shape, outShape)
	nOut := numElements(outShape)
	if data.DType == DInt64 {
		out := NewInt(outShape...)
		for oi := 0; oi < nOut; oi++ {
			out.I64[oi] = data.I64[elemOffset(oi, outShape, strides)]
		}
		return []*Tensor{out}, nil
	}
	if data.DType != DFloat32 {
		return nil, fmt.Errorf("Expand: unsupported dtype %v", data.DType)
	}
	out := rc.newFloat(n, 0, outShape...)
	for oi := 0; oi < nOut; oi++ {
		out.F32[oi] = data.F32[elemOffset(oi, outShape, strides)]
	}
	return []*Tensor{out}, nil
}

func opRange(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 3); err != nil {
		return nil, err
	}
	startI, startF, startFloat, err := tensorScalar(args[0])
	if err != nil {
		return nil, fmt.Errorf("Range start: %w", err)
	}
	limitI, limitF, _, err := tensorScalar(args[1])
	if err != nil {
		return nil, fmt.Errorf("Range limit: %w", err)
	}
	deltaI, deltaF, _, err := tensorScalar(args[2])
	if err != nil {
		return nil, fmt.Errorf("Range delta: %w", err)
	}
	if startFloat || args[0].DType == DFloat32 {
		if math.IsNaN(startF) || math.IsNaN(limitF) || math.IsNaN(deltaF) || deltaF == 0 || math.IsInf(deltaF, 0) {
			return nil, fmt.Errorf("Range: delta must be a finite non-zero number")
		}
		nOut := int(math.Ceil((limitF - startF) / deltaF))
		if nOut < 0 {
			nOut = 0
		}
		if nOut > maxTensorElements {
			return nil, fmt.Errorf("Range: length %d exceeds cap", nOut)
		}
		out := rc.newFloat(n, 0, nOut)
		for i := 0; i < nOut; i++ {
			out.F32[i] = float32(startF + float64(i)*deltaF)
		}
		return []*Tensor{out}, nil
	}
	nOut, err := intRangeLen(startI, limitI, deltaI)
	if err != nil {
		return nil, err
	}
	out := NewInt(nOut)
	v := startI
	for i := 0; i < nOut; i++ {
		out.I64[i] = v
		next := v + deltaI
		if (deltaI > 0 && next <= v) || (deltaI < 0 && next >= v) {
			return nil, fmt.Errorf("Range: overflow")
		}
		v = next
	}
	return []*Tensor{out}, nil
}

func intRangeLen(start, limit, delta int64) (int, error) {
	if delta == 0 {
		return 0, fmt.Errorf("Range: delta must be non-zero")
	}
	if (delta > 0 && start >= limit) || (delta < 0 && start <= limit) {
		return 0, nil
	}
	var n uint64
	if delta > 0 {
		n = ceilDivPos(limit, start, delta)
	} else {
		n = ceilDivPos(start, limit, -delta)
	}
	if n > uint64(maxTensorElements) {
		return 0, fmt.Errorf("Range: length exceeds cap")
	}
	return int(n), nil
}

// ceilDivPos computes ceil((hi-lo)/step) for hi > lo and step > 0.
func ceilDivPos(hi, lo, step int64) uint64 {
	span := subPos(hi, lo)
	st := uint64(step)
	return (span + st - 1) / st
}

func subPos(hi, lo int64) uint64 {
	if lo >= 0 {
		return uint64(hi - lo)
	}
	if hi >= 0 {
		if lo == math.MinInt64 {
			return uint64(hi) + (1 << 63)
		}
		return uint64(hi) + uint64(-lo)
	}
	return uint64(hi - lo)
}

func opPad(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	mode := attrStr(n, "mode", "constant")
	if mode != "constant" {
		return nil, fmt.Errorf("Pad: mode %q not supported", mode)
	}
	data := args[0]
	pads, err := tensorIntsArg(args[1])
	if err != nil {
		return nil, fmt.Errorf("Pad: %w", err)
	}
	rank := data.Rank()
	begin := make([]int, rank)
	end := make([]int, rank)
	if len(args) > 3 && args[3] != nil {
		axes, err := tensorIntsArg(args[3])
		if err != nil {
			return nil, fmt.Errorf("Pad axes: %w", err)
		}
		if len(pads) != 2*len(axes) {
			return nil, fmt.Errorf("Pad: pads len %d != 2*axes %d", len(pads), len(axes))
		}
		for i, ax := range axes {
			a, err := normalizeAxis(ax, rank)
			if err != nil {
				return nil, fmt.Errorf("Pad: %w", err)
			}
			begin[a] = int(pads[i])
			end[a] = int(pads[len(axes)+i])
		}
	} else {
		if len(pads) != 2*rank {
			return nil, fmt.Errorf("Pad: pads len %d != 2*rank %d", len(pads), rank)
		}
		for i := 0; i < rank; i++ {
			begin[i] = int(pads[i])
			end[i] = int(pads[rank+i])
		}
	}
	outShape := make([]int, rank)
	for i := 0; i < rank; i++ {
		d := data.Shape[i] + begin[i] + end[i]
		if d < 0 {
			return nil, fmt.Errorf("Pad: negative output dim %d", d)
		}
		outShape[i] = d
	}
	if _, _, err := checkedShape(intsToInt64(outShape)); err != nil {
		return nil, err
	}
	fillF := float32(0)
	fillI := int64(0)
	if len(args) > 2 && args[2] != nil {
		if args[2].NumElements() < 1 {
			return nil, fmt.Errorf("Pad: empty constant_value")
		}
		if data.DType == DInt64 {
			iv, err := args[2].Ints()
			if err != nil {
				return nil, err
			}
			fillI = iv[0]
		} else {
			fv, err := args[2].Floats()
			if err != nil {
				return nil, err
			}
			fillF = fv[0]
		}
	}
	if data.DType == DInt64 {
		out := NewInt(outShape...)
		padCopy(outShape, data.Shape, begin, func(oi int, inside bool, inOff int) {
			if inside {
				out.I64[oi] = data.I64[inOff]
			} else {
				out.I64[oi] = fillI
			}
		})
		return []*Tensor{out}, nil
	}
	if data.DType != DFloat32 {
		return nil, fmt.Errorf("Pad: unsupported dtype %v", data.DType)
	}
	out := rc.newFloat(n, 0, outShape...)
	if !padF32(out.F32, data.F32, outShape, data.Shape, begin, fillF) {
		padCopy(outShape, data.Shape, begin, func(oi int, inside bool, inOff int) {
			if inside {
				out.F32[oi] = data.F32[inOff]
			} else {
				out.F32[oi] = fillF
			}
		})
	}
	return []*Tensor{out}, nil
}

// padF32 fills a constant pad for rank 1..4. The interior is a contiguous
// copy along the last dimension. Returns false for ranks the plane walk
// does not cover.
func padF32(out, in []float32, outShape, inShape, begin []int, fill float32) bool {
	rank := len(outShape)
	if rank < 1 || rank > 4 || len(inShape) != rank || len(begin) != rank {
		return false
	}
	if len(out) > 0 {
		if fill == 0 {
			clear(out)
		} else {
			out[0] = fill
			for n := 1; n < len(out); {
				n += copy(out[n:], out[:n])
			}
		}
	}
	// Lift the shape to 4D so one walk covers every supported rank.
	var o, inD, b [4]int
	for i := 0; i < 4; i++ {
		o[i], inD[i], b[i] = 1, 1, 0
	}
	base := 4 - rank
	for i := 0; i < rank; i++ {
		o[base+i] = outShape[i]
		inD[base+i] = inShape[i]
		b[base+i] = begin[i]
	}
	n0, on, nn := padSpan(b[0], inD[0], o[0])
	c0, oc, nc := padSpan(b[1], inD[1], o[1])
	h0, oh, nh := padSpan(b[2], inD[2], o[2])
	w0, ow, nw := padSpan(b[3], inD[3], o[3])
	if nn == 0 || nc == 0 || nh == 0 || nw == 0 {
		return true
	}
	iC, iH, iW := inD[1], inD[2], inD[3]
	oC, oH, oW := o[1], o[2], o[3]
	for ni := 0; ni < nn; ni++ {
		for ci := 0; ci < nc; ci++ {
			inPlane := ((n0+ni)*iC + (c0 + ci)) * iH
			outPlane := ((on+ni)*oC + (oc + ci)) * oH
			for hi := 0; hi < nh; hi++ {
				src := in[(inPlane+h0+hi)*iW+w0:]
				dst := out[(outPlane+oh+hi)*oW+ow:]
				copy(dst[:nw], src[:nw])
			}
		}
	}
	return true
}

func padSpan(begin, inDim, outDim int) (in0, out0, n int) {
	if inDim <= 0 || outDim <= 0 {
		return 0, 0, 0
	}
	out0 = begin
	if out0 < 0 {
		in0 = -out0
		out0 = 0
	}
	if in0 > inDim || out0 >= outDim {
		return 0, 0, 0
	}
	n = inDim - in0
	if out0+n > outDim {
		n = outDim - out0
	}
	if n < 0 {
		n = 0
	}
	return in0, out0, n
}

func padCopy(outShape, inShape, begin []int, set func(oi int, inside bool, inOff int)) {
	nOut := numElements(outShape)
	if len(outShape) == 0 {
		if nOut > 0 {
			set(0, true, 0)
		}
		return
	}
	inStr := rowStrides(inShape)
	coord := make([]int, len(outShape))
	for oi := 0; oi < nOut; oi++ {
		inOff := 0
		inside := true
		for d := 0; d < len(outShape); d++ {
			ii := coord[d] - begin[d]
			if ii < 0 || ii >= inShape[d] {
				inside = false
				break
			}
			inOff += ii * inStr[d]
		}
		set(oi, inside, inOff)
		for d := len(coord) - 1; d >= 0; d-- {
			coord[d]++
			if coord[d] < outShape[d] {
				break
			}
			coord[d] = 0
		}
	}
}

func tensorScalar(t *Tensor) (int64, float64, bool, error) {
	if t == nil || t.NumElements() != 1 {
		return 0, 0, false, fmt.Errorf("expected scalar, got %v", shapeOf(t))
	}
	if t.DType == DInt64 {
		return t.I64[0], float64(t.I64[0]), false, nil
	}
	if t.DType != DFloat32 {
		return 0, 0, false, fmt.Errorf("unsupported dtype %v", t.DType)
	}
	return int64(t.F32[0]), float64(t.F32[0]), true, nil
}

func shapeOf(t *Tensor) []int {
	if t == nil {
		return nil
	}
	return t.Shape
}

func broadcast3(a, b, c []int) ([]int, error) {
	ab, err := broadcastShapes(a, b)
	if err != nil {
		return nil, err
	}
	return broadcastShapes(ab, c)
}

func rowStrides(shape []int) []int {
	s := make([]int, len(shape))
	acc := 1
	for i := len(shape) - 1; i >= 0; i-- {
		s[i] = acc
		acc *= shape[i]
	}
	return s
}

// walkRows calls fn once per last-dimension row. A row's elemOffset (last
// coordinate 0) plus j times the last stride equals elemOffset of column j,
// for dense, scalar, and partial broadcasts. ParallelRanges runs over rows
// only, and only when there are at least two.
func walkRows(outShape []int, fn func(row, inner int)) {
	nOut := numElements(outShape)
	if nOut == 0 {
		return
	}
	inner := 1
	if len(outShape) > 0 {
		inner = outShape[len(outShape)-1]
		if inner <= 0 {
			return
		}
	}
	outer := nOut / inner
	if outer >= 2 && nOut >= 8192 {
		xt.ParallelRanges(outer, func(s, e int) {
			for o := s; o < e; o++ {
				fn(o*inner, inner)
			}
		})
		return
	}
	for o := 0; o < outer; o++ {
		fn(o*inner, inner)
	}
}

// rowStep returns the broadcast offset of a row origin and the step to the
// next column. Rank 0 uses offset 0.
func rowStep(oi int, outShape, strides []int) (base, step int) {
	if len(outShape) == 0 {
		return 0, 0
	}
	return elemOffset(oi, outShape, strides), strides[len(strides)-1]
}

// elemOffset maps a flat output index through broadcast strides.
// A scalar (rank 0) output uses offset 0.
func elemOffset(oi int, outShape, strides []int) int {
	off := 0
	for d := len(outShape) - 1; d >= 0; d-- {
		dim := outShape[d]
		if dim == 0 {
			return 0
		}
		idx := oi % dim
		oi /= dim
		off += idx * strides[d]
	}
	return off
}

func flatToBroadcast(flat int, dstStrides, srcStrides []int) int {
	off := 0
	rem := flat
	for d := 0; d < len(dstStrides); d++ {
		c := 0
		if dstStrides[d] != 0 {
			c = rem / dstStrides[d]
			rem -= c * dstStrides[d]
		}
		off += c * srcStrides[d]
	}
	return off
}

func intsToInt64(s []int) []int64 {
	out := make([]int64, len(s))
	for i, v := range s {
		out[i] = int64(v)
	}
	return out
}
