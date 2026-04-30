package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_find_pitch_lags_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, res []float32, x []float32, arch int) {
	silk.FindPitchLags_FLP(psEnc, psEncCtrl, res, x, arch)
}
