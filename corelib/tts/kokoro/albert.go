package kokoro

import (
	"fmt"
	"math"
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

	x := make([]float32, seq*embDim)
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
	h := make([]float32, seq*hidden)
	if err := LinearSequenceTensor(h, x, mapW, mapB, seq, embDim, hidden); err != nil {
		return nil, 0, err
	}

	for layer := 0; layer < cfg.NumHiddenLayers; layer++ {
		var err error
		h, err = m.albertLayer(h, seq, hidden, cfg.NumAttentionHeads)
		if err != nil {
			return nil, 0, fmt.Errorf("kokoro: albert layer %d: %w", layer, err)
		}
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

	q := make([]float32, seq*hidden)
	k := make([]float32, seq*hidden)
	v := make([]float32, seq*hidden)
	if err := LinearSequenceTensor(q, x, qW, qB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	if err := LinearSequenceTensor(k, x, kW, kB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	if err := LinearSequenceTensor(v, x, vW, vB, seq, hidden, hidden); err != nil {
		return nil, err
	}

	headDim := hidden / heads
	ctx := make([]float32, seq*hidden)
	scores := make([]float32, seq)
	for h := 0; h < heads; h++ {
		base := h * headDim
		scale := float32(1 / math.Sqrt(float64(headDim)))
		for i := 0; i < seq; i++ {
			for j := 0; j < seq; j++ {
				dot := float32(0)
				qoff := i*hidden + base
				koff := j*hidden + base
				for d := 0; d < headDim; d++ {
					dot += q[qoff+d] * k[koff+d]
				}
				scores[j] = dot * scale
			}
			softmaxInplace(scores)
			for d := 0; d < headDim; d++ {
				sum := float32(0)
				for j := 0; j < seq; j++ {
					sum += scores[j] * v[j*hidden+base+d]
				}
				ctx[i*hidden+base+d] = sum
			}
		}
	}

	proj := make([]float32, seq*hidden)
	if err := LinearSequenceTensor(proj, ctx, denseW, denseB, seq, hidden, hidden); err != nil {
		return nil, err
	}
	for i := range proj {
		proj[i] += x[i]
	}
	if err := LayerNormLastDim(proj, proj, attnLnW, attnLnB, seq, hidden, 1e-12); err != nil {
		return nil, err
	}

	intermediate := len(ffB)
	ff := make([]float32, seq*intermediate)
	if err := LinearSequenceTensor(ff, proj, ffW, ffB, seq, hidden, intermediate); err != nil {
		return nil, err
	}
	for i := range ff {
		ff[i] = gelu(ff[i])
	}
	ffOut := make([]float32, seq*hidden)
	if err := LinearSequenceTensor(ffOut, ff, ffOutW, ffOutB, seq, intermediate, hidden); err != nil {
		return nil, err
	}
	for i := range ffOut {
		ffOut[i] += proj[i]
	}
	if err := LayerNormLastDim(ffOut, ffOut, fullLnW, fullLnB, seq, hidden, 1e-12); err != nil {
		return nil, err
	}
	return ffOut, nil
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
	for i, v := range x {
		e := float32(math.Exp(float64(v - max)))
		x[i] = e
		sum += e
	}
	if sum == 0 {
		return
	}
	inv := 1 / sum
	for i := range x {
		x[i] *= inv
	}
}
