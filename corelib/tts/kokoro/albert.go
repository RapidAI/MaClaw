package kokoro

import (
	"fmt"
	"math"
	"runtime"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func (m *Model) tensor(name string) (*Tensor, error) {
	if m == nil || m.Weights == nil {
		return nil, fmt.Errorf("kokoro: model weights not loaded")
	}
	t, ok := m.Weights.Get(name)
	if !ok {
		return nil, fmt.Errorf("kokoro: missing tensor %s", name)
	}
	return t, nil
}

func (m *Model) tensorData(name string) ([]float32, error) {
	t, err := m.tensor(name)
	if err != nil {
		return nil, err
	}
	return t.Float32()
}

func (m *Model) AlbertForward(inputIDs []int) ([]float32, int, error) {
	if m != nil && !m.albertNPUBypass {
		if out, hidden, ok := m.albertNPUForward(inputIDs); ok {
			return out, hidden, nil
		}
	}
	return m.albertForwardCPU(inputIDs)
}

func (m *Model) albertForwardCPU(inputIDs []int) ([]float32, int, error) {
	if m == nil || m.Config == nil {
		return nil, 0, fmt.Errorf("kokoro: model not loaded")
	}
	seq := len(inputIDs)
	if seq == 0 || seq > m.Config.PLBert.MaxPositionEmbeddings {
		return nil, 0, fmt.Errorf("kokoro: invalid albert sequence length %d", seq)
	}
	cfg := m.Config.PLBert
	embDim := 128
	hidden := cfg.HiddenSize

	word, err := m.tensorData("bert.module.embeddings.word_embeddings.weight")
	if err != nil {
		return nil, 0, err
	}
	pos, err := m.tensorData("bert.module.embeddings.position_embeddings.weight")
	if err != nil {
		return nil, 0, err
	}
	typeEmb, err := m.tensorData("bert.module.embeddings.token_type_embeddings.weight")
	if err != nil {
		return nil, 0, err
	}
	lnW, err := m.tensorData("bert.module.embeddings.LayerNorm.weight")
	if err != nil {
		return nil, 0, err
	}
	lnB, err := m.tensorData("bert.module.embeddings.LayerNorm.bias")
	if err != nil {
		return nil, 0, err
	}

	x := getFloat32Buffer(seq * embDim)
	defer putFloat32Buffer(x)
	for t, id := range inputIDs {
		if id < 0 || id >= m.Config.NToken {
			return nil, 0, fmt.Errorf("kokoro: token id %d outside vocab", id)
		}
		for d := 0; d < embDim; d++ {
			x[t*embDim+d] = word[id*embDim+d] + pos[t*embDim+d] + typeEmb[d]
		}
	}
	if err := LayerNormLastDim(x, x, lnW, lnB, seq, embDim, 1e-12); err != nil {
		return nil, 0, err
	}

	mapW, err := m.tensor("bert.module.encoder.embedding_hidden_mapping_in.weight")
	if err != nil {
		return nil, 0, err
	}
	mapB, err := m.tensorData("bert.module.encoder.embedding_hidden_mapping_in.bias")
	if err != nil {
		return nil, 0, err
	}
	h := getFloat32Buffer(seq * hidden)
	if err := LinearSequenceTensor(h, x, mapW, mapB, seq, embDim, hidden); err != nil {
		putFloat32Buffer(h)
		return nil, 0, err
	}

	for layer := 0; layer < cfg.NumHiddenLayers; layer++ {
		next, err := m.albertLayer(h, seq, hidden, cfg.NumAttentionHeads)
		if err != nil {
			putFloat32Buffer(h)
			return nil, 0, fmt.Errorf("kokoro: albert layer %d: %w", layer, err)
		}
		putFloat32Buffer(h)
		h = next
	}
	return h, hidden, nil
}

func (m *Model) albertLayer(x []float32, seq, hidden, heads int) ([]float32, error) {
	prefix := "bert.module.encoder.albert_layer_groups.0.albert_layers.0."
	qW, err := m.tensor(prefix + "attention.query.weight")
	if err != nil {
		return nil, err
	}
	qB, err := m.tensorData(prefix + "attention.query.bias")
	if err != nil {
		return nil, err
	}
	kW, err := m.tensor(prefix + "attention.key.weight")
	if err != nil {
		return nil, err
	}
	kB, err := m.tensorData(prefix + "attention.key.bias")
	if err != nil {
		return nil, err
	}
	vW, err := m.tensor(prefix + "attention.value.weight")
	if err != nil {
		return nil, err
	}
	vB, err := m.tensorData(prefix + "attention.value.bias")
	if err != nil {
		return nil, err
	}
	denseW, err := m.tensor(prefix + "attention.dense.weight")
	if err != nil {
		return nil, err
	}
	denseB, err := m.tensorData(prefix + "attention.dense.bias")
	if err != nil {
		return nil, err
	}
	attnLnW, err := m.tensorData(prefix + "attention.LayerNorm.weight")
	if err != nil {
		return nil, err
	}
	attnLnB, err := m.tensorData(prefix + "attention.LayerNorm.bias")
	if err != nil {
		return nil, err
	}
	ffW, err := m.tensor(prefix + "ffn.weight")
	if err != nil {
		return nil, err
	}
	ffB, err := m.tensorData(prefix + "ffn.bias")
	if err != nil {
		return nil, err
	}
	ffOutW, err := m.tensor(prefix + "ffn_output.weight")
	if err != nil {
		return nil, err
	}
	ffOutB, err := m.tensorData(prefix + "ffn_output.bias")
	if err != nil {
		return nil, err
	}
	fullLnW, err := m.tensorData(prefix + "full_layer_layer_norm.weight")
	if err != nil {
		return nil, err
	}
	fullLnB, err := m.tensorData(prefix + "full_layer_layer_norm.bias")
	if err != nil {
		return nil, err
	}

	q := getFloat32Buffer(seq * hidden)
	k := getFloat32Buffer(seq * hidden)
	v := getFloat32Buffer(seq * hidden)
	ctx := getFloat32Buffer(seq * hidden)
	defer putFloat32Buffer(q)
	defer putFloat32Buffer(k)
	defer putFloat32Buffer(v)
	defer putFloat32Buffer(ctx)
	if err := LinearSequenceTensor(q, x, qW, qB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	if err := LinearSequenceTensor(k, x, kW, kB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	if err := LinearSequenceTensor(v, x, vW, vB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	albertAttend(q, k, v, ctx, seq, hidden, heads)

	proj := getFloat32Buffer(seq * hidden)
	defer putFloat32Buffer(proj)
	if err := LinearSequenceTensor(proj, ctx, denseW, denseB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	addInplace32(proj, x)
	if err := LayerNormLastDim(proj, proj, attnLnW, attnLnB, seq, hidden, 1e-12); err != nil {
		return nil, err
	}

	intermediate := len(ffB)
	ff := getFloat32Buffer(seq * intermediate)
	defer putFloat32Buffer(ff)
	if err := LinearSequenceTensor(ff, proj, ffW, ffB, seq, hidden, intermediate); err != nil {
		return nil, err
	}
	geluInplace(ff)
	ffOut := getFloat32Buffer(seq * hidden)
	if err := LinearSequenceTensor(ffOut, ff, ffOutW, ffOutB, seq, intermediate, hidden); err != nil {
		putFloat32Buffer(ffOut)
		return nil, err
	}
	addInplace32(ffOut, proj)
	if err := LayerNormLastDim(ffOut, ffOut, fullLnW, fullLnB, seq, hidden, 1e-12); err != nil {
		putFloat32Buffer(ffOut)
		return nil, err
	}
	return ffOut, nil
}

// albertAttend writes each head into ctx. The GEMM path is Android by
// default. A head whose QK^T is large enough for tensor.MatMul to go
// parallel runs alone, so the two schedulers do not stack.
func albertAttend(q, k, v, ctx []float32, seq, hidden, heads int) {
	if heads <= 0 || seq <= 0 || hidden <= 0 {
		return
	}
	// A hidden size that is not a multiple of the head count used to leave
	// the tail at zero because the buffer was freshly allocated. A pooled
	// buffer is dirty, so clear that tail before the heads overwrite the rest.
	if rem := hidden % heads; rem != 0 {
		for t := 0; t < seq; t++ {
			row := ctx[t*hidden+hidden-rem : (t+1)*hidden]
			for i := range row {
				row[i] = 0
			}
		}
	}
	headDim := hidden / heads
	if headDim == 0 {
		return
	}
	useGEMM := kokoroAlbertGEMM && seq >= 2
	// Keep in step with tensor's flopThreshold (1e6). At that size MatMul
	// already spans GOMAXPROCS.
	gemmParallel := int64(seq)*int64(seq)*int64(headDim) >= 1_000_000
	parallelHeads := heads >= 2 && seq >= 16 && runtime.GOMAXPROCS(0) > 1
	if useGEMM && gemmParallel {
		parallelHeads = false
	}
	attn := func(h int) {
		if useGEMM {
			albertHeadGEMM(q, k, v, ctx, seq, hidden, h, headDim)
			return
		}
		albertHeadDot(q, k, v, ctx, seq, hidden, h, headDim)
	}
	if !parallelHeads {
		for h := 0; h < heads; h++ {
			attn(h)
		}
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > heads {
		workers = heads
	}
	var wg sync.WaitGroup
	chunk := (heads + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > heads {
			end = heads
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(h0, h1 int) {
			defer wg.Done()
			for h := h0; h < h1; h++ {
				attn(h)
			}
		}(start, end)
	}
	wg.Wait()
}

func albertHeadDot(q, k, v, ctx []float32, seq, hidden, head, headDim int) {
	base := head * headDim
	scale := float32(1 / math.Sqrt(float64(headDim)))
	scores := make([]float32, seq)
	for i := 0; i < seq; i++ {
		qRow := q[i*hidden+base : i*hidden+base+headDim]
		for j := 0; j < seq; j++ {
			scores[j] = dot32(qRow, k[j*hidden+base:j*hidden+base+headDim]) * scale
		}
		softmaxInplace(scores)
		ctxRow := ctx[i*hidden+base : i*hidden+base+headDim]
		for d := range ctxRow {
			ctxRow[d] = 0
		}
		for j := 0; j < seq; j++ {
			axpyInplace(ctxRow, v[j*hidden+base:j*hidden+base+headDim], scores[j])
		}
	}
}

func albertHeadGEMM(q, k, v, ctx []float32, seq, hidden, head, headDim int) {
	base := head * headDim
	qPack := getFloat32Buffer(seq * headDim)
	kPack := getFloat32Buffer(seq * headDim)
	vPack := getFloat32Buffer(seq * headDim)
	scores := getFloat32Buffer(seq * seq)
	vt := getFloat32Buffer(headDim * seq)
	ctxPack := getFloat32Buffer(seq * headDim)
	defer putFloat32Buffer(qPack)
	defer putFloat32Buffer(kPack)
	defer putFloat32Buffer(vPack)
	defer putFloat32Buffer(scores)
	defer putFloat32Buffer(vt)
	defer putFloat32Buffer(ctxPack)
	for i := 0; i < seq; i++ {
		qs := i * hidden
		copy(qPack[i*headDim:(i+1)*headDim], q[qs+base:qs+base+headDim])
		copy(kPack[i*headDim:(i+1)*headDim], k[qs+base:qs+base+headDim])
		copy(vPack[i*headDim:(i+1)*headDim], v[qs+base:qs+base+headDim])
	}
	scale := float32(1 / math.Sqrt(float64(headDim)))
	for i := range qPack {
		qPack[i] *= scale
	}
	tensor.MatMul(scores, qPack, kPack, seq, seq, headDim)
	for i := 0; i < seq; i++ {
		softmaxInplace(scores[i*seq : (i+1)*seq])
	}
	for d := 0; d < headDim; d++ {
		row := vt[d*seq : (d+1)*seq]
		for j := 0; j < seq; j++ {
			row[j] = vPack[j*headDim+d]
		}
	}
	tensor.MatMul(ctxPack, scores, vt, seq, headDim, seq)
	for i := 0; i < seq; i++ {
		copy(ctx[i*hidden+base:i*hidden+base+headDim], ctxPack[i*headDim:(i+1)*headDim])
	}
}

func softmaxInplace(x []float32) {
	if len(x) == 0 {
		return
	}
	max := x[0]
	for _, v := range x[1:] {
		if v > max {
			max = v
		}
	}
	sum := float32(0)
	if kokoroFastMath {
		for i, v := range x {
			e := fastExp32(v - max)
			x[i] = e
			sum += e
		}
	} else {
		for i, v := range x {
			e := float32(math.Exp(float64(v - max)))
			x[i] = e
			sum += e
		}
	}
	if sum == 0 {
		return
	}
	inv := 1 / sum
	for i := range x {
		x[i] *= inv
	}
}
