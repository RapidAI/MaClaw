package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_bwexpander_32(ar []int32, d int, chirp_Q16 int32) {
	silk.BwExpander32(ar, d, chirp_Q16)
}
