package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_encode_do_VAD_FLP(psEnc *silk_encoder_state_FLP, activity int) {
	silk.EncodeDoVAD_FLP(psEnc, activity)
}
func silk_encode_frame_FLP(psEnc *silk_encoder_state_FLP, pnBytesOut *int32, psRangeEnc *ec_enc, condCoding int, maxBits int, useCBR int) int {
	return silk.EncodeFrame_FLP(psEnc, pnBytesOut, psRangeEnc, condCoding, maxBits, useCBR)
}
