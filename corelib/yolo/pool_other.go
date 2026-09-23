//go:build !amd64

package yolo

// max5Row computes out[j] = max(x[j], ..., x[j+4]) for j in [0, n)
// (portable).
func max5Row(dst, src []float32, n, srcLimit int) {
	max5RowBody(dst, src, n, srcLimit, false)
}

// maxInto accumulates dst[j] = max(dst[j], src[j]) for j in [0, n).
func maxInto(dst, src []float32, n int) {
	for j := 0; j < n; j++ {
		if src[j] > dst[j] {
			dst[j] = src[j]
		}
	}
}

func max5RowBody(dst, src []float32, n, srcLimit int, vec bool) {
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
