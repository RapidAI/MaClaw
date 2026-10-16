//go:build amd64

package yolo

//go:noescape
func deinterleave2AVX512(dst, src *float32, n int)

// deinterleave2Stride writes dst[j] = src[2*j] for j in [0, n) without ever
// reading src[srcLimit:] (the vector kernel consumes full 32-float pairs, so
// the last vector block may need up to 2*body <= srcLimit).
func deinterleave2Stride(dst, src []float32, n, srcLimit int) {
	if n <= 0 {
		return
	}
	if hasAVX512 {
		body := n
		if v := srcLimit / 2; v < body {
			body = v
		}
		body &= ^15
		if body >= 16 {
			deinterleave2AVX512(&dst[0], &src[0], body)
		}
		for j := body; j < n; j++ {
			dst[j] = src[2*j]
		}
		return
	}
	for j := 0; j < n; j++ {
		dst[j] = src[2*j]
	}
}
