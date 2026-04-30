package libopus

import "math"

func silk_stereo_quant_pred(pred_Q13 []int32, ix [2][3]int8) {
	var (
		i              int
		j              int
		n              int
		low_Q13        int32
		step_Q13       int32
		lvl_Q13        int32
		err_min_Q13    int32
		err_Q13        int32
		quant_pred_Q13 int32 = 0
	)
	for n = 0; n < 2; n++ {
		err_min_Q13 = silk_int32_MAX
		for i = 0; i < int(STEREO_QUANT_TAB_SIZE-1); i++ {
			low_Q13 = int32(silk_stereo_pred_quant_Q13[i])
			step_Q13 = int32(((int(silk_stereo_pred_quant_Q13[i+1]) - int(low_Q13)) * int(int64(int16(int32(math.Floor((0.5/STEREO_QUANT_SUB_STEPS)*(1<<16)+0.5)))))) >> 16)
			for j = 0; j < STEREO_QUANT_SUB_STEPS; j++ {
				lvl_Q13 = int32(int(low_Q13) + int(int32(int16(step_Q13)))*int(int32(int16(j*2+1))))
				if (int(pred_Q13[n]) - int(lvl_Q13)) > 0 {
					err_Q13 = int32(int(pred_Q13[n]) - int(lvl_Q13))
				} else {
					err_Q13 = int32(-(int(pred_Q13[n]) - int(lvl_Q13)))
				}
				if int(err_Q13) < int(err_min_Q13) {
					err_min_Q13 = err_Q13
					quant_pred_Q13 = lvl_Q13
					ix[n][0] = int8(i)
					ix[n][1] = int8(j)
				} else {
					goto done
				}
			}
		}
	done:
		ix[n][2] = int8(int32(int(ix[n][0]) / 3))
		ix[n][0] -= int8(int(ix[n][2]) * 3)
		pred_Q13[n] = quant_pred_Q13
	}
	pred_Q13[0] -= pred_Q13[1]
}
