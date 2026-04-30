package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_encode_pulses(psRangeEnc *ec_enc, signalType int, quantOffsetType int, pulses []int8, frame_length int) {
	silk.EncodePulses(psRangeEnc, signalType, quantOffsetType, pulses, frame_length)
}
