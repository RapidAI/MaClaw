package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_NSQ_wrapper_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, psIndices *SideInfoIndices, psNSQ *silk_nsq_state, pulses []int8, x []float32) {
	silk.NSQ_wrapper_FLP(psEnc, psEncCtrl, psIndices, psNSQ, pulses, x)
}
