package yolo

import (
	"math"
	"math/rand"
	"testing"
)

// refMatmul computes out[m,n] = sum_k W[m,k]*B[k,n] + bias[m] in float64.
func refMatmul(W, B, bias []float32, M, K, N int) []float32 {
	out := make([]float32, M*N)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			s := float64(bias[m])
			for k := 0; k < K; k++ {
				s += float64(W[m*K+k]) * float64(B[k*N+n])
			}
			out[m*N+n] = float32(s)
		}
	}
	return out
}

func closeEnough(a, b []float32, tol float64) (int, float64) {
	worst := 0.0
	worstI := -1
	for i := range a {
		d := math.Abs(float64(a[i] - b[i]))
		scale := math.Max(1, math.Abs(float64(b[i])))
		if r := d / scale; r > worst {
			worst = r
			worstI = i
		}
	}
	if worst > tol {
		return worstI, worst
	}
	return -1, worst
}

func TestGemmF32_Correctness(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX512")
	}
	cases := []struct{ M, K, N int }{
		{1, 1, 1}, {1, 16, 16}, {3, 27, 37}, {8, 64, 128}, {16, 1152, 256},
		{64, 27, 1024}, {256, 2304, 160}, {100, 48, 33}, {7, 9, 15},
		{512, 512, 400}, {64, 3, 17},
	}
	for _, tc := range cases {
		rng := rand.New(rand.NewSource(int64(tc.M*1000000 + tc.K*1000 + tc.N)))
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
		gemmF32(got, W, B, bias, tc.M, tc.K, tc.N)
		want := refMatmul(W, B, bias, tc.M, tc.K, tc.N)
		if i, worst := closeEnough(got, want, 1e-4); i >= 0 {
			t.Errorf("M=%d K=%d N=%d: worst rel err %g at %d (got %g want %g)",
				tc.M, tc.K, tc.N, worst, i, got[i], want[i])
		}
	}
}

func TestGemmF32_LargeShapes(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX512")
	}
	// Shapes from the real model: (M, K, N)
	cases := []struct{ M, K, N int }{
		{64, 27, 102400}, {128, 576, 25600}, {256, 1152, 25600},
		{256, 2304, 6400}, {512, 4608, 1600}, {512, 512, 400},
		{256, 256, 6400}, {1, 256, 6400}, {64, 64, 400},
	}
	for _, tc := range cases {
		rng := rand.New(rand.NewSource(int64(tc.M + tc.K*7 + tc.N*13)))
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
		gemmF32(got, W, B, bias, tc.M, tc.K, tc.N)
		// Reference on a sampled subset of columns to keep the test fast.
		colStep := tc.N/64 + 1
		for n := 0; n < tc.N; n += colStep {
			for m := 0; m < tc.M; m += tc.M/8 + 1 {
				s := float64(bias[m])
				for k := 0; k < tc.K; k++ {
					s += float64(W[m*tc.K+k]) * float64(B[k*tc.N+n])
				}
				want := float32(s)
				if g := got[m*tc.N+n]; math.Abs(float64(g-want)) > 1e-3*math.Max(1, math.Abs(float64(want))) {
					t.Fatalf("M=%d K=%d N=%d at (%d,%d): got %g want %g", tc.M, tc.K, tc.N, m, n, g, want)
				}
			}
		}
	}
}

func TestSiluSlice(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX512")
	}
	data := make([]float32, 4096)
	rng := rand.New(rand.NewSource(42))
	for i := range data {
		data[i] = rng.Float32()*40 - 20
	}
	data[0] = 0
	data[1] = -100 // clamp region
	data[2] = 100
	data[3] = -87.5
	data[16] = 1e-4
	got := append([]float32(nil), data...)
	siluSlice(got)
	for i, v := range data {
		want := v * sigmoid(v)
		if math.Abs(float64(got[i]-want)) > 1e-5*math.Max(1, math.Abs(float64(want))) {
			t.Errorf("silu(%g) = %g, want %g", v, got[i], want)
		}
	}
}

func TestAxpySlice(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX512")
	}
	out := make([]float32, 1000)
	x := make([]float32, 1000)
	rng := rand.New(rand.NewSource(7))
	for i := range out {
		out[i] = rng.Float32()
		x[i] = rng.Float32()
	}
	want := append([]float32(nil), out...)
	for i := range want {
		want[i] += 1.5 * x[i]
	}
	axpySlice(out, x, 1.5)
	if i, worst := closeEnough(out, want, 1e-6); i >= 0 {
		t.Errorf("axpy worst rel err %g at %d", worst, i)
	}
}

func TestDepthwiseSIMD_MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for _, tc := range []struct{ C, H, W, KH, KW, Pad int }{
		{8, 20, 20, 3, 3, 1}, {16, 80, 80, 3, 3, 1}, {32, 7, 13, 3, 3, 1},
		{4, 5, 5, 1, 1, 0}, {8, 9, 9, 5, 5, 2}, {3, 6, 10, 7, 7, 3},
	} {
		c := &Conv2dBNSiLU{
			Weight: NewTensor(tc.C, 1, tc.KH, tc.KW),
			Bias:   make([]float32, tc.C),
			OutC:   tc.C, InC: tc.C, KH: tc.KH, KW: tc.KW,
			Stride: 1, Padding: tc.Pad, Groups: tc.C, UseSiLU: true,
		}
		for i := range c.Weight.Data {
			c.Weight.Data[i] = rng.Float32()*2 - 1
		}
		for i := range c.Bias {
			c.Bias[i] = rng.Float32()*2 - 1
		}
		input := NewTensor(1, tc.C, tc.H, tc.W)
		for i := range input.Data {
			input.Data[i] = rng.Float32()*2 - 1
		}
		a := c.forwardDepthwiseSIMD(input.Clone())
		b := c.forwardGroupedScalar(input.Clone(), tc.C)
		if i, worst := closeEnough(a.Data, b.Data, 1e-5); i >= 0 {
			t.Errorf("C=%d H=%d KH=%d Pad=%d: worst rel err %g at %d (got %g scalar %g)",
				tc.C, tc.H, tc.KH, tc.Pad, worst, i, a.Data[i], b.Data[i])
		}
	}
}
