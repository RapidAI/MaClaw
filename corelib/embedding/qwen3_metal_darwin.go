//go:build darwin

package embedding

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/mtl"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// qwen3GPU runs the Qwen3 forward on the Metal GPU (pure Go via purego, no
// cgo). Weights are repacked once at load into int8 qs [N×K] + f32 scales
// [N×K/32]; Q/K/V and gate/up are concatenated so each is one matmul.
// Several sequences are packed into one forward (block-causal attention).
type qwen3GPU struct {
	hp     Qwen3HParams
	dev    *mtl.Device
	pipe   map[string]*mtl.Pipeline
	wq     *mtl.Buffer // all int8 qs
	ws     *mtl.Buffer // all f32 scales
	norms  *mtl.Buffer // per-layer norm weights
	cos    *mtl.Buffer
	sin    *mtl.Buffer
	maxPos int
	layers []qwen3GPULayer

	mu                      sync.Mutex
	capRows                 int
	x, nrm, qkv, att, gu, g *mtl.Buffer
	part                    *mtl.Buffer // split-K partials
	pos, seg                *mtl.Buffer

	WeightBytes int
}

type gpuMat struct{ qOff, sOff, N, K int }

type qwen3GPULayer struct {
	qkv, o, gu, down               gpuMat
	attnNorm, ffNorm, qNorm, kNorm int // float offsets into norms
}

var qwen3MetalKernels = []string{"rmsnorm_rows", "matmul_q8", "qk_norm_rope", "attention", "silu_mul", "mm_reduce", "attention_tiled"}

func newQwen3GPU(q *Qwen3Embedder) (g *qwen3GPU, err error) {
	defer func() {
		if r := recover(); r != nil {
			g, err = nil, fmt.Errorf("metal init panic: %v", r)
		}
	}()
	hp := q.hp
	if hp.HeadDim != 128 || hp.HeadDim/2 > 64*2 || hp.NHeads%4 != 0 {
		return nil, fmt.Errorf("metal: unsupported head config")
	}
	for _, n := range []int{hp.QDim + 2*hp.KVDim, hp.Dim, 2 * hp.FFDim} {
		if n%64 != 0 {
			return nil, fmt.Errorf("metal: N=%d not a multiple of 64", n)
		}
	}
	dev, err := mtl.NewDevice()
	if err != nil {
		return nil, err
	}
	pipe, err := dev.Compile(os.Getenv("MACLAW_QWEN3_METAL_DEFS")+qwen3MetalSource, qwen3MetalKernels)
	if err != nil {
		return nil, err
	}
	g = &qwen3GPU{hp: hp, dev: dev, pipe: pipe}
	// Size the weight arenas.
	var qBytes, sFloats int
	for range q.layers {
		for _, nk := range [][2]int{{hp.QDim + 2*hp.KVDim, hp.Dim}, {hp.Dim, hp.QDim}, {2 * hp.FFDim, hp.Dim}, {hp.Dim, hp.FFDim}} {
			qBytes += nk[0] * nk[1]
			sFloats += nk[0] * nk[1] / 32
		}
	}
	if g.wq, err = dev.NewBuffer(qBytes); err != nil {
		return nil, err
	}
	if g.ws, err = dev.NewBuffer(sFloats * 4); err != nil {
		g.release()
		return nil, err
	}
	normFloats := len(q.layers) * (2*hp.Dim + 2*hp.HeadDim)
	if g.norms, err = dev.NewBuffer(normFloats * 4); err != nil {
		g.release()
		return nil, err
	}
	g.WeightBytes = qBytes + sFloats*4 + normFloats*4
	qa, sa, na := g.wq.Bytes(), g.ws.Float32s(), g.norms.Float32s()
	qo, so, no := 0, 0, 0
	put := func(ts ...*tensor.Q8Tensor) (gpuMat, error) {
		m := gpuMat{qOff: qo, sOff: so * 4, K: ts[0].Cols}
		for _, t := range ts {
			if t.Cols != m.K || len(t.Data) < t.Rows*t.Cols/32*34 {
				return m, fmt.Errorf("metal: bad weight shape")
			}
			nb := t.Cols / 32
			for r := 0; r < t.Rows; r++ {
				for b := 0; b < nb; b++ {
					blk := t.Data[(r*nb+b)*34 : (r*nb+b+1)*34]
					sa[so] = f16ToF32(binary.LittleEndian.Uint16(blk))
					so++
					copy(qa[qo:qo+32], blk[2:34])
					qo += 32
				}
			}
			m.N += t.Rows
		}
		return m, nil
	}
	putN := func(w []float32) int {
		off := no
		copy(na[no:], w)
		no += len(w)
		return off
	}
	g.layers = make([]qwen3GPULayer, len(q.layers))
	for i := range q.layers {
		l, gl := &q.layers[i], &g.layers[i]
		if gl.qkv, err = put(&l.wq, &l.wk, &l.wv); err == nil {
			if gl.o, err = put(&l.wo); err == nil {
				if gl.gu, err = put(&l.wGate, &l.wUp); err == nil {
					gl.down, err = put(&l.wDown)
				}
			}
		}
		if err != nil {
			g.release()
			return nil, err
		}
		gl.attnNorm, gl.ffNorm = putN(l.attnNormW), putN(l.ffNormW)
		gl.qNorm, gl.kNorm = putN(l.qNormW), putN(l.kNormW)
	}
	// RoPE tables for every position the model accepts.
	g.maxPos = hp.MaxSeqLen
	cosAll, sinAll := q.ensureRoPE(g.maxPos)
	half := hp.HeadDim / 2
	if g.cos, err = dev.NewBuffer(g.maxPos * half * 4); err == nil {
		g.sin, err = dev.NewBuffer(g.maxPos * half * 4)
	}
	if err != nil {
		g.release()
		return nil, err
	}
	copy(g.cos.Float32s(), cosAll[:g.maxPos*half])
	copy(g.sin.Float32s(), sinAll[:g.maxPos*half])
	if err := g.ensureRows(64); err != nil {
		g.release()
		return nil, err
	}
	return g, nil
}

func f16ToF32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	man := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		if man == 0 {
			return math.Float32frombits(sign)
		}
		f := float32(man) / 1024 / 16384 // subnormal: man × 2^-24
		if sign != 0 {
			return -f
		}
		return f
	case exp == 31:
		return math.Float32frombits(sign | 0x7f800000 | man<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | man<<13)
}

func (g *qwen3GPU) ensureRows(n int) error {
	if n <= g.capRows {
		return nil
	}
	if n < 2*g.capRows {
		n = 2 * g.capRows
	}
	hp := g.hp
	for _, b := range []*mtl.Buffer{g.x, g.nrm, g.qkv, g.att, g.gu, g.g, g.pos, g.seg, g.part} {
		b.Release()
	}
	g.capRows = 0
	var err error
	alloc := func(cols int) *mtl.Buffer {
		if err != nil {
			return nil
		}
		var b *mtl.Buffer
		b, err = g.dev.NewBuffer(n * cols * 4)
		return b
	}
	g.x, g.nrm = alloc(hp.Dim), alloc(hp.Dim)
	g.qkv, g.att = alloc(hp.QDim+2*hp.KVDim), alloc(hp.QDim)
	g.gu, g.g = alloc(2*hp.FFDim), alloc(hp.FFDim)
	g.pos, g.seg = alloc(1), alloc(1)
	if err == nil {
		g.part, err = g.dev.NewBuffer(qwen3MetalMaxSplit * qwen3MetalSplitMaxM * 2 * hp.FFDim * 4)
	}
	if err != nil {
		return err
	}
	g.capRows = n
	return nil
}

// ActBytes reports the current activation buffer footprint.
func (g *qwen3GPU) ActBytes() int {
	hp := g.hp
	return g.capRows * 4 * (2*hp.Dim + hp.QDim + 2*hp.KVDim + hp.QDim + 3*hp.FFDim + 2)
}

func (g *qwen3GPU) release() {
	if g == nil {
		return
	}
	for _, b := range []*mtl.Buffer{g.wq, g.ws, g.norms, g.cos, g.sin, g.x, g.nrm, g.qkv, g.att, g.gu, g.g, g.pos, g.seg, g.part} {
		b.Release()
	}
	g.capRows = 0
}

type mmParams struct {
	M, N, K, Accum, Lda, Ldc int32
	SplitBlocks, _           int32
	SplitStride              int64
}
type reduceParams struct {
	M, N, Splits, Accum int32
	Stride              int64
}
type normParams struct {
	M, D int32
	Eps  float32
	Ld   int32
}
type ropeParams struct {
	M, NH, NKV, HD int32
	Eps            float32
	Ld, KOff       int32
}
type attnParams struct {
	M, NH, NKV, HD int32
	Scale          float32
	Ld, KOff, VOff int32
	Ldo            int32
}
type siluParams struct{ M, F, Ld int32 }

// forward runs all sequences (packed) and returns each one's last hidden
// row before the output norm, written into outs[i] (len hp.Dim).
func (g *qwen3GPU) forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	hp := g.hp
	M := 0
	for _, s := range seqs {
		if len(s) == 0 || len(s) > g.maxPos {
			return fmt.Errorf("metal: sequence length %d out of range", len(s))
		}
		M += len(s)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// +8 rows: attention_tiled reads up to 7 key rows past the last one.
	if err := g.ensureRows(M + 8); err != nil {
		return err
	}
	dim, ld := hp.Dim, hp.QDim+2*hp.KVDim
	x := g.x.Float32s()
	pos, seg := g.pos.Int32s(), g.seg.Int32s()
	r := 0
	for _, s := range seqs {
		start := r
		for i, id := range s {
			if id < 0 || id >= hp.VocabSize {
				return fmt.Errorf("qwen3: token id %d out of range [0,%d)", id, hp.VocabSize)
			}
			q.tokenEmb.DequantRow(id, x[r*dim:(r+1)*dim])
			pos[r], seg[r] = int32(i), int32(start)
			r++
		}
	}
	e := g.dev.Begin()
	skip := qwen3MetalSkip
	m32 := int32(M)
	norm := func(w int) {
		p := normParams{M: m32, D: int32(dim), Eps: hp.RMSNormEps, Ld: int32(dim)}
		e.SetPipeline(g.pipe["rmsnorm_rows"])
		e.SetBuffer(g.x, 0, 0)
		e.SetBuffer(g.norms, w*4, 1)
		e.SetBuffer(g.nrm, 0, 2)
		e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 3)
		e.Dispatch(M, 1, 1, 256, 1, 1)
	}
	mm := func(a *mtl.Buffer, w gpuMat, c *mtl.Buffer, accum bool) {
		p := mmParams{M: m32, N: int32(w.N), K: int32(w.K), Lda: int32(w.K), Ldc: int32(w.N)}
		if accum {
			p.Accum = 1
		}
		if skip["mm"] {
			return
		}
		nb := w.K / 32
		tiles := w.N / 64 * ((M + 31) / 32)
		split := 1
		for M <= qwen3MetalSplitMaxM && split < qwen3MetalMaxSplit && tiles*split < qwen3MetalMinTiles && nb%(split*2) == 0 && nb/(split*2) >= 4 {
			split *= 2
		}
		p.SplitBlocks = int32(nb / split)
		dst := c
		if split > 1 {
			p.Accum, p.SplitStride, dst = 0, int64(M*w.N), g.part
		}
		e.SetPipeline(g.pipe["matmul_q8"])
		e.SetBuffer(a, 0, 0)
		e.SetBuffer(g.wq, w.qOff, 1)
		e.SetBuffer(g.ws, w.sOff, 2)
		e.SetBuffer(dst, 0, 3)
		e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
		e.Dispatch(w.N/64, (M+31)/32, split, 128, 1, 1)
		if split > 1 {
			rp := reduceParams{M: m32, N: int32(w.N), Splits: int32(split), Stride: int64(M * w.N)}
			if accum {
				rp.Accum = 1
			}
			e.SetPipeline(g.pipe["mm_reduce"])
			e.SetBuffer(g.part, 0, 0)
			e.SetBuffer(c, 0, 1)
			e.SetBytes(unsafe.Pointer(&rp), int(unsafe.Sizeof(rp)), 2)
			e.Dispatch(w.N/4/64, M, 1, 64, 1, 1)
		}
	}
	for li := range g.layers {
		l := &g.layers[li]
		norm(l.attnNorm)
		mm(g.nrm, l.qkv, g.qkv, false)
		rp := ropeParams{M: m32, NH: int32(hp.NHeads), NKV: int32(hp.NKVHeads), HD: int32(hp.HeadDim), Eps: hp.RMSNormEps, Ld: int32(ld), KOff: int32(hp.QDim)}
		e.SetPipeline(g.pipe["qk_norm_rope"])
		e.SetBuffer(g.qkv, 0, 0)
		e.SetBuffer(g.norms, l.qNorm*4, 1)
		e.SetBuffer(g.norms, l.kNorm*4, 2)
		e.SetBuffer(g.cos, 0, 3)
		e.SetBuffer(g.sin, 0, 4)
		e.SetBuffer(g.pos, 0, 5)
		e.SetBytes(unsafe.Pointer(&rp), int(unsafe.Sizeof(rp)), 6)
		e.Dispatch(M, hp.NHeads+hp.NKVHeads, 1, hp.HeadDim/2, 1, 1)
		ap := attnParams{M: m32, NH: int32(hp.NHeads), NKV: int32(hp.NKVHeads), HD: int32(hp.HeadDim),
			Scale: float32(1.0 / math.Sqrt(float64(hp.HeadDim))), Ld: int32(ld), KOff: int32(hp.QDim), VOff: int32(hp.QDim + hp.KVDim), Ldo: int32(hp.QDim)}
		if qwen3MetalAttnTiled && !skip["attn"] {
			e.SetPipeline(g.pipe["attention_tiled"])
			e.SetBuffer(g.qkv, 0, 0)
			e.SetBuffer(g.att, 0, 1)
			e.SetBuffer(g.seg, 0, 2)
			e.SetBytes(unsafe.Pointer(&ap), int(unsafe.Sizeof(ap)), 3)
			e.Dispatch((M+7)/8, hp.NHeads/4, 1, 128, 1, 1)
		} else if !skip["attn"] {
			e.SetPipeline(g.pipe["attention"])
			e.SetBuffer(g.qkv, 0, 0)
			e.SetBuffer(g.att, 0, 1)
			e.SetBuffer(g.seg, 0, 2)
			e.SetBytes(unsafe.Pointer(&ap), int(unsafe.Sizeof(ap)), 3)
			e.Dispatch(M, hp.NHeads/4, 1, 128, 1, 1)
		}
		mm(g.att, l.o, g.x, true)
		norm(l.ffNorm)
		mm(g.nrm, l.gu, g.gu, false)
		sp := siluParams{M: m32, F: int32(hp.FFDim), Ld: int32(2 * hp.FFDim)}
		e.SetPipeline(g.pipe["silu_mul"])
		e.SetBuffer(g.gu, 0, 0)
		e.SetBuffer(g.g, 0, 1)
		e.SetBytes(unsafe.Pointer(&sp), int(unsafe.Sizeof(sp)), 2)
		e.Dispatch((hp.FFDim+255)/256, M, 1, 256, 1, 1)
		mm(g.g, l.down, g.x, true)
	}
	if err := e.Wait(); err != nil {
		return err
	}
	r = 0
	for i, s := range seqs {
		r += len(s)
		copy(outs[i], x[(r-1)*dim:r*dim])
	}
	return nil
}

// qwen3MetalSkip (MACLAW_QWEN3_METAL_SKIP=mm,attn) drops kernel classes for
// timing experiments only; output is wrong when set.
var qwen3MetalSkip = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range strings.Split(os.Getenv("MACLAW_QWEN3_METAL_SKIP"), ",") {
		if k != "" {
			m[k] = true
		}
	}
	return m
}()

// Split-K for small-M matmuls: split K until the grid has at least
// qwen3MetalMinTiles threadgroups (MACLAW_QWEN3_METAL_MINTILES, 0 = off).
const (
	qwen3MetalMaxSplit  = 8
	qwen3MetalSplitMaxM = 64
)

var qwen3MetalMinTiles = envInt("MACLAW_QWEN3_METAL_MINTILES", 160)

// MACLAW_QWEN3_METAL_ATTN=simple selects the per-row attention kernel.
var qwen3MetalAttnTiled = os.Getenv("MACLAW_QWEN3_METAL_ATTN") != "simple"

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v >= 0 {
		return v
	}
	return def
}
