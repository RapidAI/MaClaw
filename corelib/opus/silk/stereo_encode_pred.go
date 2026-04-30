package silk

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func StereoEncodePred(psRangeEnc *entcode.Encoder, ix [2][3]int8) {
	var n int
	n = int(ix[0][2])*5 + int(ix[1][2])
	psRangeEnc.EncIcdf(n, silk_stereo_pred_joint_iCDF[:], 8)
	for n = 0; n < 2; n++ {
		psRangeEnc.EncIcdf(int(ix[n][0]), silk_uniform3_iCDF[:], 8)
		psRangeEnc.EncIcdf(int(ix[n][1]), silk_uniform5_iCDF[:], 8)
	}
}
func StereoEncodeMidOnly(psRangeEnc *entcode.Encoder, mid_only_flag int8) {
	psRangeEnc.EncIcdf(int(mid_only_flag), silk_stereo_only_code_mid_iCDF[:], 8)
}
