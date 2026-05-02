package kokoro

import (
	"math"
	"sync"
)

type Complex64 struct{ Re, Im float32 }

type dftTables struct {
	cosFwd []float64
	sinFwd []float64
	cosInv []float64
	sinInv []float64
}

var dftTableCache sync.Map

func getDFTTables(nFFT int) *dftTables {
	if v, ok := dftTableCache.Load(nFFT); ok {
		return v.(*dftTables)
	}
	bins := nFFT/2 + 1
	t := &dftTables{
		cosFwd: make([]float64, bins*nFFT),
		sinFwd: make([]float64, bins*nFFT),
		cosInv: make([]float64, bins*nFFT),
		sinInv: make([]float64, bins*nFFT),
	}
	for k := 0; k < bins; k++ {
		for n := 0; n < nFFT; n++ {
			base := k*nFFT + n
			angle := 2 * math.Pi * float64(k*n) / float64(nFFT)
			t.cosInv[base] = math.Cos(angle)
			t.sinInv[base] = math.Sin(angle)
			t.cosFwd[base] = t.cosInv[base]
			t.sinFwd[base] = -t.sinInv[base]
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
	bins := nFFT/2 + 1
	out := make([]Complex64, bins)
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
	return out
}

func idftFrame(spec []Complex64, nFFT int) []float32 {
	out := make([]float32, nFFT)
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
	return out
}

func stftMagnitudePhase(x []float32, nFFT, hop int) ([]float32, []float32, int) {
	// Match torch.stft defaults used by Kokoro: center=True, pad_mode="reflect".
	pad := nFFT / 2
	x = reflectPad1D(x, pad, pad)
	if len(x) < nFFT {
		padded := make([]float32, nFFT)
		copy(padded, x)
		x = padded
	}
	frames := 1 + (len(x)-nFFT)/hop
	bins := nFFT/2 + 1
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	win := hannWindow(nFFT)
	buf := make([]float32, nFFT)
	for t := 0; t < frames; t++ {
		start := t * hop
		for i := 0; i < nFFT; i++ {
			buf[i] = x[start+i] * win[i]
		}
		spec := dftFrame(buf, nFFT)
		for b, c := range spec {
			mag[b*frames+t] = float32(math.Hypot(float64(c.Re), float64(c.Im)))
			phase[b*frames+t] = float32(math.Atan2(float64(c.Im), float64(c.Re)))
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
	for t := 0; t < frames; t++ {
		for b := 0; b < bins; b++ {
			m := mag[b*frames+t]
			p := phase[b*frames+t]
			spec[b] = Complex64{m * float32(math.Cos(float64(p))), m * float32(math.Sin(float64(p)))}
		}
		frame := idftFrame(spec, nFFT)
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
	// samples of padding from both sides.
	pad := nFFT / 2
	if len(out) > 2*pad {
		cropped := make([]float32, len(out)-2*pad)
		copy(cropped, out[pad:len(out)-pad])
		return cropped
	}
	return out
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
