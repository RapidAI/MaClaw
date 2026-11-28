package kokoro

import (
	"math"
	"testing"
)

func TestDFTFrameIntoMatchesFresh(t *testing.T) {
	for _, nFFT := range []int{16, 20, 32} {
		x := make([]float32, nFFT)
		for i := range x {
			x[i] = float32(i%5) - 2
		}
		want := dftFrame(x, nFFT)
		got := make([]Complex64, len(want)+2)
		for i := range got {
			got[i] = Complex64{42, -42}
		}
		dftFrameInto(got, x, nFFT)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("nFFT=%d bin %d got %+v want %+v", nFFT, i, got[i], want[i])
			}
		}
		if got[len(want)] != (Complex64{42, -42}) || got[len(want)+1] != (Complex64{42, -42}) {
			t.Fatalf("nFFT=%d wrote past the bins", nFFT)
		}
		for i := range x {
			x[i] = 0
		}
		dftFrameInto(got, x, nFFT)
		for i := range want {
			if got[i] != (Complex64{}) {
				t.Fatalf("nFFT=%d zero bin %d = %+v", nFFT, i, got[i])
			}
		}
	}
}

func TestIDFTFrameIntoMatchesFresh(t *testing.T) {
	for _, nFFT := range []int{16, 20, 32} {
		bins := nFFT/2 + 1
		spec := make([]Complex64, bins)
		for i := range spec {
			spec[i] = Complex64{float32(i) * 0.1, float32(bins-i) * -0.05}
		}
		want := idftFrame(spec, nFFT)
		got := make([]float32, nFFT+3)
		for i := range got {
			got[i] = 99
		}
		idftFrameInto(got, spec, nFFT)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("nFFT=%d sample %d got %g want %g", nFFT, i, got[i], want[i])
			}
		}
		for _, v := range got[nFFT:] {
			if v != 99 {
				t.Fatalf("nFFT=%d wrote past nFFT: %g", nFFT, v)
			}
		}
		for i := range spec {
			spec[i] = Complex64{}
		}
		idftFrameInto(got, spec, nFFT)
		for i := 0; i < nFFT; i++ {
			if got[i] != 0 {
				t.Fatalf("nFFT=%d zero sample %d = %g", nFFT, i, got[i])
			}
		}
	}
}

func TestISTFTFastMathClose(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	const frames, bins, nFFT, hop = 180, 11, 20, 5
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	for t := 0; t < frames; t++ {
		for b := 0; b < bins; b++ {
			mag[b*frames+t] = 0.15 + 0.02*float32((b+t)%7)
			// Vocoder phase is already passed through sin, so it sits in
			// [-1, 1]. Odd frames use a wider angle to exercise quadrants.
			if t%2 == 0 {
				phase[b*frames+t] = float32(math.Sin(float64(b+t) * 0.17))
			} else {
				phase[b*frames+t] = float32(b-5) * 0.4
			}
		}
	}
	kokoroFastMath = false
	want := istft(mag, phase, frames, nFFT, hop)
	kokoroFastMath = true
	got := istft(mag, phase, frames, nFFT, hop)
	again := istft(mag, phase, frames, nFFT, hop)
	if len(got) != len(want) || len(again) != len(got) {
		t.Fatalf("len got %d again %d want %d", len(got), len(again), len(want))
	}
	var maxd float32
	for i := range want {
		if again[i] != got[i] {
			t.Fatalf("fast istft drifted at %d", i)
		}
		if got[i] != got[i] {
			t.Fatalf("non-finite fast istft at %d", i)
		}
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 5e-4 {
		t.Fatalf("istft fast maxAbs=%g", maxd)
	}
}

func TestDFT20FastMatchesFloat64(t *testing.T) {
	x := make([]float32, 20)
	for trial := 0; trial < 6; trial++ {
		var peak float32
		for i := range x {
			x[i] = float32((i*17+trial*5)%29) - 14
			if x[i] < 0 {
				if -x[i] > peak {
					peak = -x[i]
				}
			} else if x[i] > peak {
				peak = x[i]
			}
		}
		want := dftFrame(x, 20)
		got := make([]Complex64, len(want)+1)
		for i := range got {
			got[i] = Complex64{42, -42}
		}
		dft20Into(got, x)
		if got[len(want)] != (Complex64{42, -42}) {
			t.Fatal("dft20 wrote past the bins")
		}
		for i := range want {
			dr := got[i].Re - want[i].Re
			di := got[i].Im - want[i].Im
			if dr < 0 {
				dr = -dr
			}
			if di < 0 {
				di = -di
			}
			limit := float32(5e-4)
			scale := peak * 20
			if scale > 1 {
				limit = 2e-5 * scale
			}
			if dr > limit || di > limit {
				t.Fatalf("trial %d bin %d got %+v want %+v", trial, i, got[i], want[i])
			}
		}
	}
	for i := range x {
		x[i] = 0
	}
	got := make([]Complex64, 11)
	dft20Into(got, x)
	for i, c := range got {
		if c != (Complex64{}) {
			t.Fatalf("zero bin %d = %+v", i, c)
		}
	}
	spec := make([]Complex64, 11)
	for i := range spec {
		spec[i] = Complex64{float32(i) * 0.1, float32(10-i) * -0.05}
	}
	wantT := idftFrame(spec, 20)
	time := make([]float32, 23)
	for i := range time {
		time[i] = 99
	}
	idft20Into(time, spec)
	for i := range wantT {
		d := time[i] - wantT[i]
		if d < 0 {
			d = -d
		}
		if d > 2e-5 {
			t.Fatalf("idft20[%d]=%g want %g", i, time[i], wantT[i])
		}
	}
	for _, v := range time[20:] {
		if v != 99 {
			t.Fatalf("idft20 tail %g", v)
		}
	}
}

func TestReflectIndexMatchesPad(t *testing.T) {
	for _, n := range []int{1, 2, 7, 19, 20, 21, 64, 301} {
		x := make([]float32, n)
		for i := range x {
			x[i] = float32(i + 1)
		}
		padded := reflectPad1D(x, 10, 10)
		for i := range padded {
			got := x[reflectIndex(n, 10, i)]
			if got != padded[i] {
				t.Fatalf("n=%d i=%d got %g pad %g", n, i, got, padded[i])
			}
		}
	}
}

func paddedSTFTOracle(x []float32, nFFT, hop int) ([]float32, []float32, int) {
	pad := nFFT / 2
	sig := reflectPad1D(x, pad, pad)
	frames := 1 + (len(sig)-nFFT)/hop
	bins := nFFT/2 + 1
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	win := hannWindow(nFFT)
	buf := make([]float32, nFFT)
	spec := make([]Complex64, bins)
	for t := 0; t < frames; t++ {
		start := t * hop
		for i := 0; i < nFFT; i++ {
			buf[i] = sig[start+i] * win[i]
		}
		dftFrameInto(spec, buf, nFFT)
		for b := 0; b < bins; b++ {
			c := spec[b]
			mag[b*frames+t] = float32(math.Hypot(float64(c.Re), float64(c.Im)))
			phase[b*frames+t] = float32(math.Atan2(float64(c.Im), float64(c.Re)))
		}
	}
	return mag, phase, frames
}

func TestSTFTReflectMatchesPaddedOracle(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	kokoroFastMath = false
	for _, nFFT := range []int{16, 20} {
		hop := nFFT / 4
		for _, n := range []int{1, nFFT - 1, nFFT, nFFT + 3, 800} {
			x := make([]float32, n)
			for i := range x {
				x[i] = 0.01*float32(i%17) - 0.08
			}
			wantM, wantP, wantF := paddedSTFTOracle(x, nFFT, hop)
			gotM, gotP, gotF := stftMagnitudePhase(x, nFFT, hop)
			if gotF != wantF || len(gotM) != len(wantM) || len(gotP) != len(wantP) {
				t.Fatalf("nFFT=%d n=%d frames %d vs %d", nFFT, n, gotF, wantF)
			}
			for i := range wantM {
				if gotM[i] != wantM[i] || gotP[i] != wantP[i] {
					t.Fatalf("nFFT=%d n=%d bin %d mag %g/%g phase %g/%g", nFFT, n, i, gotM[i], wantM[i], gotP[i], wantP[i])
				}
			}
		}
	}
}

func TestSTFTFastMathClose(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	x := make([]float32, 800)
	for i := range x {
		x[i] = 0.8*float32(math.Sin(2*math.Pi*float64(i)/20)) + 0.15*float32(math.Sin(2*math.Pi*float64(i)/7))
	}
	kokoroFastMath = false
	mag0, ph0, frames := stftMagnitudePhase(x, 20, 5)
	kokoroFastMath = true
	mag1, ph1, frames1 := stftMagnitudePhase(x, 20, 5)
	again, againP, _ := stftMagnitudePhase(x, 20, 5)
	if frames != frames1 || len(mag0) != len(mag1) || len(again) != len(mag1) {
		t.Fatalf("frames %d vs %d", frames, frames1)
	}
	var maxMag, maxPhase float32
	const pi = float32(math.Pi)
	for i := range mag0 {
		if again[i] != mag1[i] || againP[i] != ph1[i] {
			t.Fatalf("fast stft drifted at %d", i)
		}
		dm := mag1[i] - mag0[i]
		if dm < 0 {
			dm = -dm
		}
		if dm > maxMag {
			maxMag = dm
		}
		d := ph1[i] - ph0[i]
		if d > pi {
			d -= 2 * pi
		} else if d < -pi {
			d += 2 * pi
		}
		if d < 0 {
			d = -d
		}
		if mag0[i] > 1e-3 && d > maxPhase {
			maxPhase = d
		}
	}
	if maxMag > 2e-3 || maxPhase > 2e-3 {
		t.Fatalf("stft fast mag=%g phase=%g", maxMag, maxPhase)
	}
}

func TestSTFTAndISTFTAllocsDoNotScaleWithFrames(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	kokoroFastMath = false

	stftAllocs := func(n int) float64 {
		x := make([]float32, n)
		for i := range x {
			x[i] = float32(i%9) * 0.01
		}
		_, _, _ = stftMagnitudePhase(x, 20, 5)
		return testing.AllocsPerRun(8, func() {
			_, _, _ = stftMagnitudePhase(x, 20, 5)
		})
	}
	smallST := stftAllocs(400)
	largeST := stftAllocs(20000)
	if largeST > smallST+8 {
		t.Fatalf("stft allocs 400=%v 20000=%v", smallST, largeST)
	}

	istftAllocs := func(frames int) float64 {
		const bins = 11
		mag := make([]float32, bins*frames)
		phase := make([]float32, bins*frames)
		for i := range mag {
			mag[i] = 0.3
			phase[i] = 0.2
		}
		_ = istft(mag, phase, frames, 20, 5)
		return testing.AllocsPerRun(8, func() {
			_ = istft(mag, phase, frames, 20, 5)
		})
	}
	smallI := istftAllocs(32)
	largeI := istftAllocs(3200)
	if largeI > smallI+8 {
		t.Fatalf("istft allocs 32=%v 3200=%v", smallI, largeI)
	}
	kokoroFastMath = true
	fastLarge := istftAllocs(3200)
	if fastLarge > largeI {
		t.Fatalf("fast istft allocs %v above the vek path %v", fastLarge, largeI)
	}
}
