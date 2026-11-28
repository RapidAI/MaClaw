package kokoro

import (
	"math"
	"testing"
)

// oracleHarmonics is the pre-grid harmonic bank: one float64 upsample per
// harmonic, then sin, then a channel-major store.
func oracleHarmonics(f0 []float32, sampleRate, upsampleScale, harmonicNum int, amp float32) []float32 {
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

func TestSynthHarmonicsMatchesOracle(t *testing.T) {
	cases := [][]float32{
		{0},
		{10},
		{10.1},
		{-40, 80},
		{0, 5, 12, 100, 220, 40, 8},
	}
	for _, scale := range []int{4, 300} {
		for _, f0 := range cases {
			if scale == 300 && len(f0) > 2 {
				f0 = f0[:2]
			}
			got := synthHarmonicsKokoro(f0, 24000, scale, 8, 0.1)
			want := oracleHarmonics(f0, 24000, scale, 8, 0.1)
			if len(got) != len(want) {
				t.Fatalf("len %d vs %d scale %d", len(got), len(want), scale)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("scale %d sample %d got %g want %g f0 %v", scale, i, got[i], want[i], f0)
				}
			}
		}
	}
}

func TestMixSourceHarmonicsMatchesDot(t *testing.T) {
	f0 := []float32{0, 8, 10, 12, -20, 90, 180, 40}
	const scale = 4
	const amp = float32(0.1)
	const bias = float32(-0.25)
	w := []float32{0.2, -0.4, 0.7, 0.1, -1, 0.05, 0.3, -0.2, 0.8}
	harm := synthHarmonicsKokoro(f0, 24000, scale, 8, amp)
	outLen := len(f0) * scale
	want := make([]float32, outLen)
	for t := 0; t < outLen; t++ {
		sum := bias
		for h := 0; h < 9; h++ {
			sum += harm[t*9+h] * w[h]
		}
		want[t] = sum
	}
	got := make([]float32, outLen)
	for i := range got {
		got[i] = 42
	}
	mixSourceHarmonics(got, f0, 24000, scale, 8, amp, w, bias)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mix[%d]=%g want %g", i, got[i], want[i])
		}
	}
	silent := []float32{0, 3, 10, -2}
	quiet := make([]float32, len(silent)*scale)
	for i := range quiet {
		quiet[i] = 7
	}
	mixSourceHarmonics(quiet, silent, 16000, scale, 8, amp, w, bias)
	for i, v := range quiet {
		if v != bias {
			t.Fatalf("unvoiced[%d]=%g bias %g", i, v, bias)
		}
	}
}
