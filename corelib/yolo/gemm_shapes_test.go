package yolo

import (
	"math/rand"
	"testing"
)

func TestGemmAttentionShapes(t *testing.T) {
	if !hasAVX2FMA {
		t.Skip()
	}
	for _, tc := range []struct{ M, K, N int }{
		{64, 64, 64}, {400, 64, 400}, {400, 400, 64}, {64, 64, 400}, {400, 64, 64},
	} {
		rng := rand.New(rand.NewSource(int64(tc.M + tc.K*3 + tc.N*7)))
		W := make([]float32, tc.M*tc.K)
		B := make([]float32, tc.K*tc.N)
		bias := make([]float32, tc.M)
		for i := range W {
			W[i] = rng.Float32()*2 - 1
		}
		for i := range B {
			B[i] = rng.Float32()*2 - 1
		}
		got := make([]float32, tc.M*tc.N)
		gemmConvAVX2(W, B, bias, got, tc.M, tc.K, tc.N, tc.N, false)
		want := refMatmul(W, B, bias, tc.M, tc.K, tc.N)
		if i, worst := closeEnough(got, want, 1e-4); i >= 0 {
			t.Errorf("M=%d K=%d N=%d: worst rel err %g at %d (got %g want %g)",
				tc.M, tc.K, tc.N, worst, i, got[i], want[i])
		} else {
			t.Logf("M=%d K=%d N=%d ok (worst %g)", tc.M, tc.K, tc.N, worst)
		}
	}
}
