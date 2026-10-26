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
	"strconv"
	"strings"
	"sync"
	"time"
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
	normAll     *vk.Buffer // output-normed state of every row (mean pooling)

	layers  []qwen3VKLayer
	maxPos  int
	capRows int

	outNorm int // float offset of the output norm weight in nrm

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

// initVulkan tries to bring up the Vulkan backend. Any failure returns an
// error and the CPU path stays in charge.
func (q *Qwen3Embedder) initVulkan() {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		// macOS has Metal; other platforms are not shipped.
		return
	}
	if os.Getenv("MACLAW_QWEN3_VULKAN") == "0" {
		q.setGPUReason("disabled by MACLAW_QWEN3_VULKAN=0")
		return
	}
	if !HWAccelPreferred() {
		// The user turned hardware acceleration off: never pay the ~450 MB
		// upload and probe cost just to release the device afterwards.
		q.setGPUReason("hardware acceleration off")
		return
	}
	// The weight blob lives in host-visible device-local memory, which on an
	// integrated GPU is ordinary system RAM. On a low-memory machine that
	// upload would push the process into swap, so refuse it up front.
	if os.Getenv(EmbedMemGuardEnv) != "0" {
		if need := qwen3VKWeightBytes(q) + 256<<20; need > 0 {
			if avail, ok := availableMemoryFn(); ok && avail < need {
				q.setGPUReason(fmt.Sprintf("low memory: need ~%d MB, available %d MB", need>>20, avail>>20))
				return
			}
		}
	}
	g, err := newQwen3VKGPU(q)
	if err != nil {
		q.setGPUReason(err.Error())
		return
	}
	// One probe covers both gates: the same timed rounds produce the vectors
	// for the numeric self-check and the CPU/GPU speed ratio, so the backend
	// decision costs two forwards per engine instead of four.
	probeTexts := []string{
		"Vulkan self-check: send this file to zhangsan",
		"查询 api2 服务器状态",
	}
	ratio, cpuDur, gpuDur, minCos, worst, err := q.vkProbe(g, probeTexts)
	if err != nil {
		g.release()
		q.setGPUReason("probe: " + err.Error())
		return
	}
	// Correctness before speed: a fast backend that computes the wrong vector
	// is worthless, so the numeric gate runs first and is absolute.
	if !(minCos >= qwen3VKSelfCheckMinCos) {
		g.release()
		q.setGPUGate(GPUGate{Backend: BackendVulkan, Ratio: ratio, CPUMs: ms(cpuDur), GPUMs: ms(gpuDur),
			MinRatio: qwen3VKMinSpeedRatio(), CosFloor: qwen3VKSelfCheckMinCos, MinCos: minCos,
			Reason: fmt.Sprintf("numeric check: worst cos %.6f", minCos)})
		q.setGPUReason(fmt.Sprintf("numeric check: worst cos %.6f < %.4f (probe text %d)",
			minCos, qwen3VKSelfCheckMinCos, worst))
		log.Printf("qwen3: Vulkan disabled — %s", q.GPUReason())
		return
	}
	if ratio < qwen3VKMinSpeedRatio() {
		g.release()
		q.setGPUGate(GPUGate{Backend: BackendVulkan, Ratio: ratio, CPUMs: ms(cpuDur), GPUMs: ms(gpuDur),
			MinRatio: qwen3VKMinSpeedRatio(), CosFloor: qwen3VKSelfCheckMinCos, MinCos: minCos,
			Reason: fmt.Sprintf("gpu too slow: %.2fx", ratio)})
		q.setGPUReason(fmt.Sprintf("gpu too slow: %.2fx of CPU (need %.2fx); cpu %.1fms, vulkan %.1fms",
			ratio, qwen3VKMinSpeedRatio(), ms(cpuDur), ms(gpuDur)))
		log.Printf("qwen3: Vulkan disabled — %s", q.GPUReason())
		return
	}
	q.setGPUReason("")
	q.setGPUGate(GPUGate{
		Backend:  BackendVulkan,
		Accepted: true,
		Ratio:    ratio,
		CPUMs:    ms(cpuDur),
		GPUMs:    ms(gpuDur),
		MinRatio: qwen3VKMinSpeedRatio(),
		CosFloor: qwen3VKSelfCheckMinCos,
		MinCos:   minCos,
	})
	q.vkGPU.Store(g)
	log.Printf("qwen3: Vulkan backend active (%s, %d MB weights, %.2fx CPU speed: cpu %.1fms vs vulkan %.1fms)",
		g.dev.Info.Name, g.WeightBytes>>20, ratio, ms(cpuDur), ms(gpuDur))
}

// qwen3VKBatchRows caps how many rows one Vulkan forward packs.
//
// The weights (495 MB) dominate the GPU footprint, but activations grow linearly
// with rows (~70 KB each), so an unbounded bulk import would reserve hundreds of
// MB for a forward that is weight-bandwidth-bound anyway. 512 rows keeps the
// activation arena near 35 MB with no measurable throughput loss.
func qwen3VKBatchRows() int { return qwen3VKMaxRows }

// qwen3VKActBytes is the activation footprint for rowsRows rows, matching
// qwen3VKGPU.ActBytes so the device-selection headroom is realistic.
func qwen3VKActBytes(hp Qwen3HParams, rows int) uint64 {
	return uint64(rows) * uint64(qwen3VKRowFloats(hp)) * 4
}

// qwen3VKRowFloats is the activation footprint of one row, in float32 slots.
// One definition for both the device-selection headroom and the reported
// footprint: the two had already drifted apart once (a missing buffer).
func qwen3VKRowFloats(hp Qwen3HParams) int {
	return 3*hp.Dim + // x, nrmBuf, normAll
		2*hp.QDim + // qkv Q, attention out
		2*hp.KVDim + // qkv K/V
		3*hp.FFDim + // gate, up, fused silu
		2 // positions + segment bases
}

// qwen3VKWeightSizes returns the exact arena sizes: int8 payload elements, f32
// scale elements, and norm-weight elements. Both the memory guard (which runs
// before allocation) and allocWeights use it, so they cannot disagree.
func qwen3VKWeightSizes(q *Qwen3Embedder) (qBytes, sFloats, normFloats int) {
	hp := q.hp
	for range q.layers {
		for _, nk := range [][2]int{
			{hp.QDim + 2*hp.KVDim, hp.Dim}, // qkv
			{hp.Dim, hp.QDim},              // wo
			{2 * hp.FFDim, hp.Dim},         // gate + up
			{hp.Dim, hp.FFDim},             // down
		} {
			qBytes += nk[0] * nk[1]
			sFloats += nk[0] * nk[1] / 32
		}
	}
	normFloats = len(q.layers)*(2*hp.Dim+2*hp.HeadDim) + hp.Dim // + output norm
	return qBytes, sFloats, normFloats
}

// qwen3VKWeightBytes is qwen3VKWeightSizes in bytes, for the memory guard.
func qwen3VKWeightBytes(q *Qwen3Embedder) uint64 {
	qBytes, sFloats, normFloats := qwen3VKWeightSizes(q)
	return uint64(qBytes) + uint64(sFloats)*4 + uint64(normFloats)*4
}

// qwen3VKSelfCheckMinCos is the minimum cosine between the Vulkan and CPU
// embeddings. Measured 0.99996-0.99999 on the reference hardware; the CPU's own
// Q8P-vs-float gap is larger than this.
const qwen3VKSelfCheckMinCos = 0.999

// qwen3VKMinSpeedRatio is the minimum Vulkan/CPU speed ratio required to keep
// the GPU backend (0.5 = Vulkan may be at most 2x slower than the CPU).
// MACLAW_QWEN3_VULKAN_MIN_RATIO overrides it; 0 accepts any GPU that passed the
// numeric self-check.
func qwen3VKMinSpeedRatio() float64 {
	if v := strings.TrimSpace(os.Getenv("MACLAW_QWEN3_VULKAN_MIN_RATIO")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 0.5
}

// qwen3VKProbeHook lets tests corrupt the probe's GPU output so the numeric
// gate can be verified to actually reject a wrong backend. Nil in production.
var qwen3VKProbeHook func(hid []float32)

// vkProbe runs the numeric and speed gates in one pass.
//
// It times `runs` embeddings of the same texts on both engines and returns
// cpuDuration/gpuDuration (>1 means the GPU is faster) plus the WORST cosine
// between the GPU and CPU vectors over every probe text, and which text was
// worst. Comparing all texts (not just the first) is what catches packing bugs:
// the GPU packs them into one forward, so a broken causal mask or segment base
// would corrupt some sequences while leaving the first one correct.
//
// The CPU side calls EmbedBatch (the production CPU path: parallel across
// texts, single-threaded kernels); the GPU side packs the same texts into one
// forward. Comparing a packed GPU forward against sequential single-text CPU
// forwards would flatter the GPU by a wide margin. Best-of-N is used on both
// sides. q.vkGPU must still be nil, otherwise EmbedBatch would route to the GPU.
func (q *Qwen3Embedder) vkProbe(g *qwen3VKGPU, texts []string) (ratio float64, cpuDur, gpuDur time.Duration, minCos float64, worst int, err error) {
	const runs = 2
	seqs := make([][]int, len(texts))
	for i, t := range texts {
		seqs[i] = q.Tokenize(t)
		if len(seqs[i]) == 0 {
			return 0, 0, 0, 0, 0, fmt.Errorf("empty probe text %d", i)
		}
	}
	hid := make([][]float32, len(seqs))
	for i := range hid {
		hid[i] = make([]float32, q.hp.Dim)
	}
	var cpuVecs [][]float32
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		if herr := g.forward(q, seqs, hid); herr != nil {
			return 0, 0, 0, 0, 0, herr
		}
		if d := time.Since(t0); gpuDur == 0 || d < gpuDur {
			gpuDur = d
		}
		t1 := time.Now()
		vecs, cerr := q.EmbedBatch(texts)
		if d := time.Since(t1); cpuDur == 0 || d < cpuDur {
			cpuDur = d
		}
		if cerr != nil {
			return 0, 0, 0, 0, 0, cerr
		}
		if i == 0 {
			cpuVecs = vecs
		}
	}
	if gpuDur <= 0 || len(cpuVecs) != len(hid) {
		return 0, 0, 0, 0, 0, fmt.Errorf("probe produced no measurement")
	}
	if qwen3VKProbeHook != nil {
		for k := range hid {
			qwen3VKProbeHook(hid[k])
		}
	}
	// forward returns the raw last row; EmbedBatch's vectors are already
	// output-normed and MRL-truncated, so normalise only the GPU side and
	// compare the same width on both.
	width := q.dim
	if width <= 0 || width > q.hp.Dim {
		width = q.hp.Dim
	}
	minCos, worst = 1, 0
	for i := range hid {
		if !allFinite(hid[i]) {
			return 0, 0, 0, 0, i, fmt.Errorf("non-finite GPU output for probe text %d", i)
		}
		if len(cpuVecs[i]) < width {
			return 0, 0, 0, 0, i, fmt.Errorf("cpu probe vector %d is %d wide, want %d", i, len(cpuVecs[i]), width)
		}
		n := make([]float32, q.hp.Dim)
		tensor.RMSNorm(n, hid[i], q.outputNorm, q.hp.RMSNormEps)
		c := cosineF32(n[:width], cpuVecs[i][:width])
		if math.IsNaN(c) || c < minCos {
			minCos, worst = c, i
		}
	}
	return float64(cpuDur) / float64(gpuDur), cpuDur, gpuDur, minCos, worst, nil
}

// allFinite reports whether every value is a real number (no NaN/Inf). A GPU
// that produces NaN would otherwise only show up as a failed cosine.
func allFinite(v []float32) bool {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}

// ms renders a duration in milliseconds for log/diagnostic text.
func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

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
	// Ask for a device whose host-visible heap can hold the weights plus room
	// for activations. On a hybrid laptop this skips a discrete GPU whose BAR
	// heap is too small in favour of the integrated one, whose heap is shared
	// system memory.
	need := qwen3VKWeightBytes(q) + qwen3VKActBytes(hp, 64)
	dev, err := vk.NewDeviceWithMinHeap(need)
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
	if err := g.allocRows(qwen3VKMaxRows); err != nil {
		g.release()
		return nil, err
	}
	return g, nil
}

// allocWeights repacks every Q8_0 weight into the GPU blobs.
func (g *qwen3VKGPU) allocWeights(q *Qwen3Embedder) error {
	hp := g.hp
	qBytes, sFloats, normFloats := qwen3VKWeightSizes(q)
	var err error
	if g.wq, err = g.dev.NewBuffer(qBytes); err != nil {
		return err
	}
	if g.ws, err = g.dev.NewBuffer(sFloats * 4); err != nil {
		return err
	}
	if g.nrm, err = g.dev.NewBuffer(normFloats * 4); err != nil {
		return err
	}
	g.WeightBytes = qBytes + sFloats*4 + normFloats*4

	qa, sa, na := g.wq.Bytes(), g.ws.Float32s(), g.nrm.Float32s()
	qo, so, no := 0, 0, 0
	// put splits each Q8_0 row into a contiguous int8 payload (so the shader
	// has no per-32-block interleaving to undo) plus a separate f32 scale
	// array. This is the single largest startup cost (~440 MB touched), so the
	// loop walks the source with a running offset instead of re-slicing.
	put := func(ts ...*tensor.Q8Tensor) (vkMat, error) {
		m := vkMat{qOff: qo, sOff: so * 4, K: ts[0].Cols}
		for _, t := range ts {
			if t.Cols != m.K || len(t.Data) < t.Rows*t.Cols/32*34 {
				return m, fmt.Errorf("vulkan: bad weight shape")
			}
			data := t.Data
			nb := t.Cols / 32
			src := 0
			for r := 0; r < t.Rows; r++ {
				for b := 0; b < nb; b++ {
					sa[so] = vkF16ToF32(uint16(data[src]) | uint16(data[src+1])<<8)
					so++
					copy(qa[qo:qo+32], data[src+2:src+34])
					qo += 32
					src += 34
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
	// The output norm weight lives in the same arena so the pooling tail can
	// use the ordinary rmsnorm_rows kernel.
	g.outNorm = putN(q.outputNorm)

	// The arenas were sized from the same loop above; check the packing actually
	// filled them. An undersized estimate would truncate copy() silently, and an
	// oversized one would leave zeros the kernels would read as weights.
	if qo != qBytes || so != sFloats || no != normFloats {
		return fmt.Errorf("vulkan: weight packing mismatch (int8 %d/%d, scales %d/%d, norms %d/%d)",
			qo, qBytes, so, sFloats, no, normFloats)
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

// qwen3VKMaxRows is the activation arena size in rows, fixed at load.
//
// The arena is NOT grown at runtime. Releasing and recreating mapped buffers
// while the driver still has them in flight crashed the AMD driver inside
// vkCreateBuffer (reproduced reliably once a batch triggered a growth, with
// vkDeviceWaitIdle and correct teardown ordering already in place), so the
// buffers are allocated once and a forward that would need more rows is
// rejected — the CPU path serves it instead. 512 rows costs ~35 MB and covers
// the batch packing cap; a single longer text falls back to the CPU.
var qwen3VKMaxRows = func() int {
	if v, err := strconv.Atoi(os.Getenv("MACLAW_QWEN3_VULKAN_MAX_ROWS")); err == nil && v >= 32 {
		return v
	}
	return 512
}()

// qwen3VKMaxSeqTokens is the longest single sequence the GPU handles.
//
// Beyond ~130 tokens the GPU loses to the CPU: the 32-row matmul tile makes
// every 32 extra rows re-read the whole 495 MB weight blob, and attention grows
// quadratically. Measured on the reference hardware (p50):
//
//	 33 tok: cpu  83 ms / gpu  84 ms
//	 45 tok: cpu 104 ms / gpu  97 ms
//	131 tok: cpu 360 ms / gpu 345 ms
//	380 tok: cpu 1045 ms / gpu 1754 ms
//
// So longer texts are left to the CPU: the GPU is here to save CPU cycles, not
// to make latency worse. MACLAW_QWEN3_VULKAN_MAX_TOKENS overrides the cap.
var qwen3VKMaxSeqTokens = func() int {
	if v, err := strconv.Atoi(os.Getenv("MACLAW_QWEN3_VULKAN_MAX_TOKENS")); err == nil && v > 0 {
		return v
	}
	return 128
}()

// ensureRows reserves the activation arena. It never reallocates: see
// qwen3VKMaxRows for why, and note that a request beyond the arena is an error
// rather than a silent truncation.
func (g *qwen3VKGPU) ensureRows(n int) error {
	if n <= g.capRows {
		return nil
	}
	return fmt.Errorf("vulkan: %d rows exceed the %d-row activation arena", n, g.capRows)
}

// allocRows allocates the fixed activation arena.
func (g *qwen3VKGPU) allocRows(n int) error {
	hp := g.hp
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
	g.normAll = alloc(n * hp.Dim)
	if err != nil {
		return err
	}
	g.capRows = n
	return nil
}

// shutdown releases the device after any in-flight forward completes. The
// plain release() is for paths that already own g.mu (construction failures);
// callers racing with inference must use this.
func (g *qwen3VKGPU) shutdown() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.release()
}

// ActBytes reports the activation footprint.
func (g *qwen3VKGPU) ActBytes() int {
	return g.capRows * 4 * qwen3VKRowFloats(g.hp)
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
	return g.run(q, seqs, outs, nil)
}

// run is the forward pass. poolSink non-nil selects mean-pooling mode: the
// layers run as usual, then one extra dispatch writes the output-normed state
// of EVERY row into normAll and sink is called with it (inside the lock, so a
// concurrent forward cannot overwrite the buffer). outs is then unused.
func (g *qwen3VKGPU) run(q *Qwen3Embedder, seqs [][]int, outs [][]float32, poolSink func(all []float32, rows, dim int)) error {
	hp := g.hp
	total := 0
	for i, s := range seqs {
		if len(s) == 0 || len(s) > g.maxPos {
			return fmt.Errorf("vulkan: sequence length %d out of range", len(s))
		}
		// The token cap is enforced here as well as in embedGPU so the
		// invariant holds for every caller: long sequences belong to the CPU.
		if len(s) > qwen3VKMaxSeqTokens {
			return fmt.Errorf("vulkan: sequence of %d tokens exceeds the %d-token GPU cap",
				len(s), qwen3VKMaxSeqTokens)
		}
		if poolSink == nil {
			if i >= len(outs) || len(outs[i]) < hp.Dim {
				return fmt.Errorf("vulkan: output %d has %d floats, want %d", i, len(outs[i]), hp.Dim)
			}
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
	if err := g.dev.Err(); err != nil {
		return fmt.Errorf("vulkan: %w", err)
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
			segBase[r] = int32(r) - pos[r]
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

	if poolSink != nil {
		// out = RMSNorm(x) * outputNorm, one workgroup per row.
		poolP := vkNormParams{M: m32, D: int32(dim), Eps: hp.RMSNormEps, Ld: int32(dim), WOff: int32(g.outNorm)}
		use("rmsnorm_rows", g.x, g.normAll, g.nrm)
		e.SetBytes(unsafePtr(&poolP), 20, 0)
		e.Dispatch(total, 1, 1, 256, 1, 1)
		e.Barrier()
	}
	if err := e.Wait(); err != nil {
		return err
	}
	if poolSink != nil {
		poolSink(g.normAll.Float32s()[:total*dim], total, dim)
		return nil
	}
	r = 0
	for i, s := range seqs {
		r += len(s)
		copy(outs[i], x[(r-1)*dim:r*dim])
	}
	return nil
}

// forwardPooled runs the forward and derives the mean-pooled and last-token
// vectors that EmbedPooledWithRole returns. Both are the full hidden width,
// already output-normed; the caller truncates and normalises. The CPU path
// splits the mean into a cached prefix sum plus the suffix, which is an
// optimisation only — the mean over all positions is the same value.
func (g *qwen3VKGPU) forwardPooled(q *Qwen3Embedder, ids []int, last, mean []float32) error {
	dim := q.hp.Dim
	if len(last) < dim || len(mean) < dim {
		return fmt.Errorf("vulkan: pooled outputs are %d/%d wide, want %d", len(last), len(mean), dim)
	}
	return g.run(q, [][]int{ids}, nil, func(all []float32, rows, d int) {
		clear(mean[:d])
		for r := 0; r < rows; r++ {
			row := all[r*d : (r+1)*d]
			for i := 0; i < d; i++ {
				mean[i] += row[i]
			}
		}
		inv := 1 / float32(rows)
		for i := 0; i < d; i++ {
			mean[i] *= inv
		}
		copy(last[:d], all[(rows-1)*d:rows*d])
	})
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

// VulkanMaxSeqTokens reports the longest single sequence the GPU backend
// handles; longer texts run on the CPU (see qwen3VKMaxSeqTokens).
func VulkanMaxSeqTokens() int { return qwen3VKMaxSeqTokens }
