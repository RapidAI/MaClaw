package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NSQ_del_dec_c(psEncC *silk_encoder_state, NSQ *silk_nsq_state, psIndices *SideInfoIndices, x16 []int16, pulses []int8, PredCoef_Q12 [32]int16, LTPCoef_Q14 [20]int16, AR_Q13 [96]int16, HarmShapeGain_Q14 [4]int, Tilt_Q14 [4]int, LF_shp_Q14 [4]int32, Gains_Q16 [4]int32, pitchL [4]int, Lambda_Q10 int, LTP_scale_Q14 int) {
	silk.NSQ_del_dec_c(psEncC, NSQ, psIndices, x16, pulses, PredCoef_Q12, LTPCoef_Q14, AR_Q13, HarmShapeGain_Q14, Tilt_Q14, LF_shp_Q14, Gains_Q16, pitchL, Lambda_Q10, LTP_scale_Q14)
}
