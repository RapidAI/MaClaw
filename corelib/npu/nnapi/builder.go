// Package nnapi builds a whole neural graph and, on Android, compiles it for
// an NNAPI accelerator. A zero dimension is the one dynamic length shared by
// that execution (time or tokens). Per-layer launches are intentionally not
// representable: the builder records one graph, and the Android compiler
// refuses any device that is not an accelerator.
package nnapi

import (
	"fmt"
	"math"
)

// DType is the element type stored in a tensor.
type DType uint8

const (
	F32 DType = iota
	I32
)

type tensor struct {
	dt      DType
	shape   []int
	f32     []float32
	i32     []int32
	fill    func([]float32) error
	fillN   int
	input   bool
	compute bool
}

type opCode uint8

const (
	opAdd opCode = iota
	opMul
	opSub
	opFC
	opReshape
	opTranspose
	opSplit
	opBMM
	opSoftmax
	opDWConv
	opMean
	opRsqrt
	opArgMax
	opTanh
	opGather
)

type operation struct {
	code   opCode
	in     []int
	out    []int
	relu   bool
	adjX   bool
	adjY   bool
	axis   int
	shape  []int
	perm   []int
	splits int
}

// Builder records a graph. Tensor ids are dense from zero. A failed method
// sticks the first error and later methods return -1.
type Builder struct {
	tensors []tensor
	ops     []operation
	inputs  []int
	outputs []int
	err     error
	scalars map[uint32]int
}

// Err returns the first builder error.
func (b *Builder) Err() error { return b.err }

// NumOps is the number of recorded operations.
func (b *Builder) NumOps() int { return len(b.ops) }

// ConstBytes is the float and int constant payload, for the load log.
func (b *Builder) ConstBytes() int {
	n := 0
	for _, t := range b.tensors {
		if t.fillN > 0 {
			n += t.fillN * 4
			continue
		}
		n += len(t.f32)*4 + len(t.i32)*4
	}
	return n
}

func (b *Builder) fail(err error) int {
	if b.err == nil {
		b.err = err
	}
	return -1
}

func (b *Builder) add(t tensor) int {
	if b.err != nil {
		return -1
	}
	if t.f32 != nil || t.i32 != nil {
		n := 1
		for _, d := range t.shape {
			if d <= 0 {
				return b.fail(fmt.Errorf("nnapi: constant shape %v has a dynamic dim", t.shape))
			}
			n *= d
		}
		if t.f32 != nil && len(t.f32) != n {
			return b.fail(fmt.Errorf("nnapi: f32 const len %d, shape %v wants %d", len(t.f32), t.shape, n))
		}
		if t.i32 != nil && len(t.i32) != n {
			return b.fail(fmt.Errorf("nnapi: i32 const len %d, shape %v wants %d", len(t.i32), t.shape, n))
		}
	}
	b.tensors = append(b.tensors, t)
	return len(b.tensors) - 1
}

func (b *Builder) must(id int) bool {
	return b.err == nil && id >= 0 && id < len(b.tensors)
}

func (b *Builder) rank(id int) int {
	if !b.must(id) {
		return 0
	}
	return len(b.tensors[id].shape)
}

func (b *Builder) shapeOf(id int) []int {
	if !b.must(id) {
		return nil
	}
	return b.tensors[id].shape
}

// InputF32 adds a float model input. A 0 dimension is the dynamic length.
func (b *Builder) InputF32(shape []int) int {
	id := b.add(tensor{dt: F32, shape: append([]int(nil), shape...), input: true})
	if id >= 0 {
		b.inputs = append(b.inputs, id)
	}
	return id
}

// InputI32 adds an int32 model input.
func (b *Builder) InputI32(shape []int) int {
	id := b.add(tensor{dt: I32, shape: append([]int(nil), shape...), input: true})
	if id >= 0 {
		b.inputs = append(b.inputs, id)
	}
	return id
}

// ConstF32 adds a float constant. The slice is retained, not copied.
func (b *Builder) ConstF32(shape []int, data []float32) int {
	return b.add(tensor{dt: F32, shape: append([]int(nil), shape...), f32: data})
}

// ConstI32 adds an int32 constant. The slice is retained, not copied.
func (b *Builder) ConstI32(shape []int, data []int32) int {
	return b.add(tensor{dt: I32, shape: append([]int(nil), shape...), i32: data})
}

// ConstF32Fill adds a float constant produced when the graph is evaluated or
// uploaded. The callback writes straight into the destination so a large
// dequant does not also occupy a Go slice.
func (b *Builder) ConstF32Fill(shape []int, fill func([]float32) error) int {
	if fill == nil {
		return b.fail(fmt.Errorf("nnapi: nil const fill"))
	}
	n := 1
	for _, d := range shape {
		if d <= 0 {
			return b.fail(fmt.Errorf("nnapi: const fill shape %v", shape))
		}
		n *= d
	}
	return b.add(tensor{
		dt:    F32,
		shape: append([]int(nil), shape...),
		fill:  fill,
		fillN: n,
	})
}

// ScalarF32 returns a cached [1] float constant so broadcasts can reuse it.
func (b *Builder) ScalarF32(v float32) int {
	key := math.Float32bits(v)
	if b.scalars != nil {
		if id, ok := b.scalars[key]; ok {
			return id
		}
	}
	id := b.ConstF32([]int{1}, []float32{v})
	if id >= 0 {
		if b.scalars == nil {
			b.scalars = make(map[uint32]int)
		}
		b.scalars[key] = id
	}
	return id
}

// Output marks a tensor as a model output, in call order.
func (b *Builder) Output(id int) {
	if !b.must(id) {
		if b.err == nil {
			b.fail(fmt.Errorf("nnapi: bad output tensor %d", id))
		}
		return
	}
	b.outputs = append(b.outputs, id)
}

func (b *Builder) bin(code opCode, a, c int, outShape []int, dt DType) int {
	if !b.must(a) || !b.must(c) {
		return b.fail(fmt.Errorf("nnapi: bad binary inputs %d %d", a, c))
	}
	id := b.add(tensor{dt: dt, shape: outShape, compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: code, in: []int{a, c}, out: []int{id}})
	return id
}

func broadcastShape(a, c []int) ([]int, error) {
	n := len(a)
	if len(c) > n {
		n = len(c)
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		da, dc := 1, 1
		if i >= n-len(a) {
			da = a[i-(n-len(a))]
		}
		if i >= n-len(c) {
			dc = c[i-(n-len(c))]
		}
		switch {
		case da == dc:
			out[i] = da
		case da == 1:
			out[i] = dc
		case dc == 1:
			out[i] = da
		case da == 0 || dc == 0:
			// 0 is the shared dynamic length and broadcasts like a real dim.
			if da == 0 && (dc == 0 || dc == 1) {
				out[i] = 0
				break
			}
			if dc == 0 && da == 1 {
				out[i] = 0
				break
			}
			return nil, fmt.Errorf("nnapi: cannot broadcast %v and %v", a, c)
		default:
			return nil, fmt.Errorf("nnapi: cannot broadcast %v and %v", a, c)
		}
	}
	return out, nil
}

func (b *Builder) arith(code opCode, a, c int) int {
	if !b.must(a) || !b.must(c) {
		return b.fail(fmt.Errorf("nnapi: bad arithmetic inputs"))
	}
	if b.tensors[a].dt != F32 || b.tensors[c].dt != F32 {
		return b.fail(fmt.Errorf("nnapi: arithmetic requires float tensors"))
	}
	shape, err := broadcastShape(b.tensors[a].shape, b.tensors[c].shape)
	if err != nil {
		return b.fail(err)
	}
	return b.bin(code, a, c, shape, F32)
}

// Add is elementwise add with right-aligned broadcast.
func (b *Builder) Add(a, c int) int { return b.arith(opAdd, a, c) }

// Mul is elementwise multiply with right-aligned broadcast.
func (b *Builder) Mul(a, c int) int { return b.arith(opMul, a, c) }

// Sub is elementwise a - c with right-aligned broadcast.
func (b *Builder) Sub(a, c int) int { return b.arith(opSub, a, c) }

// FullyConnected is y = x · wᵀ + bias, optionally ReLU.
// w is [units, in], bias is [units] or -1 for zeros, x's last dim is in.
func (b *Builder) FullyConnected(x, w, bias int, relu bool) int {
	if !b.must(x) || !b.must(w) {
		return b.fail(fmt.Errorf("nnapi: bad fully-connected inputs"))
	}
	ws := b.tensors[w].shape
	if len(ws) != 2 {
		return b.fail(fmt.Errorf("nnapi: weight rank %d, want 2", len(ws)))
	}
	xs := b.tensors[x].shape
	if len(xs) < 1 || (xs[len(xs)-1] != 0 && xs[len(xs)-1] != ws[1]) {
		return b.fail(fmt.Errorf("nnapi: fully-connected k %v vs weight %v", xs, ws))
	}
	units := ws[0]
	if bias < 0 {
		bias = b.ConstF32([]int{units}, make([]float32, units))
	}
	if !b.must(bias) {
		return -1
	}
	if bs := b.tensors[bias].shape; len(bs) != 1 || bs[0] != units {
		return b.fail(fmt.Errorf("nnapi: bias shape %v, want [%d]", bs, units))
	}
	outShape := append([]int(nil), xs...)
	outShape[len(outShape)-1] = units
	id := b.add(tensor{dt: F32, shape: outShape, compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opFC, in: []int{x, w, bias}, out: []int{id}, relu: relu})
	return id
}

// Reshape changes the view. A -1 dimension is inferred; the tensor metadata
// stores that axis as 0 (the shared dynamic length).
func (b *Builder) Reshape(x int, shape []int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad reshape input"))
	}
	meta := make([]int, len(shape))
	infer := 0
	for i, d := range shape {
		if d < 0 {
			infer++
			meta[i] = 0
		} else {
			meta[i] = d
		}
	}
	if infer > 1 {
		return b.fail(fmt.Errorf("nnapi: reshape %v has more than one inferred dim", shape))
	}
	id := b.add(tensor{dt: b.tensors[x].dt, shape: meta, compute: true})
	if id < 0 {
		return id
	}
	spec := append([]int(nil), shape...)
	cst := b.ConstI32([]int{len(spec)}, intsToI32(spec))
	if cst < 0 {
		return cst
	}
	b.ops = append(b.ops, operation{code: opReshape, in: []int{x, cst}, out: []int{id}, shape: spec})
	return id
}

// Transpose permutes axes.
func (b *Builder) Transpose(x int, perm []int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad transpose input"))
	}
	src := b.tensors[x].shape
	if len(perm) != len(src) {
		return b.fail(fmt.Errorf("nnapi: transpose perm %v rank %d", perm, len(src)))
	}
	out := make([]int, len(src))
	seen := make([]bool, len(src))
	for i, p := range perm {
		if p < 0 || p >= len(src) || seen[p] {
			return b.fail(fmt.Errorf("nnapi: bad transpose perm %v", perm))
		}
		seen[p] = true
		out[i] = src[p]
	}
	id := b.add(tensor{dt: b.tensors[x].dt, shape: out, compute: true})
	if id < 0 {
		return id
	}
	cst := b.ConstI32([]int{len(perm)}, intsToI32(perm))
	if cst < 0 {
		return cst
	}
	b.ops = append(b.ops, operation{code: opTranspose, in: []int{x, cst}, out: []int{id}, perm: append([]int(nil), perm...)})
	return id
}

// SplitLast splits the last axis into n equal parts.
func (b *Builder) SplitLast(x, n int) []int {
	if !b.must(x) || n <= 0 {
		b.fail(fmt.Errorf("nnapi: bad split"))
		return nil
	}
	src := b.tensors[x].shape
	if len(src) == 0 {
		b.fail(fmt.Errorf("nnapi: split scalar"))
		return nil
	}
	last := src[len(src)-1]
	if last <= 0 || last%n != 0 {
		b.fail(fmt.Errorf("nnapi: split last dim %d by %d", last, n))
		return nil
	}
	part := append([]int(nil), src...)
	part[len(part)-1] = last / n
	outs := make([]int, n)
	for i := 0; i < n; i++ {
		outs[i] = b.add(tensor{dt: b.tensors[x].dt, shape: append([]int(nil), part...), compute: true})
		if outs[i] < 0 {
			return nil
		}
	}
	b.ops = append(b.ops, operation{code: opSplit, in: []int{x}, out: outs, axis: len(src) - 1, splits: n})
	return outs
}

// BatchMatMul multiplies the last two axes. adjY transposes the right matrix.
func (b *Builder) BatchMatMul(a, c int, adjX, adjY bool) int {
	if !b.must(a) || !b.must(c) {
		return b.fail(fmt.Errorf("nnapi: bad batch matmul"))
	}
	as, cs := b.tensors[a].shape, b.tensors[c].shape
	if len(as) < 2 || len(cs) < 2 {
		return b.fail(fmt.Errorf("nnapi: batch matmul rank %v %v", as, cs))
	}
	aRow, aCol := as[len(as)-2], as[len(as)-1]
	cRow, cCol := cs[len(cs)-2], cs[len(cs)-1]
	if adjX {
		aRow, aCol = aCol, aRow
	}
	if adjY {
		cRow, cCol = cCol, cRow
	}
	if aCol != cRow && aCol != 0 && cRow != 0 {
		return b.fail(fmt.Errorf("nnapi: batch matmul k %d vs %d", aCol, cRow))
	}
	batch, err := broadcastShape(as[:len(as)-2], cs[:len(cs)-2])
	if err != nil {
		return b.fail(err)
	}
	out := append(batch, aRow, cCol)
	id := b.add(tensor{dt: F32, shape: out, compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opBMM, in: []int{a, c}, out: []int{id}, adjX: adjX, adjY: adjY})
	return id
}

// Softmax normalizes along axis. A negative axis counts from the end.
func (b *Builder) Softmax(x, axis int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad softmax"))
	}
	id := b.add(tensor{dt: F32, shape: append([]int(nil), b.tensors[x].shape...), compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opSoftmax, in: []int{x}, out: []int{id}, axis: axis})
	return id
}

// DepthwiseSame is a stride-1 SAME depthwise conv. x and the filter are NHWC,
// filter [1, kernel, 1, channels]. Bias is [channels].
func (b *Builder) DepthwiseSame(x, filter, bias int) int {
	if !b.must(x) || !b.must(filter) || !b.must(bias) {
		return b.fail(fmt.Errorf("nnapi: bad depthwise conv"))
	}
	xs, fs, bs := b.tensors[x].shape, b.tensors[filter].shape, b.tensors[bias].shape
	if len(xs) != 4 || len(fs) != 4 || fs[0] != 1 || fs[2] != 1 || fs[3] <= 0 {
		return b.fail(fmt.Errorf("nnapi: depthwise layout x %v filter %v", xs, fs))
	}
	if xs[3] != 0 && xs[3] != fs[3] {
		return b.fail(fmt.Errorf("nnapi: depthwise channels x %v filter %v", xs, fs))
	}
	if len(bs) != 1 || bs[0] != fs[3] {
		return b.fail(fmt.Errorf("nnapi: depthwise bias %v want [%d]", bs, fs[3]))
	}
	id := b.add(tensor{dt: F32, shape: append([]int(nil), b.tensors[x].shape...), compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opDWConv, in: []int{x, filter, bias}, out: []int{id}})
	return id
}

// Mean reduces axis and keeps it as length 1.
func (b *Builder) Mean(x, axis int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad mean"))
	}
	src := b.tensors[x].shape
	ax, err := resolveAxis(axis, len(src))
	if err != nil {
		return b.fail(err)
	}
	out := append([]int(nil), src...)
	out[ax] = 1
	id := b.add(tensor{dt: F32, shape: out, compute: true})
	if id < 0 {
		return id
	}
	axisT := b.ConstI32([]int{1}, []int32{int32(ax)})
	if axisT < 0 {
		return axisT
	}
	b.ops = append(b.ops, operation{code: opMean, in: []int{x, axisT}, out: []int{id}, axis: ax})
	return id
}

// Rsqrt is 1/sqrt(x), elementwise.
func (b *Builder) Rsqrt(x int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad rsqrt"))
	}
	id := b.add(tensor{dt: F32, shape: append([]int(nil), b.tensors[x].shape...), compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opRsqrt, in: []int{x}, out: []int{id}})
	return id
}

// ArgMax returns int32 indices. The reduced axis is removed.
func (b *Builder) ArgMax(x, axis int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad argmax"))
	}
	src := b.tensors[x].shape
	ax, err := resolveAxis(axis, len(src))
	if err != nil {
		return b.fail(err)
	}
	out := make([]int, 0, len(src)-1)
	for i, d := range src {
		if i != ax {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		out = []int{1}
	}
	id := b.add(tensor{dt: I32, shape: out, compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opArgMax, in: []int{x}, out: []int{id}, axis: ax})
	return id
}

// Tanh is elementwise.
func (b *Builder) Tanh(x int) int {
	if !b.must(x) {
		return b.fail(fmt.Errorf("nnapi: bad tanh"))
	}
	id := b.add(tensor{dt: F32, shape: append([]int(nil), b.tensors[x].shape...), compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opTanh, in: []int{x}, out: []int{id}})
	return id
}

// Gather indexes params along axis. params [V, D] and indices [T] at axis 0
// produce [T, D].
func (b *Builder) Gather(params, indices, axis int) int {
	if !b.must(params) || !b.must(indices) {
		return b.fail(fmt.Errorf("nnapi: bad gather"))
	}
	ps, is := b.tensors[params].shape, b.tensors[indices].shape
	ax, err := resolveAxis(axis, len(ps))
	if err != nil {
		return b.fail(err)
	}
	out := make([]int, 0, len(ps)+len(is)-1)
	out = append(out, ps[:ax]...)
	out = append(out, is...)
	out = append(out, ps[ax+1:]...)
	id := b.add(tensor{dt: b.tensors[params].dt, shape: out, compute: true})
	if id < 0 {
		return id
	}
	b.ops = append(b.ops, operation{code: opGather, in: []int{params, indices}, out: []int{id}, axis: ax})
	return id
}

func resolveAxis(axis, rank int) (int, error) {
	if rank <= 0 {
		return 0, fmt.Errorf("nnapi: axis on rank 0")
	}
	if axis < 0 {
		axis += rank
	}
	if axis < 0 || axis >= rank {
		return 0, fmt.Errorf("nnapi: axis %d rank %d", axis, rank)
	}
	return axis, nil
}

func intsToI32(v []int) []int32 {
	out := make([]int32, len(v))
	for i, d := range v {
		out[i] = int32(d)
	}
	return out
}
