//go:build amd64

package yolo

//go:noescape
func attnExpAVX2(dst, src *float32, n int)

//go:noescape
func attnScaleAVX2(dst *float32, s float32, n int)

//go:noescape
func siluZmmAVX512(x *float32, n int)

// expSlice applies exp element-wise, AVX2 vectorized when available.
// In-place safe.
func expSlice(dst, src []float32) {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	if n == 0 {
		return
	}
	if hasAVX2FMA {
		body := n &^ 7
		if body >= 8 {
			attnExpAVX2(&dst[0], &src[0], body)
		}
		for i := body; i < n; i++ {
			dst[i] = expF32(src[i])
		}
		return
	}
	for i := 0; i < n; i++ {
		dst[i] = expF32(src[i])
	}
}

// scaleSlice multiplies every element by s in place, AVX2 when available.
func scaleSlice(dst []float32, s float32) {
	n := len(dst)
	if n == 0 {
		return
	}
	if hasAVX2FMA {
		body := n &^ 7
		if body >= 8 {
			attnScaleAVX2(&dst[0], s, body)
		}
		for i := body; i < n; i++ {
			dst[i] *= s
		}
		return
	}
	for i := range dst {
		dst[i] *= s
	}
}
