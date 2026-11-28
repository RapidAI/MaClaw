//go:build !amd64 && !arm64

package kokoro

func dotStridedFMA4x2(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3, r4, r5, r6, r7 float32) {
	return dotStridedFMA4x2Scalar(x, xStride, w, wStride, wOcStride, taps, n)
}

func dotStridedFMA4(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3 float32) {
	return dotStridedFMA4Scalar(x, xStride, w, wStride, wOcStride, taps, n)
}
