package nnapi

import (
	"fmt"
	"math"
)

// ExecIn is one concrete model input. Shape replaces every dynamic 0.
type ExecIn struct {
	F32   []float32
	I32   []int32
	Shape []int
}

// ExecOut is one model output in Builder.Output order.
type ExecOut struct {
	F32 []float32
	I32 []int32
}

// Eval runs the graph on the CPU. Tests use it to check a lowering against
// the existing kernels. Android execution goes through Session instead.
func Eval(b *Builder, ins []ExecIn) ([]ExecOut, error) {
	if b == nil {
		return nil, fmt.Errorf("nnapi: nil graph")
	}
	if b.err != nil {
		return nil, b.err
	}
	if len(ins) != len(b.inputs) {
		return nil, fmt.Errorf("nnapi: %d inputs, graph wants %d", len(ins), len(b.inputs))
	}
	dyn, err := dynamicLength(b, ins)
	if err != nil {
		return nil, err
	}
	vals := make([]value, len(b.tensors))
	for i, in := range ins {
		id := b.inputs[i]
		shape := concrete(b.tensors[id].shape, dyn)
		if product(shape) != len(in.F32)+len(in.I32) {
			return nil, fmt.Errorf("nnapi: input %d len %d shape %v", i, len(in.F32)+len(in.I32), shape)
		}
		vals[id] = value{f32: in.F32, i32: in.I32, shape: shape}
	}
	for i, t := range b.tensors {
		if t.input || t.compute {
			continue
		}
		if t.fill != nil {
			buf := make([]float32, t.fillN)
			if err := t.fill(buf); err != nil {
				return nil, fmt.Errorf("nnapi: const %d: %w", i, err)
			}
			vals[i] = value{f32: buf, shape: append([]int(nil), t.shape...)}
			continue
		}
		vals[i] = value{f32: t.f32, i32: t.i32, shape: append([]int(nil), t.shape...)}
	}
	for oi, op := range b.ops {
		if err := evalOp(op, vals); err != nil {
			return nil, fmt.Errorf("nnapi: op %d: %w", oi, err)
		}
	}
	outs := make([]ExecOut, len(b.outputs))
	for i, id := range b.outputs {
		v := vals[id]
		if b.tensors[id].dt == I32 {
			outs[i].I32 = append([]int32(nil), v.i32...)
		} else {
			outs[i].F32 = append([]float32(nil), v.f32...)
		}
	}
	return outs, nil
}

type value struct {
	f32   []float32
	i32   []int32
	shape []int
}

func dynamicLength(b *Builder, ins []ExecIn) (int, error) {
	specs := make([][]int, len(b.inputs))
	for i, id := range b.inputs {
		specs[i] = b.tensors[id].shape
	}
	return dynamicLengthShapes(specs, ins)
}

func dynamicLengthShapes(specs [][]int, ins []ExecIn) (int, error) {
	dyn := -1
	for i, in := range ins {
		spec := specs[i]
		if len(in.Shape) != len(spec) {
			return 0, fmt.Errorf("nnapi: input %d rank %d, graph %d", i, len(in.Shape), len(spec))
		}
		fixed := 1
		var got int
		for d, specD := range spec {
			if in.Shape[d] <= 0 {
				return 0, fmt.Errorf("nnapi: input %d dim %d is %d", i, d, in.Shape[d])
			}
			if specD == 0 {
				if got != 0 && got != in.Shape[d] {
					return 0, fmt.Errorf("nnapi: input %d has two dynamic lengths", i)
				}
				got = in.Shape[d]
			} else if specD != in.Shape[d] {
				return 0, fmt.Errorf("nnapi: input %d dim %d is %d, graph %d", i, d, in.Shape[d], specD)
			} else {
				fixed *= specD
			}
		}
		n := len(in.F32) + len(in.I32)
		if got == 0 {
			if n != fixed {
				return 0, fmt.Errorf("nnapi: input %d len %d, shape %v", i, n, in.Shape)
			}
			continue
		}
		if fixed == 0 || n/fixed != got || n != got*fixed {
			return 0, fmt.Errorf("nnapi: input %d len %d does not match %v", i, n, in.Shape)
		}
		if dyn >= 0 && dyn != got {
			return 0, fmt.Errorf("nnapi: dynamic length %d and %d", dyn, got)
		}
		dyn = got
	}
	if dyn < 0 {
		dyn = 1
	}
	return dyn, nil
}

func concrete(shape []int, dyn int) []int {
	out := make([]int, len(shape))
	for i, d := range shape {
		if d == 0 {
			out[i] = dyn
		} else {
			out[i] = d
		}
	}
	return out
}

func product(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

func evalOp(op operation, vals []value) error {
	switch op.code {
	case opAdd, opMul, opSub:
		return evalArith(op, vals)
	case opFC:
		return evalFC(op, vals)
	case opReshape:
		return evalReshape(op, vals)
	case opTranspose:
		return evalTranspose(op, vals)
	case opSplit:
		return evalSplit(op, vals)
	case opBMM:
		return evalBMM(op, vals)
	case opSoftmax:
		return evalSoftmax(op, vals)
	case opDWConv:
		return evalDW(op, vals)
	case opMean:
		return evalMean(op, vals)
	case opRsqrt:
		return evalRsqrt(op, vals)
	case opArgMax:
		return evalArgMax(op, vals)
	case opTanh:
		return evalTanh(op, vals)
	case opGather:
		return evalGather(op, vals)
	default:
		return fmt.Errorf("unknown op %d", op.code)
	}
}

func evalArith(op operation, vals []value) error {
	a, c := vals[op.in[0]], vals[op.in[1]]
	shape, err := broadcastShape(a.shape, c.shape)
	if err != nil {
		return err
	}
	shape = concrete(shape, 1)
	out := make([]float32, product(shape))
	an, cn := product(a.shape), product(c.shape)
	idx := make([]int, len(shape))
	var walk func(d int)
	walk = func(d int) {
		if d == len(shape) {
			ia := broadcastIndex(a.shape, shape, idx)
			ic := broadcastIndex(c.shape, shape, idx)
			io := flatIndex(shape, idx)
			av, cv := a.f32[ia%an], c.f32[ic%cn]
			switch op.code {
			case opAdd:
				out[io] = av + cv
			case opSub:
				out[io] = av - cv
			default:
				out[io] = av * cv
			}
			return
		}
		for i := 0; i < shape[d]; i++ {
			idx[d] = i
			walk(d + 1)
		}
	}
	if len(shape) == 0 {
		return fmt.Errorf("scalar arithmetic")
	}
	walk(0)
	vals[op.out[0]] = value{f32: out, shape: shape}
	return nil
}

func broadcastIndex(src, dst, idx []int) int {
	base := len(dst) - len(src)
	out := 0
	for i, n := range src {
		j := idx[base+i]
		if n == 1 {
			j = 0
		}
		out = out*n + j
	}
	return out
}

func flatIndex(shape, idx []int) int {
	out := 0
	for i, n := range shape {
		out = out*n + idx[i]
	}
	return out
}

func evalFC(op operation, vals []value) error {
	x, w, bias := vals[op.in[0]], vals[op.in[1]], vals[op.in[2]]
	if len(w.shape) != 2 {
		return fmt.Errorf("weight rank %d", len(w.shape))
	}
	units, k := w.shape[0], w.shape[1]
	if len(x.shape) == 0 || x.shape[len(x.shape)-1] != k {
		return fmt.Errorf("fc shape %v weight %v", x.shape, w.shape)
	}
	m := product(x.shape) / k
	outShape := append([]int(nil), x.shape...)
	outShape[len(outShape)-1] = units
	out := make([]float32, m*units)
	for row := 0; row < m; row++ {
		xv := x.f32[row*k : (row+1)*k]
		for u := 0; u < units; u++ {
			sum := bias.f32[u]
			wv := w.f32[u*k : (u+1)*k]
			for i := 0; i < k; i++ {
				sum += xv[i] * wv[i]
			}
			if op.relu && sum < 0 {
				sum = 0
			}
			out[row*units+u] = sum
		}
	}
	vals[op.out[0]] = value{f32: out, shape: outShape}
	return nil
}

func evalReshape(op operation, vals []value) error {
	src := vals[op.in[0]]
	n := product(src.shape)
	outShape := make([]int, len(op.shape))
	infer := -1
	known := 1
	for i, d := range op.shape {
		if d < 0 {
			infer = i
			continue
		}
		outShape[i] = d
		known *= d
	}
	if infer >= 0 {
		if known == 0 || n%known != 0 {
			return fmt.Errorf("reshape %v of %d", op.shape, n)
		}
		outShape[infer] = n / known
	}
	if product(outShape) != n {
		return fmt.Errorf("reshape %v product %d, have %d", outShape, product(outShape), n)
	}
	vals[op.out[0]] = value{f32: src.f32, i32: src.i32, shape: outShape}
	return nil
}

func evalTranspose(op operation, vals []value) error {
	src := vals[op.in[0]]
	outShape := make([]int, len(op.perm))
	for i, p := range op.perm {
		outShape[i] = src.shape[p]
	}
	n := product(outShape)
	dstF := make([]float32, 0)
	dstI := make([]int32, 0)
	if src.f32 != nil {
		dstF = make([]float32, n)
	} else {
		dstI = make([]int32, n)
	}
	idx := make([]int, len(src.shape))
	var walk func(d int)
	walk = func(d int) {
		if d == len(src.shape) {
			from := flatIndex(src.shape, idx)
			toIdx := make([]int, len(op.perm))
			for i, p := range op.perm {
				toIdx[i] = idx[p]
			}
			to := flatIndex(outShape, toIdx)
			if src.f32 != nil {
				dstF[to] = src.f32[from]
			} else {
				dstI[to] = src.i32[from]
			}
			return
		}
		for i := 0; i < src.shape[d]; i++ {
			idx[d] = i
			walk(d + 1)
		}
	}
	walk(0)
	vals[op.out[0]] = value{f32: dstF, i32: dstI, shape: outShape}
	return nil
}

func evalSplit(op operation, vals []value) error {
	src := vals[op.in[0]]
	axis := op.axis
	if src.shape[axis]%op.splits != 0 {
		return fmt.Errorf("split %v by %d", src.shape, op.splits)
	}
	part := src.shape[axis] / op.splits
	stride := 1
	for i := axis + 1; i < len(src.shape); i++ {
		stride *= src.shape[i]
	}
	outer := 1
	for i := 0; i < axis; i++ {
		outer *= src.shape[i]
	}
	outShape := append([]int(nil), src.shape...)
	outShape[axis] = part
	chunk := part * stride
	for s := 0; s < op.splits; s++ {
		dst := make([]float32, product(outShape))
		for o := 0; o < outer; o++ {
			base := o * src.shape[axis] * stride
			copy(dst[o*chunk:(o+1)*chunk], src.f32[base+s*chunk:base+(s+1)*chunk])
		}
		vals[op.out[s]] = value{f32: dst, shape: append([]int(nil), outShape...)}
	}
	return nil
}

func evalBMM(op operation, vals []value) error {
	a, c := vals[op.in[0]], vals[op.in[1]]
	aShape := append([]int(nil), a.shape...)
	cShape := append([]int(nil), c.shape...)
	if op.adjX {
		aShape[len(aShape)-2], aShape[len(aShape)-1] = aShape[len(aShape)-1], aShape[len(aShape)-2]
	}
	if op.adjY {
		cShape[len(cShape)-2], cShape[len(cShape)-1] = cShape[len(cShape)-1], cShape[len(cShape)-2]
	}
	if aShape[len(aShape)-1] != cShape[len(cShape)-2] {
		return fmt.Errorf("bmm k %v %v", a.shape, c.shape)
	}
	batchShape, err := broadcastShape(a.shape[:len(a.shape)-2], c.shape[:len(c.shape)-2])
	if err != nil {
		return err
	}
	m, k, n := aShape[len(aShape)-2], aShape[len(aShape)-1], cShape[len(cShape)-1]
	batch := product(batchShape)
	out := make([]float32, batch*m*n)
	aBatch := product(a.shape[:len(a.shape)-2])
	cBatch := product(c.shape[:len(c.shape)-2])
	for b := 0; b < batch; b++ {
		ab := b % aBatch
		cb := b % cBatch
		for i := 0; i < m; i++ {
			for j := 0; j < n; j++ {
				var sum float32
				for t := 0; t < k; t++ {
					sum += aAt(a, ab, i, t, m, k, op.adjX) * bAt(c, cb, t, j, k, n, op.adjY)
				}
				out[(b*m+i)*n+j] = sum
			}
		}
	}
	outShape := append(append([]int(nil), batchShape...), m, n)
	vals[op.out[0]] = value{f32: out, shape: outShape}
	return nil
}

func aAt(v value, batch, row, col, m, k int, adj bool) float32 {
	if adj {
		// stored [batch, k, m], logical [batch, m, k]
		return v.f32[(batch*k+col)*m+row]
	}
	return v.f32[(batch*m+row)*k+col]
}

func bAt(v value, batch, row, col, k, n int, adj bool) float32 {
	if adj {
		// stored [batch, n, k], logical [batch, k, n]; row is k, col is n
		return v.f32[(batch*n+col)*k+row]
	}
	return v.f32[(batch*k+row)*n+col]
}

func evalSoftmax(op operation, vals []value) error {
	src := vals[op.in[0]]
	axis, err := resolveAxis(op.axis, len(src.shape))
	if err != nil {
		return err
	}
	inner := 1
	for i := axis + 1; i < len(src.shape); i++ {
		inner *= src.shape[i]
	}
	axisN := src.shape[axis]
	outer := product(src.shape) / (axisN * inner)
	out := make([]float32, len(src.f32))
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			base := (o*axisN)*inner + in
			max := src.f32[base]
			for a := 1; a < axisN; a++ {
				v := src.f32[base+a*inner]
				if v > max {
					max = v
				}
			}
			sum := float32(0)
			for a := 0; a < axisN; a++ {
				e := float32(math.Exp(float64(src.f32[base+a*inner] - max)))
				out[base+a*inner] = e
				sum += e
			}
			inv := float32(1)
			if sum != 0 {
				inv = 1 / sum
			}
			for a := 0; a < axisN; a++ {
				out[base+a*inner] *= inv
			}
		}
	}
	vals[op.out[0]] = value{f32: out, shape: append([]int(nil), src.shape...)}
	return nil
}

func evalDW(op operation, vals []value) error {
	x, filter, bias := vals[op.in[0]], vals[op.in[1]], vals[op.in[2]]
	if len(x.shape) != 4 || len(filter.shape) != 4 {
		return fmt.Errorf("depthwise ranks %v %v", x.shape, filter.shape)
	}
	if x.shape[0] != 1 || x.shape[2] != 1 || filter.shape[0] != 1 || filter.shape[2] != 1 {
		return fmt.Errorf("depthwise layout %v filter %v", x.shape, filter.shape)
	}
	time, channels := x.shape[1], x.shape[3]
	k := filter.shape[1]
	if filter.shape[3] != channels || len(bias.f32) != channels {
		return fmt.Errorf("depthwise channels")
	}
	out := make([]float32, time*channels)
	padTotal := k - 1
	padLeft := padTotal / 2
	for t := 0; t < time; t++ {
		for c := 0; c < channels; c++ {
			sum := bias.f32[c]
			for ki := 0; ki < k; ki++ {
				src := t + ki - padLeft
				if src < 0 || src >= time {
					continue
				}
				sum += x.f32[src*channels+c] * filter.f32[ki*channels+c]
			}
			out[t*channels+c] = sum
		}
	}
	vals[op.out[0]] = value{f32: out, shape: append([]int(nil), x.shape...)}
	return nil
}

func evalMean(op operation, vals []value) error {
	src := vals[op.in[0]]
	axis := op.axis
	inner := 1
	for i := axis + 1; i < len(src.shape); i++ {
		inner *= src.shape[i]
	}
	axisN := src.shape[axis]
	outer := product(src.shape) / (axisN * inner)
	outShape := append([]int(nil), src.shape...)
	outShape[axis] = 1
	out := make([]float32, product(outShape))
	inv := 1 / float32(axisN)
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			sum := float32(0)
			base := o*axisN*inner + in
			for a := 0; a < axisN; a++ {
				sum += src.f32[base+a*inner]
			}
			out[o*inner+in] = sum * inv
		}
	}
	vals[op.out[0]] = value{f32: out, shape: outShape}
	return nil
}

func evalRsqrt(op operation, vals []value) error {
	src := vals[op.in[0]]
	out := make([]float32, len(src.f32))
	for i, v := range src.f32 {
		out[i] = float32(1 / math.Sqrt(float64(v)))
	}
	vals[op.out[0]] = value{f32: out, shape: append([]int(nil), src.shape...)}
	return nil
}

func evalArgMax(op operation, vals []value) error {
	src := vals[op.in[0]]
	axis := op.axis
	inner := 1
	for i := axis + 1; i < len(src.shape); i++ {
		inner *= src.shape[i]
	}
	axisN := src.shape[axis]
	outer := product(src.shape) / (axisN * inner)
	out := make([]int32, outer*inner)
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			base := o*axisN*inner + in
			best, bi := src.f32[base], 0
			for a := 1; a < axisN; a++ {
				v := src.f32[base+a*inner]
				if v > best {
					best, bi = v, a
				}
			}
			out[o*inner+in] = int32(bi)
		}
	}
	outShape := make([]int, 0, len(src.shape)-1)
	for i, d := range src.shape {
		if i != axis {
			outShape = append(outShape, d)
		}
	}
	vals[op.out[0]] = value{i32: out, shape: outShape}
	return nil
}

func evalTanh(op operation, vals []value) error {
	src := vals[op.in[0]]
	out := make([]float32, len(src.f32))
	for i, v := range src.f32 {
		out[i] = float32(math.Tanh(float64(v)))
	}
	vals[op.out[0]] = value{f32: out, shape: append([]int(nil), src.shape...)}
	return nil
}

func evalGather(op operation, vals []value) error {
	params, idx := vals[op.in[0]], vals[op.in[1]]
	axis := op.axis
	inner := 1
	for i := axis + 1; i < len(params.shape); i++ {
		inner *= params.shape[i]
	}
	axisN := params.shape[axis]
	outer := 1
	for i := 0; i < axis; i++ {
		outer *= params.shape[i]
	}
	nIdx := product(idx.shape)
	out := make([]float32, outer*nIdx*inner)
	for o := 0; o < outer; o++ {
		for j := 0; j < nIdx; j++ {
			at := int(idx.i32[j])
			if at < 0 || at >= axisN {
				return fmt.Errorf("gather index %d of %d", at, axisN)
			}
			src := (o*axisN + at) * inner
			dst := (o*nIdx + j) * inner
			copy(out[dst:dst+inner], params.f32[src:src+inner])
		}
	}
	outShape := make([]int, 0, len(params.shape)+len(idx.shape)-1)
	outShape = append(outShape, params.shape[:axis]...)
	outShape = append(outShape, idx.shape...)
	outShape = append(outShape, params.shape[axis+1:]...)
	vals[op.out[0]] = value{f32: out, shape: outShape}
	return nil
}
