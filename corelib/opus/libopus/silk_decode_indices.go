package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_decode_indices(psDec *silk_decoder_state, psRangeDec *ec_dec, FrameIndex int, decode_LBRR int, condCoding int) {
	silk.DecodeIndices(psDec, psRangeDec, FrameIndex, decode_LBRR != 0, condCoding)
}
