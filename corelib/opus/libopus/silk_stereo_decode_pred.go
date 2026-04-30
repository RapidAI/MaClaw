package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

func silk_stereo_decode_pred(psRangeDec *ec_dec, pred_Q13 []int32) {
	silk.StereoDecodePred(psRangeDec, pred_Q13)
}
func silk_stereo_decode_mid_only(psRangeDec *ec_dec, decode_only_mid *int) {
	*decode_only_mid = silk.StereoDecodeMidOnly(psRangeDec)
}
