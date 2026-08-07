package main

// Random multi-node DAG generator (round 4 fuzz extension). Builds graphs of
// 3-8 nodes mixing conv, pool, elementwise unary, broadcast binary,
// activation, reshape, transpose, concat, slice, matmul, softmax and
// reducemean, tracking exact shapes so every edge is valid. Intermediate
// values may feed two consumers (shared-consumer paths the fuse pass must
// respect), fusion-target patterns (Conv->act, Conv->BN, conv-bias-add,
// pre-conv affine chains, exact/near-miss GELU erf chains, h-swish, Identity
// noise) are injected on a deterministic rotation, and random intermediates
// are promoted to graph outputs to exercise the graph-output guards.

import (
	"fmt"
	"math"
	"math/rand"
)

type liveVal struct {
	name  string
	shape []int
}

type dagGen struct {
	rng       *rand.Rand
	nodes     []node
	inits     []tensorInit
	inputs    []valueInfo
	td        map[string]tensorData
	live      []liveVal // every value produced so far stays consumable
	valN      int
	initN     int
	extraOuts []string
}

func (d *dagGen) pick() liveVal { return d.live[d.rng.Intn(len(d.live))] }

func (d *dagGen) pick4D() (liveVal, bool) {
	var idx []int
	for i, v := range d.live {
		if len(v.shape) == 4 {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return liveVal{}, false
	}
	return d.live[idx[d.rng.Intn(len(idx))]], true
}

func (d *dagGen) emit(op string, ins []string, attrs []attr, shape []int) liveVal {
	out := fmt.Sprintf("v%d", d.valN)
	d.valN++
	d.nodes = append(d.nodes, node{op: op, inputs: ins, outs: []string{out}, attrs: attrs})
	lv := liveVal{out, shape}
	d.live = append(d.live, lv)
	return lv
}

func (d *dagGen) wInitN(prefix string, shape []int64) string {
	name := fmt.Sprintf("%s%d", prefix, d.initN)
	d.initN++
	d.inits = append(d.inits, wInit(name, shape, d.rng))
	return name
}

func (d *dagGen) fInitN(prefix string, shape []int64, data []float32) string {
	name := fmt.Sprintf("%s%d", prefix, d.initN)
	d.initN++
	d.inits = append(d.inits, tensorInit{name: name, shape: shape, dtype: 1, f32: data})
	return name
}

func (d *dagGen) iInitN(prefix string, vals ...int64) string {
	name := fmt.Sprintf("%s%d", prefix, d.initN)
	d.initN++
	d.inits = append(d.inits, iInit(name, vals...))
	return name
}

// markOutput promotes an intermediate value to a graph output. Fusion passes
// must refuse to rewrite a producer whose raw output is graph-visible.
func (d *dagGen) markOutput(name string) {
	for _, o := range d.extraOuts {
		if o == name {
			return
		}
	}
	d.extraOuts = append(d.extraOuts, name)
}

// bcastResult computes the NumPy-broadcast result shape, or reports
// incompatibility.
func bcastResult(a, b []int) ([]int, bool) {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		da, db := 1, 1
		if i < len(a) {
			da = a[len(a)-1-i]
		}
		if i < len(b) {
			db = b[len(b)-1-i]
		}
		if da != db && da != 1 && db != 1 {
			return nil, false
		}
		out[n-1-i] = da
		if db > da {
			out[n-1-i] = db
		}
	}
	return out, true
}

func concatCompat(a, b []int, axis int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if i != axis && a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// generic op steps
// ---------------------------------------------------------------------------

func (d *dagGen) stepUnary() bool {
	v := d.pick()
	switch d.rng.Intn(4) {
	case 0:
		d.emit("Relu", []string{v.name}, nil, v.shape)
	case 1:
		d.emit("Sigmoid", []string{v.name}, nil, v.shape)
	case 2:
		d.emit("Erf", []string{v.name}, nil, v.shape)
	case 3:
		d.emit("HardSigmoid", []string{v.name},
			[]attr{attrF("alpha", 0.1+d.rng.Float32()*0.4), attrF("beta", d.rng.Float32())}, v.shape)
	}
	return true
}

func (d *dagGen) stepIdentity() bool {
	v := d.pick()
	d.emit("Identity", []string{v.name}, nil, v.shape)
	return true
}

func (d *dagGen) stepBinary() bool {
	op := []string{"Add", "Sub", "Mul", "Add", "Mul", "Div"}[d.rng.Intn(6)]
	a := d.pick()
	var bShape []int
	bName := ""
	if op != "Div" && d.rng.Intn(2) == 0 {
		// live-value operand; occasionally the SAME value (shared consumer)
		for try := 0; try < 5; try++ {
			b := d.pick()
			if d.rng.Intn(4) == 0 {
				b = a
			}
			if _, ok := bcastResult(a.shape, b.shape); ok {
				bShape, bName = b.shape, b.name
				break
			}
		}
	}
	if bName == "" {
		// constant operand with a broadcastable shape (rank >= 1)
		bShape = bcastShape(d.rng, a.shape)
		data := randF32(d.rng, numEl(bShape))
		for i := range data {
			data[i] *= 0.5
			if op == "Div" { // keep the divisor well away from 0
				if data[i] < 0 {
					data[i] -= 0.5
				} else {
					data[i] += 0.5
				}
			}
		}
		bName = d.fInitN("c", toI64(bShape), data)
	}
	outShape, ok := bcastResult(a.shape, bShape)
	if !ok {
		return false
	}
	ins := []string{a.name, bName}
	if op != "Div" && d.rng.Intn(2) == 0 {
		ins[0], ins[1] = ins[1], ins[0]
	}
	d.emit(op, ins, nil, outShape)
	return true
}

// emitConv appends a Conv node consuming src (must be 4D) and returns the
// output value and its channel count.
func (d *dagGen) emitConv(src liveVal) (liveVal, int, bool) {
	C := src.shape[1]
	H, W := src.shape[2], src.shape[3]
	divs := divisors(C)
	group := divs[d.rng.Intn(len(divs))]
	if d.rng.Intn(3) != 0 {
		group = 1
	}
	M := group * (1 + d.rng.Intn(3))
	kH := 1 + d.rng.Intn(min(3, H))
	kW := 1 + d.rng.Intn(min(3, W))
	sH, sW := 1+d.rng.Intn(2), 1+d.rng.Intn(2)
	var pads [4]int
	oH, oW := 0, 0
	for try := 0; try < 16; try++ {
		pads = [4]int{d.rng.Intn(3), d.rng.Intn(3), d.rng.Intn(3), d.rng.Intn(3)}
		oH = (H+pads[0]+pads[2]-kH)/sH + 1
		oW = (W+pads[1]+pads[3]-kW)/sW + 1
		if oH >= 1 && oW >= 1 {
			break
		}
	}
	if oH < 1 || oW < 1 {
		return liveVal{}, 0, false
	}
	attrs := []attr{attrIs("kernel_shape", int64(kH), int64(kW))}
	if sH > 1 || sW > 1 {
		attrs = append(attrs, attrIs("strides", int64(sH), int64(sW)))
	}
	if group > 1 {
		attrs = append(attrs, attrI("group", int64(group)))
	}
	if pads != [4]int{} {
		attrs = append(attrs, attrIs("pads", int64(pads[0]), int64(pads[1]), int64(pads[2]), int64(pads[3])))
	}
	ins := []string{src.name, d.wInitN("w", []int64{int64(M), int64(C / group), int64(kH), int64(kW)})}
	if d.rng.Intn(2) == 0 {
		ins = append(ins, d.wInitN("b", []int64{int64(M)}))
	}
	return d.emit("Conv", ins, attrs, []int{src.shape[0], M, oH, oW}), M, true
}

func (d *dagGen) stepConv() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	_, _, ok = d.emitConv(v)
	return ok
}

func (d *dagGen) stepPool() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	H, W := v.shape[2], v.shape[3]
	op := "MaxPool"
	if d.rng.Intn(2) == 0 {
		op = "AveragePool"
	}
	kH := 1 + d.rng.Intn(min(3, H))
	kW := 1 + d.rng.Intn(min(3, W))
	sH, sW := 1+d.rng.Intn(2), 1+d.rng.Intn(2)
	var pads [4]int
	oH, oW := 0, 0
	for try := 0; try < 16; try++ {
		pads = [4]int{d.rng.Intn(min(kH, 3)), d.rng.Intn(min(kW, 3)), d.rng.Intn(min(kH, 3)), d.rng.Intn(min(kW, 3))}
		oH = (H+pads[0]+pads[2]-kH)/sH + 1
		oW = (W+pads[1]+pads[3]-kW)/sW + 1
		if oH >= 1 && oW >= 1 {
			break
		}
	}
	if oH < 1 || oW < 1 {
		return false
	}
	attrs := []attr{attrIs("kernel_shape", int64(kH), int64(kW))}
	if sH > 1 || sW > 1 {
		attrs = append(attrs, attrIs("strides", int64(sH), int64(sW)))
	}
	if pads != [4]int{} {
		attrs = append(attrs, attrIs("pads", int64(pads[0]), int64(pads[1]), int64(pads[2]), int64(pads[3])))
	}
	if op == "AveragePool" && d.rng.Intn(3) == 0 {
		attrs = append(attrs, attrI("count_include_pad", 1))
	}
	d.emit(op, []string{v.name}, attrs, []int{v.shape[0], v.shape[1], oH, oW})
	return true
}

func (d *dagGen) stepReshape() bool {
	v := d.pick()
	total := numEl(v.shape)
	divs := divisors(total)
	d1 := divs[d.rng.Intn(len(divs))]
	rest := total / d1
	var tgt []int64
	if rest > 1 && d.rng.Intn(2) == 0 {
		d2s := divisors(rest)
		d2 := d2s[d.rng.Intn(len(d2s))]
		tgt = []int64{int64(d1), int64(d2), int64(rest / d2)}
	} else {
		tgt = []int64{int64(d1), int64(rest)}
	}
	neg := -1
	prod := 1
	if d.rng.Intn(2) == 0 {
		neg = d.rng.Intn(len(tgt))
		tgt[neg] = -1
	}
	res := make([]int, len(tgt))
	for i, t := range tgt {
		if t == -1 {
			res[i] = 1
		} else {
			res[i] = int(t)
			prod *= int(t)
		}
	}
	if neg >= 0 {
		res[neg] = total / prod
	}
	sh := d.iInitN("shape", tgt...)
	d.emit("Reshape", []string{v.name, sh}, nil, res)
	return true
}

func (d *dagGen) stepTranspose() bool {
	v := d.pick()
	rank := len(v.shape)
	res := make([]int, rank)
	if d.rng.Intn(3) == 0 { // default: reversed axes
		for i := 0; i < rank; i++ {
			res[i] = v.shape[rank-1-i]
		}
		d.emit("Transpose", []string{v.name}, nil, res)
		return true
	}
	perm := d.rng.Perm(rank)
	for i, p := range perm {
		res[i] = v.shape[p]
	}
	d.emit("Transpose", []string{v.name}, []attr{attrIs("perm", toI64(perm)...)}, res)
	return true
}

func (d *dagGen) stepConcat() bool {
	v := d.pick()
	rank := len(v.shape)
	axis := d.rng.Intn(rank)
	ins := []string{v.name}
	outShape := append([]int(nil), v.shape...)
	nIn := 2 + d.rng.Intn(2) // 2-3 inputs
	for len(ins) < nIn {
		var other *liveVal
		for try := 0; try < 6; try++ {
			cand := d.pick()
			if concatCompat(v.shape, cand.shape, axis) {
				other = &cand
				break
			}
		}
		var ov liveVal
		if other != nil {
			ov = *other
		} else {
			// derive a same-shape value through a unary op; this also gives
			// v a second consumer
			ov = d.emit("Relu", []string{v.name}, nil, v.shape)
		}
		ins = append(ins, ov.name)
		outShape[axis] += ov.shape[axis]
	}
	d.emit("Concat", ins, []attr{attrI("axis", int64(axis))}, outShape)
	return true
}

func (d *dagGen) stepSlice() bool {
	v := d.pick()
	rank := len(v.shape)
	nAx := 1 + d.rng.Intn(min(2, rank))
	axes := d.rng.Perm(rank)[:nAx]
	var starts, ends, steps []int64
	outShape := append([]int(nil), v.shape...)
	for _, a := range axes {
		dim := v.shape[a]
		if dim < 2 { // only the full extent is non-degenerate
			starts = append(starts, 0)
			ends = append(ends, int64(dim))
			steps = append(steps, 1)
			continue
		}
		switch d.rng.Intn(4) {
		case 1: // stride 2 forward
			s := d.rng.Intn(dim)
			e := s + 1 + d.rng.Intn(dim-s)
			starts = append(starts, int64(s))
			ends = append(ends, int64(e))
			steps = append(steps, 2)
			outShape[a] = (e - s + 1) / 2
		case 2: // step -1, explicit in-range indices
			s := 1 + d.rng.Intn(dim-1)
			e := d.rng.Intn(s)
			starts = append(starts, int64(s))
			ends = append(ends, int64(e))
			steps = append(steps, -1)
			outShape[a] = s - e
		default: // contiguous forward range
			s := d.rng.Intn(dim)
			e := s + 1 + d.rng.Intn(dim-s)
			starts = append(starts, int64(s))
			ends = append(ends, int64(e))
			steps = append(steps, 1)
			outShape[a] = e - s
		}
	}
	sn := d.iInitN("starts", starts...)
	en := d.iInitN("ends", ends...)
	an := d.iInitN("axes", toI64(axes)...)
	st := d.iInitN("steps", steps...)
	d.emit("Slice", []string{v.name, sn, en, an, st}, nil, outShape)
	return true
}

func (d *dagGen) stepMatmul() bool {
	v := d.pick()
	k := v.shape[len(v.shape)-1]
	m := numEl(v.shape) / k
	src := v
	if len(v.shape) != 2 { // flatten to 2D first (adds a reshape to the DAG)
		sh := d.iInitN("shape", int64(m), int64(k))
		src = d.emit("Reshape", []string{v.name, sh}, nil, []int{m, k})
	}
	n := 1 + d.rng.Intn(5)
	w := d.wInitN("mw", []int64{int64(k), int64(n)})
	d.emit("MatMul", []string{src.name, w}, nil, []int{m, n})
	return true
}

func (d *dagGen) stepSoftmax() bool {
	v := d.pick()
	rank := len(v.shape)
	axis := d.rng.Intn(rank)
	a := axis
	if d.rng.Intn(3) == 0 {
		a = axis - rank // negative form
	}
	d.emit("Softmax", []string{v.name}, []attr{attrI("axis", int64(a))}, v.shape)
	return true
}

func (d *dagGen) stepReduceMean() bool {
	v := d.pick()
	rank := len(v.shape)
	keepdims := d.rng.Intn(2)
	var attrs []attr
	outShape := append([]int(nil), v.shape...)
	if d.rng.Intn(4) == 0 { // omit axes: reduce all
		if keepdims == 0 {
			outShape = []int{}
		} else {
			for i := range outShape {
				outShape[i] = 1
			}
		}
	} else {
		nAx := 1 + d.rng.Intn(rank)
		perm := d.rng.Perm(rank)[:nAx]
		dropped := map[int]bool{}
		axes := make([]int64, nAx)
		for j, a := range perm {
			if d.rng.Intn(2) == 0 {
				axes[j] = int64(a - rank)
			} else {
				axes[j] = int64(a)
			}
			dropped[a] = true
		}
		attrs = append(attrs, attrIs("axes", axes...))
		if keepdims == 1 {
			for a := range dropped {
				outShape[a] = 1
			}
		} else {
			var ns []int
			for i, dd := range v.shape {
				if !dropped[i] {
					ns = append(ns, dd)
				}
			}
			outShape = ns
		}
	}
	attrs = append(attrs, attrI("keepdims", int64(keepdims)))
	d.emit("ReduceMean", []string{v.name}, attrs, outShape)
	return true
}

// ---------------------------------------------------------------------------
// fusion-target motifs
// ---------------------------------------------------------------------------

// motifConvAct: Conv -> Relu/HardSigmoid/Sigmoid (epilogue fusion).
func (d *dagGen) motifConvAct() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cv, _, ok := d.emitConv(v)
	if !ok {
		return false
	}
	if d.rng.Intn(3) == 0 {
		d.markOutput(cv.name) // raw conv output stays graph-visible: no fusion
	}
	switch d.rng.Intn(3) {
	case 0:
		d.emit("Relu", []string{cv.name}, nil, cv.shape)
	case 1:
		d.emit("HardSigmoid", []string{cv.name},
			[]attr{attrF("alpha", 0.2), attrF("beta", 0.5)}, cv.shape)
	case 2:
		d.emit("Sigmoid", []string{cv.name}, nil, cv.shape)
	}
	return true
}

// motifConvBN: Conv -> BatchNormalization (inference BN folding).
func (d *dagGen) motifConvBN() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cv, M, ok := d.emitConv(v)
	if !ok {
		return false
	}
	if d.rng.Intn(3) == 0 {
		d.markOutput(cv.name)
	}
	sc := d.wInitN("scale", []int64{int64(M)})
	bi := d.wInitN("bias", []int64{int64(M)})
	me := d.wInitN("mean", []int64{int64(M)})
	varData := make([]float32, M)
	for j := range varData {
		varData[j] = 0.1 + d.rng.Float32()*2
	}
	va := d.fInitN("var", []int64{int64(M)}, varData)
	eps := float32(1e-5 * math.Pow(10, float64(d.rng.Intn(3))))
	d.emit("BatchNormalization", []string{cv.name, sc, bi, me, va},
		[]attr{attrF("epsilon", eps)}, cv.shape)
	return true
}

// motifConvBiasAdd: Conv -> Add/Sub with a scalar or per-channel constant
// (conv-bias folding).
func (d *dagGen) motifConvBiasAdd() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cv, M, ok := d.emitConv(v)
	if !ok {
		return false
	}
	if d.rng.Intn(3) == 0 {
		d.markOutput(cv.name)
	}
	var cn string
	if d.rng.Intn(2) == 0 {
		cn = d.fInitN("c", []int64{1}, randF32(d.rng, 1)) // scalar
	} else {
		shape := []int64{1, int64(M), 1, 1} // per-output-channel vector
		data := randF32(d.rng, M)
		cn = d.fInitN("c", shape, data)
	}
	op := "Add"
	if d.rng.Intn(3) == 0 {
		op = "Sub"
	}
	d.emit(op, []string{cv.name, cn}, nil, cv.shape)
	return true
}

// motifPreConvAffine: 1-3 scalar affine elementwise ops feeding a Conv
// (pre-conv norm folding).
func (d *dagGen) motifPreConvAffine() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cur := v
	n := 1 + d.rng.Intn(3)
	for i := 0; i < n; i++ {
		k := (d.rng.Float32()*2 - 1)
		op := []string{"Mul", "Add", "Sub", "Mul", "Add", "Div"}[d.rng.Intn(6)]
		if op == "Div" { // x / k only, k away from 0 (k / x is not affine)
			if k < 0 {
				k -= 0.5
			} else {
				k += 0.5
			}
		}
		kn := d.fInitN("k", []int64{1}, []float32{k})
		ins := []string{cur.name, kn}
		if op == "Sub" && d.rng.Intn(3) == 0 {
			ins[0], ins[1] = ins[1], ins[0] // k - x: reversed affine form
		}
		cur = d.emit(op, ins, nil, v.shape)
		if d.rng.Intn(4) == 0 {
			d.markOutput(cur.name) // blocks the fold at this point
		}
	}
	_, _, ok = d.emitConv(cur)
	return ok
}

// motifGELU: exact GELU erf chain on a conv output (shared consumer: the
// conv output feeds both the Div and the outer Mul). Occasionally a
// near-miss constant that must defeat the pattern match.
func (d *dagGen) motifGELU() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cv, _, ok := d.emitConv(v)
	if !ok {
		return false
	}
	if d.rng.Intn(4) == 0 {
		d.markOutput(cv.name)
	}
	k2 := d.fInitN("sqrt2", []int64{1}, []float32{float32(math.Sqrt2)})
	dv := d.emit("Div", []string{cv.name, k2}, nil, cv.shape)
	er := d.emit("Erf", []string{dv.name}, nil, cv.shape)
	one := d.fInitN("one", []int64{1}, []float32{1})
	ad := d.emit("Add", []string{er.name, one}, nil, cv.shape)
	mu := d.emit("Mul", []string{cv.name, ad.name}, nil, cv.shape)
	half := float32(0.5)
	if d.rng.Intn(4) == 0 {
		half = 0.5 + 0.001*(d.rng.Float32()+0.1) // near-miss: must NOT fuse
	}
	hf := d.fInitN("half", []int64{1}, []float32{half})
	d.emit("Mul", []string{mu.name, hf}, nil, cv.shape)
	return true
}

// motifHSwish: conv output feeding both HardSigmoid and Mul (h-swish).
func (d *dagGen) motifHSwish() bool {
	v, ok := d.pick4D()
	if !ok {
		return false
	}
	cv, _, ok := d.emitConv(v)
	if !ok {
		return false
	}
	if d.rng.Intn(4) == 0 {
		d.markOutput(cv.name)
	}
	hs := d.emit("HardSigmoid", []string{cv.name},
		[]attr{attrF("alpha", 1.0/6.0), attrF("beta", 0.5)}, cv.shape)
	d.emit("Mul", []string{cv.name, hs.name}, nil, cv.shape)
	return true
}

// ---------------------------------------------------------------------------
// driver
// ---------------------------------------------------------------------------

func genDAGCases(rng *rand.Rand, add func(string, modelDef, map[string]tensorData)) {
	const total = 84
	motifs := []func(*dagGen) bool{
		(*dagGen).motifConvAct,
		(*dagGen).motifConvBN,
		(*dagGen).motifConvBiasAdd,
		(*dagGen).motifPreConvAffine,
		(*dagGen).motifGELU,
		(*dagGen).motifHSwish,
	}
	steps := []func(*dagGen) bool{
		(*dagGen).stepUnary,
		(*dagGen).stepUnary,
		(*dagGen).stepBinary,
		(*dagGen).stepBinary,
		(*dagGen).stepConv,
		(*dagGen).stepPool,
		(*dagGen).stepReshape,
		(*dagGen).stepTranspose,
		(*dagGen).stepConcat,
		(*dagGen).stepSlice,
		(*dagGen).stepMatmul,
		(*dagGen).stepSoftmax,
		(*dagGen).stepReduceMean,
		(*dagGen).stepIdentity,
	}
	for i := 0; i < total; i++ {
		d := &dagGen{rng: rng, td: map[string]tensorData{}}
		x0 := []int{1 + rng.Intn(2), 1 + rng.Intn(4), 3 + rng.Intn(6), 3 + rng.Intn(6)}
		d.inputs = append(d.inputs, valueInfo{name: "x0", dtype: 1, shape: toI64(x0)})
		d.td["x0"] = f32td(x0, randF32(rng, numEl(x0)))
		d.live = append(d.live, liveVal{"x0", x0})
		if rng.Intn(3) == 0 { // occasional second graph input
			s1 := randShape(rng, 1+rng.Intn(3), 1, 5)
			d.inputs = append(d.inputs, valueInfo{name: "x1", dtype: 1, shape: toI64(s1)})
			d.td["x1"] = f32td(s1, randF32(rng, numEl(s1)))
			d.live = append(d.live, liveVal{"x1", s1})
		}
		target := 3 + rng.Intn(6)
		// deterministic motif rotation guarantees every fusion-target
		// pattern is covered even if random steps never produce one
		motifs[i%len(motifs)](d)
		for tries := 0; len(d.nodes) < target && tries < 40; tries++ {
			steps[rng.Intn(len(steps))](d)
		}
		// graph outputs: the most recent value, any marked intermediates,
		// and sometimes one other random live value
		outs := []string{d.live[len(d.live)-1].name}
		outs = append(outs, d.extraOuts...)
		if rng.Intn(3) == 0 && len(d.live) > 1 {
			outs = append(outs, d.live[rng.Intn(len(d.live)-1)].name)
		}
		seen := map[string]bool{}
		shapeOf := map[string][]int{}
		for _, lv := range d.live {
			shapeOf[lv.name] = lv.shape
		}
		var outVIs []valueInfo
		for _, nm := range outs {
			if seen[nm] {
				continue
			}
			seen[nm] = true
			// Declare the real tracked shape: a present-but-empty shape proto
			// means rank-0 scalar (not "unknown"), and for an intermediate
			// that is ALSO consumed downstream onnxruntime's shape inference
			// hard-fails on the rank conflict.
			outVIs = append(outVIs, valueInfo{name: nm, dtype: 1, shape: toI64(shapeOf[nm])})
		}
		add("graph", modelDef{
			opset:   13,
			nodes:   d.nodes,
			inits:   d.inits,
			inputs:  d.inputs,
			outputs: outVIs,
		}, d.td)
	}
}
