package yolo

import (
	"math/rand"
	"testing"
)

// TestDeinterleave2 checks the stride-2 deinterleave helper (VPERMT2PS path
// on AVX512, scalar otherwise).
func TestDeinterleave2(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for _, n := range []int{1, 7, 15, 16, 17, 33, 100, 256, 1000} {
		src := make([]float32, 2*n+64)
		for i := range src {
			src[i] = rng.Float32()*20 - 10
		}
		dst := make([]float32, n)
		deinterleave2Stride(dst, src, n, len(src))
		for j := 0; j < n; j++ {
			if dst[j] != src[2*j] {
				t.Fatalf("n=%d dst[%d] = %g, want %g", n, j, dst[j], src[2*j])
			}
		}
		// Tight srcLimit: the vector kernel must clamp its body and leave
		// the remainder to the scalar tail (regression: over-read fault).
		clear(dst)
		deinterleave2Stride(dst, src, n, 2*n-1)
		for j := 0; j < n; j++ {
			if dst[j] != src[2*j] {
				t.Fatalf("tight limit n=%d dst[%d] = %g, want %g", n, j, dst[j], src[2*j])
			}
		}
	}
}

// TestIm2colOptimized_MatchesReference re-validates im2colDirect against a
// straightforward scalar implementation, across strides and paddings.
func TestIm2colOptimized_MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for _, tc := range []struct {
		inC, inH, inW, kh, kw, stride, pad int
	}{
		{3, 32, 32, 3, 3, 2, 1}, {64, 20, 20, 3, 3, 1, 1}, {128, 13, 17, 3, 3, 1, 1},
		{16, 10, 10, 1, 1, 1, 0}, {8, 15, 9, 5, 5, 2, 2}, {4, 8, 8, 3, 3, 2, 0},
		{32, 40, 40, 3, 3, 2, 1}, {7, 6, 6, 3, 3, 1, 1},
	} {
		outH := (tc.inH+2*tc.pad-tc.kh)/tc.stride + 1
		outW := (tc.inW+2*tc.pad-tc.kw)/tc.stride + 1
		data := make([]float32, tc.inC*tc.inH*tc.inW)
		for i := range data {
			data[i] = rng.Float32()*4 - 2
		}
		got := make([]float32, tc.inC*tc.kh*tc.kw*outH*outW)
		im2colDirect(data, 0, tc.inH*tc.inW, tc.inC, tc.inH, tc.inW, tc.kh, tc.kw, tc.stride, tc.pad, outH, outW, got)
		// Reference: the original per-element walk.
		want := make([]float32, len(got))
		idx := 0
		for ic := 0; ic < tc.inC; ic++ {
			chanOff := ic * tc.inH * tc.inW
			for ky := 0; ky < tc.kh; ky++ {
				for kx := 0; kx < tc.kw; kx++ {
					for oh := 0; oh < outH; oh++ {
						ih := oh*tc.stride - tc.pad + ky
						for ow := 0; ow < outW; ow++ {
							iw := ow*tc.stride - tc.pad + kx
							if ih >= 0 && ih < tc.inH && iw >= 0 && iw < tc.inW {
								want[idx] = data[chanOff+ih*tc.inW+iw]
							}
							idx++
						}
					}
				}
			}
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("case %+v: mismatch at %d (got %g want %g)", tc, i, got[i], want[i])
			}
		}
	}
}
