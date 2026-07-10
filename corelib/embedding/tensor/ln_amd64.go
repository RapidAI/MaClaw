//go:build amd64

package tensor

// LayerNormBias512: dst = LN(src) with affine, fixed dim=512.
// dst and src may alias.
func LayerNormBias512(dst, src, w, b []float32) {
	if len(dst) < 512 || len(src) < 512 || len(w) < 512 || len(b) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum, sumsq float32
	if hasAVX2andFMA {
		sum, sumsq = sumSumsq512AVX2(&src[0])
	} else {
		sum, sumsq = sumSumsq512Scalar(src)
	}
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	if hasAVX2andFMA {
		lnAffine512AVX2(&dst[0], &src[0], &w[0], &b[0], mean, invStd)
		return
	}
	lnAffine512Scalar(dst, src, w, b, mean, invStd)
}

// LayerNormBias512Dual: two frames share w/b loads in the affine pass.
func LayerNormBias512Dual(d0, s0, d1, s1, w, b []float32) {
	if len(d0) < 512 || len(s0) < 512 || len(d1) < 512 || len(s1) < 512 ||
		len(w) < 512 || len(b) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum0, sq0, sum1, sq1 float32
	if hasAVX2andFMA {
		sum0, sq0 = sumSumsq512AVX2(&s0[0])
		sum1, sq1 = sumSumsq512AVX2(&s1[0])
	} else {
		sum0, sq0 = sumSumsq512Scalar(s0)
		sum1, sq1 = sumSumsq512Scalar(s1)
	}
	mean0 := sum0 * invDim
	var0 := sq0*invDim - mean0*mean0
	if var0 < 0 {
		var0 = 0
	}
	inv0 := invSqrt32LN(var0 + eps)
	mean1 := sum1 * invDim
	var1 := sq1*invDim - mean1*mean1
	if var1 < 0 {
		var1 = 0
	}
	inv1 := invSqrt32LN(var1 + eps)
	if hasAVX2andFMA {
		lnAffine512DualAVX2(&d0[0], &s0[0], &d1[0], &s1[0], &w[0], &b[0], mean0, inv0, mean1, inv1)
		return
	}
	lnAffine512Scalar(d0, s0, w, b, mean0, inv0)
	lnAffine512Scalar(d1, s1, w, b, mean1, inv1)
}

// FuseAdd2AndLN512: out[i] += a[i]+b[i]; dst = LN(out). Fixed dim=512.
func FuseAdd2AndLN512(out, a, b, dst, w, bias []float32) {
	if len(out) < 512 || len(a) < 512 || len(b) < 512 || len(dst) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum, sumsq float32
	if hasAVX2andFMA {
		sum, sumsq = add2SumSumsq512AVX2(&out[0], &a[0], &b[0])
	} else {
		sum, sumsq = add2SumSumsq512Scalar(out, a, b)
	}
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	if hasAVX2andFMA {
		lnAffine512AVX2(&dst[0], &out[0], &w[0], &bias[0], mean, invStd)
		return
	}
	lnAffine512Scalar(dst, out, w, bias, mean, invStd)
}

// FuseAdd2AndLN512Dual: two frames; affine shares w/bias loads.
func FuseAdd2AndLN512Dual(out0, a0, b0, dst0, out1, a1, b1, dst1, w, bias []float32) {
	if len(out0) < 512 || len(out1) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum0, sq0, sum1, sq1 float32
	if hasAVX2andFMA {
		sum0, sq0 = add2SumSumsq512AVX2(&out0[0], &a0[0], &b0[0])
		sum1, sq1 = add2SumSumsq512AVX2(&out1[0], &a1[0], &b1[0])
	} else {
		sum0, sq0 = add2SumSumsq512Scalar(out0, a0, b0)
		sum1, sq1 = add2SumSumsq512Scalar(out1, a1, b1)
	}
	mean0 := sum0 * invDim
	var0 := sq0*invDim - mean0*mean0
	if var0 < 0 {
		var0 = 0
	}
	inv0 := invSqrt32LN(var0 + eps)
	mean1 := sum1 * invDim
	var1 := sq1*invDim - mean1*mean1
	if var1 < 0 {
		var1 = 0
	}
	inv1 := invSqrt32LN(var1 + eps)
	if hasAVX2andFMA {
		lnAffine512DualAVX2(&dst0[0], &out0[0], &dst1[0], &out1[0], &w[0], &bias[0], mean0, inv0, mean1, inv1)
		return
	}
	lnAffine512Scalar(dst0, out0, w, bias, mean0, inv0)
	lnAffine512Scalar(dst1, out1, w, bias, mean1, inv1)
}

// FuseAdd1AndLN512: out[i] += a[i]; dst = LN(out). Fixed dim=512.
func FuseAdd1AndLN512(out, a, dst, w, bias []float32) {
	if len(out) < 512 || len(a) < 512 || len(dst) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum, sumsq float32
	if hasAVX2andFMA {
		sum, sumsq = add1SumSumsq512AVX2(&out[0], &a[0])
	} else {
		sum, sumsq = add1SumSumsq512Scalar(out, a)
	}
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	if hasAVX2andFMA {
		lnAffine512AVX2(&dst[0], &out[0], &w[0], &bias[0], mean, invStd)
		return
	}
	lnAffine512Scalar(dst, out, w, bias, mean, invStd)
}

// FuseAdd1AndLN512Dual: two frames; affine shares w/bias loads.
func FuseAdd1AndLN512Dual(out0, a0, dst0, out1, a1, dst1, w, bias []float32) {
	if len(out0) < 512 || len(out1) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	var sum0, sq0, sum1, sq1 float32
	if hasAVX2andFMA {
		sum0, sq0 = add1SumSumsq512AVX2(&out0[0], &a0[0])
		sum1, sq1 = add1SumSumsq512AVX2(&out1[0], &a1[0])
	} else {
		sum0, sq0 = add1SumSumsq512Scalar(out0, a0)
		sum1, sq1 = add1SumSumsq512Scalar(out1, a1)
	}
	mean0 := sum0 * invDim
	var0 := sq0*invDim - mean0*mean0
	if var0 < 0 {
		var0 = 0
	}
	inv0 := invSqrt32LN(var0 + eps)
	mean1 := sum1 * invDim
	var1 := sq1*invDim - mean1*mean1
	if var1 < 0 {
		var1 = 0
	}
	inv1 := invSqrt32LN(var1 + eps)
	if hasAVX2andFMA {
		lnAffine512DualAVX2(&dst0[0], &out0[0], &dst1[0], &out1[0], &w[0], &bias[0], mean0, inv0, mean1, inv1)
		return
	}
	lnAffine512Scalar(dst0, out0, w, bias, mean0, inv0)
	lnAffine512Scalar(dst1, out1, w, bias, mean1, inv1)
}

//go:noescape
func sumSumsq512AVX2(src *float32) (sum, sumsq float32)

//go:noescape
func add2SumSumsq512AVX2(out, a, b *float32) (sum, sumsq float32)

//go:noescape
func add1SumSumsq512AVX2(out, a *float32) (sum, sumsq float32)

//go:noescape
func lnAffine512AVX2(dst, src, w, b *float32, mean, invStd float32)

//go:noescape
func lnAffine512DualAVX2(d0, s0, d1, s1, w, b *float32, mean0, inv0, mean1, inv1 float32)

//go:noescape
func sumSumsqNAVX2(src *float32, n int) (sum, sumsq float32)

//go:noescape
func lnAffineNAVX2(dst, src, w, b *float32, n int, mean, invStd float32)

//go:noescape
func scaleAddNAVX2(dst, a, b *float32, n int, scale float32)

// LayerNormBias: LN with affine for dim multiple of 16 (SenseVoice entry dim=560).
// dst and src may alias.
func LayerNormBias(dst, src, w, b []float32, dim int) {
	if dim <= 0 || len(dst) < dim || len(src) < dim || len(w) < dim || len(b) < dim {
		return
	}
	if dim == 512 {
		LayerNormBias512(dst, src, w, b)
		return
	}
	const eps = 1e-5
	invDim := 1.0 / float32(dim)
	body := dim &^ 15
	var sum, sumsq float32
	if hasAVX2andFMA && body >= 16 {
		sum, sumsq = sumSumsqNAVX2(&src[0], body)
		for i := body; i < dim; i++ {
			v := src[i]
			sum += v
			sumsq += v * v
		}
	} else {
		sum, sumsq = sumSumsqNScalar(src, dim)
	}
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	if hasAVX2andFMA && body >= 16 {
		lnAffineNAVX2(&dst[0], &src[0], &w[0], &b[0], body, mean, invStd)
		for i := body; i < dim; i++ {
			dst[i] = (src[i]-mean)*invStd*w[i] + b[i]
		}
		return
	}
	lnAffineNScalar(dst, src, w, b, dim, mean, invStd)
}

// ScaleAdd: dst[i] = a[i]*scale + b[i] (PE: x*sqrt + pos).
func ScaleAdd(dst, a, b []float32, scale float32) {
	n := len(dst)
	if n > len(a) {
		n = len(a)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	body := n &^ 15
	if hasAVX2andFMA && body >= 16 {
		scaleAddNAVX2(&dst[0], &a[0], &b[0], body, scale)
		for i := body; i < n; i++ {
			dst[i] = a[i]*scale + b[i]
		}
		return
	}
	for i := 0; i < n; i++ {
		dst[i] = a[i]*scale + b[i]
	}
}
