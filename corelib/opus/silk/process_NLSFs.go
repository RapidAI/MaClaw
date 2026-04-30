package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func ProcessNLSFs(psEncC *EncoderState, PredCoef_Q12 [2][16]int16, pNLSF_Q15 [16]int16, prev_NLSFq_Q15 [16]int16) {
	var (
		i               int
		doInterpolate   int
		NLSF_mu_Q20     int
		i_sqr_Q15       int16
		pNLSF0_temp_Q15 [16]int16
		pNLSFW_QW       [16]int16
		pNLSFW0_temp_QW [16]int16
	)
	NLSF_mu_Q20 = int(int32(((int64(int16(psEncC.Speech_activity_Q8)) * int64(int32(math.Floor((-0.001)*(1<<28)+0.5)))) >> 16) + int64(int32(math.Floor(0.003*(1<<20)+0.5)))))
	if psEncC.Nb_subfr == 2 {
		NLSF_mu_Q20 = NLSF_mu_Q20 + (NLSF_mu_Q20 >> 1)
	}
	silk_NLSF_VQ_weights_laroia(pNLSFW_QW[:], pNLSF_Q15[:], psEncC.PredictLPCOrder)
	doInterpolate = int(libc.BoolToInt(psEncC.UseInterpolatedNLSFs == 1 && int(psEncC.Indices.NLSFInterpCoef_Q2) < 4))
	if doInterpolate != 0 {
		silk_interpolate(pNLSF0_temp_Q15, prev_NLSFq_Q15, pNLSF_Q15, int(psEncC.Indices.NLSFInterpCoef_Q2), psEncC.PredictLPCOrder)
		silk_NLSF_VQ_weights_laroia(pNLSFW0_temp_QW[:], pNLSF0_temp_Q15[:], psEncC.PredictLPCOrder)
		i_sqr_Q15 = int16(int32(int(uint32(int32(int(int32(int16(psEncC.Indices.NLSFInterpCoef_Q2)))*int(int32(int16(psEncC.Indices.NLSFInterpCoef_Q2)))))) << 11))
		for i = 0; i < psEncC.PredictLPCOrder; i++ {
			pNLSFW_QW[i] = int16((int(pNLSFW_QW[i]) >> 1) + ((int(int32(pNLSFW0_temp_QW[i])) * int(int32(i_sqr_Q15))) >> 16))
		}
	}
	NLSF_encode(psEncC.Indices.NLSFIndices[:], pNLSF_Q15[:], psEncC.PsNLSF_CB, pNLSFW_QW[:], NLSF_mu_Q20, psEncC.NLSF_MSVQ_Survivors, int(psEncC.Indices.SignalType))
	NLSF2A(PredCoef_Q12[1][:], pNLSF_Q15[:], psEncC.PredictLPCOrder, psEncC.Arch)
	if doInterpolate != 0 {
		silk_interpolate(pNLSF0_temp_Q15, prev_NLSFq_Q15, pNLSF_Q15, int(psEncC.Indices.NLSFInterpCoef_Q2), psEncC.PredictLPCOrder)
		NLSF2A(PredCoef_Q12[0][:], pNLSF0_temp_Q15[:], psEncC.PredictLPCOrder, psEncC.Arch)
	} else {
		libc.MemCpy(unsafe.Pointer(&(PredCoef_Q12[0])[0]), unsafe.Pointer(&(PredCoef_Q12[1])[0]), psEncC.PredictLPCOrder*int(unsafe.Sizeof(int16(0))))
	}
}
