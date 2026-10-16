package yolo

import (
	"math"
	"math/rand"
	"testing"
)

// scalarMaxPool is the reference per-element max pool (the pre-optimization
// implementation).
func scalarMaxPool(t *Tensor, kernel, stride, padding int) *Tensor {
	N, C, H, W := t.Shape[0], t.Shape[1], t.Shape[2], t.Shape[3]
	outH := (H+2*padding-kernel)/stride + 1
	outW := (W+2*padding-kernel)/stride + 1
	out := NewTensor(N, C, outH, outW)
	for n := 0; n < N; n++ {
		for c := 0; c < C; c++ {
			for oh := 0; oh < outH; oh++ {
				for ow := 0; ow < outW; ow++ {
					maxVal := float32(-math.MaxFloat32)
					for kh := 0; kh < kernel; kh++ {
						for kw := 0; kw < kernel; kw++ {
							ih := oh*stride - padding + kh
							iw := ow*stride - padding + kw
							if ih >= 0 && ih < H && iw >= 0 && iw < W {
								v := t.At(n, c, ih, iw)
								if v > maxVal {
									maxVal = v
								}
							}
						}
					}
					out.Set(maxVal, n, c, oh, ow)
				}
			}
		}
	}
	return out
}

func TestMaxPool5S1P2_FastMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(31))
	for _, tc := range []struct{ C, H, W int }{
		{4, 5, 5}, {8, 20, 20}, {16, 40, 40}, {3, 7, 9}, {2, 4, 20}, {5, 20, 4},
	} {
		in := NewTensor(1, tc.C, tc.H, tc.W)
		for i := range in.Data {
			in.Data[i] = rng.Float32()*8 - 4
		}
		got := in.MaxPool2d(5, 1, 2)
		want := scalarMaxPool(in, 5, 1, 2)
		for i := range got.Data {
			if got.Data[i] != want.Data[i] {
				t.Fatalf("C=%d H=%d W=%d: mismatch at %d (got %g want %g)",
					tc.C, tc.H, tc.W, i, got.Data[i], want.Data[i])
			}
		}
	}
}

func TestUpsample2x_FastMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	for _, tc := range []struct{ C, H, W int }{
		{4, 3, 5}, {16, 20, 20}, {8, 1, 1}, {2, 7, 3},
	} {
		in := NewTensor(1, tc.C, tc.H, tc.W)
		for i := range in.Data {
			in.Data[i] = rng.Float32() * 10
		}
		got := in.Upsample2x()
		want := NewTensor(1, tc.C, tc.H*2, tc.W*2)
		for c := 0; c < tc.C; c++ {
			for h := 0; h < tc.H; h++ {
				for w := 0; w < tc.W; w++ {
					v := in.At(0, c, h, w)
					want.Set(v, 0, c, h*2, w*2)
					want.Set(v, 0, c, h*2, w*2+1)
					want.Set(v, 0, c, h*2+1, w*2)
					want.Set(v, 0, c, h*2+1, w*2+1)
				}
			}
		}
		for i := range got.Data {
			if got.Data[i] != want.Data[i] {
				t.Fatalf("C=%d H=%d W=%d: mismatch at %d", tc.C, tc.H, tc.W, i)
			}
		}
	}
}

func TestSiluZmm_MatchesScalar(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX512")
	}
	data := make([]float32, 4099)
	rng := rand.New(rand.NewSource(3))
	for i := range data {
		data[i] = rng.Float32()*30 - 15
	}
	got := append([]float32(nil), data...)
	siluSlice(got)
	for i, v := range data {
		want := v * sigmoid(v)
		if math.Abs(float64(got[i]-want)) > 1e-5*math.Max(1, math.Abs(float64(want))) {
			t.Fatalf("silu(%g) = %g, want %g", v, got[i], want)
		}
	}
}
