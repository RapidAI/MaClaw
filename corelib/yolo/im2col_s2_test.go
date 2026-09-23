package yolo

import (
	"math/rand"
	"testing"
)

func TestIm2colS2P3_MatchesDirect(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for _, tc := range []struct{ C, H, W int }{
		{8, 40, 40}, {64, 80, 80}, {3, 641, 479}, {16, 20, 20}, {4, 33, 31},
	} {
		in := make([]float32, tc.C*tc.H*tc.W)
		for i := range in {
			in[i] = rng.Float32()*2 - 1
		}
		outH := (tc.H+2-3)/2 + 1
		outW := (tc.W+2-3)/2 + 1
		n := 9 * outH * outW
		a := make([]float32, tc.C*n)
		b := make([]float32, tc.C*n)
		im2colDirect(in, 0, tc.H*tc.W, tc.C, tc.H, tc.W, 3, 3, 2, 1, outH, outW, a)
		im2colS2P3(in, 0, tc.H*tc.W, tc.C, tc.H, tc.W, outH, outW, b)
		bad := 0
		for i := range a {
			if a[i] != b[i] {
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("C=%d H=%d W=%d: %d/%d mismatch", tc.C, tc.H, tc.W, bad, len(a))
		}
	}
}
