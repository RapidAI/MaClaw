// corelib/asr/sensevoice_fbank.go — Mel-filterbank + LFR + CMVN for SenseVoice.
//
// SenseVoice uses 80-band log-mel filterbank features with:
//   - 25ms window, 10ms hop (Hamming window)
//   - LFR (Low Frame Rate): stack 7 consecutive frames, skip 6 → 560-dim
//   - CMVN (Cepstral Mean and Variance Normalization) using stored stats
//
// Hot-path opts:
//   - float32 real-FFT (pack → N/2 complex FFT → power) — half butterflies, half BW
//   - sparse mel + shared power spectrum
//   - precomputed twiddles / bit-reversal / post-twiddles (float32)
//   - fused DC/preemph/window prep
//   - multi-core over frames when long enough
package asr

import (
	"math"
	"runtime"
	"sync"
)

const (
	svSampleRate = 16000
	svNumMels    = 80
	svWindowSize = 400  // 25ms at 16kHz
	svHopSize    = 160  // 10ms at 16kHz
	svFFTSize    = 512  // next power of 2 >= windowSize
	svLFRm       = 7    // stack 7 frames
	svLFRn       = 6    // skip 6 frames
	svFeatsDim   = 560  // svNumMels * svLFRm = 80 * 7
	svNBins      = svFFTSize / 2 // 256 spectrum bins used by mel
	svHalfFFT    = svFFTSize / 2 // 256-pt complex FFT inside rfft
)

// sparseMelBand holds the non-zero slice of one mel filter (triangle support).
type sparseMelBand struct {
	start int       // first non-zero bin
	w     []float32 // weights for bins [start, start+len)
}

// Cached tables (immutable after init).
var (
	svFbankOnce  sync.Once
	svHammingWin []float32
	svMelWeights []float32
	svSparseMel  []sparseMelBand

	// float64 full FFT (tests / fftInPlace)
	svFFTBitRev   [svFFTSize]int
	svFFTTwiddles []fftStageTwiddle64

	// float32 real-FFT
	svRFFTBitRev   [svHalfFFT]int
	svRFFTTwiddles []fftStageTwiddle32
	svRFFTPostCos  [svHalfFFT]float32
	svRFFTPostSin  [svHalfFFT]float32

	fftPool = sync.Pool{New: func() any {
		return &fftScratch{
			re:    make([]float32, svHalfFFT),
			im:    make([]float32, svHalfFFT),
			time:  make([]float32, svFFTSize), // windowed real frame
			power: make([]float32, svNBins),
		}
	}}
)

type fftStageTwiddle64 struct {
	cos, sin []float64
}

type fftStageTwiddle32 struct {
	cos, sin []float32
}

type fftScratch struct {
	re, im []float32 // N/2 complex working set
	time   []float32 // N real windowed samples
	power  []float32 // |X[k]|^2
}

func svInitFbankTables() {
	svFbankOnce.Do(func() {
		svHammingWin = make([]float32, svWindowSize)
		for i := range svHammingWin {
			svHammingWin[i] = float32(0.54 - 0.46*math.Cos(2.0*math.Pi*float64(i)/float64(svWindowSize)))
		}
		svMelWeights = svBuildKaldiMelFilterbank()
		svSparseMel = svBuildSparseMel(svMelWeights)
		svInitFFTTables64()
		svInitRFFTTables32()
	})
}

func svInitBitRev(dst []int, n int) {
	j := 0
	dst[0] = 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		dst[i] = j
	}
}

func svInitFFTTables64() {
	svInitBitRev(svFFTBitRev[:], svFFTSize)
	svFFTTwiddles = nil
	for size := 2; size <= svFFTSize; size <<= 1 {
		half := size >> 1
		cos := make([]float64, half)
		sin := make([]float64, half)
		angleStep := -2.0 * math.Pi / float64(size)
		for k := 0; k < half; k++ {
			angle := angleStep * float64(k)
			cos[k] = math.Cos(angle)
			sin[k] = math.Sin(angle)
		}
		svFFTTwiddles = append(svFFTTwiddles, fftStageTwiddle64{cos: cos, sin: sin})
	}
}

func svInitRFFTTables32() {
	svInitBitRev(svRFFTBitRev[:], svHalfFFT)
	svRFFTTwiddles = nil
	for size := 2; size <= svHalfFFT; size <<= 1 {
		half := size >> 1
		cos := make([]float32, half)
		sin := make([]float32, half)
		angleStep := -2.0 * math.Pi / float64(size)
		for k := 0; k < half; k++ {
			angle := angleStep * float64(k)
			cos[k] = float32(math.Cos(angle))
			sin[k] = float32(math.Sin(angle))
		}
		svRFFTTwiddles = append(svRFFTTwiddles, fftStageTwiddle32{cos: cos, sin: sin})
	}
	n := svFFTSize
	for k := 0; k < svHalfFFT; k++ {
		angle := -2.0 * math.Pi * float64(k) / float64(n)
		svRFFTPostCos[k] = float32(math.Cos(angle))
		svRFFTPostSin[k] = float32(math.Sin(angle))
	}
}

func svBuildSparseMel(dense []float32) []sparseMelBand {
	bands := make([]sparseMelBand, svNumMels)
	for m := 0; m < svNumMels; m++ {
		row := dense[m*svNBins : (m+1)*svNBins]
		start, end := 0, svNBins
		for start < svNBins && row[start] == 0 {
			start++
		}
		for end > start && row[end-1] == 0 {
			end--
		}
		if start >= end {
			bands[m] = sparseMelBand{start: 0, w: nil}
			continue
		}
		w := make([]float32, end-start)
		copy(w, row[start:end])
		bands[m] = sparseMelBand{start: start, w: w}
	}
	return bands
}

// svMelFilterbank computes 80-band log-mel spectrogram from 16kHz PCM.
func svMelFilterbank(pcm []float32) []float32 {
	nSamples := len(pcm)
	if nSamples < svWindowSize {
		return nil
	}
	numFrames := (nSamples - svWindowSize) / svHopSize + 1
	out := make([]float32, numFrames*svNumMels)
	if !svMelFilterbankInto(pcm, out) {
		return nil
	}
	return out
}

// svMelFilterbankInto writes fbank into out (must have capacity numFrames*svNumMels).
func svMelFilterbankInto(pcm, out []float32) bool {
	nSamples := len(pcm)
	if nSamples < svWindowSize {
		return false
	}
	numFrames := (nSamples - svWindowSize) / svHopSize + 1
	if len(out) < numFrames*svNumMels {
		return false
	}
	svInitFbankTables()

	nw := runtime.NumCPU()
	if nw > 8 {
		nw = 8
	}
	if numFrames < 48 || nw <= 1 {
		svMelFilterbankRange(pcm, out, 0, numFrames)
		return true
	}
	var wg sync.WaitGroup
	chunk := (numFrames + nw - 1) / nw
	for w := 0; w < nw; w++ {
		fs := w * chunk
		fe := fs + chunk
		if fe > numFrames {
			fe = numFrames
		}
		if fs >= fe {
			break
		}
		wg.Add(1)
		go func(fs, fe int) {
			defer wg.Done()
			svMelFilterbankRange(pcm, out, fs, fe)
		}(fs, fe)
	}
	wg.Wait()
	return true
}

func svMelFilterbankRange(pcm, out []float32, fs, fe int) {
	window := svHammingWin
	sparse := svSparseMel
	const preemph float32 = 0.97
	const logFloor = float32(1.19e-7)

	sc := fftPool.Get().(*fftScratch)
	re, im, time, power := sc.re, sc.im, sc.time, sc.power
	defer fftPool.Put(sc)

	for f := fs; f < fe; f++ {
		offset := f * svHopSize
		frame := pcm[offset : offset+svWindowSize]

		// Mean (DC)
		var sum float32
		i := 0
		for ; i+3 < svWindowSize; i += 4 {
			sum += frame[i] + frame[i+1] + frame[i+2] + frame[i+3]
		}
		for ; i < svWindowSize; i++ {
			sum += frame[i]
		}
		mean := sum / float32(svWindowSize)

		// DC-remove + preemph + Hamming → time[0:window]; zero-pad
		r0 := frame[0] - mean
		time[0] = r0 * (1.0 - preemph) * window[0]
		prev := r0
		for i := 1; i < svWindowSize; i++ {
			cur := frame[i] - mean
			time[i] = (cur - preemph*prev) * window[i]
			prev = cur
		}
		for i := svWindowSize; i < svFFTSize; i++ {
			time[i] = 0
		}

		rfftPower32(time, re, im, power)

		base := f * svNumMels
		for m := 0; m < svNumMels; m++ {
			band := sparse[m]
			var melEnergy float32
			w := band.w
			ks := band.start
			for j := 0; j < len(w); j++ {
				melEnergy += power[ks+j] * w[j]
			}
			if melEnergy < logFloor {
				melEnergy = logFloor
			}
			out[base+m] = fastLog32(melEnergy)
		}
	}
}

// fastLog32 is a float32 natural-log approximation (fastapprox-style).
// Max relative error ~1e-3 over typical mel energies — fine for ASR frontends.
func fastLog32(x float32) float32 {
	// log(x) = log2(x) * ln(2)
	const ln2 = 0.6931471805599453
	vx := math.Float32bits(x)
	mx := math.Float32frombits((vx & 0x007FFFFF) | 0x3f000000)
	y := float32(vx) * 1.1920928955078125e-7
	// log2(x) ≈ y - c0 - c1*mx - c2/(c3+mx)
	return float32(ln2) * (y - 124.22551499 - 1.498030302*mx - 1.72587999/(0.3520887068+mx))
}

// rfftPower32: real N-pt FFT power spectrum via N/2 complex FFT (float32).
// time holds real input [0:N); re/im are N/2 scratch; power gets N/2 bins.
func rfftPower32(time, re, im, power []float32) {
	half := svHalfFFT

	// Pack z[k] = time[2k] + j*time[2k+1]
	for k := 0; k < half; k++ {
		re[k] = time[2*k]
		im[k] = time[2*k+1]
	}

	complexFFTHalf32(re, im)

	// DC: X[0] = Re(Z[0]) + Im(Z[0])
	t0 := re[0] + im[0]
	power[0] = t0 * t0

	for k := 1; k < half; k++ {
		zr, zi := re[k], im[k]
		znr, zni := re[half-k], im[half-k]
		xeR := float32(0.5) * (zr + znr)
		xeI := float32(0.5) * (zi - zni)
		xoR := float32(0.5) * (zr - znr)
		xoI := float32(0.5) * (zi + zni)
		wc, ws := svRFFTPostCos[k], svRFFTPostSin[k]
		wXoR := wc*xoR - ws*xoI
		wXoI := wc*xoI + ws*xoR
		xr := xeR + wXoI
		xi := xeI - wXoR
		power[k] = xr*xr + xi*xi
	}
}

func complexFFTHalf32(real, imag []float32) {
	n := svHalfFFT
	for i := 1; i < n; i++ {
		j := svRFFTBitRev[i]
		if i < j {
			real[i], real[j] = real[j], real[i]
			imag[i], imag[j] = imag[j], imag[i]
		}
	}
	stage := 0
	for size := 2; size <= n; size <<= 1 {
		halfSize := size >> 1
		tw := svRFFTTwiddles[stage]
		for i := 0; i < n; i += size {
			for k := 0; k < halfSize; k++ {
				cos := tw.cos[k]
				sin := tw.sin[k]
				idx1 := i + k
				idx2 := i + k + halfSize
				tReal := real[idx2]*cos - imag[idx2]*sin
				tImag := real[idx2]*sin + imag[idx2]*cos
				real[idx2] = real[idx1] - tReal
				imag[idx2] = imag[idx1] - tImag
				real[idx1] += tReal
				imag[idx1] += tImag
			}
		}
		stage++
	}
}

// rfftPower64: float64 reference used by tests (same algorithm as float32 path).
func rfftPower64(realIn, imagScratch, power []float64) {
	half := svHalfFFT
	re := make([]float64, half)
	im := make([]float64, half)
	for k := 0; k < half; k++ {
		re[k] = realIn[2*k]
		im[k] = realIn[2*k+1]
	}
	// complex FFT half in float64 using full tables' subset via generic stages
	// bit-rev
	for i := 1; i < half; i++ {
		j := svRFFTBitRev[i]
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	stage := 0
	for size := 2; size <= half; size <<= 1 {
		halfSize := size >> 1
		// use float64 angles (same as init)
		angleStep := -2.0 * math.Pi / float64(size)
		for i := 0; i < half; i += size {
			for k := 0; k < halfSize; k++ {
				angle := angleStep * float64(k)
				cos := math.Cos(angle)
				sin := math.Sin(angle)
				idx1 := i + k
				idx2 := i + k + halfSize
				tReal := re[idx2]*cos - im[idx2]*sin
				tImag := re[idx2]*sin + im[idx2]*cos
				re[idx2] = re[idx1] - tReal
				im[idx2] = im[idx1] - tImag
				re[idx1] += tReal
				im[idx1] += tImag
			}
		}
		stage++
	}
	_ = stage
	t0 := re[0] + im[0]
	power[0] = t0 * t0
	for k := 1; k < half; k++ {
		zr, zi := re[k], im[k]
		znr, zni := re[half-k], im[half-k]
		xeR := 0.5 * (zr + znr)
		xeI := 0.5 * (zi - zni)
		xoR := 0.5 * (zr - znr)
		xoI := 0.5 * (zi + zni)
		angle := -2.0 * math.Pi * float64(k) / float64(svFFTSize)
		wc, ws := math.Cos(angle), math.Sin(angle)
		wXoR := wc*xoR - ws*xoI
		wXoI := wc*xoI + ws*xoR
		xr := xeR + wXoI
		xi := xeI - wXoR
		power[k] = xr*xr + xi*xi
	}
	_ = imagScratch
}

func svBuildKaldiMelFilterbank() []float32 {
	nBins := svNBins
	const melLowFreq = 31.748642
	const melFreqDelta = 34.6702385
	fftBinWidth := float64(svSampleRate) / float64(svFFTSize)

	weights := make([]float32, svNumMels*nBins)
	for i := 0; i < svNumMels; i++ {
		leftMel := melLowFreq + float64(i)*melFreqDelta
		centerMel := melLowFreq + float64(i+1)*melFreqDelta
		rightMel := melLowFreq + float64(i+2)*melFreqDelta
		for j := 0; j < nBins; j++ {
			freqHz := fftBinWidth * float64(j)
			melNum := 1127.0 * math.Log(1.0+freqHz/700.0)
			upSlope := (melNum - leftMel) / (centerMel - leftMel)
			downSlope := (rightMel - melNum) / (rightMel - centerMel)
			filterVal := math.Max(0.0, math.Min(upSlope, downSlope))
			weights[i*nBins+j] = float32(filterVal)
		}
	}
	return weights
}

func svApplyLFR(fbank []float32, numFrames int) ([]float32, int) {
	lfrFrames := (numFrames - svLFRm) / svLFRn + 1
	if lfrFrames <= 0 {
		lfrFrames = 1
	}
	out := make([]float32, lfrFrames*svFeatsDim)
	n := svApplyLFRInto(fbank, numFrames, out)
	return out[:n*svFeatsDim], n
}

func svApplyLFRInto(fbank []float32, numFrames int, out []float32) int {
	lfrFrames := (numFrames - svLFRm) / svLFRn + 1
	if lfrFrames <= 0 {
		clear(out[:svFeatsDim])
		for i := 0; i < svLFRm && i < numFrames; i++ {
			copy(out[i*svNumMels:(i+1)*svNumMels], fbank[i*svNumMels:(i+1)*svNumMels])
		}
		return 1
	}
	for f := 0; f < lfrFrames; f++ {
		srcStart := f * svLFRn
		for s := 0; s < svLFRm; s++ {
			srcFrame := srcStart + s
			if srcFrame >= numFrames {
				srcFrame = numFrames - 1
			}
			dstOff := f*svFeatsDim + s*svNumMels
			srcOff := srcFrame * svNumMels
			copy(out[dstOff:dstOff+svNumMels], fbank[srcOff:srcOff+svNumMels])
		}
	}
	return lfrFrames
}

func svApplyCMVN(x []float32, numFrames, dim int, means, istd []float32) {
	for f := 0; f < numFrames; f++ {
		off := f * dim
		row := x[off : off+dim]
		i := 0
		for ; i+3 < dim; i += 4 {
			row[i] = (row[i] - means[i]) * istd[i]
			row[i+1] = (row[i+1] - means[i+1]) * istd[i+1]
			row[i+2] = (row[i+2] - means[i+2]) * istd[i+2]
			row[i+3] = (row[i+3] - means[i+3]) * istd[i+3]
		}
		for ; i < dim; i++ {
			row[i] = (row[i] - means[i]) * istd[i]
		}
	}
}

// fftInPlacePrecomp: float64 full 512-pt FFT (tests).
func fftInPlacePrecomp(real, imag []float64) {
	n := svFFTSize
	for i := 1; i < n; i++ {
		j := svFFTBitRev[i]
		if i < j {
			real[i], real[j] = real[j], real[i]
			imag[i], imag[j] = imag[j], imag[i]
		}
	}
	stage := 0
	for size := 2; size <= n; size <<= 1 {
		halfSize := size >> 1
		tw := svFFTTwiddles[stage]
		for i := 0; i < n; i += size {
			for k := 0; k < halfSize; k++ {
				cos := tw.cos[k]
				sin := tw.sin[k]
				idx1 := i + k
				idx2 := i + k + halfSize
				tReal := real[idx2]*cos - imag[idx2]*sin
				tImag := real[idx2]*sin + imag[idx2]*cos
				real[idx2] = real[idx1] - tReal
				imag[idx2] = imag[idx1] - tImag
				real[idx1] += tReal
				imag[idx1] += tImag
			}
		}
		stage++
	}
}

// fftInPlace: generic float64 FFT (tests).
func fftInPlace(real, imag []float64, n int) {
	if n == svFFTSize && len(svFFTTwiddles) > 0 {
		fftInPlacePrecomp(real, imag)
		return
	}
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			real[i], real[j] = real[j], real[i]
			imag[i], imag[j] = imag[j], imag[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		halfSize := size >> 1
		angleStep := -2.0 * math.Pi / float64(size)
		for i := 0; i < n; i += size {
			for k := 0; k < halfSize; k++ {
				angle := angleStep * float64(k)
				cos := math.Cos(angle)
				sin := math.Sin(angle)
				idx1 := i + k
				idx2 := i + k + halfSize
				tReal := real[idx2]*cos - imag[idx2]*sin
				tImag := real[idx2]*sin + imag[idx2]*cos
				real[idx2] = real[idx1] - tReal
				imag[idx2] = imag[idx1] - tImag
				real[idx1] += tReal
				imag[idx1] += tImag
			}
		}
	}
}
