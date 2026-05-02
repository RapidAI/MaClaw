package kokoro

import "fmt"

type Conditioning struct {
	Durations []int
	Frames    int
	Prosody   []float32 // [Frames, 640]
	Text      []float32 // [Frames, 512]
}

func totalDuration(durations []int) int {
	total := 0
	for _, d := range durations {
		if d > 0 {
			total += d
		}
	}
	return total
}

func RepeatByDuration(seq []float32, steps, dim int, durations []int) ([]float32, int, error) {
	if len(seq) != steps*dim || len(durations) != steps {
		return nil, 0, fmt.Errorf("kokoro: repeat shape mismatch")
	}
	frames := totalDuration(durations)
	out := make([]float32, frames*dim)
	pos := 0
	for t, d := range durations {
		if d < 1 {
			d = 1
		}
		row := seq[t*dim : (t+1)*dim]
		for i := 0; i < d; i++ {
			copy(out[pos*dim:(pos+1)*dim], row)
			pos++
		}
	}
	return out, frames, nil
}

func (m *Model) BuildConditioning(phonemes string, voice *TensorFile, speed float32) (*Conditioning, error) {
	dur, err := m.PredictDurations(phonemes, voice, speed)
	if err != nil {
		return nil, err
	}
	prosody, frames, err := RepeatByDuration(dur.Encoded, len(dur.InputIDs), dur.Dim, dur.Durations)
	if err != nil {
		return nil, err
	}
	text, textDim, err := m.TextEncoderForward(dur.InputIDs)
	if err != nil {
		return nil, err
	}
	textFrames, frames2, err := RepeatByDuration(text, len(dur.InputIDs), textDim, dur.Durations)
	if err != nil {
		return nil, err
	}
	if frames2 != frames {
		return nil, fmt.Errorf("kokoro: frame mismatch prosody=%d text=%d", frames, frames2)
	}
	return &Conditioning{Durations: dur.Durations, Frames: frames, Prosody: prosody, Text: textFrames}, nil
}

func (m *Model) TextEncoderForward(inputIDs []int) ([]float32, int, error) {
	steps := len(inputIDs)
	dim := m.Config.HiddenDim
	emb, err := m.tensorData("text_encoder.module.embedding.weight")
	if err != nil {
		return nil, 0, err
	}
	xTD := make([]float32, steps*dim)
	if err := Embedding(xTD, inputIDs, emb, m.Config.NToken, dim); err != nil {
		return nil, 0, err
	}
	x := timeMajorToChannelMajor(xTD, steps, dim)
	for i := 0; i < m.Config.NLayer; i++ {
		base := fmt.Sprintf("text_encoder.module.cnn.%d", i)
		wv, err := m.tensorData(base + ".0.weight_v")
		if err != nil {
			return nil, 0, err
		}
		wg, err := m.tensorData(base + ".0.weight_g")
		if err != nil {
			return nil, 0, err
		}
		bias, err := m.tensorData(base + ".0.bias")
		if err != nil {
			return nil, 0, err
		}
		gamma, err := m.tensorData(base + ".1.gamma")
		if err != nil {
			return nil, 0, err
		}
		beta, err := m.tensorData(base + ".1.beta")
		if err != nil {
			return nil, 0, err
		}
		conv := make([]float32, dim*steps)
		kernel := m.Config.TextEncoderKernelSize
		padding := (kernel - 1) / 2
		if useKokoroSIMD() && dim >= 16 && dim*steps*dim*kernel > 50000 {
			wT, err := m.cachedFloat32(base+".0.weight_norm_transposed", func() ([]float32, error) {
				wT := make([]float32, dim*kernel*dim)
				if err := WeightNormConv1DWeightTransposed(wT, wv, flattenG(wg), dim, dim, kernel); err != nil {
					return nil, err
				}
				return wT, nil
			})
			if err != nil {
				return nil, 0, err
			}
			if err := conv1DSIMDTransposedWeight(conv, x, wT, bias, dim, steps, dim, kernel, 1, padding, 1, steps); err != nil {
				return nil, 0, err
			}
		} else {
			w, err := m.cachedFloat32(base+".0.weight_norm", func() ([]float32, error) {
				w := make([]float32, len(wv))
				if err := WeightNormConv1DWeight(w, wv, flattenG(wg), dim, dim, kernel); err != nil {
					return nil, err
				}
				return w, nil
			})
			if err != nil {
				return nil, 0, err
			}
			if err := Conv1D(conv, x, w, bias, dim, steps, dim, kernel, 1, padding, 1, 1); err != nil {
				return nil, 0, err
			}
		}
		convTD := channelMajorToTimeMajor(conv, dim, steps)
		if err := LayerNormLastDim(convTD, convTD, gamma, beta, steps, dim, 1e-5); err != nil {
			return nil, 0, err
		}
		for j := range convTD {
			convTD[j] = leakyReLU(convTD[j], 0.2)
		}
		x = timeMajorToChannelMajor(convTD, steps, dim)
	}
	lstm, err := m.loadBiLSTM("text_encoder.module.lstm", dim, dim/2)
	if err != nil {
		return nil, 0, err
	}
	lstmIn := channelMajorToTimeMajor(x, dim, steps)
	out := make([]float32, steps*dim)
	if err := BiLSTMLayer(out, lstmIn, steps, lstm); err != nil {
		return nil, 0, err
	}
	return out, dim, nil
}

func flattenG(g []float32) []float32 {
	return g
}

func timeMajorToChannelMajor(x []float32, steps, dim int) []float32 {
	out := make([]float32, dim*steps)
	for t := 0; t < steps; t++ {
		for d := 0; d < dim; d++ {
			out[d*steps+t] = x[t*dim+d]
		}
	}
	return out
}

func channelMajorToTimeMajor(x []float32, dim, steps int) []float32 {
	out := make([]float32, steps*dim)
	for d := 0; d < dim; d++ {
		for t := 0; t < steps; t++ {
			out[t*dim+d] = x[d*steps+t]
		}
	}
	return out
}
