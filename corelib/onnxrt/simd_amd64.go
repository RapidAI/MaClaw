//go:build amd64

package onnxrt

import "golang.org/x/sys/cpu"

// hasAVX2FMA reports whether AVX2+FMA is available (Haswell+).
var hasAVX2FMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA

// hasAVX512ZMM reports whether the AVX-512 ZMM kernels may run (Zen4 /
// Ice Lake+): F/DQ/VL/BW, matching the tensor package's gate.
var hasAVX512ZMM = cpu.X86.HasAVX512F && cpu.X86.HasAVX512DQ &&
	cpu.X86.HasAVX512VL && cpu.X86.HasAVX512BW

// ctcRowExpAVX512 computes row[i] = exp(max(row[i]-m, -80)) for n elements
// (n multiple of 16; n == 0 is a no-op). In-place safe.
func ctcRowExpAVX512(row *float32, n int, m float32)

// geluErfAVX512 is the 16-wide AVX-512 port of geluErfAVX2 (n multiple of
// 16; n == 0 is a no-op). In-place safe.
func geluErfAVX512(dst, src *float32, n int)

// fmaddScalarAVX2 computes out[i] += w*x[i] for n elements (n multiple of 8).
func fmaddScalarAVX2(out, x *float32, w float32, n int)

// geluErfAVX2 computes dst[i] = 0.5*src[i]*(1+erf(src[i]/sqrt(2))) for n
// elements (n multiple of 8; n == 0 is a no-op). Bit-exact with the vek32-op
// pipeline in geluErfInto; in-place safe (dst == src).
func geluErfAVX2(dst, src *float32, n int)

// transpose8x8F32 transposes an 8x8 float32 block:
// dst[j*ldDst+i] = src[i*ldSrc+j]. Pure data movement, bit-exact.
func transpose8x8F32(dst *float32, ldDst int, src *float32, ldSrc int)

// im2row3x3AVX512 is the vectorized interior of im2rowFast for 3x3 sW=1
// convolutions: for each (channel c, kernel row kh) it copies 3 consecutive
// floats x[base+p..p+2] (base = c*HW + rowOff[kh] + srcOff) into B rows for
// 4·groups consecutive output pixels at column 9c+3kh, K floats per row.
// Caller contract: see the comment above the asm implementation.
func im2row3x3AVX512(dst, x *float32, rowOff *int, groups, K, HW, Cg, srcOff int)

// fmadd3AVX2 computes out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2] for n
// elements (n multiple of 8; n == 0 is a no-op); x must extend 2 past n.
func fmadd3AVX2(out, x *float32, w0, w1, w2 float32, n int)

// fmadd5AVX2 computes out[i] += w0*x[i] + ... + w4*x[i+4] for n elements
// (n multiple of 8; n == 0 is a no-op); x must extend 4 past n.
func fmadd5AVX2(out, x *float32, w0, w1, w2, w3, w4 float32, n int)

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
	if n < 32 {
		return 0
	}
	body := n &^ 31
	if hasAVX512ZMM {
		geluErfAVX512(&dst[0], &src[0], body)
		return body
	}
	if !hasAVX2FMA {
		return 0
	}
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

// fmadd5Into computes out[i] += w0*x[i] + ... + w4*x[i+4], one pass, SIMD
// when possible. Same per-tap FMA order as five fmaddScalarInto passes.
func fmadd5Into(out, x []float32, w0, w1, w2, w3, w4 float32) {
	n := len(out)
	if len(x)-4 < n {
		n = len(x) - 4
	}
	if n <= 0 {
		return
	}
	if hasAVX2FMA && n >= 8 {
		body := n &^ 7
		fmadd5AVX2(&out[0], &x[0], w0, w1, w2, w3, w4, body)
		for i := body; i < n; i++ {
			out[i] += w0 * x[i]
			out[i] += w1 * x[i+1]
			out[i] += w2 * x[i+2]
			out[i] += w3 * x[i+3]
			out[i] += w4 * x[i+4]
		}
		return
	}
	fmadd5Scalar(out, x, w0, w1, w2, w3, w4, n)
}

// fmadd5Scalar is the portable per-tap-order fmadd5 implementation.
func fmadd5Scalar(out, x []float32, w0, w1, w2, w3, w4 float32, n int) {
	for i := 0; i < n; i++ {
		out[i] += w0 * x[i]
		out[i] += w1 * x[i+1]
		out[i] += w2 * x[i+2]
		out[i] += w3 * x[i+3]
		out[i] += w4 * x[i+4]
	}
}

// bilinear4AVX2 writes the gridBilinear product association for n elements
// (n multiple of 8). out[i] = ((a*(1-wx))*(1-wy) + (b*wx)*(1-wy)) + ...
func bilinear4AVX2(out, a, b, c, d *float32, omx, wx, omy, wy float32, n int)

// bilinear4AVX512 is the 16-wide form of bilinear4AVX2 (n multiple of 16).
func bilinear4AVX512(out, a, b, c, d *float32, omx, wx, omy, wy float32, n int)

// affineAVX2 writes out[i] = x[i]*scale + bias (n multiple of 8).
func affineAVX2(out, x *float32, scale, bias float32, n int)

// affineAVX512 is the 16-wide form of affineAVX2 (n multiple of 16).
func affineAVX512(out, x *float32, scale, bias float32, n int)

// reduceMaxAVX2 writes the 8 lane-wise maxima of x[:n] and a NaN mask.
// n is a multiple of 8. The pointers are only used for the duration of the call.
//
//go:noescape
func reduceMaxAVX2(x *float32, n int, lanes *float32, nan *uint32)

// reduceMinAVX2 writes the 8 lane-wise minima of x[:n]. n is a multiple of 8.
//
//go:noescape
func reduceMinAVX2(x *float32, n int, lanes *float32, nan *uint32)

// reduceMaxAVX512 is the 16-wide form of reduceMaxAVX2. n is a multiple of 16.
//
//go:noescape
func reduceMaxAVX512(x *float32, n int, lanes *float32, nan *uint32)

// reduceMinAVX512 is the 16-wide form of reduceMinAVX2. n is a multiple of 16.
//
//go:noescape
func reduceMinAVX512(x *float32, n int, lanes *float32, nan *uint32)

// reduceContigExtreme is the SIMD form of a contiguous ReduceMax or ReduceMin.
// It agrees with the scalar `>` / `<` loop for finite values. NaN rows and
// rows whose extreme is ±0 return ok=false so the caller keeps the scalar
// loop: lane order does not preserve which signed zero came first, and a
// NaN must be the last one in left-to-right order.
func reduceContigExtreme(row []float32, code reduceCode) (float32, bool) {
	n := len(row)
	if n < 8 || !hasAVX2FMA {
		return 0, false
	}
	var lanes [16]float32
	var nan uint32
	width := 8
	body := n &^ 7
	if hasAVX512ZMM && n >= 16 {
		body = n &^ 15
		width = 16
		if code == reduceMax {
			reduceMaxAVX512(&row[0], body, &lanes[0], &nan)
		} else {
			reduceMinAVX512(&row[0], body, &lanes[0], &nan)
		}
	} else if code == reduceMax {
		reduceMaxAVX2(&row[0], body, &lanes[0], &nan)
	} else {
		reduceMinAVX2(&row[0], body, &lanes[0], &nan)
	}
	if nan != 0 {
		return 0, false
	}
	best := lanes[0]
	for i := 1; i < width; i++ {
		if tookExtreme(code, best, lanes[i]) {
			best = lanes[i]
		}
	}
	for _, v := range row[body:] {
		if v != v {
			return 0, false
		}
		if tookExtreme(code, best, v) {
			best = v
		}
	}
	if best == 0 {
		return 0, false
	}
	return best, true
}

func tookExtreme(code reduceCode, best, v float32) bool {
	if code == reduceMax {
		return v > best
	}
	return v < best
}

func bilinear4Vec(out, a, b, c, d []float32, omx, wx, omy, wy float32) int {
	n := len(out)
	if len(a) < n {
		n = len(a)
	}
	if len(b) < n {
		n = len(b)
	}
	if len(c) < n {
		n = len(c)
	}
	if len(d) < n {
		n = len(d)
	}
	if n <= 0 {
		return 0
	}
	done := 0
	if hasAVX512ZMM && n >= 16 {
		body := n &^ 15
		bilinear4AVX512(&out[0], &a[0], &b[0], &c[0], &d[0], omx, wx, omy, wy, body)
		done = body
	}
	if hasAVX2FMA && n-done >= 8 {
		body := (n - done) &^ 7
		bilinear4AVX2(&out[done], &a[done], &b[done], &c[done], &d[done], omx, wx, omy, wy, body)
		done += body
	}
	return done
}

func affineVec(out, x []float32, scale, bias float32) int {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	if n <= 0 {
		return 0
	}
	done := 0
	if hasAVX512ZMM && n >= 16 {
		body := n &^ 15
		affineAVX512(&out[0], &x[0], scale, bias, body)
		done = body
	}
	if hasAVX2FMA && n-done >= 8 {
		body := (n - done) &^ 7
		affineAVX2(&out[done], &x[done], scale, bias, body)
		done += body
	}
	return done
}

// transposeMat4 is the NEON 4×4 tile. AVX2 uses transpose8x8F32 instead.
func transposeMat4(dst, src []float32, outer, A, B int) bool { return false }
