package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_decode(pNLSF_Q15 []int16, NLSFIndices []int8, psNLSF_CB *silk_NLSF_CB_struct) {
	silk.NLSF_decode(pNLSF_Q15, NLSFIndices, psNLSF_CB)
}
