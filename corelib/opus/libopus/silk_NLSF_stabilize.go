package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_stabilize(NLSF_Q15 []int16, NDeltaMin_Q15 []int16, L int) {
	silk.NLSF_stabilize(NLSF_Q15, NDeltaMin_Q15, L)
}
