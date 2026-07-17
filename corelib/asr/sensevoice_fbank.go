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
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

const (
	svSampleRate = 16000
	svNumMels    = 80
	svWindowSize = 400           // 25ms at 16kHz
	svHopSize    = 160           // 10ms at 16kHz
	svFFTSize    = 512           // next power of 2 >= windowSize
	svLFRm       = 7             // stack 7 frames
	svLFRn       = 6             // skip 6 frames
	svFeatsDim   = 560           // svNumMels * svLFRm = 80 * 7
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
	numFrames := (nSamples-svWindowSize)/svHopSize + 1
	out := make([]float32, numFrames*svNumMels)
	if !svMelFilterbankInto(pcm, out) {
		return nil
	}
	return out
}

// SpeakerFbank returns the 80-bin, 10 ms Kaldi-style log-mel features used by
// the local speaker-diarization models.  It deliberately does not apply the
// SenseVoice LFR or CMVN stages: speaker encoders consume one 80-bin vector per
// frame and perform their own utterance-level normalization.
//
// The returned layout is row-major [frames][80].  PCM must be 16 kHz mono.
func SpeakerFbank(pcm []float32) []float32 {
	const (
		window = 400
		hop    = 160
		fft    = 512
		mels   = 80
	)
	if len(pcm) < window {
		return nil
	}
	// Reuse the precomputed real-FFT tables shared by the SenseVoice frontend.
	svInitFbankTables()
	frames := (len(pcm)-window)/hop + 1
	filters := speakerMelFilters()
	out := make([]float32, frames*mels)
	timeBuf := make([]float32, fft)
	re := make([]float32, fft/2)
	im := make([]float32, fft/2)
	power := make([]float32, fft/2)
	for frame := 0; frame < frames; frame++ {
		off := frame * hop
		var mean float64
		for i := 0; i < window; i++ {
			mean += float64(pcm[off+i])
		}
		mean /= window
		previous := float64(pcm[off]) - mean
		timeBuf[0] = float32(previous * 0.03 * speakerPoveyWindow(0, window))
		for i := 1; i < window; i++ {
			current := float64(pcm[off+i]) - mean
			timeBuf[i] = float32((current - 0.97*previous) * speakerPoveyWindow(i, window))
			previous = current
		}
		for i := window; i < fft; i++ {
			timeBuf[i] = 0
		}
		rfftPower32(timeBuf, re, im, power)
		for mel := 0; mel < mels; mel++ {
			var energy float64
			for bin := 0; bin < fft/2; bin++ {
				energy += float64(power[bin]) * filters[mel*(fft/2)+bin]
			}
			if energy < 1e-10 {
				energy = 1e-10
			}
			out[frame*mels+mel] = float32(math.Log(energy))
		}
	}
	return out
}

// speakerMelFilters implements the defaults of torchaudio.compliance.kaldi
// fbank used by FunASR CAM++: 20 Hz low cutoff, 80 mel bins and a 512-point
// FFT. It is deliberately separate from SenseVoice's fixed filterbank.
func speakerMelFilters() []float64 {
	const fft, mels = 512, 80
	const low, high = 20.0, 8000.0
	toMel := func(hz float64) float64 { return 1127 * math.Log(1+hz/700) }
	fromMel := func(mel float64) float64 { return 700 * (math.Exp(mel/1127) - 1) }
	points := make([]float64, mels+2)
	for i := range points {
		points[i] = fromMel(toMel(low) + (toMel(high)-toMel(low))*float64(i)/float64(mels+1))
	}
	weights := make([]float64, mels*(fft/2))
	for m := 0; m < mels; m++ {
		for b := 0; b < fft/2; b++ {
			f := float64(b) * 16000 / fft
			up := (f - points[m]) / (points[m+1] - points[m])
			down := (points[m+2] - f) / (points[m+2] - points[m+1])
			weights[m*(fft/2)+b] = math.Max(0, math.Min(up, down))
		}
	}
	return weights
}

func speakerPoveyWindow(i, size int) float64 {
	// Kaldi's Povey window is a Hamming window raised to 0.85.
	hamming := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(size-1))
	return math.Pow(hamming, 0.85)
}

// svMelFilterbankInto writes fbank into out (must have capacity numFrames*svNumMels).
func svMelFilterbankInto(pcm, out []float32) bool {
	nSamples := len(pcm)
	if nSamples < svWindowSize {
		return false
	}
	numFrames := (nSamples-svWindowSize)/svHopSize + 1
	if len(out) < numFrames*svNumMels {
		return false
	}
	svInitFbankTables()

	if numFrames < 48 {
		svMelFilterbankRange(pcm, out, 0, numFrames)
		return true
	}
	// Reuse tensor matmul worker pool (no per-call goroutine spawn).
	tensor.ParallelRanges(numFrames, func(fs, fe int) {
		svMelFilterbankRange(pcm, out, fs, fe)
	})
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

		// Mean (DC) — 8-wide accumulate
		var sum float32
		i := 0
		for ; i+7 < svWindowSize; i += 8 {
			sum += frame[i] + frame[i+1] + frame[i+2] + frame[i+3] +
				frame[i+4] + frame[i+5] + frame[i+6] + frame[i+7]
		}
		for ; i < svWindowSize; i++ {
			sum += frame[i]
		}
		mean := sum / float32(svWindowSize)

		// DC-remove + preemph + Hamming → time[0:window]; zero-pad.
		// Dual-step: break preemph dep chain across even/odd for better ILP.
		r0 := frame[0] - mean
		time[0] = r0 * (1.0 - preemph) * window[0]
		prev := r0
		i = 1
		for ; i+1 < svWindowSize; i += 2 {
			c0 := frame[i] - mean
			c1 := frame[i+1] - mean
			time[i] = (c0 - preemph*prev) * window[i]
			time[i+1] = (c1 - preemph*c0) * window[i+1]
			prev = c1
		}
		for ; i < svWindowSize; i++ {
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
			w := band.w
			ks := band.start
			var melEnergy float32
			j := 0
			// Sparse bands are typically short; manual unroll beats generic Dot call overhead.
			for ; j+7 < len(w); j += 8 {
				melEnergy += power[ks+j]*w[j] + power[ks+j+1]*w[j+1] +
					power[ks+j+2]*w[j+2] + power[ks+j+3]*w[j+3] +
					power[ks+j+4]*w[j+4] + power[ks+j+5]*w[j+5] +
					power[ks+j+6]*w[j+6] + power[ks+j+7]*w[j+7]
			}
			for ; j+3 < len(w); j += 4 {
				melEnergy += power[ks+j]*w[j] + power[ks+j+1]*w[j+1] +
					power[ks+j+2]*w[j+2] + power[ks+j+3]*w[j+3]
			}
			for ; j < len(w); j++ {
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

	// Pack z[k] = time[2k] + j*time[2k+1] (8-wide)
	for k := 0; k+7 < half; k += 8 {
		re[k] = time[2*k]
		im[k] = time[2*k+1]
		re[k+1] = time[2*k+2]
		im[k+1] = time[2*k+3]
		re[k+2] = time[2*k+4]
		im[k+2] = time[2*k+5]
		re[k+3] = time[2*k+6]
		im[k+3] = time[2*k+7]
		re[k+4] = time[2*k+8]
		im[k+4] = time[2*k+9]
		re[k+5] = time[2*k+10]
		im[k+5] = time[2*k+11]
		re[k+6] = time[2*k+12]
		im[k+6] = time[2*k+13]
		re[k+7] = time[2*k+14]
		im[k+7] = time[2*k+15]
	}

	complexFFTHalf32(re, im)

	// DC: X[0] = Re(Z[0]) + Im(Z[0])
	t0 := re[0] + im[0]
	power[0] = t0 * t0

	// Dual-k post-combine (independent bins; better ILP)
	k := 1
	for ; k+1 < half; k += 2 {
		zr0, zi0 := re[k], im[k]
		znr0, zni0 := re[half-k], im[half-k]
		zr1, zi1 := re[k+1], im[k+1]
		znr1, zni1 := re[half-k-1], im[half-k-1]
		xeR0 := float32(0.5) * (zr0 + znr0)
		xeI0 := float32(0.5) * (zi0 - zni0)
		xoR0 := float32(0.5) * (zr0 - znr0)
		xoI0 := float32(0.5) * (zi0 + zni0)
		xeR1 := float32(0.5) * (zr1 + znr1)
		xeI1 := float32(0.5) * (zi1 - zni1)
		xoR1 := float32(0.5) * (zr1 - znr1)
		xoI1 := float32(0.5) * (zi1 + zni1)
		wc0, ws0 := svRFFTPostCos[k], svRFFTPostSin[k]
		wc1, ws1 := svRFFTPostCos[k+1], svRFFTPostSin[k+1]
		wXoR0 := wc0*xoR0 - ws0*xoI0
		wXoI0 := wc0*xoI0 + ws0*xoR0
		wXoR1 := wc1*xoR1 - ws1*xoI1
		wXoI1 := wc1*xoI1 + ws1*xoR1
		xr0 := xeR0 + wXoI0
		xi0 := xeI0 - wXoR0
		xr1 := xeR1 + wXoI1
		xi1 := xeI1 - wXoR1
		power[k] = xr0*xr0 + xi0*xi0
		power[k+1] = xr1*xr1 + xi1*xi1
	}
	for ; k < half; k++ {
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
	n := svHalfFFT // 256 fixed
	for i := 1; i < n; i++ {
		j := svRFFTBitRev[i]
		if i < j {
			real[i], real[j] = real[j], real[i]
			imag[i], imag[j] = imag[j], imag[i]
		}
	}
	// Stage 0: size=2, cos=1, sin=0 — pure add/sub butterflies
	for i := 0; i < n; i += 2 {
		tr, ti := real[i+1], imag[i+1]
		real[i+1] = real[i] - tr
		imag[i+1] = imag[i] - ti
		real[i] += tr
		imag[i] += ti
	}
	// Remaining stages size=4..256
	stage := 1
	for size := 4; size <= n; size <<= 1 {
		halfSize := size >> 1
		tw := svRFFTTwiddles[stage]
		cosT, sinT := tw.cos, tw.sin
		for i := 0; i < n; i += size {
			// k=0: cos=1, sin=0
			idx1, idx2 := i, i+halfSize
			tr, ti := real[idx2], imag[idx2]
			real[idx2] = real[idx1] - tr
			imag[idx2] = imag[idx1] - ti
			real[idx1] += tr
			imag[idx1] += ti
			// Octet-k then quad-k butterflies when halfSize is large (ILP).
			k := 1
			if halfSize >= 16 {
				for ; k+7 < halfSize; k += 8 {
					c0, s0 := cosT[k], sinT[k]
					c1, s1 := cosT[k+1], sinT[k+1]
					c2, s2 := cosT[k+2], sinT[k+2]
					c3, s3 := cosT[k+3], sinT[k+3]
					c4, s4 := cosT[k+4], sinT[k+4]
					c5, s5 := cosT[k+5], sinT[k+5]
					c6, s6 := cosT[k+6], sinT[k+6]
					c7, s7 := cosT[k+7], sinT[k+7]
					j1 := i + k
					hs := halfSize
					// Upper leg loads first (j1+hs..) then lower (j1..) — more ILP.
					rU0, iU0 := real[j1+hs], imag[j1+hs]
					rU1, iU1 := real[j1+1+hs], imag[j1+1+hs]
					rU2, iU2 := real[j1+2+hs], imag[j1+2+hs]
					rU3, iU3 := real[j1+3+hs], imag[j1+3+hs]
					rU4, iU4 := real[j1+4+hs], imag[j1+4+hs]
					rU5, iU5 := real[j1+5+hs], imag[j1+5+hs]
					rU6, iU6 := real[j1+6+hs], imag[j1+6+hs]
					rU7, iU7 := real[j1+7+hs], imag[j1+7+hs]
					tR0 := rU0*c0 - iU0*s0
					tI0 := rU0*s0 + iU0*c0
					tR1 := rU1*c1 - iU1*s1
					tI1 := rU1*s1 + iU1*c1
					tR2 := rU2*c2 - iU2*s2
					tI2 := rU2*s2 + iU2*c2
					tR3 := rU3*c3 - iU3*s3
					tI3 := rU3*s3 + iU3*c3
					tR4 := rU4*c4 - iU4*s4
					tI4 := rU4*s4 + iU4*c4
					tR5 := rU5*c5 - iU5*s5
					tI5 := rU5*s5 + iU5*c5
					tR6 := rU6*c6 - iU6*s6
					tI6 := rU6*s6 + iU6*c6
					tR7 := rU7*c7 - iU7*s7
					tI7 := rU7*s7 + iU7*c7
					rL0, iL0 := real[j1], imag[j1]
					rL1, iL1 := real[j1+1], imag[j1+1]
					rL2, iL2 := real[j1+2], imag[j1+2]
					rL3, iL3 := real[j1+3], imag[j1+3]
					rL4, iL4 := real[j1+4], imag[j1+4]
					rL5, iL5 := real[j1+5], imag[j1+5]
					rL6, iL6 := real[j1+6], imag[j1+6]
					rL7, iL7 := real[j1+7], imag[j1+7]
					real[j1+hs], imag[j1+hs] = rL0-tR0, iL0-tI0
					real[j1], imag[j1] = rL0+tR0, iL0+tI0
					real[j1+1+hs], imag[j1+1+hs] = rL1-tR1, iL1-tI1
					real[j1+1], imag[j1+1] = rL1+tR1, iL1+tI1
					real[j1+2+hs], imag[j1+2+hs] = rL2-tR2, iL2-tI2
					real[j1+2], imag[j1+2] = rL2+tR2, iL2+tI2
					real[j1+3+hs], imag[j1+3+hs] = rL3-tR3, iL3-tI3
					real[j1+3], imag[j1+3] = rL3+tR3, iL3+tI3
					real[j1+4+hs], imag[j1+4+hs] = rL4-tR4, iL4-tI4
					real[j1+4], imag[j1+4] = rL4+tR4, iL4+tI4
					real[j1+5+hs], imag[j1+5+hs] = rL5-tR5, iL5-tI5
					real[j1+5], imag[j1+5] = rL5+tR5, iL5+tI5
					real[j1+6+hs], imag[j1+6+hs] = rL6-tR6, iL6-tI6
					real[j1+6], imag[j1+6] = rL6+tR6, iL6+tI6
					real[j1+7+hs], imag[j1+7+hs] = rL7-tR7, iL7-tI7
					real[j1+7], imag[j1+7] = rL7+tR7, iL7+tI7
				}
			}
			if halfSize >= 8 {
				for ; k+3 < halfSize; k += 4 {
					c0, s0 := cosT[k], sinT[k]
					c1, s1 := cosT[k+1], sinT[k+1]
					c2, s2 := cosT[k+2], sinT[k+2]
					c3, s3 := cosT[k+3], sinT[k+3]
					j1 := i + k
					j2 := j1 + halfSize
					j3, j4 := j1+1, j1+1+halfSize
					j5, j6 := j1+2, j1+2+halfSize
					j7, j8 := j1+3, j1+3+halfSize
					r2, i2 := real[j2], imag[j2]
					r4, i4 := real[j4], imag[j4]
					r6, i6 := real[j6], imag[j6]
					r8, i8 := real[j8], imag[j8]
					tR0 := r2*c0 - i2*s0
					tI0 := r2*s0 + i2*c0
					tR1 := r4*c1 - i4*s1
					tI1 := r4*s1 + i4*c1
					tR2 := r6*c2 - i6*s2
					tI2 := r6*s2 + i6*c2
					tR3 := r8*c3 - i8*s3
					tI3 := r8*s3 + i8*c3
					r1, i1 := real[j1], imag[j1]
					r3, i3 := real[j3], imag[j3]
					r5, i5 := real[j5], imag[j5]
					r7, i7 := real[j7], imag[j7]
					real[j2], imag[j2] = r1-tR0, i1-tI0
					real[j1], imag[j1] = r1+tR0, i1+tI0
					real[j4], imag[j4] = r3-tR1, i3-tI1
					real[j3], imag[j3] = r3+tR1, i3+tI1
					real[j6], imag[j6] = r5-tR2, i5-tI2
					real[j5], imag[j5] = r5+tR2, i5+tI2
					real[j8], imag[j8] = r7-tR3, i7-tI3
					real[j7], imag[j7] = r7+tR3, i7+tI3
				}
			}
			for ; k+1 < halfSize; k += 2 {
				c0, s0 := cosT[k], sinT[k]
				c1, s1 := cosT[k+1], sinT[k+1]
				j1 := i + k
				j2 := j1 + halfSize
				j3 := j1 + 1
				j4 := j3 + halfSize
				r2, i2 := real[j2], imag[j2]
				r4, i4 := real[j4], imag[j4]
				tR0 := r2*c0 - i2*s0
				tI0 := r2*s0 + i2*c0
				tR1 := r4*c1 - i4*s1
				tI1 := r4*s1 + i4*c1
				r1, i1 := real[j1], imag[j1]
				r3, i3 := real[j3], imag[j3]
				real[j2] = r1 - tR0
				imag[j2] = i1 - tI0
				real[j1] = r1 + tR0
				imag[j1] = i1 + tI0
				real[j4] = r3 - tR1
				imag[j4] = i3 - tI1
				real[j3] = r3 + tR1
				imag[j3] = i3 + tI1
			}
			for ; k < halfSize; k++ {
				cos := cosT[k]
				sin := sinT[k]
				j1 := i + k
				j2 := j1 + halfSize
				tReal := real[j2]*cos - imag[j2]*sin
				tImag := real[j2]*sin + imag[j2]*cos
				real[j2] = real[j1] - tReal
				imag[j2] = imag[j1] - tImag
				real[j1] += tReal
				imag[j1] += tImag
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
	lfrFrames := (numFrames-svLFRm)/svLFRn + 1
	if lfrFrames <= 0 {
		lfrFrames = 1
	}
	out := make([]float32, lfrFrames*svFeatsDim)
	n := svApplyLFRInto(fbank, numFrames, out)
	return out[:n*svFeatsDim], n
}

func svApplyLFRInto(fbank []float32, numFrames int, out []float32) int {
	lfrFrames := (numFrames-svLFRm)/svLFRn + 1
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
			// 80 mels: unrolled copy (avoids generic copy call overhead)
			src := fbank[srcOff : srcOff+svNumMels]
			dst := out[dstOff : dstOff+svNumMels]
			j := 0
			for ; j+7 < svNumMels; j += 8 {
				dst[j] = src[j]
				dst[j+1] = src[j+1]
				dst[j+2] = src[j+2]
				dst[j+3] = src[j+3]
				dst[j+4] = src[j+4]
				dst[j+5] = src[j+5]
				dst[j+6] = src[j+6]
				dst[j+7] = src[j+7]
			}
			for ; j < svNumMels; j++ {
				dst[j] = src[j]
			}
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
