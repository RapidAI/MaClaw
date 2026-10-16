//go:build darwin

package asr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/mtl"
)

// svGPU runs the SenseVoice SAN-M encoder + CTC projection on the Metal GPU
// (pure Go via purego, CGO_ENABLED=0). Weights are uploaded once: Q8_0
// matrices as int8 qs + f32 scales, f32 matrices (the 560-wide entry QKV)
// zero-padded to a multiple of 32 columns; N is padded to a multiple of 64.
type svGPU struct {
	dev  *mtl.Device
	pipe map[string]*mtl.Pipeline
	wq   *mtl.Buffer // int8 qs
	wf   *mtl.Buffer // f32: scales, f32 weights, biases, LN, FSMN kernels

	entry      svGLayer
	layers     []svGLayer
	tp         []svGLayer
	afterNorm  [2]int // w, b offsets (floats into wf)
	tpNorm     [2]int
	ctc        svGMat
	hidden     int
	ff         int
	featsPad   int
	nHeads     int
	fsmnK      int
	vocab      int
	WeightSize int

	capRows                                       int
	feats, a, b, nrm, qkv, att, ffb, logits, part *mtl.Buffer
}

type svGMat struct {
	q8      bool
	off     int // bytes into wq (q8) or wf (f32)
	sOff    int // q8 scales: bytes into wf
	bias    int // bytes into wf, -1 = none
	N, Npad int
	K       int // padded K (multiple of 32)
}

type svGLayer struct {
	qkv, out, ff1, ff2 svGMat
	norm1, norm2       [2]int // bytes into wf
	fsmn               int    // bytes into wf
	inDim              int
}

var svMetalKernels = []string{"layernorm_rows", "matmul_q8", "matmul_f32", "mm_reduce", "fsmn_add", "attention_full"}

// svUploader lays weights out in two passes: a dry run (q, f nil) only
// advances the cursors to size the Metal buffers, the second pass writes
// straight into them (no Go-heap staging copy of the model).
type svUploader struct {
	q      []byte
	f      []float32
	nq, nf int
}

func (u *svUploader) floats(v []float32) int {
	off := u.nf * 4
	if u.f != nil {
		copy(u.f[u.nf:], v)
	}
	u.nf += len(v)
	return off
}

func (u *svUploader) mat(w linearWeight, bias []float32, padBias float32) (svGMat, error) {
	var m svGMat
	switch {
	case w.q8 != nil:
		t := w.q8
		if t.Cols%32 != 0 || len(t.Data) < t.Rows*t.Cols/32*34 {
			return m, errors.New("metal: bad q8 weight")
		}
		m.q8, m.N, m.K = true, t.Rows, t.Cols
		m.Npad = (m.N + 63) / 64 * 64
		nb := t.Cols / 32
		m.off = u.nq
		m.sOff = u.nf * 4
		if u.q != nil {
			sc := u.f[u.nf : u.nf+m.Npad*nb]
			qs := u.q[u.nq : u.nq+m.Npad*t.Cols]
			for r := 0; r < t.Rows; r++ {
				for b := 0; b < nb; b++ {
					blk := t.Data[(r*nb+b)*34 : (r*nb+b+1)*34]
					sc[r*nb+b] = svF16ToF32(binary.LittleEndian.Uint16(blk))
					copy(qs[r*t.Cols+b*32:], blk[2:34])
				}
			}
		}
		u.nq += m.Npad * t.Cols
		u.nf += m.Npad * nb
	case w.f32 != nil && w.rows > 0:
		m.N = w.rows
		cols := len(w.f32) / w.rows
		m.K = (cols + 31) / 32 * 32
		m.Npad = (m.N + 63) / 64 * 64
		m.off = u.nf * 4
		if u.f != nil {
			for r := 0; r < m.N; r++ {
				copy(u.f[u.nf+r*m.K:], w.f32[r*cols:(r+1)*cols])
			}
		}
		u.nf += m.Npad * m.K
	default:
		return m, errors.New("metal: missing weight")
	}
	m.bias = -1
	if bias != nil {
		if len(bias) < m.N {
			return m, errors.New("metal: short bias")
		}
		m.bias = u.nf * 4
		if u.f != nil {
			b := u.f[u.nf : u.nf+m.Npad]
			copy(b, bias[:m.N])
			for i := m.N; i < m.Npad; i++ {
				b[i] = padBias
			}
		}
		u.nf += m.Npad
	}
	return m, nil
}

func svF16ToF32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	man := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		f := float32(man) / 1024 / 16384
		if sign != 0 {
			return -f
		}
		return f
	case exp == 31:
		return math.Float32frombits(sign | 0x7f800000 | man<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | man<<13)
}

func newSVGPU(m *SenseVoiceModel) (g *svGPU, err error) {
	defer func() {
		if r := recover(); r != nil {
			g, err = nil, fmt.Errorf("metal init panic: %v", r)
		}
	}()
	hp := m.hp
	if hp.HiddenSize != 512 || hp.NumHeads != 4 || hp.HiddenSize/hp.NumHeads != 128 {
		return nil, fmt.Errorf("metal: unsupported SenseVoice shape")
	}
	build := func(u *svUploader) (g *svGPU, err error) {
		g = &svGPU{hidden: hp.HiddenSize, nHeads: hp.NumHeads, fsmnK: hp.FSMNKernel, vocab: hp.VocabSize}
		layer := func(l *svLayerWeights, inDim int) (svGLayer, error) {
			var gl svGLayer
			var err error
			if !l.fusedQKV || l.qW.Rows() != 3*hp.HiddenSize || l.fsmnW == nil || l.norm1W == nil || l.norm1B == nil || l.norm2W == nil || l.norm2B == nil {
				return gl, errors.New("metal: unsupported SANM layer layout")
			}
			gl.inDim = inDim
			if gl.qkv, err = u.mat(l.qW, l.qB.f32, 0); err != nil {
				return gl, err
			}
			if gl.out, err = u.mat(l.outW, l.outB.f32, 0); err != nil {
				return gl, err
			}
			if gl.ff1, err = u.mat(l.ff1W, l.ff1B.f32, 0); err != nil {
				return gl, err
			}
			if gl.ff2, err = u.mat(l.ff2W, l.ff2B.f32, 0); err != nil {
				return gl, err
			}
			if gl.qkv.N != 3*hp.HiddenSize || gl.out.N != hp.HiddenSize || gl.ff2.N != hp.HiddenSize || gl.ff1.N%64 != 0 || gl.ff2.K != gl.ff1.N {
				return gl, errors.New("metal: unexpected SANM shapes")
			}
			gl.norm1 = [2]int{u.floats(l.norm1W), u.floats(l.norm1B)}
			gl.norm2 = [2]int{u.floats(l.norm2W), u.floats(l.norm2B)}
			gl.fsmn = u.floats(l.fsmnW[:hp.FSMNKernel*hp.HiddenSize])
			return gl, nil
		}
		if g.entry, err = layer(&m.w.encoder0, hp.FeatsDim); err != nil {
			return nil, err
		}
		g.featsPad = g.entry.qkv.K
		if g.entry.out.K != hp.HiddenSize {
			return nil, errors.New("metal: entry shape")
		}
		for i := range m.w.encoders {
			gl, err := layer(&m.w.encoders[i], hp.HiddenSize)
			if err != nil {
				return nil, err
			}
			g.layers = append(g.layers, gl)
		}
		for i := range m.w.tpEncoders {
			gl, err := layer(&m.w.tpEncoders[i], hp.HiddenSize)
			if err != nil {
				return nil, err
			}
			g.tp = append(g.tp, gl)
		}
		g.ff = g.layers[0].ff1.N
		if m.w.afterNormW == nil || m.w.tpNormW == nil || m.w.afterNormB == nil || m.w.tpNormB == nil {
			return nil, errors.New("metal: missing encoder norms")
		}
		g.afterNorm = [2]int{u.floats(m.w.afterNormW), u.floats(m.w.afterNormB)}
		g.tpNorm = [2]int{u.floats(m.w.tpNormW), u.floats(m.w.tpNormB)}
		if g.ctc, err = u.mat(m.w.ctcW, m.w.ctcB, -1e30); err != nil {
			return nil, err
		}
		if g.ctc.N != hp.VocabSize {
			return nil, errors.New("metal: ctc shape")
		}
		return g, nil
	}
	dry := &svUploader{}
	if _, err := build(dry); err != nil {
		return nil, err
	}
	dev, err := mtl.NewDevice()
	if err != nil {
		return nil, err
	}
	pipe, err := dev.Compile(os.Getenv("MACLAW_SENSEVOICE_METAL_DEFS")+svMetalSource, svMetalKernels)
	if err != nil {
		return nil, err
	}
	wq, err := dev.NewBuffer(dry.nq)
	if err != nil {
		return nil, err
	}
	wf, err := dev.NewBuffer(dry.nf * 4)
	if err != nil {
		wq.Release()
		return nil, err
	}
	if g, err = build(&svUploader{q: wq.Bytes(), f: wf.Float32s()}); err != nil {
		wq.Release()
		wf.Release()
		return nil, err
	}
	g.dev, g.pipe, g.wq, g.wf = dev, pipe, wq, wf
	g.WeightSize = dry.nq + 4*dry.nf
	if err := g.ensureRows(256); err != nil {
		g.release()
		return nil, err
	}
	return g, nil
}

const svMetalMaxSplit, svMetalSplitMaxM = 8, 64

func (g *svGPU) ensureRows(n int) error {
	n += 8 // attention reads up to 7 rows past the last key
	if n <= g.capRows {
		return nil
	}
	if n < 2*g.capRows {
		n = 2 * g.capRows
	}
	g.releaseActs()
	var err error
	alloc := func(cols int) *mtl.Buffer {
		if err != nil {
			return nil
		}
		var b *mtl.Buffer
		b, err = g.dev.NewBuffer(n * cols * 4)
		return b
	}
	g.feats = alloc(g.featsPad)
	g.a, g.b, g.nrm, g.att = alloc(g.hidden), alloc(g.hidden), alloc(g.hidden), alloc(g.hidden)
	g.qkv = alloc(3 * g.hidden)
	g.ffb = alloc(g.ff)
	g.logits = alloc(g.ctc.Npad)
	if err == nil {
		g.part, err = g.dev.NewBuffer(svMetalMaxSplit * svMetalSplitMaxM * max(g.ff, 3*g.hidden) * 4)
	}
	if err != nil {
		g.releaseActs()
		return err
	}
	g.capRows = n
	return nil
}

func (g *svGPU) releaseActs() {
	for _, b := range []*mtl.Buffer{g.feats, g.a, g.b, g.nrm, g.att, g.qkv, g.ffb, g.logits, g.part} {
		b.Release()
	}
	g.capRows = 0
}

func (g *svGPU) release() {
	if g == nil {
		return
	}
	g.releaseActs()
	g.wq.Release()
	g.wf.Release()
}

// ActBytes reports the activation buffer footprint.
func (g *svGPU) ActBytes() int {
	return g.capRows * 4 * (g.featsPad + 4*g.hidden + 3*g.hidden + g.ff + g.ctc.Npad)
}

type svMMParams struct {
	M, N, K, Lda, Ldc, Accum, Relu, HasBias, SplitBlocks, _ int32
	SplitStride                                             int64
}
type svReduceParams struct {
	M, N, Splits, Accum, Relu, HasBias int32
	Stride                             int64
}
type svLNParams struct {
	M, D, Ldx, Ldo int32
	Eps            float32
}
type svAttnParams struct {
	M, NH     int32
	Scale     float32
	Ld, KOff  int32
	VOff, Ldo int32
}
type svFSMNParams struct{ M, D, Ld, VOff, Ks, Residual int32 }

// encode runs the encoder on feats (T rows × FeatsDim, already scaled and
// position-encoded) and returns logits [T × ctc.Npad] (GPU buffer view).
func (g *svGPU) encode(feats []float32, T, featsDim int) ([]float32, error) {
	if err := g.ensureRows(T); err != nil {
		return nil, err
	}
	fp := g.featsPad
	fb := g.feats.Float32s()
	for r := 0; r < T; r++ {
		copy(fb[r*fp:r*fp+featsDim], feats[r*featsDim:(r+1)*featsDim])
		clear(fb[r*fp+featsDim : (r+1)*fp])
	}
	e := g.dev.Begin()
	M := int32(T)
	ln := func(x *mtl.Buffer, ldx int, w [2]int, out *mtl.Buffer, ldo, D int) {
		p := svLNParams{M: M, D: int32(D), Ldx: int32(ldx), Ldo: int32(ldo), Eps: 1e-5}
		e.SetPipeline(g.pipe["layernorm_rows"])
		e.SetBuffer(x, 0, 0)
		e.SetBuffer(g.wf, w[0], 1)
		e.SetBuffer(g.wf, w[1], 2)
		e.SetBuffer(out, 0, 3)
		e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
		e.Dispatch(T, 1, 1, 256, 1, 1)
	}
	mm := func(a *mtl.Buffer, w svGMat, c *mtl.Buffer, relu, accum bool) {
		p := svMMParams{M: M, N: int32(w.Npad), K: int32(w.K), Lda: int32(w.K), Ldc: int32(w.Npad)}
		if relu {
			p.Relu = 1
		}
		if accum {
			p.Accum = 1
		}
		if w.bias >= 0 {
			p.HasBias = 1
		}
		nb := w.K / 32
		tiles := w.Npad / 64 * ((T + 31) / 32)
		split := 1
		for T <= svMetalSplitMaxM && w.Npad <= max(g.ff, 3*g.hidden) && split < svMetalMaxSplit && tiles*split < 160 && nb%(split*2) == 0 && nb/(split*2) >= 4 {
			split *= 2
		}
		p.SplitBlocks = int32(nb / split)
		dst := c
		if split > 1 {
			p.SplitStride, dst = int64(T*w.Npad), g.part
		}
		if w.q8 {
			e.SetPipeline(g.pipe["matmul_q8"])
			e.SetBuffer(g.wq, w.off, 1)
			e.SetBuffer(g.wf, w.sOff, 2)
		} else {
			e.SetPipeline(g.pipe["matmul_f32"])
			e.SetBuffer(g.wf, w.off, 1)
			e.SetBuffer(g.wf, 0, 2)
		}
		e.SetBuffer(a, 0, 0)
		e.SetBuffer(dst, 0, 3)
		e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
		e.SetBuffer(g.wf, max(w.bias, 0), 5)
		e.Dispatch(w.Npad/64, (T+31)/32, split, 128, 1, 1)
		if split > 1 {
			rp := svReduceParams{M: M, N: int32(w.Npad), Splits: int32(split), Accum: p.Accum, Relu: p.Relu, HasBias: p.HasBias, Stride: int64(T * w.Npad)}
			e.SetPipeline(g.pipe["mm_reduce"])
			e.SetBuffer(g.part, 0, 0)
			e.SetBuffer(c, 0, 1)
			e.SetBytes(unsafe.Pointer(&rp), int(unsafe.Sizeof(rp)), 2)
			e.SetBuffer(g.wf, max(w.bias, 0), 3)
			e.Dispatch((w.Npad+63)/64, T, 1, 64, 1, 1)
		}
	}
	H := g.hidden
	block := func(l *svGLayer, x *mtl.Buffer, out *mtl.Buffer, entry bool) {
		in := g.nrm
		if entry {
			ln(x, g.featsPad, l.norm1, x, g.featsPad, l.inDim) // in place, like the CPU entry path
			in = x
		} else {
			ln(x, H, l.norm1, g.nrm, H, H)
		}
		mm(in, l.qkv, g.qkv, false, false)
		ap := svAttnParams{M: M, NH: int32(g.nHeads), Scale: float32(1 / math.Sqrt(float64(H/g.nHeads))), Ld: int32(3 * H), KOff: int32(H), VOff: int32(2 * H), Ldo: int32(H)}
		e.SetPipeline(g.pipe["attention_full"])
		e.SetBuffer(g.qkv, 0, 0)
		e.SetBuffer(g.att, 0, 1)
		e.SetBytes(unsafe.Pointer(&ap), int(unsafe.Sizeof(ap)), 2)
		e.Dispatch((T+7)/8, (g.nHeads+3)/4, 1, 128, 1, 1)
		mm(g.att, l.out, out, false, false)
		fp := svFSMNParams{M: M, D: int32(H), Ld: int32(3 * H), VOff: int32(2 * H), Ks: int32(g.fsmnK)}
		if !entry {
			fp.Residual = 1
		}
		e.SetPipeline(g.pipe["fsmn_add"])
		e.SetBuffer(g.qkv, 0, 0)
		e.SetBuffer(g.wf, l.fsmn, 1)
		e.SetBuffer(x, 0, 2)
		e.SetBuffer(out, 0, 3)
		e.SetBytes(unsafe.Pointer(&fp), int(unsafe.Sizeof(fp)), 4)
		e.Dispatch(H/128, T, 1, 128, 1, 1)
		ln(out, H, l.norm2, g.nrm, H, H)
		mm(g.nrm, l.ff1, g.ffb, true, false)
		mm(g.ffb, l.ff2, out, false, true)
	}
	block(&g.entry, g.feats, g.a, true)
	cur, nxt := g.a, g.b
	for i := range g.layers {
		block(&g.layers[i], cur, nxt, false)
		cur, nxt = nxt, cur
	}
	ln(cur, H, g.afterNorm, cur, H, H)
	for i := range g.tp {
		block(&g.tp[i], cur, nxt, false)
		cur, nxt = nxt, cur
	}
	ln(cur, H, g.tpNorm, cur, H, H)
	mm(cur, g.ctc, g.logits, false, false)
	if err := e.Wait(); err != nil {
		return nil, err
	}
	return g.logits.Float32s()[:T*g.ctc.Npad], nil
}

func (g *svGPU) logitStride() int { return g.ctc.Npad }
