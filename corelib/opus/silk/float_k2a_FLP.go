package silk

func silk_k2a_FLP(A []float32, rc []float32, order int32) {
	var (
		k    int
		n    int
		rck  float32
		tmp1 float32
		tmp2 float32
	)
	for k = 0; k < int(order); k++ {
		rck = rc[k]
		for n = 0; n < (k+1)>>1; n++ {
			tmp1 = A[n]
			tmp2 = A[k-n-1]
			A[n] = tmp1 + tmp2*rck
			A[k-n-1] = tmp2 + tmp1*rck
		}
		A[k] = -rck
	}
}
