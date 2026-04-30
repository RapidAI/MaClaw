package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF2A(a_Q12 []int16, NLSF []int16, d int, arch int) {
	silk.NLSF2A(a_Q12, NLSF, d, arch)
}
