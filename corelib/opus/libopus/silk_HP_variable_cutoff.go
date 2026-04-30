package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_HP_variable_cutoff(state_Fxx []silk_encoder_state_FLP) {
	silk.HP_variable_cutoff(state_Fxx)
}
