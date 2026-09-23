//go:build amd64

package yolo

//go:noescape
func max5RowAVX512(out, x *float32, n int)

//go:noescape
func maxIntoAVX512(out, x *float32, n int)

// max5Row computes out[j] = max(x[j], ..., x[j+4]) for j in [0, n) without
// ever reading x[srcLimit:].
func max5Row(dst, src []float32, n, srcLimit int) {
	max5RowBody(dst, src, n, srcLimit, hasAVX512)
}

// maxInto accumulates dst[j] = max(dst[j], src[j]) for j in [0, n).
func maxInto(dst, src []float32, n int) {
	if n <= 0 {
		return
	}
	if hasAVX512 {
		body := n &^ 15
		if body >= 16 {
			maxIntoAVX512(&dst[0], &src[0], body)
		}
		for j := body; j < n; j++ {
			if src[j] > dst[j] {
				dst[j] = src[j]
			}
		}
		return
	}
	for j := 0; j < n; j++ {
		if src[j] > dst[j] {
			dst[j] = src[j]
		}
	}
}

func max5RowBody(dst, src []float32, n, srcLimit int, vec bool) {
	if n <= 0 {
		return
	}
	if vec {
		body := n &^ 15
		if v := srcLimit - 4; v < body {
			body = v
		}
		if body < 0 {
			body = 0
		}
		if body >= 16 {
			max5RowAVX512(&dst[0], &src[0], body)
		}
		for j := body; j < n; j++ {
			m := src[j]
			for t := 1; t < 5; t++ {
				if src[j+t] > m {
					m = src[j+t]
				}
			}
			dst[j] = m
		}
		return
	}
	for j := 0; j < n; j++ {
		m := src[j]
		for t := 1; t < 5; t++ {
			if src[j+t] > m {
				m = src[j+t]
			}
		}
		dst[j] = m
	}
}
