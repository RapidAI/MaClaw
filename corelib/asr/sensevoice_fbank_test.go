package asr

import (
	"math"
	"testing"
)

func TestRFFTPowerMatchesFullFFT(t *testing.T) {
	svInitFbankTables()
	x := make([]float64, svFFTSize)
	for i := range x {
		x[i] = math.Sin(2*math.Pi*float64(i)*7/float64(svFFTSize)) +
			0.3*math.Cos(2*math.Pi*float64(i)*13/float64(svFFTSize)) +
			0.01*float64(i%17)
	}

	// Full complex FFT reference (float64)
	refReal := make([]float64, svFFTSize)
	refImag := make([]float64, svFFTSize)
	copy(refReal, x)
	fftInPlacePrecomp(refReal, refImag)
	refPow := make([]float64, svNBins)
	for k := 0; k < svNBins; k++ {
		refPow[k] = refReal[k]*refReal[k] + refImag[k]*refImag[k]
	}

	// float64 rfft reference
	got64 := make([]float64, svNBins)
	rfftPower64(x, nil, got64)

	// float32 rfft (production path)
	time32 := make([]float32, svFFTSize)
	re32 := make([]float32, svHalfFFT)
	im32 := make([]float32, svHalfFFT)
	pow32 := make([]float32, svNBins)
	for i := range x {
		time32[i] = float32(x[i])
	}
	rfftPower32(time32, re32, im32, pow32)

	var maxRel64, maxRel32 float64
	for k := 0; k < svNBins; k++ {
		// float64 rfft vs full
		d64 := math.Abs(got64[k] - refPow[k])
		rel64 := d64 / (math.Abs(refPow[k]) + 1e-12)
		if rel64 > maxRel64 {
			maxRel64 = rel64
		}
		if rel64 > 1e-6 && d64 > 1e-6 {
			t.Fatalf("f64 rfft bin %d: got %g want %g", k, got64[k], refPow[k])
		}
		// float32 rfft vs full (looser tolerance)
		d32 := math.Abs(float64(pow32[k]) - refPow[k])
		rel32 := d32 / (math.Abs(refPow[k]) + 1e-6)
		if rel32 > maxRel32 {
			maxRel32 = rel32
		}
		if rel32 > 1e-3 && d32 > 1e-2 {
			t.Fatalf("f32 rfft bin %d: got %g want %g rel=%g", k, pow32[k], refPow[k], rel32)
		}
	}
	t.Logf("rfft maxRel f64=%g f32=%g", maxRel64, maxRel32)
}

func TestFbankDeterministic(t *testing.T) {
	n := svWindowSize + 10*svHopSize
	pcm := make([]float32, n)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / svSampleRate))
	}
	a := svMelFilterbank(pcm)
	b := svMelFilterbank(pcm)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-4 {
			t.Fatalf("mismatch at %d: %v vs %v", i, a[i], b[i])
		}
	}
}
