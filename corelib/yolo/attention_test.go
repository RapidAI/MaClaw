package yolo

import (
	"math/rand"
	"testing"
)

// TestAttentionFast_MatchesSlow validates the GEMM-based attention against
// the per-element reference on shapes spanning the fast-path guards.
func TestAttentionFast_MatchesSlow(t *testing.T) {
	if !hasAVX2FMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(1234))
	mk := func(halfC, heads, H, W int) *Attention {
		a := &Attention{
			QKV: &Conv2dBNSiLU{
				Weight: NewTensor(2*halfC, halfC, 1, 1), Bias: make([]float32, 2*halfC),
				OutC: 2 * halfC, InC: halfC, KH: 1, KW: 1,
				Stride: 1, Padding: 0, Groups: 1, UseSiLU: false,
			},
			Proj: &Conv2dBNSiLU{
				Weight: NewTensor(halfC, halfC, 1, 1), Bias: make([]float32, halfC),
				OutC: halfC, InC: halfC, KH: 1, KW: 1,
				Stride: 1, Padding: 0, Groups: 1, UseSiLU: false,
			},
			PE: &Conv2dBNSiLU{
				Weight: NewTensor(halfC, 1, 3, 3), Bias: make([]float32, halfC),
				OutC: halfC, InC: halfC, KH: 3, KW: 3,
				Stride: 1, Padding: 1, Groups: halfC, UseSiLU: false,
			},
			NumHeads: heads,
		}
		for _, c := range []*Conv2dBNSiLU{a.QKV, a.Proj, a.PE} {
			for i := range c.Weight.Data {
				c.Weight.Data[i] = rng.Float32()*1.6 - 0.8
			}
			for i := range c.Bias {
				c.Bias[i] = rng.Float32()*0.5 - 0.25
			}
		}
		return a
	}
	for _, tc := range []struct {
		halfC, heads, H, W int
	}{
		{64, 1, 4, 4}, {64, 2, 8, 8}, {128, 2, 8, 8}, {256, 4, 20, 20}, {64, 4, 5, 4},
		{32, 1, 4, 4}, // headDim 32 < 64: refused, exercised below
	} {
		a := mk(tc.halfC, tc.heads, tc.H, tc.W)
		input := NewTensor(1, tc.halfC, tc.H, tc.W)
		for i := range input.Data {
			input.Data[i] = rng.Float32()*2 - 1
		}
		got, ok := a.tryForwardFast(input.Clone())
		if !ok {
			// headDim/seqLen guards must refuse; nothing else to check.
			continue
		}
		want := a.forwardSlow(input.Clone())
		if i, worst := closeEnough(got.Data, want.Data, 5e-3); i >= 0 {
			t.Errorf("halfC=%d heads=%d %dx%d: worst rel err %g at %d (got %g want %g)",
				tc.halfC, tc.heads, tc.H, tc.W, worst, i, got.Data[i], want.Data[i])
		}
	}
}
