package silk

import (
	"math"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func StereoDecodePred(psRangeDec *entcode.Decoder, pred_Q13 []int32) {
	var ix [2][3]int
	n := psRangeDec.DecIcdf(silk_stereo_pred_joint_iCDF[:], 8)
	ix[0][2] = int(int32(n / 5))
	ix[1][2] = n - ix[0][2]*5
	for n := 0; n < 2; n++ {
		ix[n][0] = psRangeDec.DecIcdf(silk_uniform3_iCDF[:], 8)
		ix[n][1] = psRangeDec.DecIcdf(silk_uniform5_iCDF[:], 8)
	}
	for n := 0; n < 2; n++ {
		ix[n][0] += ix[n][2] * 3
		low_Q13 := int32(silk_stereo_pred_quant_Q13[ix[n][0]])
		step_Q13 := int32(((int(silk_stereo_pred_quant_Q13[ix[n][0]+1]) - int(low_Q13)) * int(int64(int16(int32(math.Floor((0.5/STEREO_QUANT_SUB_STEPS)*(1<<16)+0.5)))))) >> 16)
		pred_Q13[n] = int32(int(low_Q13) + int(int32(int16(step_Q13)))*int(int32(int16(ix[n][1]*2+1))))
	}
	pred_Q13[0] -= pred_Q13[1]
}
func StereoDecodeMidOnly(psRangeDec *entcode.Decoder) int {
	return psRangeDec.DecIcdf(silk_stereo_only_code_mid_iCDF[:], 8)
}
