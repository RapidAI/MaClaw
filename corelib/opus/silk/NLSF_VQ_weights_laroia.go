package silk

import "math"

func silk_NLSF_VQ_weights_laroia(pNLSFW_Q_OUT []int16, pNLSF_Q15 []int16, D int) {
	var (
		k        int
		tmp1_int int32
		tmp2_int int32
	)
	tmp1_int = int32(silk_max_int(int(pNLSF_Q15[0]), 1))
	tmp1_int = int32((1 << (int(NLSF_W_Q + 15))) / int(tmp1_int))
	tmp2_int = int32(silk_max_int(int(pNLSF_Q15[1])-int(pNLSF_Q15[0]), 1))
	tmp2_int = int32((1 << (int(NLSF_W_Q + 15))) / int(tmp2_int))
	pNLSFW_Q_OUT[0] = int16(silk_min_int(int(tmp1_int)+int(tmp2_int), math.MaxInt16))
	for k = 1; k < D-1; k += 2 {
		tmp1_int = int32(silk_max_int(int(pNLSF_Q15[k+1])-int(pNLSF_Q15[k]), 1))
		tmp1_int = int32((1 << (int(NLSF_W_Q + 15))) / int(tmp1_int))
		pNLSFW_Q_OUT[k] = int16(silk_min_int(int(tmp1_int)+int(tmp2_int), math.MaxInt16))
		tmp2_int = int32(silk_max_int(int(pNLSF_Q15[k+2])-int(pNLSF_Q15[k+1]), 1))
		tmp2_int = int32((1 << (int(NLSF_W_Q + 15))) / int(tmp2_int))
		pNLSFW_Q_OUT[k+1] = int16(silk_min_int(int(tmp1_int)+int(tmp2_int), math.MaxInt16))
	}
	tmp1_int = int32(silk_max_int((1<<15)-int(pNLSF_Q15[D-1]), 1))
	tmp1_int = int32((1 << (int(NLSF_W_Q + 15))) / int(tmp1_int))
	pNLSFW_Q_OUT[D-1] = int16(silk_min_int(int(tmp1_int)+int(tmp2_int), math.MaxInt16))
}
