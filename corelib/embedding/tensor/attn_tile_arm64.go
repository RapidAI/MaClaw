//go:build arm64

package tensor

//go:noescape
func attnQK4x4NEON(out *float32, ldSBytes int, q *float32, qStrideBytes int, k *float32, kStrideBytes int, nblk int, scale float32)

//go:noescape
func attnPV4NEON(out *float32, outStrideBytes int, pt *float32, v *float32, vStrideBytes int, n int)

//go:noescape
func softmaxExp4NEON(x *float32, n int, max, a, b, neg88 float32) float32

const attnTile4Avail = true

func attnQK4x4(out []float32, ldS int, q []float32, qStride int, k []float32, kStride, nblk int, scale float32) {
	_ = out[3*ldS+nblk*4-1]
	_ = q[3*qStride+127]
	_ = k[(nblk*4-1)*kStride+127]
	attnQK4x4NEON(&out[0], ldS*4, &q[0], qStride*4, &k[0], kStride*4, nblk, scale)
}

func attnPV4(out []float32, outStride int, pt, v []float32, vStride, n int) {
	_ = out[3*outStride+127]
	_ = pt[n*4-1]
	_ = v[(n-1)*vStride+127]
	attnPV4NEON(&out[0], outStride*4, &pt[0], &v[0], vStride*4, n)
}

func softmaxExpSum(x []float32, max float32) float32 {
	n4 := len(x) &^ 3
	var sum float32
	if n4 > 0 {
		sum = softmaxExp4NEON(&x[0], n4, max, 12102203.0, 1065353216.0-60801.0, -88)
	}
	for i := n4; i < len(x); i++ {
		v := fastExp(x[i] - max)
		x[i] = v
		sum += v
	}
	return sum
}
