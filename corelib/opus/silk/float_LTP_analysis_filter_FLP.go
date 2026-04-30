package silk

func LTP_analysis_filter_FLP(LTP_res []float32, x []float32, B [20]float32, pitchL [4]int, invGains [4]float32, subfr_length int, nb_subfr int, pre_length int) {
	var (
		Btmp     [5]float32
		inv_gain float32
	)
	x_ptr := x
	LTP_res_ptr := LTP_res
	for k := 0; k < nb_subfr; k++ {
		// In C: x_lag_ptr = x_ptr - pitchL[k], then x_lag_ptr[LTP_ORDER/2 - j] with j=0..4
		// The most negative access relative to x_ptr is: -pitchL[k] + (LTP_ORDER/2) - (LTP_ORDER-1) = -pitchL[k] - LTP_ORDER/2
		// We create a slice starting pitchL[k]+LTP_ORDER/2 elements before x_ptr[0].
		x_lag_ptr := sliceFromNeg(x_ptr, pitchL[k]+LTP_ORDER/2)
		inv_gain = invGains[k]
		for i := 0; i < LTP_ORDER; i++ {
			Btmp[i] = B[k*LTP_ORDER+i]
		}
		for i := 0; i < subfr_length+pre_length; i++ {
			LTP_res_ptr[i] = x_ptr[i]
			for j := 0; j < LTP_ORDER; j++ {
				// Original C: x_lag_ptr[LTP_ORDER/2 - j] where x_lag_ptr = x_ptr - pitchL[k] + i
				// With our shifted slice: index = (pitchL[k]+LTP_ORDER/2) + i + (LTP_ORDER/2 - j) - pitchL[k]
				//                               = LTP_ORDER/2 + i + LTP_ORDER/2 - j
				//                               = LTP_ORDER + i - j
				// Simpler: offset from x_ptr is -pitchL[k]+i+(LTP_ORDER/2-j)
				// In our slice (base at x_ptr[-pitchL[k]-LTP_ORDER/2]):
				//   index = (pitchL[k]+LTP_ORDER/2) + (-pitchL[k]+i+(LTP_ORDER/2-j))
				//         = LTP_ORDER/2 + i + LTP_ORDER/2 - j = LTP_ORDER + i - j
				LTP_res_ptr[i] -= Btmp[j] * x_lag_ptr[LTP_ORDER+i-j]
			}
			LTP_res_ptr[i] *= inv_gain
		}
		LTP_res_ptr = LTP_res_ptr[subfr_length+pre_length:]
		x_ptr = x_ptr[subfr_length:]
	}
}
