package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func DecodeParameters(psDec *DecoderState, psDecCtrl *DecoderControl, condCoding int) {
	var (
		Ix         int
		pNLSF_Q15  [16]int16
		pNLSF0_Q15 [16]int16
		cbk_ptr_Q7 []int8
	)
	GainsDequant(psDecCtrl.Gains_Q16, psDec.Indices.GainsIndices, &psDec.LastGainIndex, int(libc.BoolToInt(condCoding == CODE_CONDITIONALLY)), psDec.Nb_subfr)
	NLSF_decode(pNLSF_Q15[:], psDec.Indices.NLSFIndices[:], psDec.PsNLSF_CB)
	NLSF2A(psDecCtrl.PredCoef_Q12[1][:], pNLSF_Q15[:], psDec.LPC_order, psDec.Arch)
	if psDec.First_frame_after_reset == 1 {
		psDec.Indices.NLSFInterpCoef_Q2 = 4
	}
	if int(psDec.Indices.NLSFInterpCoef_Q2) < 4 {
		for i := 0; i < psDec.LPC_order; i++ {
			pNLSF0_Q15[i] = int16(int(psDec.PrevNLSF_Q15[i]) + ((int(psDec.Indices.NLSFInterpCoef_Q2) * (int(pNLSF_Q15[i]) - int(psDec.PrevNLSF_Q15[i]))) >> 2))
		}
		NLSF2A(psDecCtrl.PredCoef_Q12[0][:], pNLSF0_Q15[:], psDec.LPC_order, psDec.Arch)
	} else {
		libc.MemCpy(unsafe.Pointer(&(psDecCtrl.PredCoef_Q12[0])[0]), unsafe.Pointer(&(psDecCtrl.PredCoef_Q12[1])[0]), psDec.LPC_order*int(unsafe.Sizeof(int16(0))))
	}
	libc.MemCpy(unsafe.Pointer(&psDec.PrevNLSF_Q15[0]), unsafe.Pointer(&pNLSF_Q15[0]), psDec.LPC_order*int(unsafe.Sizeof(int16(0))))
	if psDec.LossCnt != 0 {
		BwExpander(psDecCtrl.PredCoef_Q12[0][:], psDec.LPC_order, BWE_AFTER_LOSS_Q16)
		BwExpander(psDecCtrl.PredCoef_Q12[1][:], psDec.LPC_order, BWE_AFTER_LOSS_Q16)
	}
	if int(psDec.Indices.SignalType) == TYPE_VOICED {
		DecodePitch(psDec.Indices.LagIndex, psDec.Indices.ContourIndex, psDecCtrl.PitchL[:], psDec.Fs_kHz, psDec.Nb_subfr)
		cbk_ptr_Q7 = silk_LTP_vq_ptrs_Q7[psDec.Indices.PERIndex]
		for k := 0; k < psDec.Nb_subfr; k++ {
			Ix = int(psDec.Indices.LTPIndex[k])
			for i := 0; i < LTP_ORDER; i++ {
				psDecCtrl.LTPCoef_Q14[k*LTP_ORDER+i] = int16(int32(int(uint32(cbk_ptr_Q7[Ix*LTP_ORDER+i])) << 7))
			}
		}
		Ix = int(psDec.Indices.LTP_scaleIndex)
		psDecCtrl.LTP_scale_Q14 = int(silk_LTPScales_table_Q14[Ix])
	} else {
		libc.MemSet(unsafe.Pointer(&psDecCtrl.PitchL[0]), 0, psDec.Nb_subfr*int(unsafe.Sizeof(int(0))))
		libc.MemSet(unsafe.Pointer(&psDecCtrl.LTPCoef_Q14[0]), 0, LTP_ORDER*psDec.Nb_subfr*int(unsafe.Sizeof(int16(0))))
		psDec.Indices.PERIndex = 0
		psDecCtrl.LTP_scale_Q14 = 0
	}
}
