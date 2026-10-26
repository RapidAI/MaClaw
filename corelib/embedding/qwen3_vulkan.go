package embedding

// qwen3_vulkan.go — Qwen3 forward pass on a Vulkan compute device (pure Go).
//
// This is the Vulkan twin of qwen3_metal_darwin.go: same weight repacking
// (Q8_0 int8 + per-32-block f32 scales), same kernel decomposition, and the
// same fallback contract (any failure disables the GPU for this embedder and
// the CPU path takes over).
//
// Differences from the Metal backend, all forced by Vulkan semantics:
//   - Vulkan does not insert inter-dispatch barriers, so every dependency is
//     recorded explicitly. The encoder records the whole forward into one
//     command buffer, so this costs no extra submits.
//   - Descriptor sets are per-pipeline and bound once at setup; the buffers
//     never move, so no rebinding is needed per dispatch.
//   - Push constants carry the per-dispatch shape/offset parameters.
//
// CPU cost per forward is one submit + one fence wait, not one per dispatch.

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/vk"
)

// vkShaderBindings is the number of storage-buffer bindings every Qwen3
// shader declares (see the .comp sources: activations, weights, scales, norms,
// RoPE tables, positions).
const vkShaderBindings = 8

// qwen3VKGPU is the Vulkan-resident state of one Qwen3 model.
type qwen3VKGPU struct {
	hp    Qwen3HParams
	dev   *vk.Device
	pipes map[string]*vk.Pipeline

	wq, ws, nrm *vk.Buffer // int8 weights, f32 scales, norm weights
	cos, sin    *vk.Buffer // RoPE tables
	x, nrmBuf   *vk.Buffer // hidden state, normalised scratch
	qkv, att    *vk.Buffer // packed QKV, attention output
	gu, g       *vk.Buffer // gate/up, fused silu output
	pos         *vk.Buffer // per-row positions, then segment bases

	layers  []qwen3VKLayer
	maxPos  int
	capRows int

	WeightBytes int
	mu          sync.Mutex
}

type vkMat struct{ qOff, sOff, N, K int }

type qwen3VKLayer struct {
	qkv, o, gu, down vkMat
	attnNorm, ffNorm int // float offsets into nrm
	qNorm, kNorm     int
}

// push-constant structs (std430/push-constant layout: 4-byte aligned, no
// implicit padding for our field order).

type vkMMParams struct {
	M, N, K, Accum, Lda, Ldc, WOff, SOff int32
}

type vkNormParams struct {
	M, D int32
	Eps  float32
	Ld   int32
	WOff int32
}

type vkRopeParams struct {
	M, NH, NKV, HD     int32
	Eps                float32
	Ld, QOff, KOff     int32
	QNormOff, KNormOff int32
	PosOff             int32
}

type vkAttnParams struct {
	M, NH, NKV, HD       int32
	Scale                float32
	Ld, QOff, KOff, VOff int32
	OLd, OOff, SegOff    int32
}

type vkSiluParams struct{ M, F, Ld int32 }

type vkPoolParams struct {
	Rows, D               int32
	Eps                   float32
	SrcLd, SrcOff, Stride int32
}

// initVulkan tries to bring up the Vulkan backend. Any failure returns an
// error and the CPU path stays in charge.
func (q *Qwen3Embedder) initVulkan() {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		// macOS has Metal; other platforms are not shipped.
		return
	}
	if os.Getenv("MACLAW_QWEN3_VULKAN") == "0" {
		q.gpuReason = "disabled by MACLAW_QWEN3_VULKAN=0"
		return
	}
	g, err := newQwen3VKGPU(q)
	if err != nil {
		q.gpuReason = err.Error()
		return
	}
	// Self-check against the CPU path before trusting the GPU.
	ids := q.Tokenize("Vulkan self-check: the quick brown fox jumps over the lazy dog 0123456789.")
	got := make([]float32, q.hp.Dim)
	if err := g.forward(q, [][]int{ids}, [][]float32{got}); err != nil {
		g.release()
		q.gpuReason = "self-check: " + err.Error()
		return
	}
	s := q.getScratch(len(ids) + 8)
	ref, err := q.forwardFrom(ids, nil, nil, s, 1)
	q.putScratch(s)
	if err == nil {
		// forward returns the raw last hidden row; forwardFrom already applies
		// the output norm, so normalise only the GPU side.
		n := make([]float32, q.hp.Dim)
		tensor.RMSNorm(n, got, q.outputNorm, q.hp.RMSNormEps)
		if c := cosineF32(n, ref); !(c >= qwen3VKSelfCheckMinCos) {
			err = fmt.Errorf("cos %.6f < %.4f", c, qwen3VKSelfCheckMinCos)
		}
	}
	if err != nil {
		g.release()
		q.gpuReason = "self-check: " + err.Error()
		return
	}
	q.vkGPU.Store(g)
	log.Printf("qwen3: Vulkan backend active (%s, %d MB weights)", g.dev.Info.Name, g.WeightBytes>>20)
}

const qwen3VKSelfCheckMinCos = 0.999

func cosineF32(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return d / math.Sqrt(na*nb)
}

func newQwen3VKGPU(q *Qwen3Embedder) (g *qwen3VKGPU, err error) {
	defer func() {
		if r := recover(); r != nil {
			g, err = nil, fmt.Errorf("vulkan init panic: %v", r)
		}
	}()
	hp := q.hp
	if hp.HeadDim != 128 {
		return nil, fmt.Errorf("vulkan: head_dim %d unsupported (want 128)", hp.HeadDim)
	}
	dev, err := vk.NewDevice()
	if err != nil {
		return nil, err
	}
	g = &qwen3VKGPU{hp: hp, dev: dev, pipes: map[string]*vk.Pipeline{}}

	// Compile every kernel once; pipelines are reused for the whole session.
	// Bindings are declared per shader (see the .comp sources).
	for name, nb := range map[string]int{
		"matmul":       vkShaderBindings,
		"rmsnorm_rows": vkShaderBindings,
		"qk_norm_rope": vkShaderBindings,
		"attention":    vkShaderBindings,
		"silu_mul":     vkShaderBindings,
	} {
		spv, ok := vk.ShaderSPIRV(name)
		if !ok {
			g.release()
			return nil, fmt.Errorf("vulkan: missing shader %q", name)
		}
		p, cerr := dev.Compile(spv, nb, 64)
		if cerr != nil {
			g.release()
			return nil, fmt.Errorf("vulkan: compile %s: %w", name, cerr)
		}
		g.pipes[name] = p
	}

	if err := g.allocWeights(q); err != nil {
		g.release()
		return nil, err
	}
	if err := g.ensureRows(64); err != nil {
		g.release()
		return nil, err
	}
	return g, nil
}

// allocWeights repacks every Q8_0 weight into the GPU blobs.
func (g *qwen3VKGPU) allocWeights(q *Qwen3Embedder) error {
	hp := g.hp
	var qBytes, sFloats int
	for range q.layers {
		for _, nk := range [][2]int{
			{hp.QDim + 2*hp.KVDim, hp.Dim},
			{hp.Dim, hp.QDim},
			{2 * hp.FFDim, hp.Dim},
			{hp.Dim, hp.FFDim},
		} {
			qBytes += nk[0] * nk[1]
			sFloats += nk[0] * nk[1] / 32
		}
	}
	var err error
	if g.wq, err = g.dev.NewBuffer(qBytes); err != nil {
		return err
	}
	if g.ws, err = g.dev.NewBuffer(sFloats * 4); err != nil {
		return err
	}
	normFloats := len(q.layers) * (2*hp.Dim + 2*hp.HeadDim)
	if g.nrm, err = g.dev.NewBuffer(normFloats * 4); err != nil {
		return err
	}
	g.WeightBytes = qBytes + sFloats*4 + normFloats*4

	qa, sa, na := g.wq.Bytes(), g.ws.Float32s(), g.nrm.Float32s()
	qo, so, no := 0, 0, 0
	put := func(ts ...*tensor.Q8Tensor) (vkMat, error) {
		m := vkMat{qOff: qo, sOff: so * 4, K: ts[0].Cols}
		for _, t := range ts {
			if t.Cols != m.K || len(t.Data) < t.Rows*t.Cols/32*34 {
				return m, fmt.Errorf("vulkan: bad weight shape")
			}
			nb := t.Cols / 32
			for r := 0; r < t.Rows; r++ {
				for b := 0; b < nb; b++ {
					blk := t.Data[(r*nb+b)*34 : (r*nb+b+1)*34]
					sa[so] = vkF16ToF32(uint16(blk[0]) | uint16(blk[1])<<8)
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
	g.layers = make([]qwen3VKLayer, len(q.layers))
	for i := range q.layers {
		l, gl := &q.layers[i], &g.layers[i]
		if gl.qkv, err = put(&l.wq, &l.wk, &l.wv); err != nil {
			return err
		}
		if gl.o, err = put(&l.wo); err != nil {
			return err
		}
		if gl.gu, err = put(&l.wGate, &l.wUp); err != nil {
			return err
		}
		if gl.down, err = put(&l.wDown); err != nil {
			return err
		}
		gl.attnNorm, gl.ffNorm = putN(l.attnNormW), putN(l.ffNormW)
		gl.qNorm, gl.kNorm = putN(l.qNormW), putN(l.kNormW)
	}

	// RoPE tables cover every position the model accepts.
	g.maxPos = hp.MaxSeqLen
	cosAll, sinAll := q.ensureRoPE(g.maxPos)
	half := hp.HeadDim / 2
	if g.cos, err = g.dev.NewBuffer(g.maxPos * half * 4); err != nil {
		return err
	}
	if g.sin, err = g.dev.NewBuffer(g.maxPos * half * 4); err != nil {
		return err
	}
	copy(g.cos.Float32s(), cosAll[:g.maxPos*half])
	copy(g.sin.Float32s(), sinAll[:g.maxPos*half])
	return nil
}

// ensureRows grows the activation buffers to hold n rows.
func (g *qwen3VKGPU) ensureRows(n int) error {
	if n <= g.capRows {
		return nil
	}
	if n < 2*g.capRows {
		n = 2 * g.capRows
	}
	hp := g.hp
	for _, b := range []*vk.Buffer{g.x, g.nrmBuf, g.qkv, g.att, g.gu, g.g, g.pos} {
		if b != nil {
			b.Release()
		}
	}
	g.capRows = 0
	var err error
	alloc := func(elems int) *vk.Buffer {
		if err != nil {
			return nil
		}
		var b *vk.Buffer
		b, err = g.dev.NewBuffer(elems * 4)
		return b
	}
	g.x = alloc(n * hp.Dim)
	g.nrmBuf = alloc(n * hp.Dim)
	g.qkv = alloc(n * (hp.QDim + 2*hp.KVDim))
	g.att = alloc(n * hp.QDim)
	g.gu = alloc(n * 2 * hp.FFDim)
	g.g = alloc(n * hp.FFDim)
	g.pos = alloc(2 * n) // [positions][segment bases]
	if err != nil {
		return err
	}
	g.capRows = n
	return nil
}

// ActBytes reports the activation footprint.
func (g *qwen3VKGPU) ActBytes() int {
	hp := g.hp
	return g.capRows * 4 * (2*hp.Dim + hp.QDim + 2*hp.KVDim + hp.QDim + 3*hp.FFDim + 1)
}

func (g *qwen3VKGPU) release() {
	if g == nil {
		return
	}
	for _, p := range g.pipes {
		p.Release()
	}
	for _, b := range []*vk.Buffer{g.wq, g.ws, g.nrm, g.cos, g.sin, g.x, g.nrmBuf, g.qkv, g.att, g.gu, g.g, g.pos} {
		if b != nil {
			b.Release()
		}
	}
	if g.dev != nil {
		g.dev.Close()
	}
	g.capRows = 0
}

// forward runs the packed sequences and writes each one's last hidden row
// (before the output norm) into outs[i].
//
// All dispatches are recorded into one command buffer and submitted once, so
// the CPU pays a single submit + fence wait per forward regardless of depth.
func (g *qwen3VKGPU) forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	hp := g.hp
	total := 0
	for _, s := range seqs {
		if len(s) == 0 || len(s) > g.maxPos {
			return fmt.Errorf("vulkan: sequence length %d out of range", len(s))
		}
		total += len(s)
	}
	if total == 0 {
		return fmt.Errorf("vulkan: empty batch")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.ensureRows(total); err != nil {
		return err
	}

	dim := hp.Dim
	ld := hp.QDim + 2*hp.KVDim
	x := g.x.Float32s()
	pos := g.pos.Int32s()
	r := 0
	for _, s := range seqs {
		for i, id := range s {
			if id < 0 || id >= hp.VocabSize {
				return fmt.Errorf("qwen3: token id %d out of range [0,%d)", id, hp.VocabSize)
			}
			q.tokenEmb.DequantRow(id, x[r*dim:(r+1)*dim])
			pos[r] = int32(i)
			r++
		}
	}
	// The second half of the positions buffer holds each row's segment base so
	// the attention kernel can serve every sequence from one dispatch.
	segBase := pos[total : 2*total]
	r = 0
	for _, s := range seqs {
		for range s {
			segBase[r] = int32(int32(r) - int32(pos[r]))
			r++
		}
	}
	m32 := int32(total)

	e := g.dev.Begin()
	use := func(name string, a, b, nrm *vk.Buffer) {
		p := g.pipes[name]
		p.Bind(a, b, g.wq, g.ws, nrm, g.cos, g.sin, g.pos)
		e.SetPipeline(p)
	}
	// mm dispatches C[M,N] (=|+=) A . W^T over one workgroup tile per 32x32.
	mm := func(a *vk.Buffer, w vkMat, c *vk.Buffer, accum bool) {
		p := vkMMParams{M: m32, N: int32(w.N), K: int32(w.K), Lda: int32(w.K), Ldc: int32(w.N),
			WOff: int32(w.qOff), SOff: int32(w.sOff / 4)}
		if accum {
			p.Accum = 1
		}
		use("matmul", a, c, g.nrm)
		e.SetBytes(unsafePtr(&p), 32, 0)
		e.Dispatch((w.N+31)/32, (total+31)/32, 1, 16, 16, 1)
		e.Barrier()
	}
	// norm dispatches out = RMSNorm(x) * nrm[wOff:wOff+dim].
	norm := func(wOff int) {
		p := vkNormParams{M: m32, D: int32(dim), Eps: hp.RMSNormEps, Ld: int32(dim), WOff: int32(wOff)}
		use("rmsnorm_rows", g.x, g.nrmBuf, g.nrm)
		e.SetBytes(unsafePtr(&p), 20, 0)
		e.Dispatch(total, 1, 1, 256, 1, 1)
		e.Barrier()
	}

	for li := range g.layers {
		l := &g.layers[li]

		norm(l.attnNorm)
		mm(g.nrmBuf, l.qkv, g.qkv, false)

		rp := vkRopeParams{M: m32, NH: int32(hp.NHeads), NKV: int32(hp.NKVHeads), HD: int32(hp.HeadDim),
			Eps: hp.RMSNormEps, Ld: int32(ld), QOff: 0, KOff: int32(hp.QDim),
			QNormOff: int32(l.qNorm), KNormOff: int32(l.kNorm), PosOff: 0}
		use("qk_norm_rope", g.qkv, g.att, g.nrm)
		e.SetBytes(unsafePtr(&rp), 44, 0)
		e.Dispatch(total, hp.NHeads+hp.NKVHeads, 1, hp.HeadDim/2, 1, 1)
		e.Barrier()

		ap := vkAttnParams{M: m32, NH: int32(hp.NHeads), NKV: int32(hp.NKVHeads), HD: int32(hp.HeadDim),
			Scale: float32(1.0 / math.Sqrt(float64(hp.HeadDim))), Ld: int32(ld),
			QOff: 0, KOff: int32(hp.QDim), VOff: int32(hp.QDim + hp.KVDim),
			OLd: int32(hp.QDim), OOff: 0, SegOff: m32}
		use("attention", g.qkv, g.att, g.nrm)
		e.SetBytes(unsafePtr(&ap), 48, 0)
		e.Dispatch(total, hp.NHeads, 1, hp.HeadDim, 1, 1)
		e.Barrier()

		mm(g.att, l.o, g.x, true)

		norm(l.ffNorm)
		mm(g.nrmBuf, l.gu, g.gu, false)

		sp := vkSiluParams{M: m32, F: int32(hp.FFDim), Ld: int32(2 * hp.FFDim)}
		use("silu_mul", g.gu, g.g, g.nrm)
		e.SetBytes(unsafePtr(&sp), 12, 0)
		e.Dispatch((hp.FFDim+255)/256, total, 1, 256, 1, 1)
		e.Barrier()

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

// unsafePtr returns the address of v for push-constant uploads. Vulkan copies
// push-constant data during vkCmdPushConstants, so the caller need not keep it
// alive past the call (the encoder keeps it alive until submit anyway).
func unsafePtr[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }

// vkF16ToF32 converts a half-precision bit pattern to float32. (The darwin
// backend has its own f16ToF32; this one is shared by the Vulkan build tags.)
func vkF16ToF32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	man := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		if man == 0 {
			return math.Float32frombits(sign)
		}
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
