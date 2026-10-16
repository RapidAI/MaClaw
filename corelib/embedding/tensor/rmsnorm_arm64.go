//go:build arm64

package tensor

import "math"

//go:noescape
func sumSq16NEON(x *float32, n int) float32

//go:noescape
func scaleMul16NEON(out, x, w *float32, n int, scale float32)

// rmsNormAsm: NEON RMSNorm (vek has no arm64 SIMD; its Go fallback was
// ~3 scalar passes per row). n must be a positive multiple of 16.
func rmsNormAsm(out, x, weight []float32, eps float32) bool {
	n := len(x)
	if n == 0 || n%16 != 0 || len(out) < n || len(weight) < n {
		return false
	}
	ss := sumSq16NEON(&x[0], n)
	scale := 1.0 / float32(math.Sqrt(float64(ss/float32(n)+eps)))
	scaleMul16NEON(&out[0], &x[0], &weight[0], n, scale)
	return true
}
