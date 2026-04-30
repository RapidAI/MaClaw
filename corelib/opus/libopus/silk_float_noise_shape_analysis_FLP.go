package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_noise_shape_analysis_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, pitch_res []float32, x []float32) {
	silk.NoiseShapeAnalysis_FLP(psEnc, psEncCtrl, pitch_res, x)
}
