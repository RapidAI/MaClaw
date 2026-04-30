package silk

func silk_warped_autocorrelation_FLP(corr []float32, input []float32, warping float32, length int, order int) {
	var (
		n     int
		i     int
		tmp1  float64
		tmp2  float64
		state [25]float64 = [25]float64{}
		C     [25]float64 = [25]float64{}
	)
	for n = 0; n < length; n++ {
		tmp1 = float64(input[n])
		for i = 0; i < order; i += 2 {
			tmp2 = state[i] + float64(warping)*(state[i+1]-tmp1)
			state[i] = tmp1
			C[i] += state[0] * tmp1
			tmp1 = state[i+1] + float64(warping)*(state[i+2]-tmp2)
			state[i+1] = tmp2
			C[i+1] += state[0] * tmp2
		}
		state[order] = tmp1
		C[order] += state[0] * tmp1
	}
	for i = 0; i < order+1; i++ {
		corr[i] = float32(C[i])
	}
}
