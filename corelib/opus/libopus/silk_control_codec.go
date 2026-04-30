package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_control_encoder(psEnc *silk_encoder_state_FLP, encControl *silk_EncControlStruct, allow_bw_switch int, channelNb int, force_fs_kHz int) int {
	return silk.ControlEncoder(psEnc, encControl, allow_bw_switch, channelNb, force_fs_kHz)
}
