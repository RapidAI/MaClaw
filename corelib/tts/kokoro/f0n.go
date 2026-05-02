package kokoro

import (
	"fmt"
	"math"
)

type F0NResult struct {
	F0     []float32 // [Frames]
	Noise  []float32 // [Frames]
	Frames int
}

func (m *Model) PredictF0N(cond *Conditioning, voice *TensorFile) (*F0NResult, error) {
	if cond == nil || cond.Frames <= 0 || len(cond.Prosody) != cond.Frames*640 {
		return nil, fmt.Errorf("kokoro: invalid conditioning for F0/N")
	}
	style, err := m.SelectVoiceStyle(voice, len(cond.Durations)-2)
	if err != nil {
		return nil, err
	}
	s := style[128:]
	shared, err := m.loadBiLSTM("predictor.module.shared", 640, 256)
	if err != nil {
		return nil, err
	}
	sharedOut := make([]float32, cond.Frames*512)
	if err := BiLSTMLayer(sharedOut, cond.Prosody, cond.Frames, shared); err != nil {
		return nil, err
	}
	f0Feat := timeMajorToChannelMajor(sharedOut, cond.Frames, 512)
	noiseFeat := make([]float32, len(f0Feat))
	copy(noiseFeat, f0Feat)
	f0Frames := cond.Frames
	noiseFrames := cond.Frames
	f0Feat, f0Frames, err = m.adainResStack(f0Feat, cond.Frames, 512, s, "predictor.module.F0")
	if err != nil {
		return nil, err
	}
	noiseFeat, noiseFrames, err = m.adainResStack(noiseFeat, cond.Frames, 512, s, "predictor.module.N")
	if err != nil {
		return nil, err
	}
	f0W, err := m.tensorData("predictor.module.F0_proj.weight")
	if err != nil {
		return nil, err
	}
	f0B, err := m.tensorData("predictor.module.F0_proj.bias")
	if err != nil {
		return nil, err
	}
	nW, err := m.tensorData("predictor.module.N_proj.weight")
	if err != nil {
		return nil, err
	}
	nB, err := m.tensorData("predictor.module.N_proj.bias")
	if err != nil {
		return nil, err
	}
	if noiseFrames != f0Frames {
		return nil, fmt.Errorf("kokoro: F0/N frame mismatch %d vs %d", f0Frames, noiseFrames)
	}
	f0 := make([]float32, f0Frames)
	noise := make([]float32, noiseFrames)
	if err := Conv1D(f0, f0Feat, f0W, f0B, 256, f0Frames, 1, 1, 1, 0, 1, 1); err != nil {
		return nil, err
	}
	if err := Conv1D(noise, noiseFeat, nW, nB, 256, noiseFrames, 1, 1, 1, 0, 1, 1); err != nil {
		return nil, err
	}
	return &F0NResult{F0: f0, Noise: noise, Frames: f0Frames}, nil
}

func (m *Model) adainResStack(x []float32, frames, inC int, style []float32, prefix string) ([]float32, int, error) {
	var err error
	x, frames, inC, err = m.adainResBlk1d(x, frames, inC, 512, style, prefix+".0", false)
	if err != nil {
		return nil, 0, err
	}
	x, frames, inC, err = m.adainResBlk1d(x, frames, inC, 256, style, prefix+".1", true)
	if err != nil {
		return nil, 0, err
	}
	x, frames, inC, err = m.adainResBlk1d(x, frames, inC, 256, style, prefix+".2", false)
	if err != nil {
		return nil, 0, err
	}
	return x, frames, nil
}

func (m *Model) adainResBlk1d(x []float32, inT, inC, outC int, style []float32, prefix string, upsample bool) ([]float32, int, int, error) {
	shortcut := x
	shortT := inT
	var err error
	if upsample {
		shortcut = upsampleNearest1D(shortcut, inC, inT, 2)
		shortT = inT * 2
	}
	if inC != outC {
		shortcut, err = m.weightNormConv1D(shortcut, inC, shortT, outC, 1, 1, 0, 1, 1, prefix+".conv1x1")
		if err != nil {
			return nil, 0, 0, err
		}
	}

	res := make([]float32, len(x))
	if err := adaIN1D(res, x, inC, inT, style, mustTensorData(m, prefix+".norm1.fc.weight"), mustTensorData(m, prefix+".norm1.fc.bias")); err != nil {
		return nil, 0, 0, err
	}
	for i := range res {
		res[i] = leakyReLU(res[i], 0.2)
	}
	resT := inT
	if upsample {
		res, err = m.weightNormConvTranspose1D(res, inC, inT, inC, 3, 2, 1, 1, inC, prefix+".pool")
		if err != nil {
			return nil, 0, 0, err
		}
		resT = inT * 2
	}
	res, err = m.weightNormConv1D(res, inC, resT, outC, 3, 1, 1, 1, 1, prefix+".conv1")
	if err != nil {
		return nil, 0, 0, err
	}
	res2 := make([]float32, len(res))
	if err := adaIN1D(res2, res, outC, resT, style, mustTensorData(m, prefix+".norm2.fc.weight"), mustTensorData(m, prefix+".norm2.fc.bias")); err != nil {
		return nil, 0, 0, err
	}
	for i := range res2 {
		res2[i] = leakyReLU(res2[i], 0.2)
	}
	res2, err = m.weightNormConv1D(res2, outC, resT, outC, 3, 1, 1, 1, 1, prefix+".conv2")
	if err != nil {
		return nil, 0, 0, err
	}
	if len(res2) != len(shortcut) {
		return nil, 0, 0, fmt.Errorf("kokoro: residual/shortcut shape mismatch %s: %d vs %d", prefix, len(res2), len(shortcut))
	}
	out := make([]float32, len(res2))
	addInto32(out, res2, shortcut)
	mulNumberInplace32(out, float32(1/math.Sqrt2))
	return out, resT, outC, nil
}

func mustTensorData(m *Model, name string) []float32 {
	d, err := m.tensorData(name)
	if err != nil {
		panic(err)
	}
	return d
}

func (m *Model) weightNormConv1D(x []float32, inC, inT, outC, kernel, stride, padding, dilation, groups int, prefix string) ([]float32, error) {
	wv, err := m.tensorData(prefix + ".weight_v")
	if err != nil {
		return nil, err
	}
	wg, err := m.tensorData(prefix + ".weight_g")
	if err != nil {
		return nil, err
	}
	bias, _ := m.tensorData(prefix + ".bias")
	outT := (inT+2*padding-dilation*(kernel-1)-1)/stride + 1
	out := make([]float32, outC*outT)
	if useKokoroSIMD() && groups == 1 && kernel == 1 && stride == 1 && padding == 0 && dilation == 1 && inC >= 16 && outC*inT*inC > 50000 {
		w, err := m.cachedFloat32(prefix+".weight_norm", func() ([]float32, error) {
			w := make([]float32, len(wv))
			if err := WeightNormConv1DWeight(w, wv, wg, outC, inC, kernel); err != nil {
				return nil, err
			}
			return w, nil
		})
		if err != nil {
			return nil, err
		}
		if err := conv1DPointwiseSIMD(out, x, w, bias, inC, inT, outC); err != nil {
			return nil, err
		}
		return out, nil
	}
	if useKokoroSIMD() && useKokoroConvMatMul() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		w, err := m.cachedFloat32(prefix+".weight_norm", func() ([]float32, error) {
			w := make([]float32, len(wv))
			if err := WeightNormConv1DWeight(w, wv, wg, outC, inC, kernel); err != nil {
				return nil, err
			}
			return w, nil
		})
		if err != nil {
			return nil, err
		}
		if err := conv1DMatMul(out, x, w, bias, inC, inT, outC, kernel, stride, padding, dilation, outT); err != nil {
			return nil, err
		}
		return out, nil
	}
	if useKokoroSIMD() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		wT, err := m.cachedFloat32(prefix+".weight_norm_transposed", func() ([]float32, error) {
			wT := make([]float32, outC*kernel*inC)
			if err := WeightNormConv1DWeightTransposed(wT, wv, wg, outC, inC, kernel); err != nil {
				return nil, err
			}
			return wT, nil
		})
		if err != nil {
			return nil, err
		}
		if err := conv1DSIMDTransposedWeight(out, x, wT, bias, inC, inT, outC, kernel, stride, padding, dilation, outT); err != nil {
			return nil, err
		}
		return out, nil
	}
	w, err := m.cachedFloat32(prefix+".weight_norm", func() ([]float32, error) {
		w := make([]float32, len(wv))
		if err := WeightNormConv1DWeight(w, wv, wg, outC, inC/groups, kernel); err != nil {
			return nil, err
		}
		return w, nil
	})
	if err != nil {
		return nil, err
	}
	if err := Conv1D(out, x, w, bias, inC, inT, outC, kernel, stride, padding, dilation, groups); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Model) weightNormConvTranspose1D(x []float32, inC, inT, outC, kernel, stride, padding, outputPadding, groups int, prefix string) ([]float32, error) {
	wv, err := m.tensorData(prefix + ".weight_v")
	if err != nil {
		return nil, err
	}
	wg, err := m.tensorData(prefix + ".weight_g")
	if err != nil {
		return nil, err
	}
	bias, _ := m.tensorData(prefix + ".bias")
	// ConvTranspose1d weight layout is [inC, outC/groups, K]. Weight norm is over dims except dim=0 in PyTorch default, so normalize per input channel here.
	w, err := m.cachedFloat32(prefix+".weight_norm", func() ([]float32, error) {
		w := make([]float32, len(wv))
		if err := WeightNormConvTranspose1DWeight(w, wv, wg, inC, outC/groups, kernel); err != nil {
			return nil, err
		}
		return w, nil
	})
	if err != nil {
		return nil, err
	}
	outT := (inT-1)*stride - 2*padding + kernel + outputPadding
	out := make([]float32, outC*outT)
	if useKokoroSIMD() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		wT, err := m.cachedFloat32(prefix+".weight_norm_transposed", func() ([]float32, error) {
			wT := make([]float32, kernel*outC*inC)
			transposeConvTranspose1DWeight(wT, w, inC, outC, kernel)
			return wT, nil
		})
		if err != nil {
			return nil, err
		}
		if err := convTranspose1DSIMDTransposedWeight(out, x, wT, bias, inC, inT, outC, kernel, stride, padding, outT); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := ConvTranspose1D(out, x, w, bias, inC, inT, outC, kernel, stride, padding, outputPadding, groups); err != nil {
		return nil, err
	}
	return out, nil
}

func WeightNormConvTranspose1DWeight(out, v, g []float32, inC, outCPerGroup, kernel int) error {
	if len(out) != len(v) || len(v) != inC*outCPerGroup*kernel || len(g) < inC {
		return fmt.Errorf("kokoro: convtranspose weightnorm shape mismatch")
	}
	for ic := 0; ic < inC; ic++ {
		base := ic * outCPerGroup * kernel
		norm := float32(0)
		for i := 0; i < outCPerGroup*kernel; i++ {
			vv := v[base+i]
			norm += vv * vv
		}
		scale := g[ic] / float32(math.Sqrt(float64(norm+1e-12)))
		for i := 0; i < outCPerGroup*kernel; i++ {
			out[base+i] = v[base+i] * scale
		}
	}
	return nil
}

func adaIN1D(out, x []float32, channels, frames int, style, fcW, fcB []float32) error {
	if len(out) != channels*frames || len(x) != channels*frames {
		return fmt.Errorf("kokoro: adain shape mismatch")
	}
	params := make([]float32, channels*2)
	if err := Linear(params, style, fcW, fcB, len(style), channels*2); err != nil {
		return err
	}
	for c := 0; c < channels; c++ {
		mean := float32(0)
		for t := 0; t < frames; t++ {
			mean += x[c*frames+t]
		}
		mean /= float32(frames)
		variance := float32(0)
		for t := 0; t < frames; t++ {
			d := x[c*frames+t] - mean
			variance += d * d
		}
		inv := 1 / float32(math.Sqrt(float64(variance/float32(frames)+1e-5)))
		gamma := params[c]
		beta := params[channels+c]
		for t := 0; t < frames; t++ {
			out[c*frames+t] = (1+gamma)*(x[c*frames+t]-mean)*inv + beta
		}
	}
	return nil
}

func upsampleNearest1D(x []float32, channels, frames, scale int) []float32 {
	out := make([]float32, channels*frames*scale)
	for c := 0; c < channels; c++ {
		for t := 0; t < frames; t++ {
			v := x[c*frames+t]
			for s := 0; s < scale; s++ {
				out[c*frames*scale+t*scale+s] = v
			}
		}
	}
	return out
}
