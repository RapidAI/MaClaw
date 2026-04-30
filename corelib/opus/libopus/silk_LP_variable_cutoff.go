package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_LP_variable_cutoff(psLP *silk_LP_state, frame []int16, frame_length int) {
	silk.LP_variable_cutoff(psLP, frame, frame_length)
}
