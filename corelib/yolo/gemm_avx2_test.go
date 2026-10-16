package yolo

import (
	"math"
	"math/rand"
	"testing"
)

// TestGemmConvAVX2_Correctness validates the register-blocked AVX2 kernel
// (including the ragged-M tail path and the fused SiLU epilogue) against a
// float64 reference.
func TestGemmConvAVX2_Correctness(t *testing.T) {
	if !hasAVX2FMA {
		t.Skip("no AVX2+FMA")
	}
	cases := []struct {
		M, K, N int
		silu    bool
	}{
		{6, 1, 16, false}, {6, 1, 16, true},
		{12, 3, 32, false}, {7, 27, 1600, false}, {7, 27, 1600, true},
		{64, 576, 640, false}, {64, 576, 640, true},
		{66, 1152, 1600, true}, {130, 2304, 400, true},
		{512, 4608, 1600, true}, {1, 64, 16, true}, {5, 9, 4096, true},
		{256, 256, 6400, false},
	}
	for _, tc := range cases {
		rng := rand.New(rand.NewSource(int64(tc.M*100003 + tc.K*101 + tc.N)))
		W := make([]float32, tc.M*tc.K)
		B := make([]float32, tc.K*tc.N)
		bias := make([]float32, tc.M)
		for i := range W {
			W[i] = rng.Float32()*2 - 1
		}
		for i := range B {
			B[i] = rng.Float32()*2 - 1
		}
		for i := range bias {
			bias[i] = rng.Float32()*2 - 1
		}
		got := make([]float32, tc.M*tc.N)
		gemmConvAVX2(W, B, bias, got, tc.M, tc.K, tc.N, tc.N, tc.silu)
		want := refMatmul(W, B, bias, tc.M, tc.K, tc.N)
		if tc.silu {
			for i, v := range want {
				want[i] = float32(float64(v) / (1 + math.Exp(-float64(v))))
			}
		}
		tol := 1e-4
		if tc.silu {
			tol = 2e-3
		}
		if i, worst := closeEnough(got, want, tol); i >= 0 {
			t.Errorf("M=%d K=%d N=%d silu=%v: worst rel err %g at %d (got %g want %g)",
				tc.M, tc.K, tc.N, tc.silu, worst, i, got[i], want[i])
		}
	}
}

// TestConvForward_FusedMatchesReference checks that the production conv
// forward (AVX2 kernel + fused SiLU) matches the legacy matmul + separate
// activation, layer for layer, on representative shapes.
func TestConvForward_FusedMatchesReference(t *testing.T) {
	if !hasAVX2FMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(7))
	shapes := []struct {
		outC, inC, kh, kw, stride, pad int
		silu                           bool
	}{
		{64, 3, 3, 3, 2, 1, true},   // stem conv
		{128, 64, 3, 3, 2, 1, true}, // stride-2 conv
		{256, 256, 3, 3, 1, 1, true},
		{512, 512, 1, 1, 1, 0, true},   // 1x1 conv (C3k2 cv1/cv2)
		{65, 128, 1, 1, 1, 0, false},   // detect head final conv
		{512, 512, 3, 3, 2, 1, true},   // downsampling conv
		{256, 256, 3, 3, 1, 1, false},  // no activation
	}
	for _, s := range shapes {
		c := &Conv2dBNSiLU{
			Weight:  NewTensor(s.outC, s.inC, s.kh, s.kw),
			Bias:    make([]float32, s.outC),
			OutC:    s.outC, InC: s.inC, KH: s.kh, KW: s.kw,
			Stride: s.stride, Padding: s.pad, Groups: 1, UseSiLU: s.silu,
		}
		for i := range c.Weight.Data {
			c.Weight.Data[i] = rng.Float32()*2 - 1
		}
		for i := range c.Bias {
			c.Bias[i] = rng.Float32()*2 - 1
		}
		// Small spatial size keeps the reference fast while still spanning
		// multiple N panels (W=40 → N=1600 at stride 1... use 36 → 1296? keep 40).
		inW, inH := 40, 40
		if s.stride == 2 {
			inW, inH = 80, 80
		}
		input := NewTensor(1, s.inC, inH, inW)
		for i := range input.Data {
			input.Data[i] = rng.Float32()*2 - 1
		}

		got := c.Forward(input)

		// Reference: legacy matmul + separate SiLU.
		outH := (inH+2*s.pad-s.kh)/s.stride + 1
		outW := (inW+2*s.pad-s.kw)/s.stride + 1
		colSize := s.inC * s.kh * s.kw
		n := outH * outW
		col := make([]float32, colSize*n)
		im2colParallel(input, 0, s.kh, s.kw, s.stride, s.pad, outH, outW, col)
		want := make([]float32, s.outC*n)
		matmulConvLegacy(c.Weight.Data, col, c.Bias, want, s.outC, colSize, n)
		if s.silu {
			for i, v := range want {
				want[i] = v * sigmoid(v)
			}
		}
		tol := 1e-4
		if s.silu {
			tol = 2e-3
		}
		if i, worst := closeEnough(got.Data, want, tol); i >= 0 {
			t.Errorf("conv %d→%d k=%dx%d s=%d silu=%v: worst rel err %g at %d (got %g want %g)",
				s.inC, s.outC, s.kh, s.kw, s.stride, s.silu, worst, i, got.Data[i], want[i])
		}
	}
}
