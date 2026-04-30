package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_encode(NLSFIndices []int8, pNLSF_Q15 []int16, psNLSF_CB *silk_NLSF_CB_struct, pW_Q2 []int16, NLSF_mu_Q20 int, nSurvivors int, signalType int) int32 {
	return silk.NLSF_encode(NLSFIndices, pNLSF_Q15, psNLSF_CB, pW_Q2, NLSF_mu_Q20, nSurvivors, signalType)
}
