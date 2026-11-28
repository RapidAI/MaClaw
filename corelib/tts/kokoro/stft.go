package kokoro

import (
	"github.com/viterin/vek/vek32"
	"math"
	"sync"
)

type Complex64 struct{ Re, Im float32 }

type dftTables struct {
	cosFwd   []float64
	sinFwd   []float64
	cosInv   []float64
	sinInv   []float64
	cosFwd32 []float32
	sinFwd32 []float32
	cosInv32 []float32
	sinInv32 []float32
}

var dftTableCache sync.Map

func getDFTTables(nFFT int) *dftTables {
	if v, ok := dftTableCache.Load(nFFT); ok {
		return v.(*dftTables)
	}
	bins := nFFT/2 + 1
	slots := bins * nFFT
	t := &dftTables{
		cosFwd:   make([]float64, slots),
		sinFwd:   make([]float64, slots),
		cosInv:   make([]float64, slots),
		sinInv:   make([]float64, slots),
		cosFwd32: make([]float32, slots),
		sinFwd32: make([]float32, slots),
		cosInv32: make([]float32, slots),
		sinInv32: make([]float32, slots),
	}
	for k := 0; k < bins; k++ {
		for n := 0; n < nFFT; n++ {
			base := k*nFFT + n
			angle := 2 * math.Pi * float64(k*n) / float64(nFFT)
			t.cosInv[base] = math.Cos(angle)
			t.sinInv[base] = math.Sin(angle)
			t.cosFwd[base] = t.cosInv[base]
			t.sinFwd[base] = -t.sinInv[base]
			t.cosInv32[base] = float32(t.cosInv[base])
			t.sinInv32[base] = float32(t.sinInv[base])
			t.cosFwd32[base] = float32(t.cosFwd[base])
			t.sinFwd32[base] = float32(t.sinFwd[base])
		}
	}
	actual, _ := dftTableCache.LoadOrStore(nFFT, t)
	return actual.(*dftTables)
}

func hannWindow(n int) []float32 {
	w := make([]float32, n)
	for i := 0; i < n; i++ {
		w[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n)))
	}
	return w
}

func dftFrame(x []float32, nFFT int) []Complex64 {
	out := make([]Complex64, nFFT/2+1)
	dftFrameInto(out, x, nFFT)
	return out
}

// dftFrameInto writes bins into out. The k-then-n float64 order matches
// dftFrame, so a reused buffer stays bit-identical with a fresh one.
func dftFrameInto(out []Complex64, x []float32, nFFT int) {
	bins := nFFT/2 + 1
	tables := getDFTTables(nFFT)
	for k := 0; k < bins; k++ {
		var re, im float64
		base := k * nFFT
		for n := 0; n < nFFT; n++ {
			xn := float64(x[n])
			re += xn * tables.cosFwd[base+n]
			im += xn * tables.sinFwd[base+n]
		}
		out[k] = Complex64{float32(re), float32(im)}
	}
}

func idftFrame(spec []Complex64, nFFT int) []float32 {
	out := make([]float32, nFFT)
	idftFrameInto(out, spec, nFFT)
	return out
}

// idftFrameInto writes nFFT samples into out. Hermitian bins are doubled
// in the same k order as idftFrame.
func idftFrameInto(out []float32, spec []Complex64, nFFT int) {
	bins := nFFT/2 + 1
	tables := getDFTTables(nFFT)
	for n := 0; n < nFFT; n++ {
		var sum float64
		for k := 0; k < bins; k++ {
			c := spec[k]
			base := k*nFFT + n
			term := float64(c.Re)*tables.cosInv[base] - float64(c.Im)*tables.sinInv[base]
			if k != 0 && k != bins-1 {
				term *= 2
			}
			sum += term
		}
		out[n] = float32(sum / float64(nFFT))
	}
}

// dft20Into is the nFFT=20 forward transform in float32. The k-then-n
// order matches dftFrameInto. kokoroFastMath uses it for the source STFT.
func dft20Into(out []Complex64, x []float32) {
	const nFFT = 20
	const bins = 11
	tables := getDFTTables(nFFT)
	for k := 0; k < bins; k++ {
		var re, im float32
		base := k * nFFT
		for n := 0; n < nFFT; n++ {
			xn := x[n]
			re += xn * tables.cosFwd32[base+n]
			im += xn * tables.sinFwd32[base+n]
		}
		out[k] = Complex64{re, im}
	}
}

// idft20Into is the matching inverse. Hermitian bins are doubled, then
// the sum is divided by 20, in the same k order as idftFrameInto.
func idft20Into(out []float32, spec []Complex64) {
	const nFFT = 20
	const bins = 11
	tables := getDFTTables(nFFT)
	for n := 0; n < nFFT; n++ {
		var sum float32
		for k := 0; k < bins; k++ {
			c := spec[k]
			base := k*nFFT + n
			term := c.Re*tables.cosInv32[base] - c.Im*tables.sinInv32[base]
			if k != 0 && k != bins-1 {
				term *= 2
			}
			sum += term
		}
		out[n] = sum / float32(nFFT)
	}
}

func stftMagnitudePhase(x []float32, nFFT, hop int) ([]float32, []float32, int) {
	// Match torch.stft defaults used by Kokoro: center=True, pad_mode="reflect".
	// The pad is read through reflectIndex so the source signal is not copied.
	pad := nFFT / 2
	n := len(x)
	paddedLen := n + 2*pad
	var padded []float32
	if paddedLen < nFFT {
		padded = reflectPad1D(x, pad, pad)
		if len(padded) < nFFT {
			buf := make([]float32, nFFT)
			copy(buf, padded)
			padded = buf
		}
		paddedLen = len(padded)
	}
	frames := 1 + (paddedLen-nFFT)/hop
	bins := nFFT/2 + 1
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	win := hannWindow(nFFT)
	buf := make([]float32, nFFT)
	spec := make([]Complex64, bins)
	// nFFT=20 is the vocoder source size. The float32 transform plus
	// fast hypot/atan2 run only under kokoroFastMath, so desktop libm
	// magnitude and phase stay bit-identical.
	fast20 := kokoroFastMath && nFFT == 20
	for t := 0; t < frames; t++ {
		start := t * hop
		if padded != nil {
			for i := 0; i < nFFT; i++ {
				buf[i] = padded[start+i] * win[i]
			}
		} else if start >= pad && start+nFFT <= pad+n {
			src := x[start-pad:]
			for i := 0; i < nFFT; i++ {
				buf[i] = src[i] * win[i]
			}
		} else {
			for i := 0; i < nFFT; i++ {
				buf[i] = x[reflectIndex(n, pad, start+i)] * win[i]
			}
		}
		if fast20 {
			dft20Into(spec, buf)
		} else {
			dftFrameInto(spec, buf, nFFT)
		}
		for b := 0; b < bins; b++ {
			c := spec[b]
			if kokoroFastMath {
				mag[b*frames+t] = fastHypot32(c.Re, c.Im)
				phase[b*frames+t] = fastAtan232(c.Im, c.Re)
			} else {
				mag[b*frames+t] = float32(math.Hypot(float64(c.Re), float64(c.Im)))
				phase[b*frames+t] = float32(math.Atan2(float64(c.Im), float64(c.Re)))
			}
		}
	}
	return mag, phase, frames
}

func istft(mag, phase []float32, frames, nFFT, hop int) []float32 {
	bins := nFFT/2 + 1
	outLen := (frames-1)*hop + nFFT
	out := make([]float32, outLen)
	den := make([]float32, outLen)
	win := hannWindow(nFFT)
	spec := make([]Complex64, bins)
	frame := make([]float32, nFFT)
	// Bulk sin/cos is the desktop path: vek stays bit-identical there.
	// kokoroFastMath (Android, or KOKORO_FAST_MATH=1) takes both values
	// from one reduction per bin and does not build those four buffers.
	var reFull, imFull []float32
	if !kokoroFastMath && useKokoroSIMD() && optRound2 && len(phase) >= 64 {
		cosBuf := make([]float32, len(phase))
		sinBuf := make([]float32, len(phase))
		vek32.Cos_Into(cosBuf, phase)
		vek32.Sin_Into(sinBuf, phase)
		reFull = make([]float32, len(phase))
		imFull = make([]float32, len(phase))
		vek32.Mul_Into(reFull, mag, cosBuf)
		vek32.Mul_Into(imFull, mag, sinBuf)
	}
	for t := 0; t < frames; t++ {
		if reFull != nil {
			for b := 0; b < bins; b++ {
				spec[b] = Complex64{reFull[b*frames+t], imFull[b*frames+t]}
			}
		} else if kokoroFastMath {
			for b := 0; b < bins; b++ {
				m := mag[b*frames+t]
				s, c := fastSinCos32(phase[b*frames+t])
				spec[b] = Complex64{m * c, m * s}
			}
		} else {
			for b := 0; b < bins; b++ {
				m := mag[b*frames+t]
				p := phase[b*frames+t]
				spec[b] = Complex64{m * float32(math.Cos(float64(p))), m * float32(math.Sin(float64(p)))}
			}
		}
		if kokoroFastMath && nFFT == 20 {
			idft20Into(frame, spec)
		} else {
			idftFrameInto(frame, spec, nFFT)
		}
		start := t * hop
		for i := 0; i < nFFT; i++ {
			w := win[i]
			out[start+i] += frame[i] * w
			den[start+i] += w * w
		}
	}
	for i := range out {
		if den[i] > 1e-8 {
			out[i] /= den[i]
		}
	}
	// Match torch.istft center=True with no explicit length: remove n_fft/2
	// samples of padding from both sides. The shift reuses out, so the
	// returned slice is shorter than its backing array.
	pad := nFFT / 2
	if len(out) > 2*pad {
		n := len(out) - 2*pad
		copy(out[:n], out[pad:pad+n])
		return out[:n]
	}
	return out
}

// reflectIndex maps a center-padded index back into x. left samples sit
// before x[0]. The reflection does not repeat the edge sample.
func reflectIndex(n, left, i int) int {
	if i < left {
		idx := left - i
		if idx >= n {
			idx = n - 1
		}
		if idx < 0 {
			idx = 0
		}
		return idx
	}
	if i < left+n {
		return i - left
	}
	idx := n - 2 - (i - (left + n))
	if idx < 0 {
		idx = 0
	}
	return idx
}

func synthSineSource(f0 []float32, sampleRate int, amp float32) []float32 {
	out := make([]float32, len(f0))
	phase := float64(0)
	for i, hz := range f0 {
		if hz > 10 {
			phase += 2 * math.Pi * float64(hz) / float64(sampleRate)
			out[i] = amp * float32(math.Sin(phase))
		}
	}
	return out
}

func reflectPad1D(x []float32, left, right int) []float32 {
	out := make([]float32, left+len(x)+right)
	for i := 0; i < left; i++ {
		idx := left - i
		if idx >= len(x) {
			idx = len(x) - 1
		}
		if idx < 0 {
			idx = 0
		}
		out[i] = x[idx]
	}
	copy(out[left:left+len(x)], x)
	for i := 0; i < right; i++ {
		idx := len(x) - 2 - i
		if idx < 0 {
			idx = 0
		}
		out[left+len(x)+i] = x[idx]
	}
	return out
}
