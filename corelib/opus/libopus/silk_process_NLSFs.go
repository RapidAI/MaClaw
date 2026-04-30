package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_process_NLSFs(psEncC *silk_encoder_state, PredCoef_Q12 [2][16]int16, pNLSF_Q15 [16]int16, prev_NLSFq_Q15 [16]int16) {
	silk.ProcessNLSFs(psEncC, PredCoef_Q12, pNLSF_Q15, prev_NLSFq_Q15)
}
