package onnxrt

import (
	"math"
	"testing"
)

// TestGeluErfFastBitExact verifies the fused AVX2 GELU kernel (where
// available) is bit-identical to the portable vek32-op pipeline, including
// hostile inputs (±Inf, NaN, -0, huge/tiny magnitudes) and unaligned tails.
func TestGeluErfFastBitExact(t *testing.T) {
	if geluErfFast(make([]float32, 64), make([]float32, 64)) == 0 {
		t.Skip("no fused gelu kernel on this platform")
	}
	sizes := []int{8, 16, 24, 25, 31, 33, 100, 1000, 4099}
	for _, n := range sizes {
		src := make([]float32, n)
		var state uint32 = 12345 + uint32(n)
		for i := range src {
			state = state*1664525 + 1013904223
			src[i] = float32(state>>8)/float32(1<<24)*40 - 20 // [-20, 20]
		}
		// hostile values
		hostile := []float32{
			float32(math.Inf(1)), float32(math.Inf(-1)),
			float32(math.NaN()), 0, float32(math.Copysign(0, -1)),
			1e-38, -1e-38, 1e38, -1e38, 88.0, -88.0, 9.0, -9.0,
		}
		for i, v := range hostile {
			if i < n {
				src[i] = v
			}
		}
		got := make([]float32, n)
		want := make([]float32, n)
		geluErfInto(got, src)
		geluErfIntoVek(want, src)
		for i := range got {
			g, w := got[i], want[i]
			if math.IsNaN(float64(g)) && math.IsNaN(float64(w)) {
				continue
			}
			if math.Float32bits(g) != math.Float32bits(w) {
				t.Fatalf("n=%d i=%d src=%v: got %v (0x%08x), want %v (0x%08x)",
					n, i, src[i], g, math.Float32bits(g), w, math.Float32bits(w))
			}
		}
		// in-place must match too
		inplace := append([]float32(nil), src...)
		geluErfInto(inplace, inplace)
		for i := range inplace {
			if math.IsNaN(float64(inplace[i])) && math.IsNaN(float64(want[i])) {
				continue
			}
			if math.Float32bits(inplace[i]) != math.Float32bits(want[i]) {
				t.Fatalf("in-place n=%d i=%d: got %v, want %v", n, i, inplace[i], want[i])
			}
		}
	}
}
