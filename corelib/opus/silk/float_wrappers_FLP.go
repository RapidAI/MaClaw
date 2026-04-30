package silk

import "unsafe"

func A2NLSF_FLP(NLSF_Q15 []int16, pAR []float32, LPC_order int) {
	var (
		i         int
		a_fix_Q16 [16]int32
	)
	for i = 0; i < LPC_order; i++ {
		a_fix_Q16[i] = int32(pAR[i] * 65536.0)
	}
	A2NLSF(NLSF_Q15, a_fix_Q16[:], LPC_order)
}
func NLSF2A_FLP(pAR []float32, NLSF_Q15 []int16, LPC_order int, arch int) {
	var (
		i         int
		a_fix_Q12 [16]int16
	)
	NLSF2A(a_fix_Q12[:], NLSF_Q15, LPC_order, arch)
	for i = 0; i < LPC_order; i++ {
		pAR[i] = float32(a_fix_Q12[i]) * (1.0 / 4096.0)
	}
}
func ProcessNLSFs_FLP(psEncC *EncoderState, PredCoef [2][16]float32, NLSF_Q15 [16]int16, prev_NLSF_Q15 [16]int16) {
	var (
		i            int
		j            int
		PredCoef_Q12 [2][16]int16
	)
	ProcessNLSFs(psEncC, PredCoef_Q12, NLSF_Q15, prev_NLSF_Q15)
	for j = 0; j < 2; j++ {
		for i = 0; i < psEncC.PredictLPCOrder; i++ {
			PredCoef[j][i] = float32(PredCoef_Q12[j][i]) * (1.0 / 4096.0)
		}
	}
}
func NSQ_wrapper_FLP(psEnc *EncoderStateFLP, psEncCtrl *EncoderControlFLP, psIndices *SideInfoIndices, psNSQ *NSQState, pulses []int8, x []float32) {
	var (
		i                 int
		j                 int
		x16               [320]int16
		Gains_Q16         [4]int32
		PredCoef_Q12      [2][16]int16
		LTPCoef_Q14       [20]int16
		LTP_scale_Q14     int
		AR_Q13            [96]int16
		LF_shp_Q14        [4]int32
		Lambda_Q10        int
		Tilt_Q14          [4]int
		HarmShapeGain_Q14 [4]int
	)
	for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
		for j = 0; j < psEnc.SCmn.ShapingLPCOrder; j++ {
			AR_Q13[i*MAX_SHAPE_LPC_ORDER+j] = int16(int32(psEncCtrl.AR[i*MAX_SHAPE_LPC_ORDER+j] * 8192.0))
		}
	}
	for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
		LF_shp_Q14[i] = int32(int(int32(int(uint32(int32(psEncCtrl.LF_AR_shp[i]*16384.0)))<<16)) | int(uint16(int16(int32(psEncCtrl.LF_MA_shp[i]*16384.0)))))
		Tilt_Q14[i] = int(int32(psEncCtrl.Tilt[i] * 16384.0))
		HarmShapeGain_Q14[i] = int(int32(psEncCtrl.HarmShapeGain[i] * 16384.0))
	}
	Lambda_Q10 = int(int32(psEncCtrl.Lambda * 1024.0))
	for i = 0; i < psEnc.SCmn.Nb_subfr*LTP_ORDER; i++ {
		LTPCoef_Q14[i] = int16(int32(psEncCtrl.LTPCoef[i] * 16384.0))
	}
	for j = 0; j < 2; j++ {
		for i = 0; i < psEnc.SCmn.PredictLPCOrder; i++ {
			PredCoef_Q12[j][i] = int16(int32(psEncCtrl.PredCoef[j][i] * 4096.0))
		}
	}
	for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
		Gains_Q16[i] = int32(psEncCtrl.Gains[i] * 65536.0)
	}
	if int(psIndices.SignalType) == TYPE_VOICED {
		LTP_scale_Q14 = int(silk_LTPScales_table_Q14[psIndices.LTP_scaleIndex])
	} else {
		LTP_scale_Q14 = 0
	}
	for i = 0; i < psEnc.SCmn.Frame_length; i++ {
		x16[i] = int16(int32(x[i]))
	}
	if psEnc.SCmn.NStatesDelayedDecision > 1 || psEnc.SCmn.Warping_Q16 > 0 {
		NSQ_del_dec_c(&psEnc.SCmn, psNSQ, psIndices, x16[:], pulses, *(*[32]int16)(unsafe.Pointer(&PredCoef_Q12[0])), LTPCoef_Q14, AR_Q13, HarmShapeGain_Q14, Tilt_Q14, LF_shp_Q14, Gains_Q16, psEncCtrl.PitchL, Lambda_Q10, LTP_scale_Q14)
	} else {
		NSQ_c(&psEnc.SCmn, psNSQ, psIndices, x16[:], pulses, *(*[32]int16)(unsafe.Pointer(&PredCoef_Q12[0])), LTPCoef_Q14, AR_Q13, HarmShapeGain_Q14, Tilt_Q14, LF_shp_Q14, Gains_Q16, psEncCtrl.PitchL, Lambda_Q10, LTP_scale_Q14)
	}
}
func QuantLTPGains_FLP(B [20]float32, cbk_index [4]int8, periodicity_index *int8, sum_log_gain_Q7 *int32, pred_gain_dB *float32, XX [100]float32, xX [20]float32, subfr_len int, nb_subfr int, arch int) {
	var (
		i               int
		pred_gain_dB_Q7 int
		B_Q14           [20]int16
		XX_Q17          [100]int32
		xX_Q17          [20]int32
	)
	i = 0
	for {
		XX_Q17[i] = int32(XX[i] * 131072.0)
		if func() int {
			p := &i
			*p++
			return *p
		}() >= nb_subfr*LTP_ORDER*LTP_ORDER {
			break
		}
	}
	i = 0
	for {
		xX_Q17[i] = int32(xX[i] * 131072.0)
		if func() int {
			p := &i
			*p++
			return *p
		}() >= nb_subfr*LTP_ORDER {
			break
		}
	}
	QuantLTPGains(B_Q14, cbk_index, periodicity_index, sum_log_gain_Q7, &pred_gain_dB_Q7, XX_Q17, xX_Q17, subfr_len, nb_subfr, arch)
	for i = 0; i < nb_subfr*LTP_ORDER; i++ {
		B[i] = float32(B_Q14[i]) * (1.0 / 16384.0)
	}
	*pred_gain_dB = float32(pred_gain_dB_Q7) * (1.0 / 128.0)
}
