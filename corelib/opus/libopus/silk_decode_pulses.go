package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_decode_pulses(psRangeDec *ec_dec, pulses []int16, signalType int, quantOffsetType int, frame_length int) {
	silk.DecodePulses(psRangeDec, pulses, signalType, quantOffsetType, frame_length)
}
