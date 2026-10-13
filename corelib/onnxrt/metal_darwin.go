//go:build darwin

package onnxrt

import (
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/mtl"
)

// Metal backend for onnxrt. GPU-capable nodes are encoded into one open
// command buffer and their outputs live in shared (unified-memory) Metal
// buffers, so consecutive GPU ops never round-trip through Go memory. Before
// any CPU op the pending work is committed and waited for; the CPU op then
// reads the shared buffers in place. Unsupported ops/shapes fall back per op.

type gbuf struct {
	b    *mtl.Buffer
	base uintptr
	size int
	refs int
}

type gconv struct {
	dw    bool
	w     *mtl.Buffer
	bias  *mtl.Buffer
	Kpad  int
	nbias bool
}

type gpuGraph struct {
	mu      sync.Mutex
	dev     *mtl.Device
	pipe    map[string]*mtl.Pipeline
	consts  map[uintptr]*mtl.Buffer // uploaded initializers by data pointer
	convs   map[*Node]*gconv
	free    map[int][]*gbuf // pooled buffers by size class
	lastUse map[string]int
	dummy   *mtl.Buffer
}

var (
	onnxGPUOnce sync.Once
	onnxGPUDev  *mtl.Device
	onnxGPUPipe map[string]*mtl.Pipeline
	onnxGPUErr  error
)

func onnxMetalDisabled() bool {
	return os.Getenv("MACLAW_ONNXRT_METAL") == "0" || os.Getenv("MACLAW_METAL") == "0"
}

func onnxMetalInit() (*mtl.Device, map[string]*mtl.Pipeline, error) {
	onnxGPUOnce.Do(func() {
		dev, err := mtl.NewDevice()
		if err != nil {
			onnxGPUErr = err
			return
		}
		p, err := dev.CompileOpts(onnxMetalSrc, []string{"conv_gemm", "conv_dw", "binary4", "unary", "gap"}, false)
		if err != nil {
			onnxGPUErr = err
			return
		}
		onnxGPUDev, onnxGPUPipe = dev, p
		if err := onnxMetalSelfCheck(); err != nil {
			onnxGPUErr = fmt.Errorf("onnxrt metal self-check: %w", err)
			onnxGPUDev, onnxGPUPipe = nil, nil
		}
	})
	return onnxGPUDev, onnxGPUPipe, onnxGPUErr
}

// gpuFor returns the graph's GPU state, or nil when Metal is off/unavailable.
func (g *Graph) gpuFor() *gpuGraph {
	if onnxMetalDisabled() {
		return nil
	}
	g.gpuOnce.Do(func() {
		dev, pipe, err := onnxMetalInit()
		if err != nil {
			if os.Getenv("ONNXRT_METAL_DEBUG") != "" {
				fmt.Fprintln(os.Stderr, "onnxrt: metal disabled:", err)
			}
			return
		}
		gg := &gpuGraph{dev: dev, pipe: pipe, consts: map[uintptr]*mtl.Buffer{},
			convs: map[*Node]*gconv{}, free: map[int][]*gbuf{}, lastUse: map[string]int{}}
		for i, nd := range g.nodes {
			for _, in := range nd.Inputs {
				gg.lastUse[in] = i
			}
		}
		for _, o := range g.outputNames {
			gg.lastUse[o] = math.MaxInt
		}
		d, err := dev.NewBuffer(16)
		if err != nil {
			return
		}
		gg.dummy = d
		g.gpu = gg
	})
	return g.gpu
}

type gpuRun struct {
	gg      *gpuGraph
	g       *Graph
	enc     *mtl.Encoder
	live    []*gbuf
	stage   []*gbuf // staging copies, recycled only after a flush
	byVal   map[string]*gbuf
	pending int
}

func (g *Graph) gpuStart() *gpuRun {
	gg := g.gpuFor()
	if gg == nil {
		return nil
	}
	gg.mu.Lock()
	return &gpuRun{gg: gg, g: g, byVal: map[string]*gbuf{}}
}

func sizeClass(n int) int {
	c := 4096
	for c < n {
		c <<= 1
	}
	return c
}

func (r *gpuRun) alloc(nfloat int) (*gbuf, error) {
	need := nfloat * 4
	if need == 0 {
		need = 4
	}
	sc := sizeClass(need)
	if l := r.gg.free[sc]; len(l) > 0 {
		b := l[len(l)-1]
		r.gg.free[sc] = l[:len(l)-1]
		b.refs = 0
		return b, nil
	}
	mb, err := r.gg.dev.NewBuffer(sc)
	if err != nil {
		return nil, err
	}
	return &gbuf{b: mb, base: uintptr(unsafe.Pointer(&mb.Bytes()[0])), size: sc}, nil
}

func (r *gpuRun) newOut(shape ...int) (*Tensor, *gbuf, error) {
	n := 1
	for _, d := range shape {
		n *= d
	}
	b, err := r.alloc(n)
	if err != nil {
		return nil, nil, err
	}
	r.live = append(r.live, b)
	f := b.b.Float32s()[:n]
	return &Tensor{Shape: append([]int(nil), shape...), DType: DFloat32, F32: f}, b, nil
}

// find returns the live GPU buffer holding p, if any.
func (r *gpuRun) find(f []float32) (*gbuf, int) {
	if len(f) == 0 {
		return nil, 0
	}
	p := uintptr(unsafe.Pointer(&f[0]))
	for _, b := range r.live {
		if p >= b.base && p < b.base+uintptr(b.size) {
			return b, int(p - b.base)
		}
	}
	return nil, 0
}

// bufOf returns a Metal buffer + byte offset holding t's data, uploading
// constants once and staging other host tensors.
func (r *gpuRun) bufOf(t *Tensor, isConst bool) (*mtl.Buffer, int, error) {
	if b, off := r.find(t.F32); b != nil {
		return b.b, off, nil
	}
	if len(t.F32) == 0 {
		return r.gg.dummy, 0, nil
	}
	p := uintptr(unsafe.Pointer(&t.F32[0]))
	if isConst {
		if b, ok := r.gg.consts[p]; ok {
			return b, 0, nil
		}
		mb, err := r.gg.dev.NewBuffer(len(t.F32) * 4)
		if err != nil {
			return nil, 0, err
		}
		copy(mb.Float32s(), t.F32)
		r.gg.consts[p] = mb
		return mb, 0, nil
	}
	b, err := r.alloc(len(t.F32))
	if err != nil {
		return nil, 0, err
	}
	copy(b.b.Float32s()[:len(t.F32)], t.F32)
	r.stage = append(r.stage, b)
	return b.b, 0, nil
}

func (r *gpuRun) encoder() *mtl.Encoder {
	if r.enc == nil {
		r.enc = r.gg.dev.Begin()
	}
	r.pending++
	return r.enc
}

func (r *gpuRun) flush() error {
	if r.enc == nil {
		return nil
	}
	err := r.enc.Wait()
	r.enc = nil
	r.pending = 0
	for _, b := range r.stage {
		r.gg.free[b.size] = append(r.gg.free[b.size], b)
	}
	r.stage = r.stage[:0]
	return err
}

// track registers node outputs that live in GPU buffers (GPU op outputs and
// CPU views aliasing them) so liveness can recycle the buffers.
func (r *gpuRun) track(nd *Node, outs []*Tensor) {
	for i, name := range nd.Outputs {
		if name == "" || i >= len(outs) || outs[i] == nil || outs[i].DType != DFloat32 {
			continue
		}
		if b, _ := r.find(outs[i].F32); b != nil {
			b.refs++
			r.byVal[name] = b
		}
	}
}

// dead drops references of values whose last use is nodeIdx.
func (r *gpuRun) dead(nodeIdx int, nd *Node) {
	drop := func(name string) {
		b, ok := r.byVal[name]
		if !ok || r.gg.lastUse[name] > nodeIdx {
			return
		}
		delete(r.byVal, name)
		b.refs--
		if b.refs <= 0 {
			r.release(b)
		}
	}
	for _, in := range nd.Inputs {
		drop(in)
	}
	for _, o := range nd.Outputs {
		if _, used := r.gg.lastUse[o]; !used {
			drop(o)
		}
	}
}

func (r *gpuRun) release(b *gbuf) {
	for i, l := range r.live {
		if l == b {
			r.live = append(r.live[:i], r.live[i+1:]...)
			break
		}
	}
	// Serial dispatch order makes reuse by later GPU writes safe; CPU-side
	// staging copies only use buffers recycled after a flush (see flush).
	if r.enc != nil {
		r.stage = append(r.stage, b)
		return
	}
	r.gg.free[b.size] = append(r.gg.free[b.size], b)
}

// finish copies GPU-backed results to Go memory and recycles everything.
func (r *gpuRun) finish(result map[string]*Tensor) error {
	err := r.flush()
	for k, t := range result {
		if t != nil && t.DType == DFloat32 {
			if b, _ := r.find(t.F32); b != nil {
				c := *t
				c.F32 = append([]float32(nil), t.F32...)
				c.Shape = append([]int(nil), t.Shape...)
				result[k] = &c
			}
		}
	}
	r.abort()
	return err
}

// abort releases all buffers (error path too) and unlocks the graph.
func (r *gpuRun) abort() {
	if r.enc != nil {
		r.enc.Wait()
		r.enc = nil
	}
	for _, b := range r.live {
		r.gg.free[b.size] = append(r.gg.free[b.size], b)
	}
	for _, b := range r.stage {
		r.gg.free[b.size] = append(r.gg.free[b.size], b)
	}
	r.live, r.stage = nil, nil
	r.gg.trim(1 << 30)
	r.gg.mu.Unlock()
}

// trim keeps the pooled bytes under limit, freeing the largest first.
func (gg *gpuGraph) trim(limit int) {
	total := 0
	var cls []int
	for c, l := range gg.free {
		total += c * len(l)
		cls = append(cls, c)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(cls)))
	for _, c := range cls {
		for total > limit && len(gg.free[c]) > 0 {
			l := gg.free[c]
			l[len(l)-1].b.Release()
			gg.free[c] = l[:len(l)-1]
			total -= c
		}
	}
}

func (r *gpuRun) isConst(name string) bool {
	_, ok := r.g.initializers[name]
	return ok
}

// tryOp runs nd on the GPU when supported. ok=false means "use the CPU".
func (r *gpuRun) tryOp(rc *runCtx, nd *Node, args []*Tensor) ([]*Tensor, bool, error) {
	for _, a := range args {
		if a != nil && a.DType != DFloat32 {
			return nil, false, nil
		}
	}
	switch nd.OpType {
	case "Conv":
		return r.conv(rc, nd, args)
	case "Add", "Sub", "Mul", "Div":
		return r.binary(nd, args)
	case "Relu", "Sigmoid", "HardSigmoid", "Sqrt", "Erf":
		return r.unary(nd, args)
	case "GlobalAveragePool":
		return r.gap(nd, args)
	}
	return nil, false, nil
}

type convPGPU struct {
	N, C, H, W, M, Cg, Mg, group   int32
	kH, kW, sH, sW, pT, pL, dH, dW int32
	oH, oW, K, Kpad                int32
	epi, hasBias                   int32
	alpha, beta                    float32
}

func (r *gpuRun) conv(rc *runCtx, nd *Node, args []*Tensor) ([]*Tensor, bool, error) {
	if len(args) < 2 || args[0] == nil || args[1] == nil || args[0].Rank() != 4 || args[1].Rank() != 4 || !r.isConst(nd.Inputs[1]) {
		return nil, false, nil
	}
	if len(args) > 2 && args[2] != nil && len(args[2].F32) > 0 && !r.isConst(nd.Inputs[2]) {
		return nil, false, nil
	}
	x, w := args[0], args[1]
	p, err := parseConvParams(nd, w.Shape, false)
	if err != nil {
		return nil, false, nil
	}
	autoPad := attrStr(nd, "auto_pad", "NOTSET")
	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	M, Cg := w.Shape[0], w.Shape[1]
	if autoPad == "SAME_UPPER" || autoPad == "SAME_LOWER" {
		p.pads[0], p.pads[2] = convAutoPads(autoPad, H, p.kH, p.strides[0], p.dilations[0])
		p.pads[1], p.pads[3] = convAutoPads(autoPad, W, p.kW, p.strides[1], p.dilations[1])
	}
	if p.group <= 0 || C%p.group != 0 || M%p.group != 0 || Cg*p.group != C {
		return nil, false, nil
	}
	oH := (H+p.pads[0]+p.pads[2]-p.dilations[0]*(p.kH-1)-1)/p.strides[0] + 1
	oW := (W+p.pads[1]+p.pads[3]-p.dilations[1]*(p.kW-1)-1)/p.strides[1] + 1
	if oH <= 0 || oW <= 0 {
		return nil, false, nil
	}
	dw := p.group == C && Cg == 1 && C > 1 && M == C
	K := Cg * p.kH * p.kW
	gc, ok := r.gg.convs[nd]
	if !ok {
		gc = &gconv{dw: dw}
		if dw {
			mb, err := r.gg.dev.NewBuffer(len(w.F32) * 4)
			if err != nil {
				return nil, false, err
			}
			copy(mb.Float32s(), w.F32)
			gc.w = mb
		} else {
			gc.Kpad = (K + 31) / 32 * 32
			mb, err := r.gg.dev.NewBuffer(M * gc.Kpad * 4)
			if err != nil {
				return nil, false, err
			}
			dst := mb.Float32s()
			for m := 0; m < M; m++ {
				copy(dst[m*gc.Kpad:m*gc.Kpad+K], w.F32[m*K:(m+1)*K])
			}
			gc.w = mb
		}
		if len(args) > 2 && args[2] != nil && len(args[2].F32) == M {
			mb, err := r.gg.dev.NewBuffer(M * 4)
			if err != nil {
				return nil, false, err
			}
			copy(mb.Float32s(), args[2].F32)
			gc.bias = mb
		}
		r.gg.convs[nd] = gc
	}
	if gc.dw != dw {
		return nil, false, nil
	}
	xb, xoff, err := r.bufOf(x, false)
	if err != nil {
		return nil, false, err
	}
	out, ob, err := r.newOut(N, M, oH, oW)
	if err != nil {
		return nil, false, err
	}
	cp := convPGPU{N: int32(N), C: int32(C), H: int32(H), W: int32(W), M: int32(M), Cg: int32(Cg), Mg: int32(M / p.group), group: int32(p.group),
		kH: int32(p.kH), kW: int32(p.kW), sH: int32(p.strides[0]), sW: int32(p.strides[1]), pT: int32(p.pads[0]), pL: int32(p.pads[1]),
		dH: int32(p.dilations[0]), dW: int32(p.dilations[1]), oH: int32(oH), oW: int32(oW), K: int32(K), Kpad: int32(gc.Kpad)}
	if e := epilogueFor(rc, nd); e != nil {
		cp.epi, cp.alpha, cp.beta = int32(e.kind), e.alpha, e.beta
	}
	bias := r.gg.dummy
	if gc.bias != nil {
		bias, cp.hasBias = gc.bias, 1
	}
	enc := r.encoder()
	enc.SetBuffer(xb, xoff, 0)
	enc.SetBuffer(gc.w, 0, 1)
	enc.SetBuffer(bias, 0, 2)
	enc.SetBuffer(ob.b, 0, 3)
	enc.SetBytes(unsafe.Pointer(&cp), int(unsafe.Sizeof(cp)), 4)
	if dw {
		enc.SetPipeline(r.gg.pipe["conv_dw"])
		tot := N * C * oH * oW
		enc.Dispatch((tot+255)/256, 1, 1, 256, 1, 1)
	} else {
		enc.SetPipeline(r.gg.pipe["conv_gemm"])
		enc.Dispatch((oH*oW+63)/64, (M/p.group+31)/32, N*p.group, 128, 1, 1)
	}
	return []*Tensor{out}, true, nil
}

type binPGPU struct {
	n, op      int32
	od, as, bs [4]int32
}

func (r *gpuRun) binary(nd *Node, args []*Tensor) ([]*Tensor, bool, error) {
	if len(args) != 2 || args[0] == nil || args[1] == nil {
		return nil, false, nil
	}
	a, b := args[0], args[1]
	shape, err := broadcastShapes(a.Shape, b.Shape)
	if err != nil || len(shape) > 4 || len(a.F32) == 0 || len(b.F32) == 0 {
		return nil, false, nil
	}
	n := numElements(shape)
	if n < 1 {
		return nil, false, nil
	}
	sa, sb := broadcastStrides(a.Shape, shape), broadcastStrides(b.Shape, shape)
	var bp binPGPU
	bp.n = int32(n)
	bp.op = map[string]int32{"Add": 0, "Sub": 1, "Mul": 2, "Div": 3}[nd.OpType]
	off := 4 - len(shape)
	for i := 0; i < 4; i++ {
		bp.od[i] = 1
	}
	for i := range shape {
		bp.od[off+i], bp.as[off+i], bp.bs[off+i] = int32(shape[i]), int32(sa[i]), int32(sb[i])
	}
	ab, aoff, err := r.bufOf(a, r.isConst(nd.Inputs[0]))
	if err != nil {
		return nil, false, err
	}
	bb, boff, err := r.bufOf(b, r.isConst(nd.Inputs[1]))
	if err != nil {
		return nil, false, err
	}
	out, ob, err := r.newOut(shape...)
	if err != nil {
		return nil, false, err
	}
	enc := r.encoder()
	enc.SetPipeline(r.gg.pipe["binary4"])
	enc.SetBuffer(ab, aoff, 0)
	enc.SetBuffer(bb, boff, 1)
	enc.SetBuffer(ob.b, 0, 2)
	enc.SetBytes(unsafe.Pointer(&bp), int(unsafe.Sizeof(bp)), 3)
	enc.Dispatch((n+255)/256, 1, 1, 256, 1, 1)
	return []*Tensor{out}, true, nil
}

type unPGPU struct {
	n, op       int32
	alpha, beta float32
}

func (r *gpuRun) unary(nd *Node, args []*Tensor) ([]*Tensor, bool, error) {
	if len(args) < 1 || args[0] == nil || len(args[0].F32) == 0 {
		return nil, false, nil
	}
	x := args[0]
	up := unPGPU{n: int32(len(x.F32))}
	switch nd.OpType {
	case "Relu":
		up.op = 0
	case "Sigmoid":
		up.op = 1
	case "HardSigmoid":
		up.op, up.alpha, up.beta = 2, attrFloat(nd, "alpha", 0.2), attrFloat(nd, "beta", 0.5)
	case "Sqrt":
		up.op = 3
	case "Erf":
		up.op = 4
	}
	xb, xoff, err := r.bufOf(x, r.isConst(nd.Inputs[0]))
	if err != nil {
		return nil, false, err
	}
	out, ob, err := r.newOut(x.Shape...)
	if err != nil {
		return nil, false, err
	}
	enc := r.encoder()
	enc.SetPipeline(r.gg.pipe["unary"])
	enc.SetBuffer(xb, xoff, 0)
	enc.SetBuffer(ob.b, 0, 1)
	enc.SetBytes(unsafe.Pointer(&up), int(unsafe.Sizeof(up)), 2)
	n := len(x.F32)
	enc.Dispatch((n+255)/256, 1, 1, 256, 1, 1)
	return []*Tensor{out}, true, nil
}

func (r *gpuRun) gap(nd *Node, args []*Tensor) ([]*Tensor, bool, error) {
	x := args[0]
	if x == nil || x.Rank() < 3 || len(x.F32) == 0 {
		return nil, false, nil
	}
	N, C := x.Shape[0], x.Shape[1]
	hw := int32(numElements(x.Shape[2:]))
	shape := []int{N, C}
	for range x.Shape[2:] {
		shape = append(shape, 1)
	}
	xb, xoff, err := r.bufOf(x, r.isConst(nd.Inputs[0]))
	if err != nil {
		return nil, false, err
	}
	out, ob, err := r.newOut(shape...)
	if err != nil {
		return nil, false, err
	}
	enc := r.encoder()
	enc.SetPipeline(r.gg.pipe["gap"])
	enc.SetBuffer(xb, xoff, 0)
	enc.SetBuffer(ob.b, 0, 1)
	enc.SetBytes(unsafe.Pointer(&hw), 4, 2)
	enc.Dispatch(N*C, 1, 1, 256, 1, 1)
	return []*Tensor{out}, true, nil
}

// onnxMetalSelfCheck runs a small conv (+GELU), a grouped conv, a depthwise
// conv and a broadcast Add on GPU and CPU and compares.
func onnxMetalSelfCheck() error {
	rng := uint32(12345)
	rnd := func(n int) []float32 {
		v := make([]float32, n)
		for i := range v {
			rng = rng*1664525 + 1013904223
			v[i] = float32(int32(rng>>8)%2000)/1000 - 1
		}
		return v
	}
	type tc struct {
		C, M, group, k, s, pad int
		epi                    EpilogueKind
	}
	cases := []tc{{C: 13, M: 37, group: 1, k: 3, s: 2, pad: 1, epi: EpiGELU},
		{C: 8, M: 8, group: 4, k: 3, s: 1, pad: 1, epi: EpiHSwish},
		{C: 16, M: 16, group: 16, k: 5, s: 1, pad: 2, epi: EpiRelu},
		{C: 40, M: 70, group: 1, k: 1, s: 1, pad: 0, epi: EpiSigmoid}}
	for ci, c := range cases {
		H, W := 11, 19
		g := &Graph{initializers: map[string]*Tensor{}, epilogues: map[*Node]*epilogue{}}
		nd := &Node{OpType: "Conv", Name: "selfcheck", Inputs: []string{"x", "w", "b"}, Outputs: []string{"y"},
			Attrs: map[string]Attr{
				"group":   {Name: "group", I: int64(c.group)},
				"strides": {Name: "strides", IntVals: []int64{int64(c.s), int64(c.s)}},
				"pads":    {Name: "pads", IntVals: []int64{int64(c.pad), int64(c.pad), int64(c.pad), int64(c.pad)}},
			}}
		x := &Tensor{Shape: []int{1, c.C, H, W}, DType: DFloat32, F32: rnd(c.C * H * W)}
		w := &Tensor{Shape: []int{c.M, c.C / c.group, c.k, c.k}, DType: DFloat32, F32: rnd(c.M * c.C / c.group * c.k * c.k)}
		b := &Tensor{Shape: []int{c.M}, DType: DFloat32, F32: rnd(c.M)}
		g.initializers["w"], g.initializers["b"] = w, b
		g.epilogues[nd] = &epilogue{kind: c.epi, alpha: 1.0 / 6, beta: 0.5}
		g.nodes = []*Node{nd}
		rc := &runCtx{Graph: g}
		want, err := opConv(rc, nd, []*Tensor{x, w, b})
		if err != nil {
			return err
		}
		gg := &gpuGraph{dev: onnxGPUDev, pipe: onnxGPUPipe, consts: map[uintptr]*mtl.Buffer{}, convs: map[*Node]*gconv{}, free: map[int][]*gbuf{}, lastUse: map[string]int{}}
		gg.dummy, _ = onnxGPUDev.NewBuffer(16)
		gg.mu.Lock()
		r := &gpuRun{gg: gg, g: g, byVal: map[string]*gbuf{}}
		got, ok, err := r.conv(rc, nd, []*Tensor{x, w, b})
		if err != nil || !ok {
			r.abort()
			return fmt.Errorf("case %d: gpu conv unavailable: %v", ci, err)
		}
		// broadcast add of the conv output with a per-channel constant
		addN := &Node{OpType: "Add", Inputs: []string{"y", "b4"}, Outputs: []string{"z"}}
		b4 := &Tensor{Shape: []int{1, c.M, 1, 1}, DType: DFloat32, F32: rnd(c.M)}
		g.initializers["b4"] = b4
		gz, ok, err := r.binary(addN, []*Tensor{got[0], b4})
		if err != nil || !ok {
			r.abort()
			return fmt.Errorf("case %d: gpu add unavailable: %v", ci, err)
		}
		if err := r.flush(); err != nil {
			r.abort()
			return err
		}
		maxd := 0.0
		for i, v := range want[0].F32 {
			d := math.Abs(float64(v - got[0].F32[i]))
			bc := (i / (len(want[0].F32) / c.M)) % c.M
			d2 := math.Abs(float64(v + b4.F32[bc] - gz[0].F32[i]))
			maxd = math.Max(maxd, math.Max(d, d2))
		}
		for _, mb := range gg.convs {
			mb.w.Release()
			if mb.bias != nil {
				mb.bias.Release()
			}
		}
		for _, mb := range gg.consts {
			mb.Release()
		}
		r.abort()
		gg.trim(0)
		gg.dummy.Release()
		if maxd > 1e-3 || math.IsNaN(maxd) {
			return fmt.Errorf("case %d: max |gpu-cpu| = %g", ci, maxd)
		}
	}
	return nil
}
