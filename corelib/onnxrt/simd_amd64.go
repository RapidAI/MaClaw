//go:build amd64

package onnxrt

import "golang.org/x/sys/cpu"

// hasAVX2FMA reports whether AVX2+FMA is available (Haswell+).
var hasAVX2FMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA

// fmaddScalarAVX2 computes out[i] += w*x[i] for n elements (n multiple of 8).
func fmaddScalarAVX2(out, x *float32, w float32, n int)

// geluErfAVX2 computes dst[i] = 0.5*src[i]*(1+erf(src[i]/sqrt(2))) for n
// elements (n multiple of 8; n == 0 is a no-op). Bit-exact with the vek32-op
// pipeline in geluErfInto; in-place safe (dst == src).
func geluErfAVX2(dst, src *float32, n int)

// transpose8x8F32 transposes an 8x8 float32 block:
// dst[j*ldDst+i] = src[i*ldSrc+j]. Pure data movement, bit-exact.
func transpose8x8F32(dst *float32, ldDst int, src *float32, ldSrc int)

// fmadd3AVX2 computes out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2] for n
// elements (n multiple of 8; n == 0 is a no-op); x must extend 2 past n.
func fmadd3AVX2(out, x *float32, w0, w1, w2 float32, n int)

// fmadd3Into computes out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2], one pass,
// SIMD when possible. Same per-tap FMA order as three fmaddScalarInto
// passes, so results are bit-identical.
func fmadd3Into(out, x []float32, w0, w1, w2 float32) {
	n := len(out)
	if len(x)-2 < n {
		n = len(x) - 2
	}
	if n <= 0 {
		return
	}
	if hasAVX2FMA && n >= 8 {
		body := n &^ 7
		fmadd3AVX2(&out[0], &x[0], w0, w1, w2, body)
		for i := body; i < n; i++ {
			out[i] += w0 * x[i]
			out[i] += w1 * x[i+1]
			out[i] += w2 * x[i+2]
		}
		return
	}
	fmadd3Scalar(out, x, w0, w1, w2, n)
}

// geluErfFast applies the fused AVX2 GELU(erf) kernel to a prefix of src and
// returns the number of elements processed (0 when AVX2+FMA is unavailable or
// n < 32). The caller handles the tail with the portable pipeline.
//
// The prefix is rounded down to a multiple of 32 on purpose: vek32's Inv uses
// its rcp+Newton approximation only for the (n &^ 31) bulk and exact division
// past it, so stopping at the same boundary keeps the fused kernel
// bit-identical to the portable pipeline for every element.
func geluErfFast(dst, src []float32) int {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	if !hasAVX2FMA || n < 32 {
		return 0
	}
	body := n &^ 31
	geluErfAVX2(&dst[0], &src[0], body)
	return body
}

// fmaddScalarInto computes out[i] += w*x[i], one pass, SIMD when possible.
//
// w == 0 is an explicit no-op fast path (hot in conv with zero-padded or
// pruned weights). This intentionally deviates from IEEE: with non-finite x,
// 0*±Inf/0*NaN would propagate NaN, while the skip leaves out unchanged.
// Conv inputs are finite for any finite model input, so the divergence is
// unreachable in practice; the branch stays because zero weights are common
// enough in conv (padding/pruning) to make the skip worthwhile.
// (The !amd64 fallback does compute 0*x — harmless either way for finite x.)
//
// A second, related deviation: the AVX2 kernel rounds each tap once (FMA),
// the portable loop rounds twice (mul, then add). For finite data the two
// agree to a couple ulp; only an intermediate product that overflows float32
// (|w*x| > MaxFloat32 while the exact sum stays finite, or vice versa) can
// make them diverge in class (Inf vs NaN). Unreachable for finite conv data.
func fmaddScalarInto(out, x []float32, w float32) {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	if n == 0 || w == 0 {
		return
	}
	if hasAVX2FMA && n >= 8 {
		body := n &^ 7
		fmaddScalarAVX2(&out[0], &x[0], w, body)
		for i := body; i < n; i++ {
			out[i] += w * x[i]
		}
		return
	}
	fmaddScalarScalar(out, x, w, n)
}

// fmaddScalarScalar is the portable fallback.
func fmaddScalarScalar(out, x []float32, w float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w * x[i]
	}
}

// fmadd3Scalar is the portable per-tap-order fmadd3 implementation.
func fmadd3Scalar(out, x []float32, w0, w1, w2 float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w0 * x[i]
		out[i] += w1 * x[i+1]
		out[i] += w2 * x[i+2]
	}
}
