package tensor

import (
	"math"
	"testing"
)

func TestLayerNormBias512_MatchesScalar(t *testing.T) {
	src := make([]float32, 512)
	w := make([]float32, 512)
	b := make([]float32, 512)
	for i := 0; i < 512; i++ {
		src[i] = float32(i%17)*0.1 - 0.8
		w[i] = 1.0 + float32(i%5)*0.01
		b[i] = float32(i%3) * 0.02
	}
	got := make([]float32, 512)
	want := make([]float32, 512)
	LayerNormBias512(got, src, w, b)
	// Scalar path reference
	const invDim = float32(1.0 / 512.0)
	sum, sumsq := sumSumsq512Scalar(src)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + 1e-5)
	lnAffine512Scalar(want, src, w, b, mean, invStd)
	for i := 0; i < 512; i++ {
		if math.Abs(float64(got[i]-want[i])) > 1e-4 {
			t.Fatalf("idx %d: got %g want %g", i, got[i], want[i])
		}
	}
}

func TestLayerNormBias560_MatchesScalar(t *testing.T) {
	const dim = 560
	src := make([]float32, dim)
	w := make([]float32, dim)
	b := make([]float32, dim)
	for i := 0; i < dim; i++ {
		src[i] = float32(i%19)*0.07 - 0.6
		w[i] = 0.9 + float32(i%7)*0.02
		b[i] = float32(i%5) * 0.01
	}
	got := make([]float32, dim)
	want := make([]float32, dim)
	LayerNormBias(got, src, w, b, dim)
	sum, sumsq := sumSumsqNScalar(src, dim)
	invDim := float32(1.0 / float32(dim))
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + 1e-5)
	lnAffineNScalar(want, src, w, b, dim, mean, invStd)
	for i := 0; i < dim; i++ {
		if math.Abs(float64(got[i]-want[i])) > 1e-4 {
			t.Fatalf("idx %d: got %g want %g", i, got[i], want[i])
		}
	}
}

func TestScaleAdd_MatchesScalar(t *testing.T) {
	a := make([]float32, 560)
	b := make([]float32, 560)
	for i := range a {
		a[i] = float32(i) * 0.01
		b[i] = float32(i%11) * 0.02
	}
	got := make([]float32, 560)
	scale := float32(22.627417)
	ScaleAdd(got, a, b, scale)
	for i := range a {
		want := a[i]*scale + b[i]
		if math.Abs(float64(got[i]-want)) > 1e-5 {
			t.Fatalf("idx %d: got %g want %g", i, got[i], want)
		}
	}
}

func TestFuseAdd2AndLN512_MatchesScalar(t *testing.T) {
	out := make([]float32, 512)
	a := make([]float32, 512)
	b := make([]float32, 512)
	w := make([]float32, 512)
	bias := make([]float32, 512)
	for i := 0; i < 512; i++ {
		out[i] = float32(i%11) * 0.05
		a[i] = float32(i%7)*0.03 - 0.1
		b[i] = float32(i%13)*0.02 - 0.05
		w[i] = 1.1
		bias[i] = 0.01
	}
	outRef := append([]float32(nil), out...)
	dst := make([]float32, 512)
	dstRef := make([]float32, 512)
	FuseAdd2AndLN512(out, a, b, dst, w, bias)
	sum, sumsq := add2SumSumsq512Scalar(outRef, a, b)
	const invDim = float32(1.0 / 512.0)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + 1e-5)
	lnAffine512Scalar(dstRef, outRef, w, bias, mean, invStd)
	for i := 0; i < 512; i++ {
		if math.Abs(float64(out[i]-outRef[i])) > 1e-4 {
			t.Fatalf("out idx %d: got %g want %g", i, out[i], outRef[i])
		}
		if math.Abs(float64(dst[i]-dstRef[i])) > 1e-4 {
			t.Fatalf("dst idx %d: got %g want %g", i, dst[i], dstRef[i])
		}
	}
}
