package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

const SILK_RESAMPLER_MAX_FIR_ORDER = silk.SILK_RESAMPLER_MAX_FIR_ORDER
const SILK_RESAMPLER_MAX_IIR_ORDER = silk.SILK_RESAMPLER_MAX_IIR_ORDER

type silk_resampler_state_struct = silk.ResamplerState
