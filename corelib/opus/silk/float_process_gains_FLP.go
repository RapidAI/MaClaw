package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func Process_gains_FLP(psEnc *EncoderStateFLP, psEncCtrl *EncoderControlFLP, condCoding int) {
	var (
		psShapeSt    = &psEnc.SShape
		k            int
		pGains_Q16   [4]int32
		s            float32
		InvMaxSqrVal float32
		gain         float32
		quant_offset float32
	)
	if int(psEnc.SCmn.Indices.SignalType) == TYPE_VOICED {
		s = 1.0 - silk_sigmoid((psEncCtrl.LTPredCodGain-12.0)*0.25)*0.5
		for k = 0; k < psEnc.SCmn.Nb_subfr; k++ {
			psEncCtrl.Gains[k] *= s
		}
	}
	InvMaxSqrVal = float32(math.Pow(2.0, (21.0-float64(psEnc.SCmn.SNR_dB_Q7)*(1/128.0))*0.33) / float64(psEnc.SCmn.Subfr_length))
	for k = 0; k < psEnc.SCmn.Nb_subfr; k++ {
		gain = psEncCtrl.Gains[k]
		gain = float32(math.Sqrt(float64(gain*gain + psEncCtrl.ResNrg[k]*InvMaxSqrVal)))
		if gain < 32767.0 {
			psEncCtrl.Gains[k] = gain
		} else {
			psEncCtrl.Gains[k] = 32767.0
		}
	}
	for k = 0; k < psEnc.SCmn.Nb_subfr; k++ {
		pGains_Q16[k] = int32(psEncCtrl.Gains[k] * 65536.0)
	}
	libc.MemCpy(unsafe.Pointer(&psEncCtrl.GainsUnq_Q16[0]), unsafe.Pointer(&pGains_Q16[0]), psEnc.SCmn.Nb_subfr*int(unsafe.Sizeof(int32(0))))
	psEncCtrl.LastGainIndexPrev = psShapeSt.LastGainIndex
	GainsQuant(psEnc.SCmn.Indices.GainsIndices, pGains_Q16, &psShapeSt.LastGainIndex, int(libc.BoolToInt(condCoding == CODE_CONDITIONALLY)), psEnc.SCmn.Nb_subfr)
	for k = 0; k < psEnc.SCmn.Nb_subfr; k++ {
		psEncCtrl.Gains[k] = float32(float64(pGains_Q16[k]) / 65536.0)
	}
	if int(psEnc.SCmn.Indices.SignalType) == TYPE_VOICED {
		if float64(psEncCtrl.LTPredCodGain)+float64(psEnc.SCmn.Input_tilt_Q15)*(1.0/32768.0) > 1.0 {
			psEnc.SCmn.Indices.QuantOffsetType = 0
		} else {
			psEnc.SCmn.Indices.QuantOffsetType = 1
		}
	}
	quant_offset = float32(float64(silk_Quantization_Offsets_Q10[int(psEnc.SCmn.Indices.SignalType)>>1][psEnc.SCmn.Indices.QuantOffsetType]) / 1024.0)
	psEncCtrl.Lambda = float32(LAMBDA_OFFSET + float64(psEnc.SCmn.NStatesDelayedDecision)*(-0.05) + float64(psEnc.SCmn.Speech_activity_Q8)*(-0.2)*(1.0/256.0) + float64(psEncCtrl.Input_quality*(-0.1)) + float64(psEncCtrl.Coding_quality*(-0.2)) + float64(LAMBDA_QUANT_OFFSET*quant_offset))
}
