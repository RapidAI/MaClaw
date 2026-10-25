//go:build arm64

package onnxrt

import xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"

//go:noescape
func fmadd3NEON(out, x *float32, w0, w1, w2 float32, n int)

//go:noescape
func fmadd5NEON(out, x *float32, w0, w1, w2, w3, w4 float32, n int)

func fmadd3Vec(out, x []float32, w0, w1, w2 float32, n int) int {
	body := n &^ 3
	if body < 4 {
		return 0
	}
	fmadd3NEON(&out[0], &x[0], w0, w1, w2, body)
	return body
}

func fmadd5Vec(out, x []float32, w0, w1, w2, w3, w4 float32, n int) int {
	body := n &^ 3
	if body < 4 {
		return 0
	}
	fmadd5NEON(&out[0], &x[0], w0, w1, w2, w3, w4, body)
	return body
}

//go:noescape
func bilinear4NEON(out, a, b, c, d *float32, omx, wx, omy, wy float32, n int)

//go:noescape
func affineNEON(out, x *float32, scale, bias float32, n int)

//go:noescape
func transpose4x4F32(dst *float32, ldDst int, src *float32, ldSrc int)

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
	body := n &^ 3
	if body < 4 {
		return 0
	}
	bilinear4NEON(&out[0], &a[0], &b[0], &c[0], &d[0], omx, wx, omy, wy, body)
	return body
}

func affineVec(out, x []float32, scale, bias float32) int {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	body := n &^ 3
	if body < 4 {
		return 0
	}
	affineNEON(&out[0], &x[0], scale, bias, body)
	return body
}

func transposeMat4(dst, src []float32, outer, A, B int) bool {
	if A < 4 || B < 4 {
		return false
	}
	// Same one-level rule as transposeMat8: a worker that waits on a nested
	// ParallelRanges deadlocks the fixed pool.
	if outer >= 2 && outer*A*B >= 8192 {
		xt.ParallelRanges(outer, func(start, end int) {
			for o := start; o < end; o++ {
				transposeMat4Band(dst[o*A*B:], src[o*A*B:], A, B, 0, A)
			}
		})
		return true
	}
	for o := 0; o < outer; o++ {
		transposeMat4Outer(dst[o*A*B:], src[o*A*B:], A, B)
	}
	return true
}

func transposeMat4Outer(dst, src []float32, A, B int) {
	tiles := A / 4
	if tiles >= 4 && A*B >= 8192 {
		xt.ParallelRanges(tiles, func(start, end int) {
			transposeMat4Band(dst, src, A, B, start*4, end*4)
		})
		if tail := tiles * 4; tail < A {
			transposeMat4Band(dst, src, A, B, tail, A)
		}
		return
	}
	transposeMat4Band(dst, src, A, B, 0, A)
}

func transposeMat4Band(dst, src []float32, A, B, a0, a1 int) {
	a := a0
	for ; a+4 <= a1; a += 4 {
		b := 0
		for ; b+4 <= B; b += 4 {
			transpose4x4F32(&dst[b*A+a], A, &src[a*B+b], B)
		}
		for ; b < B; b++ {
			for i := 0; i < 4; i++ {
				dst[b*A+a+i] = src[(a+i)*B+b]
			}
		}
	}
	for ; a < a1; a++ {
		for b := 0; b < B; b++ {
			dst[b*A+a] = src[a*B+b]
		}
	}
}
