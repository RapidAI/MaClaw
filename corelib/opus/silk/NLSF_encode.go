package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func NLSF_encode(NLSFIndices []int8, pNLSF_Q15 []int16, psNLSF_CB *NLSF_CB, pW_Q2 []int16, NLSF_mu_Q20 int, nSurvivors int, signalType int) int32 {
	var (
		ind1         int
		prob_Q8      int
		bits_q7      int
		W_tmp_Q9     int32
		ret          int32
		res_Q10      [16]int16
		NLSF_tmp_Q15 [16]int16
		W_adj_Q5     [16]int16
		pred_Q8      [16]uint8
		ec_ix        [16]int16
	)
	NLSF_stabilize(pNLSF_Q15, psNLSF_CB.DeltaMin_Q15, int(psNLSF_CB.Order))
	err_Q24 := make([]int32, int(uintptr(psNLSF_CB.NVectors)))
	NLSF_VQ([]int32(err_Q24), pNLSF_Q15, []uint8(psNLSF_CB.CB1_NLSF_Q8), psNLSF_CB.CB1_Wght_Q9, int(psNLSF_CB.NVectors), int(psNLSF_CB.Order))
	tempIndices1 := make([]int, nSurvivors)
	silk_insertion_sort_increasing([]int32(err_Q24), []int(tempIndices1), int(psNLSF_CB.NVectors), nSurvivors)
	RD_Q25 := make([]int32, nSurvivors)
	tempIndices2 := make([]int8, nSurvivors*MAX_LPC_ORDER)
	for s := 0; s < nSurvivors; s++ {
		ind1 = tempIndices1[s]
		pCB_element := psNLSF_CB.CB1_NLSF_Q8[ind1*int(psNLSF_CB.Order):]
		pCB_Wght_Q9 := psNLSF_CB.CB1_Wght_Q9[ind1*int(psNLSF_CB.Order):]
		for i := 0; i < int(psNLSF_CB.Order); i++ {
			NLSF_tmp_Q15[i] = int16(int(uint16(int16(pCB_element[i]))) << 7)
			W_tmp_Q9 = int32(pCB_Wght_Q9[i])
			res_Q10[i] = int16((int(int32(int16(int(pNLSF_Q15[i])-int(NLSF_tmp_Q15[i])))) * int(int32(int16(W_tmp_Q9)))) >> 14)
			W_adj_Q5[i] = int16(silk_DIV32_varQ(int32(pW_Q2[i]), int32(int(int32(int16(W_tmp_Q9)))*int(int32(int16(W_tmp_Q9)))), 21))
		}
		NLSF_unpack(ec_ix[:], pred_Q8[:], psNLSF_CB, ind1)
		RD_Q25[s] = NLSF_del_dec_quant(tempIndices2[s*MAX_LPC_ORDER:], res_Q10[:], W_adj_Q5[:], pred_Q8[:], ec_ix[:], psNLSF_CB.Ec_Rates_Q5, int(psNLSF_CB.QuantStepSize_Q16), psNLSF_CB.InvQuantStepSize_Q6, int32(NLSF_mu_Q20), psNLSF_CB.Order)
		iCDF_ptr := psNLSF_CB.CB1_iCDF[(signalType>>1)*int(psNLSF_CB.NVectors):]
		if ind1 == 0 {
			prob_Q8 = 256 - int(iCDF_ptr[ind1])
		} else {
			prob_Q8 = int(iCDF_ptr[ind1-1]) - int(iCDF_ptr[ind1])
		}
		bits_q7 = (8 << 7) - int(silk_lin2log(int32(prob_Q8)))
		RD_Q25[s] = int32(int(RD_Q25[s]) + int(int32(int16(bits_q7)))*int(int32(int16(NLSF_mu_Q20>>2))))
	}
	var bestIndexArr [1]int
	silk_insertion_sort_increasing([]int32(RD_Q25), bestIndexArr[:], nSurvivors, 1)
	bestIndex := bestIndexArr[0]
	NLSFIndices[0] = int8(tempIndices1[bestIndex])
	libc.MemCpy(unsafe.Pointer(&NLSFIndices[1]), unsafe.Pointer(&tempIndices2[bestIndex*MAX_LPC_ORDER]), int(uintptr(psNLSF_CB.Order)*unsafe.Sizeof(int8(0))))
	NLSF_decode(pNLSF_Q15, NLSFIndices, psNLSF_CB)
	ret = RD_Q25[0]
	return ret
}
