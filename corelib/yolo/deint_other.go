//go:build !amd64

package yolo

// deinterleave2Stride writes dst[j] = src[2*j] for j in [0, n) (portable;
// srcLimit is unused — the scalar walk never reads past src[2*(n-1)]).
func deinterleave2Stride(dst, src []float32, n, srcLimit int) {
	for j := 0; j < n; j++ {
		dst[j] = src[2*j]
	}
}
