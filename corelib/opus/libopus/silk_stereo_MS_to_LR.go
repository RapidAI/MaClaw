package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_stereo_MS_to_LR(state *stereo_dec_state, x1 []int16, x2 []int16, pred_Q13 []int32, fs_kHz int, frame_length int) {
	silk.StereoMStoLR(state, x1, x2, pred_Q13, fs_kHz, frame_length)
}
