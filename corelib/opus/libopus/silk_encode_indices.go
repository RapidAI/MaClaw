package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_encode_indices(psEncC *silk_encoder_state, psRangeEnc *ec_enc, FrameIndex int, encode_LBRR int, condCoding int) {
	silk.EncodeIndices(psEncC, psRangeEnc, FrameIndex, encode_LBRR, condCoding)
}
