package asr

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/npu/nnapi"
)

// svNPU is a whole-encoder NNAPI session. Nil means the CPU kernels stay in use.
type svNPU struct {
	sess *nnapi.Session
}

// initNPU compiles the SAN-M encoder and CTC argmax as one accelerator graph.
// Desktop builds return immediately. A missing accelerator, a rejected graph,
// or a failed numeric check leaves the existing Go kernels in place.
func (m *SenseVoiceModel) initNPU() {
	if runtime.GOOS != "android" {
		return
	}
	if nnapi.Disabled("MACLAW_SENSEVOICE_NPU") {
		m.npuReason = "disabled by env"
		log.Printf("asr: SenseVoice NPU unavailable: %s", m.npuReason)
		return
	}
	if !nnapi.Available() {
		m.npuReason = nnapi.UnavailableReason()
		log.Printf("asr: SenseVoice NPU unavailable: %s", m.npuReason)
		return
	}
	b, err := m.buildSenseVoiceNPU()
	if err != nil {
		m.npuReason = err.Error()
		log.Printf("asr: SenseVoice NPU unavailable: %s", m.npuReason)
		return
	}
	uploaded := b.ConstBytes()
	log.Printf("asr: compiling SenseVoice encoder for NPU (%d MB)", uploaded>>20)
	cache := nnapi.CacheDir(m.modelPath)
	if cache == "" {
		log.Printf("asr: SenseVoice NPU cache unavailable, this compile will not be reused")
	}
	sess, err := nnapi.Compile(b, cache, m.svNPUCacheToken())
	debug.FreeOSMemory()
	if err != nil {
		m.npuReason = err.Error()
		log.Printf("asr: SenseVoice NPU unavailable: %s", m.npuReason)
		return
	}
	if err := m.svNPUSelfCheck(sess); err != nil {
		sess.Release()
		m.npuReason = "self-check: " + err.Error()
		log.Printf("asr: SenseVoice NPU unavailable: %s", m.npuReason)
		return
	}
	m.npu = &svNPU{sess: sess}
	log.Printf("asr: SenseVoice NPU backend active (%s, %d MB)", sess.Device(), uploaded>>20)
}

// NPUBackend reports whether the accelerator encoder is active and, if not, why.
func (m *SenseVoiceModel) NPUBackend() (bool, string) {
	if m == nil || m.npu == nil || m.npu.sess == nil {
		if m == nil {
			return false, ""
		}
		return false, m.npuReason
	}
	return true, m.npu.sess.Device()
}

func (m *SenseVoiceModel) releaseNPU() {
	if m.npu != nil && m.npu.sess != nil {
		m.npu.sess.Release()
	}
	m.npu = nil
}

// encodeArgmaxNPU runs the compiled graph. Caller holds m.mu.
// A runtime failure releases the session and returns ok=false.
func (m *SenseVoiceModel) encodeArgmaxNPU(lfrFeats []float32, lfrFrames int) ([]int, bool) {
	if m.npu == nil || m.npu.sess == nil {
		return nil, false
	}
	feats, total := m.prepFeats(lfrFeats, lfrFrames)
	outs, err := m.npu.sess.Run([]nnapi.ExecIn{{
		F32:   feats,
		Shape: []int{total, m.hp.FeatsDim},
	}})
	if err != nil || len(outs) < 2 || len(outs[1].I32) < total {
		if err != nil && nnapi.Transient(err) {
			log.Printf("asr: SenseVoice NPU transient failure, keeping session: %v", err)
		} else {
			log.Printf("asr: SenseVoice NPU failed, falling back to CPU: %v", err)
			m.releaseNPU()
		}
		return nil, false
	}
	bufs := m.encBufs
	if cap(bufs.ctcIDs) < total {
		bufs.ctcIDs = make([]int, total)
	}
	ids := bufs.ctcIDs[:total]
	for i := 0; i < total; i++ {
		ids[i] = int(outs[1].I32[i])
	}
	return ids, true
}

func (m *SenseVoiceModel) svNPUSelfCheck(sess *nnapi.Session) error {
	if m.hp.FeatsDim != svFeatsDim {
		return fmt.Errorf("feats dim %d", m.hp.FeatsDim)
	}
	// The graph uploads dequantized Q8 floats. Android's CPU encoder quantizes
	// activations (Q8P) and, when enabled, the FFN-down Q4 panel. Those kernels
	// are a different algorithm, so the cosine gate has to see the float path
	// or a correct accelerator graph is rejected. Production CPU fallback gets
	// the panels back when this returns.
	restore := m.detachQuantPanels()
	defer restore()
	pcm := make([]float32, 16000*2)
	seed := uint32(12345)
	for i := range pcm {
		seed = seed*1664525 + 1013904223
		t := float64(i) / 16000
		pcm[i] = float32(0.3*math.Sin(2*math.Pi*(220+200*t)*t) + 0.05*(float64(seed>>8)/float64(1<<24)-0.5))
	}
	_, lfr, n := m.frontend(pcm)
	if n <= 0 {
		return fmt.Errorf("frontend failed")
	}
	prepared, total := m.prepFeats(lfr, n)
	snap := append([]float32(nil), prepared...)
	ref, totalCPU := m.encodeHidden(lfr, n)
	if totalCPU != total {
		return fmt.Errorf("frames npu %d cpu %d", total, totalCPU)
	}
	hidden := m.hp.HiddenSize
	refCopy := append([]float32(nil), ref[:total*hidden]...)
	outs, err := sess.Run([]nnapi.ExecIn{{F32: snap, Shape: []int{total, m.hp.FeatsDim}}})
	if err != nil {
		return err
	}
	if len(outs) < 2 || len(outs[0].F32) < total*hidden || len(outs[1].I32) < total {
		return fmt.Errorf("short npu output")
	}
	mean, nOK, bad := svRowCosine(refCopy, outs[0].F32, total, hidden)
	ids := make([]int, total)
	matMulLinearArgmax(ids, refCopy, m.w.ctcW, m.w.ctcB, total, m.hp.VocabSize, hidden)
	agree := 0
	for i, id := range ids {
		if id == int(outs[1].I32[i]) {
			agree++
		}
	}
	min := svNPUCosMin()
	log.Printf("asr: SenseVoice NPU self-check cosine %.4f (%d/%d rows >= 0.85) argmax %d/%d", mean, nOK, total, agree, total)
	if bad || mean < min || nOK*2 < total {
		return fmt.Errorf("cosine %.4f < %.2f or only %d/%d rows >= 0.85", mean, min, nOK, total)
	}
	return nil
}

// detachQuantPanels hides the int8 activation panels and the Q4 FFN panel
// for the duration of the returned restore call.
func (m *SenseVoiceModel) detachQuantPanels() func() {
	type saved struct {
		w  *linearWeight
		p  *tensor.Q8Packed
		q4 *tensor.Q4Tensor
	}
	var kept []saved
	stash := func(w *linearWeight) {
		if w.q8p == nil && w.q4 == nil {
			return
		}
		kept = append(kept, saved{w, w.q8p, w.q4})
		w.q8p = nil
		w.q4 = nil
	}
	layers := make([]*svLayerWeights, 0, 1+len(m.w.encoders)+len(m.w.tpEncoders))
	layers = append(layers, &m.w.encoder0)
	for i := range m.w.encoders {
		layers = append(layers, &m.w.encoders[i])
	}
	for i := range m.w.tpEncoders {
		layers = append(layers, &m.w.tpEncoders[i])
	}
	for _, l := range layers {
		stash(&l.qW)
		stash(&l.qB)
		stash(&l.kW)
		stash(&l.kB)
		stash(&l.vW)
		stash(&l.vB)
		stash(&l.outW)
		stash(&l.outB)
		stash(&l.ff1W)
		stash(&l.ff1B)
		stash(&l.ff2W)
		stash(&l.ff2B)
	}
	stash(&m.w.ctcW)
	return func() {
		for _, s := range kept {
			s.w.q8p = s.p
			s.w.q4 = s.q4
		}
	}
}

func svNPUCosMin() float64 {
	s := os.Getenv("MACLAW_SENSEVOICE_NPU_COS")
	if s == "" {
		return 0.90
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.90
	}
	return v
}

func svRowCosine(a, b []float32, rows, dim int) (mean float64, nOK int, bad bool) {
	if rows <= 0 || dim <= 0 || len(a) < rows*dim || len(b) < rows*dim {
		return 0, 0, true
	}
	sum := 0.0
	for r := 0; r < rows; r++ {
		var dot, na, nb float64
		for c := 0; c < dim; c++ {
			x := float64(a[r*dim+c])
			y := float64(b[r*dim+c])
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				return 0, 0, true
			}
			dot += x * y
			na += x * x
			nb += y * y
		}
		cos := 0.0
		if na > 0 && nb > 0 {
			cos = dot / math.Sqrt(na*nb)
		}
		sum += cos
		if cos >= 0.85 {
			nOK++
		}
	}
	return sum / float64(rows), nOK, false
}

func (m *SenseVoiceModel) svNPUCacheToken() [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "sv-npu-v2 %d %d %d %d %d %d %d %d\n",
		m.hp.HiddenSize, m.hp.NumHeads, m.hp.LinearUnits, m.hp.FSMNKernel,
		m.hp.FeatsDim, m.hp.VocabSize, len(m.w.encoders), len(m.w.tpEncoders))
	var layers []svLayerWeights
	layers = append(layers, m.w.encoder0)
	layers = append(layers, m.w.encoders...)
	layers = append(layers, m.w.tpEncoders...)
	for _, l := range layers {
		sampleLinear(h, l.qW)
		sampleLinear(h, l.qB)
		sampleLinear(h, l.kW)
		sampleLinear(h, l.kB)
		sampleLinear(h, l.vW)
		sampleLinear(h, l.vB)
		sampleLinear(h, l.outW)
		sampleLinear(h, l.outB)
		sampleLinear(h, l.ff1W)
		sampleLinear(h, l.ff1B)
		sampleLinear(h, l.ff2W)
		sampleLinear(h, l.ff2B)
		sampleF32(h, l.fsmnW)
		sampleF32(h, l.norm1W)
		sampleF32(h, l.norm1B)
		sampleF32(h, l.norm2W)
		sampleF32(h, l.norm2B)
	}
	sampleLinear(h, m.w.ctcW)
	sampleF32(h, m.w.ctcB)
	sampleF32(h, m.w.afterNormW)
	sampleF32(h, m.w.afterNormB)
	sampleF32(h, m.w.tpNormW)
	sampleF32(h, m.w.tpNormB)
	var token [32]byte
	copy(token[:], h.Sum(nil))
	return token
}

// sampleLinear, sampleBytes, and sampleF32 digest every constant the graph
// uploads. NNAPI identifies a cached compilation by the 32-byte token alone
// and cannot detect a collision; a hit may run the other graph. The shape
// rides along because the same bytes are a different matrix when rows change.
func sampleLinear(h interface{ Write([]byte) (int, error) }, w linearWeight) {
	if w.q8 != nil {
		var dim [8]byte
		binary.LittleEndian.PutUint32(dim[0:4], uint32(w.q8.Rows))
		binary.LittleEndian.PutUint32(dim[4:8], uint32(w.q8.Cols))
		_, _ = h.Write(dim[:])
		sampleBytes(h, w.q8.Data)
		return
	}
	var rows [4]byte
	binary.LittleEndian.PutUint32(rows[:], uint32(w.rows))
	_, _ = h.Write(rows[:])
	sampleF32(h, w.f32)
}

func sampleBytes(h interface{ Write([]byte) (int, error) }, b []byte) {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(b)))
	_, _ = h.Write(hdr[:])
	_, _ = h.Write(b)
}

func sampleF32(h interface{ Write([]byte) (int, error) }, v []float32) {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(v)))
	_, _ = h.Write(hdr[:])
	if len(v) == 0 {
		return
	}
	// arm64 and amd64 store float32 little-endian, the same image
	// binary.LittleEndian of Float32bits would write.
	_, _ = h.Write(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), len(v)*4))
}

// buildSenseVoiceNPU lowers the encoder the CPU runs in encodeHidden, plus CTC argmax.
// The graph input is the already-prepared [time, feats] feature matrix from prepFeats.
// Outputs are the encoder hidden state and the per-frame CTC argmax.
func (m *SenseVoiceModel) buildSenseVoiceNPU() (*nnapi.Builder, error) {
	if m.hp.HiddenSize <= 0 || m.hp.FeatsDim <= 0 || m.hp.VocabSize <= 0 || m.hp.NumHeads <= 0 {
		return nil, fmt.Errorf("sensevoice npu: incomplete hyperparameters")
	}
	if m.hp.HiddenSize%m.hp.NumHeads != 0 {
		return nil, fmt.Errorf("sensevoice npu: hidden %d heads %d", m.hp.HiddenSize, m.hp.NumHeads)
	}
	b := &nnapi.Builder{}
	x := b.InputF32([]int{0, m.hp.FeatsDim})
	var err error
	x, err = m.lowerSANM(b, x, m.hp.FeatsDim, &m.w.encoder0)
	if err != nil {
		return nil, fmt.Errorf("sensevoice npu: entry: %w", err)
	}
	for i := range m.w.encoders {
		x, err = m.lowerSANM(b, x, m.hp.HiddenSize, &m.w.encoders[i])
		if err != nil {
			return nil, fmt.Errorf("sensevoice npu: encoder %d: %w", i, err)
		}
	}
	x, err = m.npuNorm(b, x, m.w.afterNormW, m.w.afterNormB, "after_norm")
	if err != nil {
		return nil, err
	}
	for i := range m.w.tpEncoders {
		x, err = m.lowerSANM(b, x, m.hp.HiddenSize, &m.w.tpEncoders[i])
		if err != nil {
			return nil, fmt.Errorf("sensevoice npu: tp %d: %w", i, err)
		}
	}
	x, err = m.npuNorm(b, x, m.w.tpNormW, m.w.tpNormB, "tp_norm")
	if err != nil {
		return nil, err
	}
	logits, err := m.npuFC(b, x, m.w.ctcW, m.w.ctcB, m.hp.VocabSize, false)
	if err != nil {
		return nil, fmt.Errorf("sensevoice npu: ctc: %w", err)
	}
	ids := b.ArgMax(logits, 1)
	if err := b.Err(); err != nil {
		return nil, err
	}
	b.Output(x)
	b.Output(ids)
	if err := b.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func (m *SenseVoiceModel) npuNorm(b *nnapi.Builder, x int, w, bias []float32, name string) (int, error) {
	h := m.hp.HiddenSize
	if len(w) != h || len(bias) != h {
		return -1, fmt.Errorf("sensevoice npu: %s len %d/%d want %d", name, len(w), len(bias), h)
	}
	y := b.LayerNormLast(x, b.ConstF32([]int{h}, w), b.ConstF32([]int{h}, bias), 1e-5)
	if err := b.Err(); err != nil {
		return -1, err
	}
	return y, nil
}

func (m *SenseVoiceModel) lowerSANM(b *nnapi.Builder, x, inDim int, l *svLayerWeights) (int, error) {
	hidden := m.hp.HiddenSize
	if len(l.norm1W) != inDim || len(l.norm1B) != inDim {
		return -1, fmt.Errorf("norm1 %d/%d want %d", len(l.norm1W), len(l.norm1B), inDim)
	}
	h := b.LayerNormLast(x, b.ConstF32([]int{inDim}, l.norm1W), b.ConstF32([]int{inDim}, l.norm1B), 1e-5)
	var q, k, v int
	var err error
	if l.fusedQKV {
		var qkv int
		qkv, err = m.npuFC(b, h, l.qW, l.qB.f32, 3*hidden, false)
		if err != nil {
			return -1, err
		}
		parts := b.SplitLast(qkv, 3)
		if len(parts) != 3 {
			if b.Err() != nil {
				return -1, b.Err()
			}
			return -1, fmt.Errorf("split qkv")
		}
		q, k, v = parts[0], parts[1], parts[2]
	} else {
		if q, err = m.npuFC(b, h, l.qW, l.qB.f32, hidden, false); err != nil {
			return -1, err
		}
		if k, err = m.npuFC(b, h, l.kW, l.kB.f32, hidden, false); err != nil {
			return -1, err
		}
		if v, err = m.npuFC(b, h, l.vW, l.vB.f32, hidden, false); err != nil {
			return -1, err
		}
	}
	attn := b.Attention(q, k, v, hidden, m.hp.NumHeads)
	proj, err := m.npuFC(b, attn, l.outW, l.outB.f32, hidden, false)
	if err != nil {
		return -1, err
	}
	fsmn, err := m.lowerFSMN(b, v, l)
	if err != nil {
		return -1, err
	}
	sum := b.Add(proj, fsmn)
	if inDim == hidden {
		sum = b.Add(sum, x)
	}
	if len(l.norm2W) != hidden || len(l.norm2B) != hidden {
		return -1, fmt.Errorf("norm2 %d/%d want %d", len(l.norm2W), len(l.norm2B), hidden)
	}
	ln2 := b.LayerNormLast(sum, b.ConstF32([]int{hidden}, l.norm2W), b.ConstF32([]int{hidden}, l.norm2B), 1e-5)
	ffRows := l.ff1W.Rows()
	if ffRows <= 0 {
		return -1, fmt.Errorf("ff1 rows")
	}
	ff, err := m.npuFC(b, ln2, l.ff1W, l.ff1B.f32, ffRows, true)
	if err != nil {
		return -1, err
	}
	ff2, err := m.npuFC(b, ff, l.ff2W, l.ff2B.f32, hidden, false)
	if err != nil {
		return -1, err
	}
	y := b.Add(sum, ff2)
	if err := b.Err(); err != nil {
		return -1, err
	}
	return y, nil
}

func (m *SenseVoiceModel) lowerFSMN(b *nnapi.Builder, v int, l *svLayerWeights) (int, error) {
	hidden := m.hp.HiddenSize
	k := m.hp.FSMNKernel
	if k <= 0 {
		return -1, fmt.Errorf("fsmn kernel")
	}
	if len(l.fsmnW) == 0 {
		return v, nil
	}
	if len(l.fsmnW) != k*hidden {
		return -1, fmt.Errorf("fsmn len %d want %d", len(l.fsmnW), k*hidden)
	}
	rv := b.Reshape(v, []int{1, -1, 1, hidden})
	filter := b.ConstF32([]int{1, k, 1, hidden}, l.fsmnW)
	bias := b.ConstF32([]int{hidden}, make([]float32, hidden))
	dw := b.DepthwiseSame(rv, filter, bias)
	back := b.Reshape(dw, []int{-1, hidden})
	y := b.Add(back, v)
	if err := b.Err(); err != nil {
		return -1, err
	}
	return y, nil
}

func (m *SenseVoiceModel) npuFC(b *nnapi.Builder, x int, w linearWeight, bias []float32, units int, relu bool) (int, error) {
	wid, rows, _, err := svNPUWeight(b, w)
	if err != nil {
		return -1, err
	}
	if rows != units {
		return -1, fmt.Errorf("weight rows %d want %d", rows, units)
	}
	bid := -1
	if bias != nil {
		if len(bias) < units {
			return -1, fmt.Errorf("bias %d want %d", len(bias), units)
		}
		bid = b.ConstF32([]int{units}, bias[:units])
	}
	y := b.FullyConnected(x, wid, bid, relu)
	if err := b.Err(); err != nil {
		return -1, err
	}
	return y, nil
}

// svNPUWeight places a fully-connected matrix on the graph. Q8_0 rows are
// dequantized into ashmem at upload time, so the Go heap does not hold a
// second copy of the encoder.
func svNPUWeight(b *nnapi.Builder, w linearWeight) (int, int, int, error) {
	if w.q8 != nil {
		rows, cols := w.q8.Rows, w.q8.Cols
		if rows <= 0 || cols <= 0 || cols%32 != 0 {
			return -1, 0, 0, fmt.Errorf("bad q8 %dx%d", rows, cols)
		}
		need := rows * (cols / 32) * 34
		if len(w.q8.Data) < need {
			return -1, 0, 0, fmt.Errorf("q8 data %d want %d", len(w.q8.Data), need)
		}
		q8 := w.q8
		id := b.ConstF32Fill([]int{rows, cols}, func(dst []float32) error {
			return svDequantQ8Into(q8, dst)
		})
		if err := b.Err(); err != nil {
			return -1, 0, 0, err
		}
		return id, rows, cols, nil
	}
	data, rows, cols, err := svLinearF32(w)
	if err != nil {
		return -1, 0, 0, err
	}
	id := b.ConstF32([]int{rows, cols}, data)
	if err := b.Err(); err != nil {
		return -1, 0, 0, err
	}
	return id, rows, cols, nil
}

// svNPUDequantParallelMin is the float count where row dequant is split
// across cores. Smaller matrices stay on one core; the launch is larger
// than the copy they save.
const svNPUDequantParallelMin = 256 * 1024

// svDequantQ8Into writes FP32 rows straight into dst. DequantRow's assembly
// stores exactly Cols floats from the pointer and does not read the slice
// cap, so each row is a three-index slice whose backing store is that row.
func svDequantQ8Into(q8 *tensor.Q8Tensor, dst []float32) error {
	if q8 == nil {
		return fmt.Errorf("dequant nil")
	}
	rows, cols := q8.Rows, q8.Cols
	if rows <= 0 || cols <= 0 || len(dst) != rows*cols {
		return fmt.Errorf("dequant dst %d", len(dst))
	}
	write := func(begin, end int) {
		for r := begin; r < end; r++ {
			q8.DequantRow(r, dst[r*cols:(r+1)*cols:(r+1)*cols])
		}
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 || rows < 2 || rows*cols < svNPUDequantParallelMin {
		write(0, rows)
		return nil
	}
	if workers > rows {
		workers = rows
	}
	chunk := (rows + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		begin := w * chunk
		if begin >= rows {
			break
		}
		end := begin + chunk
		if end > rows {
			end = rows
		}
		wg.Add(1)
		go func(begin, end int) {
			defer wg.Done()
			write(begin, end)
		}(begin, end)
	}
	wg.Wait()
	return nil
}

func svLinearF32(w linearWeight) ([]float32, int, int, error) {
	if w.q8 != nil {
		rows, cols := w.q8.Rows, w.q8.Cols
		if rows <= 0 || cols <= 0 {
			return nil, 0, 0, fmt.Errorf("bad q8 %dx%d", rows, cols)
		}
		data := make([]float32, rows*cols)
		row := make([]float32, cols)
		for r := 0; r < rows; r++ {
			w.q8.DequantRow(r, row)
			copy(data[r*cols:(r+1)*cols], row)
		}
		return data, rows, cols, nil
	}
	rows := w.Rows()
	if rows <= 0 || len(w.f32) == 0 || len(w.f32)%rows != 0 {
		return nil, 0, 0, fmt.Errorf("bad f32 weight rows %d len %d", rows, len(w.f32))
	}
	return w.f32, rows, len(w.f32) / rows, nil
}
