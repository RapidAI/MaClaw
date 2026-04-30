package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_resampler_down2_3(S []int32, out []int16, in []int16, inLen int32) {
	silk.ResamplerDown2_3(S, out, in, inLen)
}
