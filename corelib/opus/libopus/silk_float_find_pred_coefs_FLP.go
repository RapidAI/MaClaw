package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_find_pred_coefs_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, res_pitch []float32, x []float32, condCoding int) {
	silk.Find_pred_coefs_FLP(psEnc, psEncCtrl, res_pitch, x, condCoding)
}
