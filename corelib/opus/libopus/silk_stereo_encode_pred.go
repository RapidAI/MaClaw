package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_stereo_encode_pred(psRangeEnc *ec_enc, ix [2][3]int8) {
	silk.StereoEncodePred(psRangeEnc, ix)
}
func silk_stereo_encode_mid_only(psRangeEnc *ec_enc, mid_only_flag int8) {
	silk.StereoEncodeMidOnly(psRangeEnc, mid_only_flag)
}
