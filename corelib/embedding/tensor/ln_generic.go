//go:build !amd64

package tensor

// LayerNormBias512: dst = LN(src) with affine, fixed dim=512.
func LayerNormBias512(dst, src, w, b []float32) {
	if len(dst) < 512 || len(src) < 512 || len(w) < 512 || len(b) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	sum, sumsq := sumSumsq512Scalar(src)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	lnAffine512Scalar(dst, src, w, b, mean, invStd)
}

// LayerNormBias512Dual: two frames (generic fallback = two singles).
func LayerNormBias512Dual(d0, s0, d1, s1, w, b []float32) {
	LayerNormBias512(d0, s0, w, b)
	LayerNormBias512(d1, s1, w, b)
}

// FuseAdd2AndLN512: out[i] += a[i]+b[i]; dst = LN(out).
func FuseAdd2AndLN512(out, a, b, dst, w, bias []float32) {
	if len(out) < 512 || len(a) < 512 || len(b) < 512 || len(dst) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	sum, sumsq := add2SumSumsq512Scalar(out, a, b)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	lnAffine512Scalar(dst, out, w, bias, mean, invStd)
}

func FuseAdd2AndLN512Dual(out0, a0, b0, dst0, out1, a1, b1, dst1, w, bias []float32) {
	FuseAdd2AndLN512(out0, a0, b0, dst0, w, bias)
	FuseAdd2AndLN512(out1, a1, b1, dst1, w, bias)
}

// FuseAdd1AndLN512: out[i] += a[i]; dst = LN(out).
func FuseAdd1AndLN512(out, a, dst, w, bias []float32) {
	if len(out) < 512 || len(a) < 512 || len(dst) < 512 {
		return
	}
	const (
		eps    = 1e-5
		invDim = float32(1.0 / 512.0)
	)
	sum, sumsq := add1SumSumsq512Scalar(out, a)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	lnAffine512Scalar(dst, out, w, bias, mean, invStd)
}

func FuseAdd1AndLN512Dual(out0, a0, dst0, out1, a1, dst1, w, bias []float32) {
	FuseAdd1AndLN512(out0, a0, dst0, w, bias)
	FuseAdd1AndLN512(out1, a1, dst1, w, bias)
}

// LayerNormBias: LN with affine for arbitrary dim.
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
	sum, sumsq := sumSumsqNScalar(src, dim)
	mean := sum * invDim
	variance := sumsq*invDim - mean*mean
	if variance < 0 {
		variance = 0
	}
	invStd := invSqrt32LN(variance + eps)
	lnAffineNScalar(dst, src, w, b, dim, mean, invStd)
}

// ScaleAdd: dst[i] = a[i]*scale + b[i].
func ScaleAdd(dst, a, b []float32, scale float32) {
	n := len(dst)
	if n > len(a) {
		n = len(a)
	}
	if n > len(b) {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dst[i] = a[i]*scale + b[i]
	}
}
