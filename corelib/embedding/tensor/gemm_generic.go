//go:build !amd64 && !arm64

package tensor

// multiDot4 computes 4 dots of consecutive A rows against B.
// a is [4][K] packed (row-major), b is [K], results written to out[0..3].
func multiDot4(out *[4]float32, a, b []float32, K int) {
	var s0, s1, s2, s3 float32
	for k := 0; k < K; k++ {
		bk := b[k]
		s0 += a[k] * bk
		s1 += a[K+k] * bk
		s2 += a[2*K+k] * bk
		s3 += a[3*K+k] * bk
	}
	out[0], out[1], out[2], out[3] = s0, s1, s2, s3
}

func multiDot8(out *[8]float32, a, b []float32, K int) {
	var d0, d1 [4]float32
	multiDot4(&d0, a[:4*K], b, K)
	multiDot4(&d1, a[4*K:8*K], b, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func multiDot4DualB(out *[8]float32, a, b0, b1 []float32, K int) {
	var d0, d1 [4]float32
	multiDot4(&d0, a, b0, K)
	multiDot4(&d1, a, b1, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func multiDot8DualB(out0, out1 *[8]float32, a, b0, b1 []float32, K int) {
	multiDot4DualB(out0, a[:4*K], b0, b1, K)
	multiDot4DualB(out1, a[4*K:8*K], b0, b1, K)
}

func multiDot2DualB(out *[4]float32, a, b0, b1 []float32, K int) {
	*out = [4]float32{}
	for k := 0; k < K; k++ {
		x0, x1 := a[k], a[K+k]
		out[0] += x0 * b0[k]
		out[1] += x1 * b0[k]
		out[2] += x0 * b1[k]
		out[3] += x1 * b1[k]
	}
}

func multiDot4TripleB(out *[12]float32, a, b0, b1, b2 []float32, K int) {
	var d8 [8]float32
	var d4 [4]float32
	multiDot4DualB(&d8, a, b0, b1, K)
	multiDot4(&d4, a, b2, K)
	out[0], out[1], out[2], out[3] = d8[0], d8[1], d8[2], d8[3]
	out[4], out[5], out[6], out[7] = d8[4], d8[5], d8[6], d8[7]
	out[8], out[9], out[10], out[11] = d4[0], d4[1], d4[2], d4[3]
}

// MultiDot4TripleB is the public API for 4 A x 3 B micro-kernel.
func MultiDot4TripleB(out *[12]float32, a, b0, b1, b2 []float32, K int) {
	multiDot4TripleB(out, a, b0, b1, b2, K)
}

func multiDot8TripleB(out0, out1 *[12]float32, a, b0, b1, b2 []float32, K int) {
	multiDot4TripleB(out0, a[:4*K], b0, b1, b2, K)
	multiDot4TripleB(out1, a[4*K:8*K], b0, b1, b2, K)
}

func multiDot8TripleReLU(out, a, b0, b1, b2 []float32, m, n, N, K int, bn0, bn1, bn2 float32) bool {
	return false
}

func multiDot8TriplePlain(out, a, b0, b1, b2 []float32, m, n, N, K int, bn0, bn1, bn2 float32) bool {
	return false
}

func multiDot8TripleArgmax(bestV []float32, bestI []int, a, b0, b1, b2 []float32, n, K int, bn0, bn1, bn2 float32) bool {
	return false
}

func multiDot8DualArgmax(bestV []float32, bestI []int, a, b0, b1 []float32, n, K int, bn0, bn1 float32) bool {
	return false
}

func multiDot8DualPlain(out, a, b0, b1 []float32, m, n, N, K int, bn0, bn1 float32) bool {
	return false
}

func multiDot8DualReLU(out, a, b0, b1 []float32, m, n, N, K int, bn0, bn1 float32) bool {
	return false
}
