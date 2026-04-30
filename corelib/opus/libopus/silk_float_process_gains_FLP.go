package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_process_gains_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, condCoding int) {
	silk.Process_gains_FLP(psEnc, psEncCtrl, condCoding)
}
