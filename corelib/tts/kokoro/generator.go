package kokoro

import (
	"fmt"
	"math"
	"strings"
)

func (m *Model) GeneratorForward(feat *DecoderFeatures, f0n *F0NResult, voice *TensorFile) ([]float32, error) {
	if feat == nil || f0n == nil || feat.Frames <= 0 || len(feat.X) != 512*feat.Frames {
		return nil, fmt.Errorf("kokoro: invalid generator input")
	}
	style := feat.Style
	if len(style) != 256 {
		var err error
		style, err = m.SelectVoiceStyle(voice, 1)
		if err != nil {
			return nil, err
		}
	}
	s := style[:128]
	var err error
	x := append([]float32(nil), feat.X...)
	frames := feat.Frames
	inC := 512
	upsampleRates := []int{10, 6}
	upsampleKernels := []int{20, 12}
	for i := 0; i < 2; i++ {
		for j := range x {
			x[j] = leakyReLU(x[j], 0.1)
		}
		outC := 256
		if i == 1 {
			outC = 128
		}
		k := upsampleKernels[i]
		u := upsampleRates[i]
		x, err = m.weightNormConvTranspose1D(x, inC, frames, outC, k, u, (k-u)/2, 0, 1, fmt.Sprintf("decoder.module.generator.ups.%d", i))
		if err != nil {
			return nil, err
		}
		frames = (frames-1)*u - 2*((k-u)/2) + k
		inC = outC
		if i == 1 {
			x = reflectionPadLeft1(x, inC, frames)
			frames++
		}
		xSource, err := m.generatorSourceForStage(f0n.F0, frames, outC, s, i)
		if err != nil {
			return nil, err
		}
		for j := range x {
			x[j] += xSource[j]
		}
		var sum []float32
		for j := 0; j < 3; j++ {
			blockIdx := i*3 + j
			y, err := m.generatorAdaINResBlock1(x, inC, frames, s, fmt.Sprintf("decoder.module.generator.resblocks.%d", blockIdx))
			if err != nil {
				return nil, err
			}
			if sum == nil {
				sum = y
			} else {
				for n := range sum {
					sum[n] += y[n]
				}
			}
		}
		for n := range sum {
			sum[n] /= 3
		}
		x = sum
	}
	for i := range x {
		x[i] = leakyReLU(x[i], 0.01)
	}
	x, err = m.weightNormConv1D(x, 128, frames, 22, 7, 1, 3, 1, 1, "decoder.module.generator.conv_post")
	if err != nil {
		return nil, err
	}
	bins := 11
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	for b := 0; b < bins; b++ {
		for t := 0; t < frames; t++ {
			mag[b*frames+t] = float32(math.Exp(float64(x[b*frames+t])))
		}
		copy(phase[b*frames:(b+1)*frames], x[(bins+b)*frames:(bins+b+1)*frames])
		sinInplace32(phase[b*frames : (b+1)*frames])
	}
	pcm := istft(mag, phase, frames, 20, 5)
	peak := float32(0)
	for _, v := range pcm {
		av := v
		if av < 0 {
			av = -av
		}
		if av > peak {
			peak = av
		}
	}
	if peak > 1 {
		inv := 0.95 / peak
		for i := range pcm {
			pcm[i] *= inv
		}
	}
	return pcm, nil
}

func (m *Model) generatorAdaINResBlock1(x []float32, channels, frames int, style []float32, prefix string) ([]float32, error) {
	out := make([]float32, len(x))
	copy(out, x)
	for i := 0; i < 3; i++ {
		xt := make([]float32, len(out))
		if err := adaIN1D(xt, out, channels, frames, style, mustTensorData(m, fmt.Sprintf("%s.adain1.%d.fc.weight", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.adain1.%d.fc.bias", prefix, i))); err != nil {
			return nil, err
		}
		alpha1 := mustTensorData(m, fmt.Sprintf("%s.alpha1.%d", prefix, i))
		snakeInplace(xt, alpha1, channels, frames)
		var err error
		xt, err = m.weightNormConv1D(xt, channels, frames, channels, kernelForGeneratorBlock(prefix), 1, paddingForKernel(kernelForGeneratorBlock(prefix), dilationForIndex(i)), dilationForIndex(i), 1, fmt.Sprintf("%s.convs1.%d", prefix, i))
		if err != nil {
			return nil, err
		}
		xt2 := make([]float32, len(xt))
		if err := adaIN1D(xt2, xt, channels, frames, style, mustTensorData(m, fmt.Sprintf("%s.adain2.%d.fc.weight", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.adain2.%d.fc.bias", prefix, i))); err != nil {
			return nil, err
		}
		alpha2 := mustTensorData(m, fmt.Sprintf("%s.alpha2.%d", prefix, i))
		snakeInplace(xt2, alpha2, channels, frames)
		xt2, err = m.weightNormConv1D(xt2, channels, frames, channels, kernelForGeneratorBlock(prefix), 1, paddingForKernel(kernelForGeneratorBlock(prefix), 1), 1, 1, fmt.Sprintf("%s.convs2.%d", prefix, i))
		if err != nil {
			return nil, err
		}
		for n := range out {
			out[n] += xt2[n]
		}
	}
	return out, nil
}

func kernelForGeneratorBlock(prefix string) int {
	if strings.Contains(prefix, ".noise_res.0") {
		return 7
	}
	if strings.Contains(prefix, ".noise_res.1") {
		return 11
	}
	idx := -1
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] == '.' {
			for _, ch := range prefix[i+1:] {
				if ch < '0' || ch > '9' {
					return 3
				}
				if idx < 0 {
					idx = 0
				}
				idx = idx*10 + int(ch-'0')
			}
			break
		}
	}
	if idx < 0 {
		return 3
	}
	if idx <= 2 {
		return []int{3, 7, 11}[idx%3]
	}
	return []int{3, 7, 11}[(idx-3)%3]
}

func dilationForIndex(i int) int {
	return []int{1, 3, 5}[i]
}

func paddingForKernel(kernel, dilation int) int {
	return (kernel*dilation - dilation) / 2
}

func snakeInplace(x, alpha []float32, channels, frames int) {
	if frames == 0 {
		return
	}
	scratch := make([]float32, frames)
	for c := 0; c < channels; c++ {
		a := alpha[c]
		if a == 0 {
			a = 1
		}
		row := x[c*frames : (c+1)*frames]
		copy(scratch, row)
		if a != 1 {
			mulNumberInplace32(scratch, a)
		}
		sinInplace32(scratch)
		for i, v := range scratch {
			scratch[i] = v * v
		}
		mulNumberInplace32(scratch, 1/a)
		addInplace32(row, scratch)
	}
}

func reflectionPadLeft1(x []float32, channels, frames int) []float32 {
	out := make([]float32, channels*(frames+1))
	for c := 0; c < channels; c++ {
		src := x[c*frames : (c+1)*frames]
		dst := out[c*(frames+1) : (c+1)*(frames+1)]
		if frames > 1 {
			dst[0] = src[1]
		} else if frames == 1 {
			dst[0] = src[0]
		}
		copy(dst[1:], src)
	}
	return out
}

func (m *Model) generatorSourceForStage(f0 []float32, frames, channels int, style []float32, stage int) ([]float32, error) {
	const sourceScale = 300
	harmonics := synthHarmonicsKokoro(f0, 24000, sourceScale, 8, 0.1)
	merged := make([]float32, len(f0)*sourceScale)
	w, err := m.tensorData("decoder.module.generator.m_source.l_linear.weight")
	if err != nil {
		return nil, err
	}
	b, err := m.tensorData("decoder.module.generator.m_source.l_linear.bias")
	if err != nil {
		return nil, err
	}
	for t := range merged {
		sum := b[0]
		for h := 0; h < 9; h++ {
			sum += harmonics[t*9+h] * w[h]
		}
		merged[t] = tanh(sum)
	}
	mag, phase, stftFrames := stftMagnitudePhase(merged, 20, 5)
	har := make([]float32, 22*stftFrames)
	for c := 0; c < 11; c++ {
		copy(har[c*stftFrames:(c+1)*stftFrames], mag[c*stftFrames:(c+1)*stftFrames])
		copy(har[(11+c)*stftFrames:(12+c)*stftFrames], phase[c*stftFrames:(c+1)*stftFrames])
	}
	var source []float32
	if stage == 0 {
		source, err = m.plainConv1D(har, 22, stftFrames, channels, 12, 6, 3, 1, 1, "decoder.module.generator.noise_convs.0")
		if err != nil {
			return nil, err
		}
		source, _, err = m.generatorNoiseResBlock(source, len(source)/channels, channels, style, "decoder.module.generator.noise_res.0")
		if err != nil {
			return nil, err
		}
	} else {
		source, err = m.plainConv1D(har, 22, stftFrames, channels, 1, 1, 0, 1, 1, "decoder.module.generator.noise_convs.1")
		if err != nil {
			return nil, err
		}
		source, _, err = m.generatorNoiseResBlock(source, len(source)/channels, channels, style, "decoder.module.generator.noise_res.1")
		if err != nil {
			return nil, err
		}
	}
	if len(source) == channels*frames {
		return source, nil
	}
	return resizeChannelTime(source, channels, len(source)/channels, frames), nil
}

func (m *Model) plainConv1D(x []float32, inC, inT, outC, kernel, stride, padding, dilation, groups int, prefix string) ([]float32, error) {
	w, err := m.tensorData(prefix + ".weight")
	if err != nil {
		return nil, err
	}
	b, err := m.tensorData(prefix + ".bias")
	if err != nil {
		return nil, err
	}
	outT := (inT+2*padding-dilation*(kernel-1)-1)/stride + 1
	out := make([]float32, outC*outT)
	if useKokoroSIMD() && groups == 1 && kernel == 1 && stride == 1 && padding == 0 && dilation == 1 && inC >= 16 && outC*inT*inC > 50000 {
		if err := conv1DPointwiseSIMD(out, x, w, b, inC, inT, outC); err != nil {
			return nil, err
		}
		return out, nil
	}
	if useKokoroSIMD() && useKokoroConvMatMul() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		if err := conv1DMatMul(out, x, w, b, inC, inT, outC, kernel, stride, padding, dilation, outT); err != nil {
			return nil, err
		}
		return out, nil
	}
	if useKokoroSIMD() && groups == 1 && inC >= 16 && outC*outT*inC*kernel > 50000 {
		wT, err := m.cachedFloat32(prefix+".weight_transposed", func() ([]float32, error) {
			wT := make([]float32, outC*kernel*inC)
			transposeConv1DWeight(wT, w, inC, outC, kernel)
			return wT, nil
		})
		if err != nil {
			return nil, err
		}
		if err := conv1DSIMDTransposedWeight(out, x, wT, b, inC, inT, outC, kernel, stride, padding, dilation, outT); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := Conv1D(out, x, w, b, inC, inT, outC, kernel, stride, padding, dilation, groups); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Model) generatorNoiseResBlock(x []float32, frames, channels int, style []float32, prefix string) ([]float32, int, error) {
	y, err := m.generatorAdaINResBlock1(x, channels, frames, style, prefix)
	if err != nil {
		return nil, 0, err
	}
	return y, frames, nil
}

func linearUpsample(x []float32, scale int) []float32 {
	if len(x) == 0 || scale <= 1 {
		return append([]float32(nil), x...)
	}
	out := make([]float32, len(x)*scale)
	for i := range out {
		pos := float64(i) / float64(scale)
		lo := int(pos)
		if lo >= len(x)-1 {
			out[i] = x[len(x)-1]
			continue
		}
		frac := float32(pos - float64(lo))
		out[i] = x[lo]*(1-frac) + x[lo+1]*frac
	}
	return out
}

func synthHarmonicsKokoro(f0 []float32, sampleRate, upsampleScale, harmonicNum int, amp float32) []float32 {
	dim := harmonicNum + 1
	outLen := len(f0) * upsampleScale
	out := make([]float32, outLen*dim)
	if len(f0) == 0 || upsampleScale <= 0 {
		return out
	}
	phaseLow := make([]float32, len(f0))
	phase := make([]float32, outLen)
	for h := 0; h < dim; h++ {
		cumsum := float64(0)
		mul := float64(h + 1)
		for t, base := range f0 {
			rad := math.Mod(float64(base)*mul/float64(sampleRate), 1)
			if rad < 0 {
				rad += 1
			}
			cumsum += rad
			phaseLow[t] = float32(cumsum * 2 * math.Pi * float64(upsampleScale))
		}
		for t := 0; t < outLen; t++ {
			pos := (float64(t)+0.5)/float64(upsampleScale) - 0.5
			lo := int(math.Floor(pos))
			frac := float32(pos - float64(lo))
			if lo < 0 {
				phase[t] = phaseLow[0]
			} else if lo >= len(f0)-1 {
				phase[t] = phaseLow[len(f0)-1]
			} else {
				phase[t] = phaseLow[lo]*(1-frac) + phaseLow[lo+1]*frac
			}
		}
		sinInplace32(phase)
		for t, v := range phase {
			if f0[t/upsampleScale] > 10 {
				out[t*dim+h] = amp * v
			} else {
				out[t*dim+h] = 0
			}
		}
	}
	return out
}

func resizeChannelTime(x []float32, channels, inT, outT int) []float32 {
	out := make([]float32, channels*outT)
	if inT <= 0 {
		return out
	}
	if outT == 1 {
		for c := 0; c < channels; c++ {
			out[c] = x[c*inT]
		}
		return out
	}
	for c := 0; c < channels; c++ {
		for t := 0; t < outT; t++ {
			pos := float64(t) * float64(inT-1) / float64(outT-1)
			lo := int(pos)
			if lo >= inT-1 {
				out[c*outT+t] = x[c*inT+inT-1]
				continue
			}
			frac := float32(pos - float64(lo))
			out[c*outT+t] = x[c*inT+lo]*(1-frac) + x[c*inT+lo+1]*frac
		}
	}
	return out
}
