package tensor

import "math"

// invSqrt32LN: two Newton steps of Quake-style rsqrt (matches SenseVoice LN).
func invSqrt32LN(x float32) float32 {
	if x <= 0 {
		return 0
	}
	bits := math.Float32bits(x)
	bits = 0x5f3759df - (bits >> 1)
	y := math.Float32frombits(bits)
	y = y * (1.5 - 0.5*x*y*y)
	y = y * (1.5 - 0.5*x*y*y)
	return y
}

func sumSumsq512Scalar(src []float32) (sum, sumsq float32) {
	return sumSumsqNScalar(src, 512)
}

func sumSumsqNScalar(src []float32, dim int) (sum, sumsq float32) {
	i := 0
	for ; i+7 < dim; i += 8 {
		v0, v1, v2, v3 := src[i], src[i+1], src[i+2], src[i+3]
		v4, v5, v6, v7 := src[i+4], src[i+5], src[i+6], src[i+7]
		sum += v0 + v1 + v2 + v3 + v4 + v5 + v6 + v7
		sumsq += v0*v0 + v1*v1 + v2*v2 + v3*v3 + v4*v4 + v5*v5 + v6*v6 + v7*v7
	}
	for ; i < dim; i++ {
		v := src[i]
		sum += v
		sumsq += v * v
	}
	return
}

func lnAffineNScalar(dst, src, w, b []float32, dim int, mean, invStd float32) {
	for i := 0; i < dim; i++ {
		dst[i] = (src[i]-mean)*invStd*w[i] + b[i]
	}
}

func add2SumSumsq512Scalar(out, a, b []float32) (sum, sumsq float32) {
	for i := 0; i < 512; i += 8 {
		v0 := out[i] + a[i] + b[i]
		v1 := out[i+1] + a[i+1] + b[i+1]
		v2 := out[i+2] + a[i+2] + b[i+2]
		v3 := out[i+3] + a[i+3] + b[i+3]
		v4 := out[i+4] + a[i+4] + b[i+4]
		v5 := out[i+5] + a[i+5] + b[i+5]
		v6 := out[i+6] + a[i+6] + b[i+6]
		v7 := out[i+7] + a[i+7] + b[i+7]
		out[i], out[i+1], out[i+2], out[i+3] = v0, v1, v2, v3
		out[i+4], out[i+5], out[i+6], out[i+7] = v4, v5, v6, v7
		sum += v0 + v1 + v2 + v3 + v4 + v5 + v6 + v7
		sumsq += v0*v0 + v1*v1 + v2*v2 + v3*v3 + v4*v4 + v5*v5 + v6*v6 + v7*v7
	}
	return
}

func add1SumSumsq512Scalar(out, a []float32) (sum, sumsq float32) {
	for i := 0; i < 512; i += 8 {
		v0 := out[i] + a[i]
		v1 := out[i+1] + a[i+1]
		v2 := out[i+2] + a[i+2]
		v3 := out[i+3] + a[i+3]
		v4 := out[i+4] + a[i+4]
		v5 := out[i+5] + a[i+5]
		v6 := out[i+6] + a[i+6]
		v7 := out[i+7] + a[i+7]
		out[i], out[i+1], out[i+2], out[i+3] = v0, v1, v2, v3
		out[i+4], out[i+5], out[i+6], out[i+7] = v4, v5, v6, v7
		sum += v0 + v1 + v2 + v3 + v4 + v5 + v6 + v7
		sumsq += v0*v0 + v1*v1 + v2*v2 + v3*v3 + v4*v4 + v5*v5 + v6*v6 + v7*v7
	}
	return
}

func lnAffine512Scalar(dst, src, w, b []float32, mean, invStd float32) {
	lnAffineNScalar(dst, src, w, b, 512, mean, invStd)
}
