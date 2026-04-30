package silk

import (
	"math"
)

func NLSF_residual_dequant(x_Q10 []int16, indices []int8, pred_coef_Q8 []uint8, quant_step_size_Q16 int, order int16) {
	var out_Q10 int
	for i := int(order) - 1; i >= 0; i-- {
		pred_Q10 := (int(int32(int16(out_Q10))) * int(int32(int16(pred_coef_Q8[i])))) >> 8
		out_Q10 = int(int32(int(uint32(indices[i])) << 10))
		if out_Q10 > 0 {
			out_Q10 = out_Q10 - int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5)))
		} else if out_Q10 < 0 {
			out_Q10 = out_Q10 + int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5)))
		}
		out_Q10 = int(int32(pred_Q10 + int((int64(int32(out_Q10))*int64(int16(quant_step_size_Q16)))>>16)))
		x_Q10[i] = int16(out_Q10)
	}
}
func NLSF_decode(pNLSF_Q15 []int16, NLSFIndices []int8, psNLSF_CB *NLSF_CB) {
	var (
		pred_Q8 [16]uint8
		ec_ix   [16]int16
		res_Q10 [16]int16
	)
	NLSF_unpack(ec_ix[:], pred_Q8[:], psNLSF_CB, int(NLSFIndices[0]))
	NLSF_residual_dequant(res_Q10[:], NLSFIndices[1:], pred_Q8[:], int(psNLSF_CB.QuantStepSize_Q16), psNLSF_CB.Order)
	pCB_element := psNLSF_CB.CB1_NLSF_Q8[int(NLSFIndices[0])*int(psNLSF_CB.Order):]
	pCB_Wght_Q9 := psNLSF_CB.CB1_Wght_Q9[int(NLSFIndices[0])*int(psNLSF_CB.Order):]
	for i := 0; i < int(psNLSF_CB.Order); i++ {
		NLSF_Q15_tmp := int32(int(int32(int(int32(int(uint32(int32(res_Q10[i])))<<14))/int(pCB_Wght_Q9[i]))) + int(int32(int(uint32(int16(pCB_element[i])))<<7)))
		if 0 > math.MaxInt16 {
			if int(NLSF_Q15_tmp) > 0 {
				pNLSF_Q15[i] = 0
			} else if int(NLSF_Q15_tmp) < math.MaxInt16 {
				pNLSF_Q15[i] = math.MaxInt16
			} else {
				pNLSF_Q15[i] = int16(NLSF_Q15_tmp)
			}
		} else {
			if int(NLSF_Q15_tmp) > math.MaxInt16 {
				pNLSF_Q15[i] = math.MaxInt16
			} else if int(NLSF_Q15_tmp) < 0 {
				pNLSF_Q15[i] = 0
			} else {
				pNLSF_Q15[i] = int16(NLSF_Q15_tmp)
			}
		}
	}
	NLSF_stabilize(pNLSF_Q15, psNLSF_CB.DeltaMin_Q15, int(psNLSF_CB.Order))
}
