package onnxrt

import (
	"fmt"
	"math"
	"sort"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Ops used by PP-DocLayoutV3. Bool results stay int64 0/1, matching Equal and Less.

func opGreater(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opCompare(n, args, func(a, b float32) bool { return a > b }, func(a, b int64) bool { return a > b })
}

func opAnd(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opCompare(n, args,
		func(a, b float32) bool { return a != 0 && b != 0 },
		func(a, b int64) bool { return a != 0 && b != 0 })
}

func opClip(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, x.Shape...)
	copy(out.F32, xf)
	if len(args) > 1 && args[1] != nil && args[1].NumElements() > 0 {
		if err := broadcastInto(out, args[1], func(v, bound float32) float32 {
			if v < bound {
				return bound
			}
			return v
		}); err != nil {
			return nil, fmt.Errorf("Clip min: %w", err)
		}
	}
	if len(args) > 2 && args[2] != nil && args[2].NumElements() > 0 {
		if err := broadcastInto(out, args[2], func(v, bound float32) float32 {
			if v > bound {
				return bound
			}
			return v
		}); err != nil {
			return nil, fmt.Errorf("Clip max: %w", err)
		}
	}
	return []*Tensor{out}, nil
}

func opMax(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	var first *Tensor
	for _, a := range args {
		if a != nil {
			first = a
			break
		}
	}
	if first == nil {
		return nil, fmt.Errorf("Max: no inputs")
	}
	if first.DType == DInt64 {
		allInt := true
		for _, a := range args {
			if a != nil && a.DType != DInt64 {
				allInt = false
				break
			}
		}
		if allInt {
			acc := first
			for _, b := range args {
				if b == nil || b == first {
					continue
				}
				next, err := binaryInt(acc, b, func(x, y int64) int64 {
					if y > x {
						return y
					}
					return x
				})
				if err != nil {
					return nil, err
				}
				acc = next
			}
			return []*Tensor{acc}, nil
		}
	}
	outShape := append([]int{}, first.Shape...)
	var err error
	for _, b := range args {
		if b == nil {
			continue
		}
		outShape, err = broadcastShapes(outShape, b.Shape)
		if err != nil {
			return nil, err
		}
	}
	acc := rc.newFloat(n, 0, outShape...)
	for i := range acc.F32 {
		acc.F32[i] = float32(math.Inf(-1))
	}
	for _, b := range args {
		if b == nil {
			continue
		}
		if err := broadcastInto(acc, b, func(x, y float32) float32 {
			if y > x || (math.IsNaN(float64(y)) && !math.IsNaN(float64(x))) {
				return y
			}
			return x
		}); err != nil {
			return nil, err
		}
	}
	return []*Tensor{acc}, nil
}

// broadcastInto writes fn(out, bound) back into out. bound broadcasts onto out.
func broadcastInto(out, bound *Tensor, fn func(v, bound float32) float32) error {
	bf, err := bound.Floats()
	if err != nil {
		return err
	}
	got, err := broadcastShapes(out.Shape, bound.Shape)
	if err != nil {
		return err
	}
	if !shapeEqual(got, out.Shape) {
		return fmt.Errorf("shape %v does not broadcast to %v", bound.Shape, out.Shape)
	}
	sb := broadcastStrides(bound.Shape, out.Shape)
	for oi := 0; oi < out.NumElements(); oi++ {
		out.F32[oi] = fn(out.F32[oi], bf[elemOffset(oi, out.Shape, sb)])
	}
	return nil
}

func opReduceMax(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opReduce(rc, n, args, reduceMax)
}

func opReduceMin(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opReduce(rc, n, args, reduceMin)
}

func opReduceSum(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	return opReduce(rc, n, args, reduceSum)
}

type reduceCode int

const (
	reduceSum reduceCode = iota
	reduceMax
	reduceMin
)

func opReduce(rc *runCtx, n *Node, args []*Tensor, code reduceCode) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	rank := x.Rank()
	var axesList []int64
	hasAxes := false
	if ax := attrInts(n, "axes"); ax != nil {
		axesList, hasAxes = ax, true
	} else if len(args) > 1 && args[1] != nil {
		ax, err := tensorIntsArg(args[1])
		if err != nil {
			return nil, err
		}
		axesList, hasAxes = ax, true
	}
	keepdims := attrInt(n, "keepdims", 1) != 0
	noop := attrInt(n, "noop_with_empty_axes", 0) != 0
	if hasAxes && len(axesList) == 0 && noop {
		out := rc.newFloat(n, 0, x.Shape...)
		copy(out.F32, xf)
		return []*Tensor{out}, nil
	}
	reduce := make([]bool, rank)
	if !hasAxes || len(axesList) == 0 {
		for d := range reduce {
			reduce[d] = true
		}
	} else {
		for _, a := range axesList {
			na, err := normalizeAxis(a, rank)
			if err != nil {
				return nil, fmt.Errorf("Reduce: %w", err)
			}
			reduce[na] = true
		}
	}
	keptShape := make([]int, rank)
	var squeezed []int
	for d := 0; d < rank; d++ {
		if reduce[d] {
			keptShape[d] = 1
		} else {
			keptShape[d] = x.Shape[d]
			squeezed = append(squeezed, x.Shape[d])
		}
		if reduce[d] && keepdims {
			squeezed = append(squeezed, 1)
		}
	}
	if squeezed == nil {
		squeezed = []int{}
	}
	accShape := keptShape
	if rank == 0 {
		accShape = []int{}
	}
	acc := rc.newFloat(n, 0, accShape...)
	// A single axis is the whole layout model (axes [-1], and one ReduceMax
	// on [-2]). Assigning each output overwrites a dirty arena buffer, so
	// ReduceSum stays out of needsZero. Multi-axis sum still clears first:
	// the fallback accumulates with +=.
	if axis, ok := singleReduceAxis(reduce); ok {
		reduceAlong(acc.F32, xf, x.Shape, axis, code)
	} else if len(acc.F32) > 0 {
		switch code {
		case reduceMax:
			for i := range acc.F32 {
				acc.F32[i] = float32(math.Inf(-1))
			}
		case reduceMin:
			for i := range acc.F32 {
				acc.F32[i] = float32(math.Inf(1))
			}
		default:
			clear(acc.F32)
		}
		if len(xf) > 0 {
			inStride := make([]int, rank)
			step := 1
			for d := rank - 1; d >= 0; d-- {
				inStride[d] = step
				if x.Shape[d] > 0 {
					step *= x.Shape[d]
				}
			}
			keptStride := make([]int, rank)
			step = 1
			for d := rank - 1; d >= 0; d-- {
				keptStride[d] = step
				if keptShape[d] > 0 {
					step *= keptShape[d]
				}
			}
			for i, v := range xf {
				rem := i
				oi := 0
				for d := 0; d < rank; d++ {
					coord := 0
					if inStride[d] > 0 {
						coord = rem / inStride[d]
						rem -= coord * inStride[d]
					}
					if !reduce[d] {
						oi += coord * keptStride[d]
					}
				}
				switch code {
				case reduceSum:
					acc.F32[oi] += v
				case reduceMax:
					if v > acc.F32[oi] || math.IsNaN(float64(v)) {
						acc.F32[oi] = v
					}
				case reduceMin:
					if v < acc.F32[oi] || math.IsNaN(float64(v)) {
						acc.F32[oi] = v
					}
				}
			}
		}
	}
	if keepdims || shapeEqual(acc.Shape, squeezed) {
		return []*Tensor{acc}, nil
	}
	return []*Tensor{acc.Reshape(squeezed...)}, nil
}

// singleReduceAxis reports the only reduced axis. Rank 0 and multi-axis
// reductions stay on the general loop.
func singleReduceAxis(reduce []bool) (int, bool) {
	axis := -1
	for d, on := range reduce {
		if !on {
			continue
		}
		if axis >= 0 {
			return 0, false
		}
		axis = d
	}
	if axis < 0 {
		return 0, false
	}
	return axis, true
}

// reduceAlong writes every output of one reduced axis. inner == 1 is a
// contiguous row (axes [-1]). Any other axis steps by inner. ParallelRanges
// covers either the outer index or the inner index, never both.
func reduceAlong(acc, xf []float32, shape []int, axis int, code reduceCode) {
	outer := numElements(shape[:axis])
	span := shape[axis]
	inner := numElements(shape[axis+1:])
	if span <= 0 {
		fill := reduceIdent(code)
		for i := range acc {
			acc[i] = fill
		}
		return
	}
	n := outer * span * inner
	writeInner := func(o, j0, j1 int) {
		for j := j0; j < j1; j++ {
			acc[o*inner+j] = reduceSpan(xf, (o*span)*inner+j, inner, span, code)
		}
	}
	switch {
	case outer >= 2 && n >= 8192:
		xt.ParallelRanges(outer, func(s, e int) {
			for o := s; o < e; o++ {
				if inner == 1 {
					acc[o] = reduceSpan(xf[o*span:(o+1)*span], 0, 1, span, code)
				} else {
					writeInner(o, 0, inner)
				}
			}
		})
	case outer <= 1 && inner >= 2 && n >= 8192:
		xt.ParallelRanges(inner, func(s, e int) {
			writeInner(0, s, e)
		})
	default:
		for o := 0; o < outer; o++ {
			if inner == 1 {
				acc[o] = reduceSpan(xf[o*span:(o+1)*span], 0, 1, span, code)
			} else {
				writeInner(o, 0, inner)
			}
		}
	}
}

func reduceIdent(code reduceCode) float32 {
	switch code {
	case reduceMax:
		return float32(math.Inf(-1))
	case reduceMin:
		return float32(math.Inf(1))
	default:
		return 0
	}
}

// reduceSpan is a left-to-right reduction of xf[base+s*step] for s in 0..span.
// Sum starts at zero, matching a cleared accumulator. Max/min start at ±Inf
// and replace on NaN, matching the scalar loop's compare. A contiguous
// max/min (the layout mask's last axis, 40000 wide) uses the SIMD lane
// reduction when that result is bit-exact.
func reduceSpan(xf []float32, base, step, span int, code reduceCode) float32 {
	if step == 1 && (code == reduceMax || code == reduceMin) {
		if m, ok := reduceContigExtreme(xf[base:base+span], code); ok {
			return m
		}
	}
	switch code {
	case reduceMax:
		best := float32(math.Inf(-1))
		for s := 0; s < span; s++ {
			v := xf[base+s*step]
			if v > best || math.IsNaN(float64(v)) {
				best = v
			}
		}
		return best
	case reduceMin:
		best := float32(math.Inf(1))
		for s := 0; s < span; s++ {
			v := xf[base+s*step]
			if v < best || math.IsNaN(float64(v)) {
				best = v
			}
		}
		return best
	default:
		sum := float32(0)
		for s := 0; s < span; s++ {
			sum += xf[base+s*step]
		}
		return sum
	}
}

func opGatherElements(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data, indices := args[0], args[1]
	if data.Rank() != indices.Rank() {
		return nil, fmt.Errorf("GatherElements: data rank %d != indices rank %d", data.Rank(), indices.Rank())
	}
	if data.Rank() == 0 {
		return nil, fmt.Errorf("GatherElements: scalar data")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", 0), data.Rank())
	if err != nil {
		return nil, fmt.Errorf("GatherElements: %w", err)
	}
	raw, err := tensorIntsArg(indices)
	if err != nil {
		return nil, err
	}
	axisSize := data.Shape[axis]
	idx := make([]int, len(raw))
	for i, v := range raw {
		if v < 0 {
			v += int64(axisSize)
		}
		if v < 0 || v >= int64(axisSize) {
			return nil, fmt.Errorf("GatherElements: index %d out of range for axis %d", raw[i], axis)
		}
		idx[i] = int(v)
	}
	if _, _, err := checkedShape(intsToInt64(indices.Shape)); err != nil {
		return nil, err
	}
	stride := prefixStrides(data.Shape)
	indexStride := prefixStrides(indices.Shape)
	if data.DType == DInt64 {
		out := NewInt(indices.Shape...)
		for p := range idx {
			src := elementAtAxis(p, indexStride, stride, axis, idx[p])
			out.I64[p] = data.I64[src]
		}
		return []*Tensor{out}, nil
	}
	df, err := data.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, indices.Shape...)
	for p := range idx {
		src := elementAtAxis(p, indexStride, stride, axis, idx[p])
		out.F32[p] = df[src]
	}
	return []*Tensor{out}, nil
}

func prefixStrides(shape []int) []int {
	stride := make([]int, len(shape))
	step := 1
	for d := len(shape) - 1; d >= 0; d-- {
		stride[d] = step
		if shape[d] > 0 {
			step *= shape[d]
		}
	}
	return stride
}

func elementAtAxis(pos int, indexStride, dataStride []int, axis, axisIndex int) int {
	src := 0
	rem := pos
	for d := 0; d < len(indexStride); d++ {
		coord := 0
		if indexStride[d] > 0 {
			coord = rem / indexStride[d]
			rem -= coord * indexStride[d]
		}
		if d == axis {
			coord = axisIndex
		}
		src += coord * dataStride[d]
	}
	return src
}

func opGatherND(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data, indices := args[0], args[1]
	if indices.Rank() < 1 {
		return nil, fmt.Errorf("GatherND: indices must have a tuple axis")
	}
	batchDims := int(attrInt(n, "batch_dims", 0))
	if batchDims < 0 || batchDims >= indices.Rank() || batchDims > data.Rank() {
		return nil, fmt.Errorf("GatherND: batch_dims %d out of range", batchDims)
	}
	k := indices.Shape[indices.Rank()-1]
	if batchDims+k > data.Rank() {
		return nil, fmt.Errorf("GatherND: index tuple %d exceeds data rank %d", k, data.Rank())
	}
	raw, err := tensorIntsArg(indices)
	if err != nil {
		return nil, err
	}
	outShape := append([]int{}, indices.Shape[:indices.Rank()-1]...)
	outShape = append(outShape, data.Shape[batchDims+k:]...)
	if _, _, err := checkedShape(intsToInt64(outShape)); err != nil {
		return nil, err
	}
	dataStride := prefixStrides(data.Shape)
	tupleCount := numElements(indices.Shape[:indices.Rank()-1])
	inner := numElements(data.Shape[batchDims+k:])
	if data.DType == DInt64 {
		out := NewInt(outShape...)
		for t := 0; t < tupleCount; t++ {
			src, err := gatherNDOffset(data.Shape, dataStride, raw[t*k:(t+1)*k], batchDims, t, indices.Shape[:indices.Rank()-1])
			if err != nil {
				return nil, err
			}
			copy(out.I64[t*inner:(t+1)*inner], data.I64[src:src+inner])
		}
		return []*Tensor{out}, nil
	}
	df, err := data.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, outShape...)
	for t := 0; t < tupleCount; t++ {
		src, err := gatherNDOffset(data.Shape, dataStride, raw[t*k:(t+1)*k], batchDims, t, indices.Shape[:indices.Rank()-1])
		if err != nil {
			return nil, err
		}
		copy(out.F32[t*inner:(t+1)*inner], df[src:src+inner])
	}
	return []*Tensor{out}, nil
}

func gatherNDOffset(shape, stride []int, tuple []int64, batchDims, tupleIndex int, tupleShape []int) (int, error) {
	coords := unravel(tupleIndex, tupleShape)
	src := 0
	for d := 0; d < batchDims; d++ {
		src += coords[d] * stride[d]
	}
	for j, iv := range tuple {
		dim := shape[batchDims+j]
		if iv < 0 {
			iv += int64(dim)
		}
		if iv < 0 || iv >= int64(dim) {
			return 0, fmt.Errorf("GatherND: index %d out of range for axis %d", tuple[j], batchDims+j)
		}
		src += int(iv) * stride[batchDims+j]
	}
	return src, nil
}

func unravel(index int, shape []int) []int {
	coords := make([]int, len(shape))
	for d := len(shape) - 1; d >= 0; d-- {
		if shape[d] == 0 {
			continue
		}
		coords[d] = index % shape[d]
		index /= shape[d]
	}
	return coords
}

func opGridSample(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	x, grid := args[0], args[1]
	if x.Rank() != 4 || grid.Rank() != 4 || grid.Shape[3] != 2 {
		return nil, fmt.Errorf("GridSample: want NCHW input and (N,H,W,2) grid")
	}
	if x.Shape[0] != grid.Shape[0] {
		return nil, fmt.Errorf("GridSample: batch %d != grid batch %d", x.Shape[0], grid.Shape[0])
	}
	mode := attrStr(n, "mode", "linear")
	if mode == "linear" {
		mode = "bilinear"
	}
	if mode != "bilinear" && mode != "nearest" {
		return nil, fmt.Errorf("GridSample: mode %q is not supported", mode)
	}
	padding := attrStr(n, "padding_mode", "zeros")
	if padding != "zeros" && padding != "border" && padding != "reflection" {
		return nil, fmt.Errorf("GridSample: padding_mode %q is not supported", padding)
	}
	alignCorners := attrInt(n, "align_corners", 0) != 0
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	gf, err := grid.Floats()
	if err != nil {
		return nil, err
	}
	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oH, oW := grid.Shape[1], grid.Shape[2]
	out := rc.newFloat(n, 0, N, C, oH, oW)
	gridSampleRun(out.F32, xf, gf, N, C, H, W, oH, oW, mode, padding, alignCorners)
	return []*Tensor{out}, nil
}

func gridCoord(g float64, size int, alignCorners bool) float64 {
	if alignCorners {
		return (g + 1) * float64(size-1) / 2
	}
	return ((g+1)*float64(size) - 1) / 2
}

func gridBilinear(plane []float32, w, h int, x, y float64, padding string) float32 {
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	x1 := x0 + 1
	y1 := y0 + 1
	wx := float32(x - x0)
	wy := float32(y - y0)
	v00 := gridSampleAt(plane, w, h, int(y0), int(x0), padding)
	v01 := gridSampleAt(plane, w, h, int(y0), int(x1), padding)
	v10 := gridSampleAt(plane, w, h, int(y1), int(x0), padding)
	v11 := gridSampleAt(plane, w, h, int(y1), int(x1), padding)
	return v00*(1-wx)*(1-wy) + v01*wx*(1-wy) + v10*(1-wx)*wy + v11*wx*wy
}

func gridSampleAt(plane []float32, w, h, y, x int, padding string) float32 {
	switch padding {
	case "border":
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
	case "reflection":
		x = reflectIndex(x, w)
		y = reflectIndex(y, h)
	default:
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0
		}
	}
	if w == 0 || h == 0 {
		return 0
	}
	return plane[y*w+x]
}

func reflectIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return 0
	}
	period := 2 * (n - 1)
	i %= period
	if i < 0 {
		i += period
	}
	if i >= n {
		i = period - i
	}
	return i
}

func opScatterElements(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 3); err != nil {
		return nil, err
	}
	data, indices, updates := args[0], args[1], args[2]
	if !shapeEqual(indices.Shape, updates.Shape) {
		return nil, fmt.Errorf("ScatterElements: indices shape %v != updates shape %v", indices.Shape, updates.Shape)
	}
	if data.Rank() == 0 || indices.Rank() != data.Rank() {
		return nil, fmt.Errorf("ScatterElements: rank mismatch")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", 0), data.Rank())
	if err != nil {
		return nil, fmt.Errorf("ScatterElements: %w", err)
	}
	reduction := attrStr(n, "reduction", "none")
	raw, err := tensorIntsArg(indices)
	if err != nil {
		return nil, err
	}
	axisSize := data.Shape[axis]
	stride := prefixStrides(data.Shape)
	indexStride := prefixStrides(indices.Shape)
	if data.DType == DInt64 && updates.DType == DInt64 {
		out := data.Clone()
		for p, iv := range raw {
			if iv < 0 {
				iv += int64(axisSize)
			}
			if iv < 0 || iv >= int64(axisSize) {
				return nil, fmt.Errorf("ScatterElements: index %d out of range", raw[p])
			}
			dst := elementAtAxis(p, indexStride, stride, axis, int(iv))
			switch reduction {
			case "add":
				out.I64[dst] += updates.I64[p]
			case "mul":
				out.I64[dst] *= updates.I64[p]
			case "none":
				out.I64[dst] = updates.I64[p]
			default:
				return nil, fmt.Errorf("ScatterElements: reduction %q is not supported", reduction)
			}
		}
		return []*Tensor{out}, nil
	}
	df, err := data.Floats()
	if err != nil {
		return nil, err
	}
	uf, err := updates.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, data.Shape...)
	copy(out.F32, df)
	for p, iv := range raw {
		if iv < 0 {
			iv += int64(axisSize)
		}
		if iv < 0 || iv >= int64(axisSize) {
			return nil, fmt.Errorf("ScatterElements: index %d out of range", raw[p])
		}
		dst := elementAtAxis(p, indexStride, stride, axis, int(iv))
		switch reduction {
		case "add":
			out.F32[dst] += uf[p]
		case "mul":
			out.F32[dst] *= uf[p]
		case "none":
			out.F32[dst] = uf[p]
		default:
			return nil, fmt.Errorf("ScatterElements: reduction %q is not supported", reduction)
		}
	}
	return []*Tensor{out}, nil
}

func opSplit(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	data := args[0]
	if data.Rank() == 0 {
		return nil, fmt.Errorf("Split: scalar input")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", 0), data.Rank())
	if err != nil {
		return nil, fmt.Errorf("Split: %w", err)
	}
	var sizes []int64
	switch {
	case len(args) > 1 && args[1] != nil:
		sizes, err = tensorIntsArg(args[1])
		if err != nil {
			return nil, fmt.Errorf("Split: %w", err)
		}
	case attrInts(n, "split") != nil:
		sizes = attrInts(n, "split")
	default:
		parts := len(n.Outputs)
		if parts < 1 {
			parts = int(attrInt(n, "num_outputs", 0))
		}
		if parts < 1 || data.Shape[axis]%parts != 0 {
			return nil, fmt.Errorf("Split: cannot divide axis %d into %d parts", axis, parts)
		}
		sizes = make([]int64, parts)
		each := int64(data.Shape[axis] / parts)
		for i := range sizes {
			sizes[i] = each
		}
	}
	sum := 0
	for _, s := range sizes {
		if s < 0 {
			return nil, fmt.Errorf("Split: negative size %d", s)
		}
		sum += int(s)
	}
	if sum != data.Shape[axis] {
		return nil, fmt.Errorf("Split: sizes sum %d != axis %d", sum, data.Shape[axis])
	}
	inner := numElements(data.Shape[axis+1:])
	outer := numElements(data.Shape[:axis])
	outs := make([]*Tensor, len(sizes))
	offset := 0
	for p, s := range sizes {
		shape := append([]int{}, data.Shape...)
		shape[axis] = int(s)
		count := outer * int(s) * inner
		if data.DType == DInt64 {
			chunk := NewInt(shape...)
			for o := 0; o < outer; o++ {
				src := (o*data.Shape[axis] + offset) * inner
				dst := o * int(s) * inner
				copy(chunk.I64[dst:dst+int(s)*inner], data.I64[src:src+int(s)*inner])
			}
			outs[p] = chunk
		} else {
			df, err := data.Floats()
			if err != nil {
				return nil, err
			}
			chunk := rc.newFloat(n, p, shape...)
			for o := 0; o < outer; o++ {
				src := (o*data.Shape[axis] + offset) * inner
				dst := o * int(s) * inner
				copy(chunk.F32[dst:dst+int(s)*inner], df[src:src+int(s)*inner])
			}
			outs[p] = chunk
		}
		offset += int(s)
		if count != outs[p].NumElements() {
			return nil, fmt.Errorf("Split: chunk %d element count", p)
		}
	}
	return outs, nil
}

func opTile(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	data, repeatsT := args[0], args[1]
	repeats, err := tensorIntsArg(repeatsT)
	if err != nil {
		return nil, fmt.Errorf("Tile: %w", err)
	}
	if len(repeats) != data.Rank() {
		return nil, fmt.Errorf("Tile: repeats %d != rank %d", len(repeats), data.Rank())
	}
	outDims := make([]int64, len(repeats))
	for i, r := range repeats {
		if r < 0 {
			return nil, fmt.Errorf("Tile: negative repeat %d", r)
		}
		outDims[i] = int64(data.Shape[i]) * r
	}
	outShape, _, err := checkedShape(outDims)
	if err != nil {
		return nil, err
	}
	if data.DType == DInt64 {
		out := NewInt(outShape...)
		fillTile(out.I64, data.I64, data.Shape, outShape)
		return []*Tensor{out}, nil
	}
	df, err := data.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, outShape...)
	fillTileFloat(out.F32, df, data.Shape, outShape)
	return []*Tensor{out}, nil
}

func fillTile(dst, src []int64, inShape, outShape []int) {
	if dst == nil {
		return
	}
	n := len(dst)
	for i := 0; i < n; i++ {
		dst[i] = src[tileSrc(i, inShape, outShape)]
	}
}

func fillTileFloat(dst, src []float32, inShape, outShape []int) {
	for i := range dst {
		dst[i] = src[tileSrc(i, inShape, outShape)]
	}
}

func tileSrc(index int, inShape, outShape []int) int {
	src := 0
	for d := 0; d < len(outShape); d++ {
		coord := 0
		if outShape[d] > 0 {
			stride := 1
			for k := d + 1; k < len(outShape); k++ {
				stride *= outShape[k]
			}
			coord = (index / stride) % inShape[d]
		}
		inner := 1
		for k := d + 1; k < len(inShape); k++ {
			inner *= inShape[k]
		}
		src += coord * inner
	}
	return src
}

func opTopK(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 2); err != nil {
		return nil, err
	}
	x, kT := args[0], args[1]
	if x.Rank() == 0 {
		return nil, fmt.Errorf("TopK: scalar input")
	}
	ks, err := tensorIntsArg(kT)
	if err != nil || len(ks) != 1 || ks[0] < 0 {
		return nil, fmt.Errorf("TopK: K must be one non-negative integer")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", -1), x.Rank())
	if err != nil {
		return nil, fmt.Errorf("TopK: %w", err)
	}
	k := int(ks[0])
	if k > x.Shape[axis] {
		return nil, fmt.Errorf("TopK: K %d > axis %d", k, x.Shape[axis])
	}
	largest := attrInt(n, "largest", 1) != 0
	sortedOut := attrInt(n, "sorted", 1) != 0
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	outShape := append([]int{}, x.Shape...)
	outShape[axis] = k
	values := rc.newFloat(n, 0, outShape...)
	indices := NewInt(outShape...)
	inner := numElements(x.Shape[axis+1:])
	outer := numElements(x.Shape[:axis])
	axisSize := x.Shape[axis]
	order := make([]int, axisSize)
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			for a := 0; a < axisSize; a++ {
				order[a] = a
			}
			at := func(a int) float32 {
				return xf[((o*axisSize+a)*inner + in)]
			}
			sort.SliceStable(order, func(i, j int) bool {
				vi, vj := at(order[i]), at(order[j])
				if vi == vj || (math.IsNaN(float64(vi)) && math.IsNaN(float64(vj))) {
					return order[i] < order[j]
				}
				if math.IsNaN(float64(vi)) {
					return largest
				}
				if math.IsNaN(float64(vj)) {
					return !largest
				}
				if largest {
					return vi > vj
				}
				return vi < vj
			})
			if !sortedOut {
				sort.Ints(order[:k])
			}
			for r := 0; r < k; r++ {
				dst := (o*k+r)*inner + in
				values.F32[dst] = at(order[r])
				indices.I64[dst] = int64(order[r])
			}
		}
	}
	return []*Tensor{values, indices}, nil
}

func opTrilu(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	if x.Rank() < 2 {
		return nil, fmt.Errorf("Trilu: rank %d", x.Rank())
	}
	upper := attrInt(n, "upper", 1) != 0
	k := int64(0)
	if len(args) > 1 && args[1] != nil {
		ks, err := tensorIntsArg(args[1])
		if err != nil || len(ks) != 1 {
			return nil, fmt.Errorf("Trilu: k must be a scalar")
		}
		k = ks[0]
	}
	rows, cols := x.Shape[x.Rank()-2], x.Shape[x.Rank()-1]
	batch := numElements(x.Shape[:x.Rank()-2])
	if x.DType == DInt64 {
		out := NewInt(x.Shape...)
		for b := 0; b < batch; b++ {
			base := b * rows * cols
			for i := 0; i < rows; i++ {
				for j := 0; j < cols; j++ {
					dst := base + i*cols + j
					if triluKeep(upper, i, j, k) {
						out.I64[dst] = x.I64[dst]
					} else {
						out.I64[dst] = 0
					}
				}
			}
		}
		return []*Tensor{out}, nil
	}
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	out := rc.newFloat(n, 0, x.Shape...)
	for b := 0; b < batch; b++ {
		base := b * rows * cols
		for i := 0; i < rows; i++ {
			for j := 0; j < cols; j++ {
				dst := base + i*cols + j
				if triluKeep(upper, i, j, k) {
					out.F32[dst] = xf[dst]
				} else {
					out.F32[dst] = 0
				}
			}
		}
	}
	return []*Tensor{out}, nil
}

func opArgMax(rc *runCtx, n *Node, args []*Tensor) ([]*Tensor, error) {
	if err := requireArgs(n, args, 1); err != nil {
		return nil, err
	}
	x := args[0]
	xf, err := x.Floats()
	if err != nil {
		return nil, err
	}
	rank := x.Rank()
	if rank == 0 {
		return nil, fmt.Errorf("ArgMax: scalar input")
	}
	axis, err := normalizeAxis(attrInt(n, "axis", 0), rank)
	if err != nil {
		return nil, fmt.Errorf("ArgMax: %w", err)
	}
	keepdims := attrInt(n, "keepdims", 1) != 0
	selectLast := attrInt(n, "select_last_index", 0) != 0
	axisSize := x.Shape[axis]
	if axisSize == 0 {
		return nil, fmt.Errorf("ArgMax: empty axis")
	}
	inner := numElements(x.Shape[axis+1:])
	outer := numElements(x.Shape[:axis])
	outShape := make([]int, 0, rank)
	for i, d := range x.Shape {
		if i == axis {
			if keepdims {
				outShape = append(outShape, 1)
			}
			continue
		}
		outShape = append(outShape, d)
	}
	out := NewInt(outShape...)
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			best := 0
			bestV := xf[o*axisSize*inner+in]
			for a := 1; a < axisSize; a++ {
				v := xf[(o*axisSize+a)*inner+in]
				better := v > bestV
				if selectLast {
					better = v >= bestV
				}
				if better {
					best = a
					bestV = v
				}
			}
			out.I64[o*inner+in] = int64(best)
		}
	}
	return []*Tensor{out}, nil
}

func triluKeep(upper bool, row, col int, k int64) bool {
	if upper {
		return int64(col) >= int64(row)+k
	}
	return int64(col) <= int64(row)+k
}
