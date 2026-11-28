package kokoro

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/npu/nnapi"
)

// albertNPU is the PL-BERT text encoder compiled as one accelerator graph.
// The vocoder (AdaIN, source, conv, iSTFT) stays on the CPU kernels.
type albertNPU struct {
	sess   *nnapi.Session
	vocab  int
	maxPos int
	hidden int
	emb    int
}

type albertGraphSpec struct {
	emb, hidden, heads, intermediate, layers int
	vocab, maxPos                            int
}

type albertNPUWeights struct {
	word, pos, typeEmb []float32
	lnW, lnB           []float32
	mapW, mapB         []float32
	qW, qB             []float32
	kW, kB             []float32
	vW, vB             []float32
	denseW, denseB     []float32
	attnLnW, attnLnB   []float32
	ffW, ffB           []float32
	ffOutW, ffOutB     []float32
	fullLnW, fullLnB   []float32
}

// albertNPUForward tries the accelerator. ok is false when the CPU path should run.
func (m *Model) albertNPUForward(ids []int) ([]float32, int, bool) {
	if m == nil || m.albertNPUBypass || runtime.GOOS != "android" || nnapi.Disabled("MACLAW_KOKORO_NPU") {
		return nil, 0, false
	}
	m.albertOnce.Do(m.initAlbertNPU)
	a := m.albert
	if a == nil || a.sess == nil {
		return nil, 0, false
	}
	seq := len(ids)
	if seq == 0 || seq > a.maxPos {
		return nil, 0, false
	}
	tok := make([]int32, seq)
	pos := make([]int32, seq)
	for i, id := range ids {
		if id < 0 || id >= a.vocab {
			return nil, 0, false
		}
		tok[i] = int32(id)
		pos[i] = int32(i)
	}
	outs, err := a.sess.Run([]nnapi.ExecIn{
		{I32: tok, Shape: []int{seq}},
		{I32: pos, Shape: []int{seq}},
	})
	if err != nil || len(outs) == 0 || len(outs[0].F32) != seq*a.hidden {
		if err != nil && nnapi.Transient(err) {
			log.Printf("kokoro: PL-BERT NPU transient failure, keeping session: %v", err)
		} else {
			log.Printf("kokoro: PL-BERT NPU failed, falling back to CPU: %v", err)
			a.sess.Release()
			m.albert = nil
		}
		return nil, 0, false
	}
	return outs[0].F32, a.hidden, true
}

// ReleaseNPU frees the accelerator graph. Weights stay until the model is discarded.
func (m *Model) ReleaseNPU() {
	if m == nil {
		return
	}
	if m.albert != nil && m.albert.sess != nil {
		m.albert.sess.Release()
	}
	m.albert = nil
}

// NPUBackend reports whether PL-BERT is running on an accelerator.
// The vocoder is never part of that graph.
func (m *Model) NPUBackend() (bool, string) {
	if m == nil || m.albert == nil || m.albert.sess == nil {
		if m == nil {
			return false, ""
		}
		return false, m.albertNPUReason
	}
	return true, m.albert.sess.Device()
}

func (m *Model) initAlbertNPU() {
	if nnapi.Disabled("MACLAW_KOKORO_NPU") {
		m.albertNPUReason = "disabled by env"
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	if !nnapi.Available() {
		m.albertNPUReason = nnapi.UnavailableReason()
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	spec, w, err := m.loadAlbertNPUWeights()
	if err != nil {
		m.albertNPUReason = err.Error()
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	b, err := buildAlbertGraph(spec, w)
	if err != nil {
		m.albertNPUReason = err.Error()
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	uploaded := b.ConstBytes()
	log.Printf("kokoro: compiling PL-BERT for NPU (%d MB)", uploaded>>20)
	cache := nnapi.CacheDir(m.weightsPath)
	if cache == "" {
		log.Printf("kokoro: PL-BERT NPU cache unavailable, this compile will not be reused")
	}
	sess, err := nnapi.Compile(b, cache, albertNPUToken(spec, w))
	if err != nil {
		m.albertNPUReason = err.Error()
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	m.albertNPUBypass = true
	err = m.albertNPUSelfCheck(sess, spec)
	m.albertNPUBypass = false
	if err != nil {
		sess.Release()
		m.albertNPUReason = "self-check: " + err.Error()
		log.Printf("kokoro: PL-BERT NPU unavailable: %s", m.albertNPUReason)
		return
	}
	m.albert = &albertNPU{sess: sess, vocab: spec.vocab, maxPos: spec.maxPos, hidden: spec.hidden, emb: spec.emb}
	log.Printf("kokoro: PL-BERT NPU active (%s, %d MB); vocoder stays on CPU", sess.Device(), uploaded>>20)
}

func (m *Model) albertNPUSelfCheck(sess *nnapi.Session, spec albertGraphSpec) error {
	n := 4
	if spec.maxPos < n {
		n = spec.maxPos
	}
	if n <= 0 {
		return fmt.Errorf("no positions")
	}
	limit := spec.vocab
	if m.Config != nil && m.Config.NToken > 0 && m.Config.NToken < limit {
		limit = m.Config.NToken
	}
	if limit <= 0 {
		return fmt.Errorf("empty vocab")
	}
	ids := make([]int, n)
	for i := range ids {
		ids[i] = (i * 3) % limit
	}
	saved := kokoroFastMath
	kokoroFastMath = false
	ref, hidden, err := m.albertForwardCPU(ids)
	kokoroFastMath = saved
	if err != nil {
		return err
	}
	if hidden != spec.hidden || len(ref) != n*hidden {
		return fmt.Errorf("cpu hidden %d", hidden)
	}
	tok := make([]int32, n)
	pos := make([]int32, n)
	for i, id := range ids {
		tok[i] = int32(id)
		pos[i] = int32(i)
	}
	outs, err := sess.Run([]nnapi.ExecIn{
		{I32: tok, Shape: []int{n}},
		{I32: pos, Shape: []int{n}},
	})
	if err != nil {
		return err
	}
	if len(outs) == 0 || len(outs[0].F32) < n*hidden {
		return fmt.Errorf("short npu output")
	}
	mean, nOK, bad := albertRowCosine(ref, outs[0].F32, n, hidden)
	min := albertNPUCosMin()
	log.Printf("kokoro: PL-BERT NPU self-check cosine %.4f (%d/%d rows >= 0.85)", mean, nOK, n)
	if bad || mean < min || nOK*2 < n {
		return fmt.Errorf("cosine %.4f < %.2f or only %d/%d rows >= 0.85", mean, min, nOK, n)
	}
	return nil
}

func albertNPUCosMin() float64 {
	s := os.Getenv("MACLAW_KOKORO_NPU_COS")
	if s == "" {
		return 0.90
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.90
	}
	return v
}

func albertRowCosine(a, b []float32, rows, dim int) (mean float64, nOK int, bad bool) {
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

func (m *Model) loadAlbertNPUWeights() (albertGraphSpec, albertNPUWeights, error) {
	var spec albertGraphSpec
	var w albertNPUWeights
	if m == nil || m.Config == nil {
		return spec, w, fmt.Errorf("kokoro: model not loaded")
	}
	cfg := m.Config.PLBert
	const emb = 128
	word, err := m.tensorData("bert.module.embeddings.word_embeddings.weight")
	if err != nil {
		return spec, w, err
	}
	pos, err := m.tensorData("bert.module.embeddings.position_embeddings.weight")
	if err != nil {
		return spec, w, err
	}
	typeEmb, err := m.tensorData("bert.module.embeddings.token_type_embeddings.weight")
	if err != nil {
		return spec, w, err
	}
	if len(word)%emb != 0 || len(pos)%emb != 0 || len(typeEmb) < emb {
		return spec, w, fmt.Errorf("kokoro: albert embedding shape")
	}
	spec = albertGraphSpec{
		emb: emb, hidden: cfg.HiddenSize, heads: cfg.NumAttentionHeads,
		intermediate: cfg.IntermediateSize, layers: cfg.NumHiddenLayers,
		vocab: len(word) / emb, maxPos: len(pos) / emb,
	}
	if spec.hidden <= 0 || spec.heads <= 0 || spec.hidden%spec.heads != 0 || spec.layers <= 0 || spec.intermediate <= 0 {
		return spec, w, fmt.Errorf("kokoro: albert config %+v", spec)
	}
	prefix := "bert.module.encoder.albert_layer_groups.0.albert_layers.0."
	take := func(name string) ([]float32, error) { return m.tensorData(name) }
	w.word, w.pos, w.typeEmb = word, pos, typeEmb[:emb]
	if w.lnW, err = take("bert.module.embeddings.LayerNorm.weight"); err != nil {
		return spec, w, err
	}
	if w.lnB, err = take("bert.module.embeddings.LayerNorm.bias"); err != nil {
		return spec, w, err
	}
	if w.mapW, err = take("bert.module.encoder.embedding_hidden_mapping_in.weight"); err != nil {
		return spec, w, err
	}
	if w.mapB, err = take("bert.module.encoder.embedding_hidden_mapping_in.bias"); err != nil {
		return spec, w, err
	}
	if w.qW, err = take(prefix + "attention.query.weight"); err != nil {
		return spec, w, err
	}
	if w.qB, err = take(prefix + "attention.query.bias"); err != nil {
		return spec, w, err
	}
	if w.kW, err = take(prefix + "attention.key.weight"); err != nil {
		return spec, w, err
	}
	if w.kB, err = take(prefix + "attention.key.bias"); err != nil {
		return spec, w, err
	}
	if w.vW, err = take(prefix + "attention.value.weight"); err != nil {
		return spec, w, err
	}
	if w.vB, err = take(prefix + "attention.value.bias"); err != nil {
		return spec, w, err
	}
	if w.denseW, err = take(prefix + "attention.dense.weight"); err != nil {
		return spec, w, err
	}
	if w.denseB, err = take(prefix + "attention.dense.bias"); err != nil {
		return spec, w, err
	}
	if w.attnLnW, err = take(prefix + "attention.LayerNorm.weight"); err != nil {
		return spec, w, err
	}
	if w.attnLnB, err = take(prefix + "attention.LayerNorm.bias"); err != nil {
		return spec, w, err
	}
	if w.ffW, err = take(prefix + "ffn.weight"); err != nil {
		return spec, w, err
	}
	if w.ffB, err = take(prefix + "ffn.bias"); err != nil {
		return spec, w, err
	}
	if w.ffOutW, err = take(prefix + "ffn_output.weight"); err != nil {
		return spec, w, err
	}
	if w.ffOutB, err = take(prefix + "ffn_output.bias"); err != nil {
		return spec, w, err
	}
	if w.fullLnW, err = take(prefix + "full_layer_layer_norm.weight"); err != nil {
		return spec, w, err
	}
	if w.fullLnB, err = take(prefix + "full_layer_layer_norm.bias"); err != nil {
		return spec, w, err
	}
	if len(w.ffB) != spec.intermediate {
		return spec, w, fmt.Errorf("kokoro: albert ffn %d config %d", len(w.ffB), spec.intermediate)
	}
	return spec, w, nil
}

func buildAlbertGraph(spec albertGraphSpec, w albertNPUWeights) (*nnapi.Builder, error) {
	if spec.emb <= 0 || spec.hidden <= 0 || spec.heads <= 0 || spec.layers <= 0 {
		return nil, fmt.Errorf("kokoro npu: bad albert spec")
	}
	if len(w.word) != spec.vocab*spec.emb || len(w.pos) != spec.maxPos*spec.emb || len(w.typeEmb) < spec.emb {
		return nil, fmt.Errorf("kokoro npu: embedding lengths")
	}
	b := &nnapi.Builder{}
	ids := b.InputI32([]int{0})
	posIDs := b.InputI32([]int{0})
	word := b.Gather(b.ConstF32([]int{spec.vocab, spec.emb}, w.word), ids, 0)
	pos := b.Gather(b.ConstF32([]int{spec.maxPos, spec.emb}, w.pos), posIDs, 0)
	x := b.Add(word, pos)
	x = b.Add(x, b.ConstF32([]int{spec.emb}, w.typeEmb[:spec.emb]))
	x = b.LayerNormLast(x, b.ConstF32([]int{spec.emb}, w.lnW), b.ConstF32([]int{spec.emb}, w.lnB), 1e-12)
	x = fcChecked(b, x, w.mapW, w.mapB, spec.hidden, spec.emb)
	qw := b.ConstF32([]int{spec.hidden, spec.hidden}, w.qW)
	qb := vecConst(b, w.qB, spec.hidden)
	kw := b.ConstF32([]int{spec.hidden, spec.hidden}, w.kW)
	kb := vecConst(b, w.kB, spec.hidden)
	vw := b.ConstF32([]int{spec.hidden, spec.hidden}, w.vW)
	vb := vecConst(b, w.vB, spec.hidden)
	dw := b.ConstF32([]int{spec.hidden, spec.hidden}, w.denseW)
	db := vecConst(b, w.denseB, spec.hidden)
	attnW := b.ConstF32([]int{spec.hidden}, w.attnLnW)
	attnB := b.ConstF32([]int{spec.hidden}, w.attnLnB)
	ffW := b.ConstF32([]int{spec.intermediate, spec.hidden}, w.ffW)
	ffB := vecConst(b, w.ffB, spec.intermediate)
	ffOutW := b.ConstF32([]int{spec.hidden, spec.intermediate}, w.ffOutW)
	ffOutB := vecConst(b, w.ffOutB, spec.hidden)
	fullW := b.ConstF32([]int{spec.hidden}, w.fullLnW)
	fullB := b.ConstF32([]int{spec.hidden}, w.fullLnB)
	for layer := 0; layer < spec.layers; layer++ {
		q := b.FullyConnected(x, qw, qb, false)
		k := b.FullyConnected(x, kw, kb, false)
		v := b.FullyConnected(x, vw, vb, false)
		ctx := b.Attention(q, k, v, spec.hidden, spec.heads)
		proj := b.FullyConnected(ctx, dw, db, false)
		sum := b.Add(proj, x)
		norm := b.LayerNormLast(sum, attnW, attnB, 1e-12)
		ff := b.GELU(b.FullyConnected(norm, ffW, ffB, false))
		ffOut := b.FullyConnected(ff, ffOutW, ffOutB, false)
		sum2 := b.Add(ffOut, norm)
		x = b.LayerNormLast(sum2, fullW, fullB, 1e-12)
		if b.Err() != nil {
			return nil, fmt.Errorf("kokoro npu: albert layer %d: %w", layer, b.Err())
		}
	}
	b.Output(x)
	if err := b.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func fcChecked(b *nnapi.Builder, x int, weight, bias []float32, units, in int) int {
	return b.FullyConnected(x, b.ConstF32([]int{units, in}, weight), vecConst(b, bias, units), false)
}

func vecConst(b *nnapi.Builder, v []float32, n int) int {
	if len(v) < n {
		return b.ConstF32([]int{n}, []float32{})
	}
	return b.ConstF32([]int{n}, v[:n])
}

func albertNPUToken(spec albertGraphSpec, w albertNPUWeights) [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "albert-npu-v2 %d %d %d %d %d %d %d\n",
		spec.emb, spec.hidden, spec.heads, spec.intermediate, spec.layers, spec.vocab, spec.maxPos)
	// Every constant the graph uploads. NNAPI looks the compilation up by
	// this token alone and can return another graph's outputs on a collision.
	for _, v := range [][]float32{
		w.word, w.pos, w.typeEmb,
		w.lnW, w.lnB,
		w.mapW, w.mapB,
		w.qW, w.qB, w.kW, w.kB, w.vW, w.vB,
		w.denseW, w.denseB,
		w.attnLnW, w.attnLnB,
		w.ffW, w.ffB, w.ffOutW, w.ffOutB,
		w.fullLnW, w.fullLnB,
	} {
		sampleAlbertF32(h, v)
	}
	var token [32]byte
	copy(token[:], h.Sum(nil))
	return token
}

func sampleAlbertF32(h interface{ Write([]byte) (int, error) }, v []float32) {
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
