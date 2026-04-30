package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_find_LPC_FLP(psEncC *silk_encoder_state, NLSF_Q15 []int16, x []float32, minInvGain float32) {
	silk.Find_LPC_FLP(psEncC, NLSF_Q15, x, minInvGain)
}
