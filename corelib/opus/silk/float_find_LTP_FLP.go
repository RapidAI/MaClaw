package silk

func Find_LTP_FLP(XX [100]float32, xX [20]float32, r_ptr []float32, lag [4]int, subfr_length int, nb_subfr int) {
	var (
		xx   float32
		temp float32
	)
	xX_ptr := xX[:]
	XX_ptr := XX[:]
	for k := 0; k < nb_subfr; k++ {
		lag_ptr := sliceFromNeg(r_ptr, lag[k]+int(LTP_ORDER/2))
		silk_corrMatrix_FLP([]float32(lag_ptr), subfr_length, LTP_ORDER, []float32(XX_ptr))
		silk_corrVector_FLP([]float32(lag_ptr), r_ptr, subfr_length, LTP_ORDER, []float32(xX_ptr))
		xx = float32(silk_energy_FLP(r_ptr, subfr_length+LTP_ORDER))
		temp = 1.0 / (func() float32 {
			if xx > (LTP_CORR_INV_MAX*0.5*(XX_ptr[0]+XX_ptr[24]) + 1.0) {
				return xx
			}
			return LTP_CORR_INV_MAX*0.5*(XX_ptr[0]+XX_ptr[24]) + 1.0
		}())
		silk_scale_vector_FLP(XX_ptr, temp, int(LTP_ORDER*LTP_ORDER))
		silk_scale_vector_FLP(xX_ptr, temp, LTP_ORDER)
		r_ptr = r_ptr[subfr_length:]
		XX_ptr = XX_ptr[LTP_ORDER*LTP_ORDER:]
		xX_ptr = xX_ptr[LTP_ORDER:]
	}
}
