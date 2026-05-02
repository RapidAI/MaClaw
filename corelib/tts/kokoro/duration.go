package kokoro

import "fmt"

type DurationResult struct {
	InputIDs  []int
	Durations []int
	Encoded   []float32 // [T, 640] duration encoder output
	Dim       int
}

func (m *Model) SelectVoiceStyle(voice *TensorFile, phonemeCount int) ([]float32, error) {
	pack, ok := voice.Get("pack")
	if !ok {
		return nil, fmt.Errorf("kokoro: voice pack tensor not found")
	}
	if len(pack.Dims) != 3 || pack.Dims[2] != 256 {
		return nil, fmt.Errorf("kokoro: unexpected voice pack dims %v", pack.Dims)
	}
	idx := phonemeCount - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= pack.Dims[0] {
		idx = pack.Dims[0] - 1
	}
	packData, err := pack.Float32()
	if err != nil {
		return nil, err
	}
	style := make([]float32, 256)
	copy(style, packData[idx*256:(idx+1)*256])
	return style, nil
}

func (m *Model) PredictDurations(phonemes string, voice *TensorFile, speed float32) (*DurationResult, error) {
	if speed <= 0 {
		speed = 1
	}
	ids, err := TokenizePhonemes(m.Config, phonemes)
	if err != nil {
		return nil, err
	}
	bert, bertDim, err := m.AlbertForward(ids)
	if err != nil {
		return nil, err
	}
	encW, err := m.tensor("bert_encoder.module.weight")
	if err != nil {
		return nil, err
	}
	encB, err := m.tensorData("bert_encoder.module.bias")
	if err != nil {
		return nil, err
	}
	dEn := make([]float32, len(ids)*m.Config.HiddenDim)
	if err := LinearSequenceTensor(dEn, bert, encW, encB, len(ids), bertDim, m.Config.HiddenDim); err != nil {
		return nil, err
	}
	style, err := m.SelectVoiceStyle(voice, len([]rune(phonemes)))
	if err != nil {
		return nil, err
	}
	durEnc, err := m.durationEncoder(dEn, len(ids), m.Config.HiddenDim, style[128:])
	if err != nil {
		return nil, err
	}
	lstm, err := m.loadBiLSTM("predictor.module.lstm", 640, 256)
	if err != nil {
		return nil, err
	}
	x := make([]float32, len(ids)*512)
	if err := BiLSTMLayer(x, durEnc, len(ids), lstm); err != nil {
		return nil, err
	}
	projW, err := m.tensor("predictor.module.duration_proj.linear_layer.weight")
	if err != nil {
		return nil, err
	}
	projB, err := m.tensorData("predictor.module.duration_proj.linear_layer.bias")
	if err != nil {
		return nil, err
	}
	logits := make([]float32, len(ids)*m.Config.MaxDur)
	if err := LinearSequenceTensor(logits, x, projW, projB, len(ids), 512, m.Config.MaxDur); err != nil {
		return nil, err
	}
	durs := make([]int, len(ids))
	for t := range ids {
		sum := float32(0)
		for i := 0; i < m.Config.MaxDur; i++ {
			sum += sigmoid(logits[t*m.Config.MaxDur+i])
		}
		v := int(sum/speed + 0.5)
		if v < 1 {
			v = 1
		}
		durs[t] = v
	}
	return &DurationResult{InputIDs: ids, Durations: durs, Encoded: durEnc, Dim: 640}, nil
}

func (m *Model) durationEncoder(x []float32, steps, dim int, style []float32) ([]float32, error) {
	if dim != 512 || len(style) != 128 {
		return nil, fmt.Errorf("kokoro: duration encoder expects dim=512 style=128")
	}
	cur := make([]float32, steps*(dim+128))
	for t := 0; t < steps; t++ {
		copy(cur[t*(dim+128):t*(dim+128)+dim], x[t*dim:(t+1)*dim])
		copy(cur[t*(dim+128)+dim:(t+1)*(dim+128)], style)
	}
	curDim := dim + 128
	for block := 0; block < m.Config.NLayer; block++ {
		lstm, err := m.loadBiLSTM(fmt.Sprintf("predictor.module.text_encoder.lstms.%d", block*2), curDim, 256)
		if err != nil {
			return nil, err
		}
		next := make([]float32, steps*512)
		if err := BiLSTMLayer(next, cur, steps, lstm); err != nil {
			return nil, err
		}
		fcW, err := m.tensorData(fmt.Sprintf("predictor.module.text_encoder.lstms.%d.fc.weight", block*2+1))
		if err != nil {
			return nil, err
		}
		fcB, err := m.tensorData(fmt.Sprintf("predictor.module.text_encoder.lstms.%d.fc.bias", block*2+1))
		if err != nil {
			return nil, err
		}
		normed := make([]float32, steps*512)
		if err := adaLayerNormSequence(normed, next, style, fcW, fcB, steps, 512); err != nil {
			return nil, err
		}
		cur = make([]float32, steps*640)
		for t := 0; t < steps; t++ {
			copy(cur[t*640:t*640+512], normed[t*512:(t+1)*512])
			copy(cur[t*640+512:(t+1)*640], style)
		}
		curDim = 640
	}
	return cur, nil
}

func (m *Model) loadBiLSTM(prefix string, inputDim, hidden int) (BiLSTMWeights, error) {
	fwIH, err := m.tensor(prefix + ".weight_ih_l0")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	fwHH, err := m.tensor(prefix + ".weight_hh_l0")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	fwBIH, err := m.tensorData(prefix + ".bias_ih_l0")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	fwBHH, err := m.tensorData(prefix + ".bias_hh_l0")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	rvIH, err := m.tensor(prefix + ".weight_ih_l0_reverse")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	rvHH, err := m.tensor(prefix + ".weight_hh_l0_reverse")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	rvBIH, err := m.tensorData(prefix + ".bias_ih_l0_reverse")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	rvBHH, err := m.tensorData(prefix + ".bias_hh_l0_reverse")
	if err != nil {
		return BiLSTMWeights{}, err
	}
	return BiLSTMWeights{
		Forward: makeLSTMWeights(fwIH, fwHH, fwBIH, fwBHH, inputDim, hidden),
		Reverse: makeLSTMWeights(rvIH, rvHH, rvBIH, rvBHH, inputDim, hidden),
	}, nil
}

func makeLSTMWeights(ih, hh *Tensor, biasIH, biasHH []float32, inputDim, hidden int) LSTMWeights {
	out := LSTMWeights{BiasIH: biasIH, BiasHH: biasHH, InputDim: inputDim, Hidden: hidden}
	if ih != nil && ih.DType == TensorQ8Rowwise && useKokoroQ8Direct() {
		out.WeightIHTensor = ih
	} else if ih != nil {
		out.WeightIH, _ = ih.Float32()
	}
	if hh != nil && hh.DType == TensorQ8Rowwise && useKokoroQ8Direct() {
		out.WeightHHTensor = hh
	} else if hh != nil {
		out.WeightHH, _ = hh.Float32()
	}
	return out
}

func adaLayerNormSequence(out, x, style, fcW, fcB []float32, steps, channels int) error {
	params := make([]float32, channels*2)
	if err := Linear(params, style, fcW, fcB, len(style), channels*2); err != nil {
		return err
	}
	gamma := params[:channels]
	beta := params[channels:]
	unitGamma := make([]float32, channels)
	for i := range unitGamma {
		unitGamma[i] = 1
	}
	for t := 0; t < steps; t++ {
		row := x[t*channels : (t+1)*channels]
		if err := LayerNorm1D(out[t*channels:(t+1)*channels], row, unitGamma, nil, 1e-5); err != nil {
			return err
		}
		for c := 0; c < channels; c++ {
			out[t*channels+c] = (1+gamma[c])*out[t*channels+c] + beta[c]
		}
	}
	return nil
}
