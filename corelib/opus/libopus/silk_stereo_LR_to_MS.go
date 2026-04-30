package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_stereo_LR_to_MS(state *stereo_enc_state, x1 []int16, x2 []int16, ix [2][3]int8, mid_only_flag *int8, mid_side_rates_bps []int32, total_rate_bps int32, prev_speech_act_Q8 int, toMono int, fs_kHz int, frame_length int) {
	silk.StereoLRtoMS(state, x1, x2, ix, mid_only_flag, mid_side_rates_bps, total_rate_bps, prev_speech_act_Q8, toMono, fs_kHz, frame_length)
}
