package kokoro

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"
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
	// Both stages consume the same harmonic spectrum. Building it once
	// avoids a second full-rate sine bank and STFT.
	har, err := m.sourceHarmonicSpectrum(f0n.F0)
	if err != nil {
		return nil, err
	}
	upsampleRates := []int{10, 6}
	upsampleKernels := []int{20, 12}
	for i := 0; i < 2; i++ {
		leakyReLUInplace(x, 0.1)
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
		xSource, err := m.generatorSourceForStage(har, frames, outC, s, i)
		if err != nil {
			return nil, err
		}
		addInplace32(x, xSource)
		if sum, ok, err := m.gpuResBlocks(x, inC, frames, s, i); err != nil {
			return nil, err
		} else if ok {
			x = sum
			continue
		}
		// CPU stages average three resblocks. xSource has been added into x,
		// so the helper may overwrite it as the block-1/2 scratch.
		x, err = m.averageGeneratorResBlocks(x, xSource, inC, frames, s, i)
		if err != nil {
			return nil, err
		}
	}
	leakyReLUInplace(x, 0.01)
	x, err = m.weightNormConv1D(x, 128, frames, 22, 7, 1, 3, 1, 1, "decoder.module.generator.conv_post")
	if err != nil {
		return nil, err
	}
	bins := 11
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	for b := 0; b < bins; b++ {
		expInto32(mag[b*frames:(b+1)*frames], x[b*frames:(b+1)*frames])
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

// averageGeneratorResBlocks averages the three AdaIN resblocks of one
// vocoder stage. Block 0 is written into a fresh accumulator the caller
// keeps. Blocks 1 and 2 share one scratch. scratch is used for that when
// it has the same length and does not overlap x or the temps; otherwise a
// pooled buffer is used and returned on the way out. x is only read.
// xt and mid are shared across the three blocks; each block overwrites them.
func (m *Model) averageGeneratorResBlocks(x, scratch []float32, channels, frames int, style []float32, stage int) ([]float32, error) {
	sum := make([]float32, len(x))
	xt := getFloat32Buffer(len(x))
	mid := getFloat32Buffer(len(x))
	defer putFloat32Buffer(xt)
	defer putFloat32Buffer(mid)
	if err := m.generatorAdaINResBlock1Temps(sum, x, xt, mid, channels, frames, style, fmt.Sprintf("decoder.module.generator.resblocks.%d", stage*3)); err != nil {
		return nil, err
	}
	extra := scratch
	pooled := false
	if len(extra) != len(x) || float32Overlaps(extra, x) || float32Overlaps(extra, xt) || float32Overlaps(extra, mid) || float32Overlaps(extra, sum) {
		extra = getFloat32Buffer(len(x))
		pooled = true
	}
	defer func() {
		if pooled {
			putFloat32Buffer(extra)
		}
	}()
	for j := 1; j < 3; j++ {
		if err := m.generatorAdaINResBlock1Temps(extra, x, xt, mid, channels, frames, style, fmt.Sprintf("decoder.module.generator.resblocks.%d", stage*3+j)); err != nil {
			return nil, err
		}
		addInplace32(sum, extra)
	}
	mulNumberInplace32(sum, 1.0/3)
	return sum, nil
}

// float32Overlaps reports whether a and b share any element.
func float32Overlaps(a, b []float32) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	as := uintptr(unsafe.Pointer(unsafe.SliceData(a)))
	bs := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	ae := as + uintptr(len(a))*unsafe.Sizeof(a[0])
	be := bs + uintptr(len(b))*unsafe.Sizeof(b[0])
	return as < be && bs < ae
}

// generatorAdaINResBlock1 allocates its output. Debug stats and the noise
// resblock keep a private buffer so a later block cannot overwrite it.
func (m *Model) generatorAdaINResBlock1(x []float32, channels, frames int, style []float32, prefix string) ([]float32, error) {
	out := make([]float32, len(x))
	if err := m.generatorAdaINResBlock1Into(out, x, channels, frames, style, prefix); err != nil {
		return nil, err
	}
	return out, nil
}

// generatorAdaINResBlock1Into writes one resblock into dst. dst may alias x.
// dst must not alias the temps this function checks out.
func (m *Model) generatorAdaINResBlock1Into(dst, x []float32, channels, frames int, style []float32, prefix string) error {
	if len(dst) != len(x) {
		return fmt.Errorf("kokoro: resblock out length %d, want %d", len(dst), len(x))
	}
	xt := getFloat32Buffer(len(dst))
	mid := getFloat32Buffer(len(dst))
	defer putFloat32Buffer(xt)
	defer putFloat32Buffer(mid)
	return m.generatorAdaINResBlock1Temps(dst, x, xt, mid, channels, frames, style, prefix)
}

// generatorAdaINResBlock1Temps writes one resblock into dst using caller-owned
// temps. The first dilation reads x and adds it as the residual, which is the
// old opening copy followed by an in-place residual. Later dilations read dst.
// conv1 copies xt before it returns, so the second snake reuses xt. A dirty
// dst is fully overwritten by that first residual conv.
func (m *Model) generatorAdaINResBlock1Temps(dst, x, xt, mid []float32, channels, frames int, style []float32, prefix string) error {
	if len(dst) != len(x) || len(xt) != len(dst) || len(mid) != len(dst) {
		return fmt.Errorf("kokoro: resblock lengths dst %d x %d xt %d mid %d", len(dst), len(x), len(xt), len(mid))
	}
	for i := 0; i < 3; i++ {
		src := dst
		residual := dst
		if i == 0 {
			src = x
			residual = x
		}
		k := kernelForGeneratorBlock(prefix)
		dil := dilationForIndex(i)
		if err := adaINSnake1D(xt, src, channels, frames, style, mustTensorData(m, fmt.Sprintf("%s.adain1.%d.fc.weight", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.adain1.%d.fc.bias", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.alpha1.%d", prefix, i))); err != nil {
			return err
		}
		if err := m.weightNormConv1DInto(mid, xt, channels, frames, channels, k, 1, paddingForKernel(k, dil), dil, 1, fmt.Sprintf("%s.convs1.%d", prefix, i)); err != nil {
			return err
		}
		if err := adaINSnake1D(xt, mid, channels, frames, style, mustTensorData(m, fmt.Sprintf("%s.adain2.%d.fc.weight", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.adain2.%d.fc.bias", prefix, i)), mustTensorData(m, fmt.Sprintf("%s.alpha2.%d", prefix, i))); err != nil {
			return err
		}
		// convs2 accumulates straight into dst (fused residual epilogue).
		if err := m.weightNormConv1DResidual(dst, xt, channels, frames, channels, k, 1, paddingForKernel(k, 1), 1, 1, fmt.Sprintf("%s.convs2.%d", prefix, i), residual); err != nil {
			return err
		}
	}
	return nil
}

// adaINSnake1D fuses adaIN1D with the following snake activation: it writes
// the normalized rows to out (the conv input) and applies
// out += sin(alpha*out)^2 / alpha in place, using one fused normalization
// pass that also emits the scaled snake argument.
func adaINSnake1D(out, x []float32, channels, frames int, style, fcW, fcB, alpha []float32) error {
	if len(out) != channels*frames || len(x) != channels*frames {
		return fmt.Errorf("kokoro: adain snake shape mismatch")
	}
	params := getFloat32Buffer(channels * 2)
	defer putFloat32Buffer(params)
	if err := Linear(params, style, fcW, fcB, len(style), channels*2); err != nil {
		return err
	}
	normRange := func(c0, c1 int) {
		scratch := getFloat32Buffer(frames)
		defer putFloat32Buffer(scratch)
		for c := c0; c < c1; c++ {
			row := x[c*frames : (c+1)*frames]
			dst := out[c*frames : (c+1)*frames]
			mean := sum32(row) / float32(frames)
			variance := dot32(row, row)/float32(frames) - mean*mean
			if variance < 0 {
				variance = 0
			}
			inv := 1 / float32(math.Sqrt(float64(variance+1e-5)))
			gamma := params[c]
			beta := params[channels+c]
			a := alpha[c]
			if a == 0 {
				a = 1
			}
			normScaleScaledInto(dst, scratch, row, mean, inv*(1+gamma), beta, a)
			sinInplace32(scratch)
			squareInplace(scratch)
			axpyInplace(dst, scratch, 1/a)
		}
	}
	workers := runtime.GOMAXPROCS(0)
	if channels*frames < 1<<15 || workers < 2 {
		normRange(0, channels)
		return nil
	}
	if workers > channels {
		workers = channels
	}
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	chunk := (channels + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > channels {
			end = channels
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(c0, c1 int) {
			defer wg.Done()
			normRange(c0, c1)
		}(start, end)
	}
	wg.Wait()
	return nil
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
	snakeRange := func(c0, c1 int) {
		scratch := make([]float32, frames)
		for c := c0; c < c1; c++ {
			a := alpha[c]
			if a == 0 {
				a = 1
			}
			row := x[c*frames : (c+1)*frames]
			// scratch = (sin(a*row))^2 / a, then row += scratch.
			// Fused into four passes: scale, sin, square, axpy.
			mulNumberInto32(scratch, row, a)
			sinInplace32(scratch)
			squareInplace(scratch)
			axpyInplace(row, scratch, 1/a)
		}
	}
	workers := runtime.GOMAXPROCS(0)
	if channels*frames < 1<<15 || workers < 2 {
		snakeRange(0, channels)
		return
	}
	if workers > channels {
		workers = channels
	}
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	chunk := (channels + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > channels {
			end = channels
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(c0, c1 int) {
			defer wg.Done()
			snakeRange(c0, c1)
		}(start, end)
	}
	wg.Wait()
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

// sourceHarmonicSpectrum packs 11 magnitude bins and 11 phase bins of the
// F0 harmonic source. Both vocoder stages read this same buffer.
func (m *Model) sourceHarmonicSpectrum(f0 []float32) ([]float32, error) {
	const sourceScale = 300
	merged := make([]float32, len(f0)*sourceScale)
	w, err := m.tensorData("decoder.module.generator.m_source.l_linear.weight")
	if err != nil {
		return nil, err
	}
	b, err := m.tensorData("decoder.module.generator.m_source.l_linear.bias")
	if err != nil {
		return nil, err
	}
	// Mix in harmonic order directly. That is the same sum as materializing
	// the 9-channel bank and dotting it with this weight.
	mixSourceHarmonics(merged, f0, 24000, sourceScale, 8, 0.1, w, b[0])
	// tanh elementwise via exp: tanh(x) = 1 - 2/(e^{2x}+1); one fused
	// vector pass instead of 1.7M scalar math.Tanh calls.
	tanhInplace32(merged)
	mag, phase, stftFrames := stftMagnitudePhase(merged, 20, 5)
	har := make([]float32, 22*stftFrames)
	for c := 0; c < 11; c++ {
		copy(har[c*stftFrames:(c+1)*stftFrames], mag[c*stftFrames:(c+1)*stftFrames])
		copy(har[(11+c)*stftFrames:(12+c)*stftFrames], phase[c*stftFrames:(c+1)*stftFrames])
	}
	return har, nil
}

func (m *Model) generatorSourceForStage(har []float32, frames, channels int, style []float32, stage int) ([]float32, error) {
	if len(har)%22 != 0 || channels <= 0 {
		return nil, fmt.Errorf("kokoro: harmonic spectrum length %d channels %d", len(har), channels)
	}
	stftFrames := len(har) / 22
	var err error
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
		if err := conv1DSIMDTransposedWeight(out, x, wT, b, nil, inC, inT, outC, kernel, stride, padding, dilation, outT); err != nil {
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
	if len(f0) == 0 || upsampleScale <= 0 || harmonicNum < 0 {
		return out
	}
	lo, frac := buildHarmonicGrid(outLen, len(f0), upsampleScale)
	phaseLow := make([]float32, len(f0))
	phase := make([]float32, outLen)
	for h := 0; h < dim; h++ {
		fillHarmonicPhaseLow(phaseLow, f0, sampleRate, upsampleScale, h)
		interpHarmonicPhase(phase, phaseLow, lo, frac)
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

// mixSourceHarmonics writes bias plus the harmonic bank dotted with w.
// Harmonic 0 is added first. The upsample grid is built once and reused
// for every harmonic. dst length is len(f0)*upsampleScale.
func mixSourceHarmonics(dst, f0 []float32, sampleRate, upsampleScale, harmonicNum int, amp float32, w []float32, bias float32) {
	if len(dst) == 0 || len(f0) == 0 || upsampleScale <= 0 || harmonicNum < 0 {
		return
	}
	for i := range dst {
		dst[i] = bias
	}
	lo, frac := buildHarmonicGrid(len(dst), len(f0), upsampleScale)
	phaseLow := make([]float32, len(f0))
	phase := make([]float32, len(dst))
	for h := 0; h <= harmonicNum; h++ {
		fillHarmonicPhaseLow(phaseLow, f0, sampleRate, upsampleScale, h)
		interpHarmonicPhase(phase, phaseLow, lo, frac)
		sinInplace32(phase)
		wh := w[h]
		for t, v := range phase {
			if f0[t/upsampleScale] > 10 {
				dst[t] += (amp * v) * wh
			}
		}
	}
}

// buildHarmonicGrid records the half-pixel upsample used by the harmonic
// bank. lo < 0 selects the first low-rate sample. lo >= nLow-1 selects the
// last one. Other entries lerp phaseLow[lo] and phaseLow[lo+1] by frac.
func buildHarmonicGrid(outLen, nLow, scale int) (lo []int32, frac []float32) {
	lo = make([]int32, outLen)
	frac = make([]float32, outLen)
	if outLen <= 0 || nLow <= 0 || scale <= 0 {
		return lo, frac
	}
	scaleF := float64(scale)
	last := int32(nLow - 1)
	for t := 0; t < outLen; t++ {
		pos := (float64(t)+0.5)/scaleF - 0.5
		l := int32(math.Floor(pos))
		if l < 0 {
			lo[t] = -1
			continue
		}
		if l >= last {
			lo[t] = last
			continue
		}
		lo[t] = l
		frac[t] = float32(pos - float64(l))
	}
	return lo, frac
}

func fillHarmonicPhaseLow(phaseLow, f0 []float32, sampleRate, upsampleScale, harmonic int) {
	cumsum := float64(0)
	mul := float64(harmonic + 1)
	for t, base := range f0 {
		rad := math.Mod(float64(base)*mul/float64(sampleRate), 1)
		if rad < 0 {
			rad += 1
		}
		cumsum += rad
		phaseLow[t] = float32(cumsum * 2 * math.Pi * float64(upsampleScale))
	}
}

func interpHarmonicPhase(phase, phaseLow []float32, lo []int32, frac []float32) {
	last := int32(len(phaseLow) - 1)
	for t := range phase {
		l := lo[t]
		if l < 0 {
			phase[t] = phaseLow[0]
			continue
		}
		if l >= last {
			phase[t] = phaseLow[last]
			continue
		}
		f := frac[t]
		phase[t] = phaseLow[l]*(1-f) + phaseLow[l+1]*f
	}
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
