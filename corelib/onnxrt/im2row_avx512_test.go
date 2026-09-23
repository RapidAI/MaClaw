package onnxrt

import (
	"math/rand"
	"testing"
)

// TestIm2row3x3AVX512MatchesGeneric compares the vectorized 3x3 sW=1
// interior path against the reference per-pixel walk across shapes that
// exercise padded kernel rows, edge segments, and odd widths.
func TestIm2row3x3AVX512MatchesGeneric(t *testing.T) {
	if !hasAVX512ZMM {
		t.Skip("AVX-512 unavailable")
	}
	rng := rand.New(rand.NewSource(42))
	shapes := []struct{ H, W, Cg, padT, padL int }{
		{8, 10, 1, 1, 1},   // small, fully padded borders
		{16, 16, 4, 1, 1},  // classic det conv
		{12, 9, 2, 0, 0},   // no padding
		{9, 12, 3, 2, 1},   // asymmetric padding
		{7, 7, 8, 1, 1},    // tiny, interior < 8 px wide
		{20, 5, 2, 1, 1},   // narrow rows
		{5, 20, 1, 1, 1},   // narrow columns
		{33, 34, 4, 1, 1},  // large enough for several 4-px groups
		{10, 10, 5, 3, 3},  // pads wider than half the kernel
	}
	for _, sh := range shapes {
		H, W, Cg := sh.H, sh.W, sh.Cg
		C := Cg // single group: xG = 0
		// Same geometry as the det convs: oH = H+2p-2, oW = W+2p-2.
		p := &convParams{
			kH: 3, kW: 3,
			strides:   [2]int{1, 1},
			pads:      [4]int{sh.padT, sh.padL},
			dilations: [2]int{1, 1},
		}
		oH := H + 2*sh.padT - 3 + 1
		oW := W + 2*sh.padL - 3 + 1
		if oH < 1 || oW < 1 {
			continue
		}
		K := Cg * 9
		x := make([]float32, C*H*W)
		for i := range x {
			x[i] = rng.Float32()*2 - 1
		}
		pixs := oH * oW
		got := make([]float32, pixs*K)
		want := make([]float32, pixs*K)
		im2rowFast(got, x, 0, 0, pixs, oW, H, W, Cg, p)
		im2rowGeneric(want, x, 0, 0, pixs, oW, H, W, Cg, p)
		for i := range want {
			if got[i] != want[i] {
				pi := i / K
				col := i % K
				t.Fatalf("shape %dx%d Cg=%d pad=%d,%d: row %d col %d: got %v want %v",
					H, W, Cg, sh.padT, sh.padL, pi, col, got[i], want[i])
			}
		}
	}
}
