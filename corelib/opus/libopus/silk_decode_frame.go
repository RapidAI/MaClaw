package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_decode_frame(psDec *silk_decoder_state, psRangeDec *ec_dec, pOut []int16, pN *int32, lostFlag int, condCoding int, arch int) int {
	return silk.DecodeFrame(psDec, psRangeDec, pOut, pN, lostFlag, condCoding, arch)
}
