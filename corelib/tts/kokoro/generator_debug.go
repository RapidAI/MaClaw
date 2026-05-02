package kokoro

import (
	"fmt"
	"math"
)

type TensorStats struct {
	Shape []int   `json:"shape"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
	RMS   float64 `json:"rms"`
}

func StatsOf(x []float32, shape ...int) TensorStats {
	if len(x) == 0 {
		return TensorStats{Shape: shape}
	}
	min, max := float64(x[0]), float64(x[0])
	sum, ss := 0.0, 0.0
	for _, fv := range x {
		v := float64(fv)
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
		ss += v * v
	}
	return TensorStats{Shape: shape, Min: min, Max: max, Mean: sum / float64(len(x)), RMS: math.Sqrt(ss / float64(len(x)))}
}

func (m *Model) GeneratorDebugStats(feat *DecoderFeatures, f0n *F0NResult, voice *TensorFile) (map[string]TensorStats, error) {
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
	stats := map[string]TensorStats{"input": StatsOf(x, 1, 512, frames)}
	upsampleRates := []int{10, 6}
	upsampleKernels := []int{20, 12}
	for i := 0; i < 2; i++ {
		for j := range x {
			x[j] = leakyReLU(x[j], 0.1)
		}
		stats[fmt.Sprintf("stage%d_after_lrelu", i)] = StatsOf(x, 1, inC, frames)
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
		stats[fmt.Sprintf("stage%d_after_ups", i)] = StatsOf(x, 1, inC, frames)
		if i == 1 {
			x = reflectionPadLeft1(x, inC, frames)
			frames++
			stats["stage1_after_reflect"] = StatsOf(x, 1, inC, frames)
		}
		xSource, err := m.generatorSourceForStage(f0n.F0, frames, outC, s, i)
		if err != nil {
			return nil, err
		}
		stats[fmt.Sprintf("stage%d_source", i)] = StatsOf(xSource, 1, outC, frames)
		for j := range x {
			x[j] += xSource[j]
		}
		stats[fmt.Sprintf("stage%d_after_source_add", i)] = StatsOf(x, 1, inC, frames)
		var sum []float32
		for j := 0; j < 3; j++ {
			blockIdx := i*3 + j
			y, err := m.generatorAdaINResBlock1(x, inC, frames, s, fmt.Sprintf("decoder.module.generator.resblocks.%d", blockIdx))
			if err != nil {
				return nil, err
			}
			stats[fmt.Sprintf("resblock%d_out", blockIdx)] = StatsOf(y, 1, inC, frames)
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
		stats[fmt.Sprintf("stage%d_out", i)] = StatsOf(x, 1, inC, frames)
	}
	for i := range x {
		x[i] = leakyReLU(x[i], 0.01)
	}
	stats["pre_conv_post_lrelu"] = StatsOf(x, 1, 128, frames)
	post, err := m.weightNormConv1D(x, 128, frames, 22, 7, 1, 3, 1, 1, "decoder.module.generator.conv_post")
	if err != nil {
		return nil, err
	}
	stats["conv_post"] = StatsOf(post, 1, 22, frames)
	bins := 11
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	for b := 0; b < bins; b++ {
		for t := 0; t < frames; t++ {
			mag[b*frames+t] = float32(math.Exp(float64(post[b*frames+t])))
			phase[b*frames+t] = float32(math.Sin(float64(post[(bins+b)*frames+t])))
		}
	}
	pcm := istft(mag, phase, frames, 20, 5)
	stats["audio"] = StatsOf(pcm, 1, 1, len(pcm))
	return stats, nil
}
