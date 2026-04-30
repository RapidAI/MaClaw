package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_A2NLSF(NLSF []int16, a_Q16 []int32, d int) {
	silk.A2NLSF(NLSF, a_Q16, d)
}
