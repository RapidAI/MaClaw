package silk

import "math"

func silk_stereo_find_predictor(ratio_Q14 *int32, x []int16, y []int16, mid_res_amp_Q0 []int32, length int, smooth_coef_Q16 int) int32 {
	var (
		scale     int
		scale1    int
		scale2    int
		nrgx      int32
		nrgy      int32
		corr      int32
		pred_Q13  int32
		pred2_Q10 int32
	)
	silk_sum_sqr_shift(&nrgx, &scale1, x, length)
	silk_sum_sqr_shift(&nrgy, &scale2, y, length)
	scale = silk_max_int(scale1, scale2)
	scale = scale + (scale & 1)
	nrgy = int32(int(nrgy) >> (scale - scale2))
	nrgx = int32(int(nrgx) >> (scale - scale1))
	nrgx = int32(silk_max_int(int(nrgx), 1))
	corr = silk_inner_prod_aligned_scale(x, y, scale, length)
	pred_Q13 = silk_DIV32_varQ(corr, nrgx, 13)
	if (-(1 << 14)) > (1 << 14) {
		if int(pred_Q13) > (-(1 << 14)) {
			pred_Q13 = -(1 << 14)
		} else if int(pred_Q13) < (1 << 14) {
			pred_Q13 = 1 << 14
		} else {
			pred_Q13 = pred_Q13
		}
	} else if int(pred_Q13) > (1 << 14) {
		pred_Q13 = 1 << 14
	} else if int(pred_Q13) < (-(1 << 14)) {
		pred_Q13 = -(1 << 14)
	} else {
		pred_Q13 = pred_Q13
	}
	pred2_Q10 = int32((int64(pred_Q13) * int64(int16(pred_Q13))) >> 16)
	smooth_coef_Q16 = silk_max_int(smooth_coef_Q16, func() int {
		if int(pred2_Q10) > 0 {
			return int(pred2_Q10)
		}
		return int(-pred2_Q10)
	}())
	scale = scale >> 1
	mid_res_amp_Q0[0] = int32(int(mid_res_amp_Q0[0]) + (((int(int32(int(uint32(silk_SQRT_APPROX(nrgx)))<<scale)) - int(mid_res_amp_Q0[0])) * int(int64(int16(smooth_coef_Q16)))) >> 16))
	nrgy = int32(int(nrgy) - int(int32(int(uint32(int32((int64(corr)*int64(int16(pred_Q13)))>>16)))<<(3+1))))
	nrgy = int32(int(nrgy) + int(int32(int(uint32(int32((int64(nrgx)*int64(int16(pred2_Q10)))>>16)))<<6)))
	mid_res_amp_Q0[1] = int32(int(mid_res_amp_Q0[1]) + (((int(int32(int(uint32(silk_SQRT_APPROX(nrgy)))<<scale)) - int(mid_res_amp_Q0[1])) * int(int64(int16(smooth_coef_Q16)))) >> 16))
	*ratio_Q14 = silk_DIV32_varQ(mid_res_amp_Q0[1], int32(func() int {
		if int(mid_res_amp_Q0[0]) > 1 {
			return int(mid_res_amp_Q0[0])
		}
		return 1
	}()), 14)
	if 0 > math.MaxInt16 {
		if int(*ratio_Q14) > 0 {
			*ratio_Q14 = 0
		} else if int(*ratio_Q14) < math.MaxInt16 {
			*ratio_Q14 = math.MaxInt16
		} else {
			*ratio_Q14 = *ratio_Q14
		}
	} else if int(*ratio_Q14) > math.MaxInt16 {
		*ratio_Q14 = math.MaxInt16
	} else if int(*ratio_Q14) < 0 {
		*ratio_Q14 = 0
	} else {
		*ratio_Q14 = *ratio_Q14
	}
	return pred_Q13
}
