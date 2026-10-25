//go:build !amd64

package onnxrt

// hasAVX2FMA is always false off amd64 (no AVX2 kernels there).
const hasAVX2FMA = false

// hasAVX512ZMM is always false off amd64 (no AVX-512 kernels there).
const hasAVX512ZMM = false

// reduceContigExtreme is the amd64 SIMD path. Other architectures keep the
// scalar reduction in opReduce.
func reduceContigExtreme(row []float32, code reduceCode) (float32, bool) {
	return 0, false
}

// ctcRowExpClamp now lives in ctc.go (untagged): it calls ctcRowExpAVX512 only
// when hasAVX512ZMM, and otherwise runs the same portable vek32 pipeline that
// used to sit here. Only the fused kernel needs a !amd64 counterpart.

// ctcRowExpAVX512 is the !amd64 stub; ctcRowExpClamp never calls it because
// hasAVX512ZMM is false.
func ctcRowExpAVX512(row *float32, n int, m float32) {
	panic("onnxrt: ctcRowExpAVX512 is amd64-only")
}

// transpose8x8F32 is the !amd64 stub; transposePlanesRange never calls it
// because hasAVX2FMA is false.
func transpose8x8F32(dst *float32, ldDst int, src *float32, ldSrc int) {
	panic("onnxrt: transpose8x8F32 is amd64-only")
}

// im2row3x3AVX512 is the !amd64 stub; im2rowFast never calls it because
// hasAVX512ZMM is false.
func im2row3x3AVX512(dst, x *float32, rowOff *int, groups, K, HW, Cg, srcOff int) {
	panic("onnxrt: im2row3x3AVX512 is amd64-only")
}

// fmaddScalarInto computes out[i] += w*x[i] (portable fallback).
// Unlike the amd64 path it does not special-case w == 0; for finite x the
// result is identical (see the comment in simd_amd64.go).
func fmaddScalarInto(out, x []float32, w float32) {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	fmaddScalarScalar(out, x, w, n)
}

// fmaddScalarScalar is the portable implementation.
func fmaddScalarScalar(out, x []float32, w float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w * x[i]
	}
}

// geluErfFast is the portable fallback: no fused kernel, process nothing.
func geluErfFast(dst, src []float32) int { return 0 }

// fmadd3Into computes out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2] (portable
// fallback).
func fmadd3Into(out, x []float32, w0, w1, w2 float32) {
	n := len(out)
	if len(x)-2 < n {
		n = len(x) - 2
	}
	if n <= 0 {
		return
	}
	if body := fmadd3Vec(out, x, w0, w1, w2, n); body < n {
		fmadd3Scalar(out[body:], x[body:], w0, w1, w2, n-body)
	}
}

// fmadd3Scalar is the portable per-tap-order implementation.
func fmadd3Scalar(out, x []float32, w0, w1, w2 float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w0 * x[i]
		out[i] += w1 * x[i+1]
		out[i] += w2 * x[i+2]
	}
}

// fmadd5Into computes out[i] += w0*x[i] + ... + w4*x[i+4] (portable fallback).
func fmadd5Into(out, x []float32, w0, w1, w2, w3, w4 float32) {
	n := len(out)
	if len(x)-4 < n {
		n = len(x) - 4
	}
	if n <= 0 {
		return
	}
	if body := fmadd5Vec(out, x, w0, w1, w2, w3, w4, n); body < n {
		fmadd5Scalar(out[body:], x[body:], w0, w1, w2, w3, w4, n-body)
	}
}

// fmadd5Scalar is the portable per-tap-order implementation.
func fmadd5Scalar(out, x []float32, w0, w1, w2, w3, w4 float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w0 * x[i]
		out[i] += w1 * x[i+1]
		out[i] += w2 * x[i+2]
		out[i] += w3 * x[i+3]
		out[i] += w4 * x[i+4]
	}
}
