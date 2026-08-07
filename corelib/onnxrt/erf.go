package onnxrt

import (
	"math"

	"github.com/viterin/vek/vek32"
)

// Abramowitz & Stegun 7.1.26 coefficients: |epsilon| <= 1.5e-7.
var erfAS = [5]float32{
	0.254829592,
	-0.284496736,
	1.421413741,
	-1.453152027,
	1.061405429,
}

const erfP = 0.3275911

// erf32 computes erf(x) in float32 via the A&S 7.1.26 rational approximation.
func erf32(x float32) float32 {
	sign := float32(1)
	if x < 0 {
		sign = -1
		x = -x
	}
	t := 1 / (1 + erfP*x)
	poly := ((((erfAS[4]*t+erfAS[3])*t+erfAS[2])*t+erfAS[1])*t + erfAS[0]) * t
	y := 1 - poly*float32(math.Exp(float64(-x*x)))
	return sign * y
}

// erf32Into computes dst[i] = erf(src[i]) using vectorized primitives.
// The expensive exp() is evaluated via vek32's AVX2 Exp approximation.
func erf32Into(dst, src []float32) {
	n := len(src)
	// ax = |x|; keep sign for the final restore.
	ax := getScratch(n)
	vek32.Abs_Into(ax, src)

	// t = 1 / (1 + p*ax)
	t := getScratch(n)
	vek32.MulNumber_Into(t, ax, erfP)
	vek32.AddNumber_Inplace(t, 1)
	vek32.Inv_Inplace(t)

	// Horner: poly = ((((a4*t+a3)*t+a2)*t+a1)*t+a0)*t
	poly := getScratch(n)
	vek32.MulNumber_Into(poly, t, erfAS[4])
	vek32.AddNumber_Inplace(poly, erfAS[3])
	vek32.Mul_Inplace(poly, t)
	vek32.AddNumber_Inplace(poly, erfAS[2])
	vek32.Mul_Inplace(poly, t)
	vek32.AddNumber_Inplace(poly, erfAS[1])
	vek32.Mul_Inplace(poly, t)
	vek32.AddNumber_Inplace(poly, erfAS[0])
	vek32.Mul_Inplace(poly, t)

	// e = exp(-ax*ax); clamp the exponent: exp(-80) ≈ 2e-35, already below
	// float32 precision of the result, and vek32's Exp breaks on subnormal
	// outputs.
	e := getScratch(n)
	vek32.Mul_Into(e, ax, ax)
	vek32.Neg_Inplace(e)
	vek32.MaximumNumber_Inplace(e, -80)
	vek32.Exp_Inplace(e)

	// dst = 1 - poly*e, with the sign restore fused into the final store
	// (safe for dst == src: each element is read before it is written).
	// A NaN in the pipeline means the input was non-finite: vek32's Inv is
	// a reciprocal approximation that turns 1/+Inf into NaN on the AVX2
	// path. Clamp erf(±Inf) = ±1 exactly; NaN inputs propagate as NaN.
	vek32.Mul_Inplace(poly, e)
	vek32.Neg_Inplace(poly)
	vek32.AddNumber_Inplace(poly, 1)
	for i := 0; i < n; i++ {
		v := poly[i]
		if v != v {
			switch {
			case math.IsInf(float64(src[i]), 1):
				v = 1
			case math.IsInf(float64(src[i]), -1):
				v = -1
			}
		} else if src[i] < 0 {
			v = -v
		}
		dst[i] = v
	}

	putScratch(ax)
	putScratch(t)
	putScratch(poly)
	putScratch(e)
}

// geluErfInto computes dst[i] = 0.5*x*(1+erf(x/sqrt(2))) (exact-form GELU).
func geluErfInto(dst, src []float32) {
	// Fused single-pass kernel for the 8-aligned prefix (bit-exact with the
	// vek32 pipeline below); the scalar tail keeps the previous code path.
	if done := geluErfFast(dst, src); done > 0 {
		dst = dst[done:]
		src = src[done:]
		if len(src) == 0 {
			return
		}
	}
	geluErfIntoVek(dst, src)
}

// geluErfIntoVek is the portable pipeline built from vek32 vector ops.
func geluErfIntoVek(dst, src []float32) {
	n := len(src)
	tmp := getScratch(n)
	const invSqrt2 = 0.7071067811865476
	vek32.MulNumber_Into(tmp, src, invSqrt2)
	erf32Into(tmp, tmp)
	vek32.AddNumber_Inplace(tmp, 1)
	vek32.Mul_Inplace(tmp, src)
	vek32.MulNumber_Into(dst, tmp, 0.5) // scale straight into dst (no extra copy)
	putScratch(tmp)
}
