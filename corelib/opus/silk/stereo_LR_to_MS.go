package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func StereoLRtoMS(state *StereoEncState, x1 []int16, x2 []int16, ix [2][3]int8, mid_only_flag *int8, mid_side_rates_bps []int32, total_rate_bps int32, prev_speech_act_Q8 int, toMono int, fs_kHz int, frame_length int) {
	var (
		n                int
		is10msFrame      int
		denom_Q16        int
		delta0_Q13       int
		delta1_Q13       int
		sum              int32
		diff             int32
		smooth_coef_Q16  int32
		pred_Q13         [2]int32
		pred0_Q13        int32
		pred1_Q13        int32
		LP_ratio_Q14     int32
		HP_ratio_Q14     int32
		frac_Q16         int32
		frac_3_Q16       int32
		min_mid_rate_bps int32
		width_Q14        int32
		w_Q24            int32
		deltaw_Q24       int32
	)
	side := make([]int16, frame_length+2)
	// FIXME
	mid := unsafe.Slice((*int16)(unsafe.Add(unsafe.Pointer(&x1[0]), -2*2)), len(x1)+2)
	for n = 0; n < frame_length+2; n++ {
		sum = int32(int(x1[n-2]) + int(int32(x2[n-2])))
		diff = int32(int(x1[n-2]) - int(int32(x2[n-2])))
		if 1 == 1 {
			mid[n] = int16((int(sum) >> 1) + (int(sum) & 1))
		} else {
			mid[n] = int16(((int(sum) >> (1 - 1)) + 1) >> 1)
		}
		if (func() int {
			if 1 == 1 {
				return (int(diff) >> 1) + (int(diff) & 1)
			}
			return ((int(diff) >> (1 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			side[n] = math.MaxInt16
		} else if (func() int {
			if 1 == 1 {
				return (int(diff) >> 1) + (int(diff) & 1)
			}
			return ((int(diff) >> (1 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			side[n] = math.MinInt16
		} else if 1 == 1 {
			side[n] = int16((int(diff) >> 1) + (int(diff) & 1))
		} else {
			side[n] = int16(((int(diff) >> (1 - 1)) + 1) >> 1)
		}
	}
	copy(mid[:2], state.SMid[:])
	copy(side[:2], state.SSide[:])
	copy(state.SMid[:], mid[frame_length:frame_length+2])
	copy(state.SSide[:], side[frame_length:frame_length+2])
	LP_mid := make([]int16, frame_length)
	HP_mid := make([]int16, frame_length)
	for n = 0; n < frame_length; n++ {
		if 2 == 1 {
			sum = int32((((int(mid[n]) + int(int32(mid[n+2]))) + int(int32(int(uint32(mid[n+1]))<<1))) >> 1) + (((int(mid[n]) + int(int32(mid[n+2]))) + int(int32(int(uint32(mid[n+1]))<<1))) & 1))
		} else {
			sum = int32(((((int(mid[n]) + int(int32(mid[n+2]))) + int(int32(int(uint32(mid[n+1]))<<1))) >> (2 - 1)) + 1) >> 1)
		}
		LP_mid[n] = int16(sum)
		HP_mid[n] = int16(int(mid[n+1]) - int(sum))
	}
	LP_side := make([]int16, frame_length)
	HP_side := make([]int16, frame_length)
	for n = 0; n < frame_length; n++ {
		if 2 == 1 {
			sum = int32((((int(side[n]) + int(int32(side[n+2]))) + int(int32(int(uint32(side[n+1]))<<1))) >> 1) + (((int(side[n]) + int(int32(side[n+2]))) + int(int32(int(uint32(side[n+1]))<<1))) & 1))
		} else {
			sum = int32(((((int(side[n]) + int(int32(side[n+2]))) + int(int32(int(uint32(side[n+1]))<<1))) >> (2 - 1)) + 1) >> 1)
		}
		LP_side[n] = int16(sum)
		HP_side[n] = int16(int(side[n+1]) - int(sum))
	}
	is10msFrame = int(libc.BoolToInt(frame_length == fs_kHz*10))
	if is10msFrame != 0 {
		smooth_coef_Q16 = int32(math.Floor((STEREO_RATIO_SMOOTH_COEF/2)*(1<<16) + 0.5))
	} else {
		smooth_coef_Q16 = int32(math.Floor(STEREO_RATIO_SMOOTH_COEF*(1<<16) + 0.5))
	}
	smooth_coef_Q16 = int32(((int(int32(int16(prev_speech_act_Q8))) * int(int32(int16(prev_speech_act_Q8)))) * int(int64(int16(smooth_coef_Q16)))) >> 16)
	pred_Q13[0] = silk_stereo_find_predictor(&LP_ratio_Q14, []int16(LP_mid), []int16(LP_side), state.Mid_side_amp_Q0[0:], frame_length, int(smooth_coef_Q16))
	pred_Q13[1] = silk_stereo_find_predictor(&HP_ratio_Q14, []int16(HP_mid), []int16(HP_side), state.Mid_side_amp_Q0[2:], frame_length, int(smooth_coef_Q16))
	frac_Q16 = int32(int(HP_ratio_Q14) + int(int32(int16(LP_ratio_Q14)))*3)
	if int(frac_Q16) < int(int32(math.Floor(1*(1<<16)+0.5))) {
		frac_Q16 = frac_Q16
	} else {
		frac_Q16 = int32(math.Floor(1*(1<<16) + 0.5))
	}
	if is10msFrame != 0 {
		total_rate_bps -= 1200
	} else {
		total_rate_bps -= 600
	}
	if int(total_rate_bps) < 1 {
		total_rate_bps = 1
	}
	min_mid_rate_bps = int32(int(int32(int16(fs_kHz)))*600 + 2000)
	frac_3_Q16 = int32(int(frac_Q16) * 3)
	mid_side_rates_bps[0] = silk_DIV32_varQ(total_rate_bps, int32(int(frac_3_Q16)+int(int32(math.Floor((8+5)*(1<<16)+0.5)))), 16+3)
	if int(mid_side_rates_bps[0]) < int(min_mid_rate_bps) {
		mid_side_rates_bps[0] = min_mid_rate_bps
		mid_side_rates_bps[1] = int32(int(total_rate_bps) - int(mid_side_rates_bps[0]))
		width_Q14 = silk_DIV32_varQ(int32(int(int32(int(uint32(mid_side_rates_bps[1]))<<1))-int(min_mid_rate_bps)), int32(((int(frac_3_Q16)+int(int32(math.Floor(1*(1<<16)+0.5))))*int(int64(int16(min_mid_rate_bps))))>>16), 14+2)
		if 0 > int(int32(math.Floor(1*(1<<14)+0.5))) {
			if int(width_Q14) > 0 {
				width_Q14 = 0
			} else if int(width_Q14) < int(int32(math.Floor(1*(1<<14)+0.5))) {
				width_Q14 = int32(math.Floor(1*(1<<14) + 0.5))
			} else {
				width_Q14 = width_Q14
			}
		} else if int(width_Q14) > int(int32(math.Floor(1*(1<<14)+0.5))) {
			width_Q14 = int32(math.Floor(1*(1<<14) + 0.5))
		} else if int(width_Q14) < 0 {
			width_Q14 = 0
		} else {
			width_Q14 = width_Q14
		}
	} else {
		mid_side_rates_bps[1] = int32(int(total_rate_bps) - int(mid_side_rates_bps[0]))
		width_Q14 = int32(math.Floor(1*(1<<14) + 0.5))
	}
	state.Smth_width_Q14 = int16(int32(int(state.Smth_width_Q14) + (((int(width_Q14) - int(state.Smth_width_Q14)) * int(int64(int16(smooth_coef_Q16)))) >> 16)))
	*mid_only_flag = 0
	if toMono != 0 {
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
		silk_stereo_quant_pred(pred_Q13[:], ix)
	} else if int(state.Width_prev_Q14) == 0 && (int(total_rate_bps)*8 < int(min_mid_rate_bps)*13 || int(int32((int64(frac_Q16)*int64(state.Smth_width_Q14))>>16)) < int(int32(math.Floor(0.05*(1<<14)+0.5)))) {
		pred_Q13[0] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[0])))) >> 14)
		pred_Q13[1] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[1])))) >> 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
		mid_side_rates_bps[0] = total_rate_bps
		mid_side_rates_bps[1] = 0
		*mid_only_flag = 1
	} else if int(state.Width_prev_Q14) != 0 && (int(total_rate_bps)*8 < int(min_mid_rate_bps)*11 || int(int32((int64(frac_Q16)*int64(state.Smth_width_Q14))>>16)) < int(int32(math.Floor(0.02*(1<<14)+0.5)))) {
		pred_Q13[0] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[0])))) >> 14)
		pred_Q13[1] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[1])))) >> 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
	} else if int(state.Smth_width_Q14) > int(int32(math.Floor(0.95*(1<<14)+0.5))) {
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = int32(math.Floor(1*(1<<14) + 0.5))
	} else {
		pred_Q13[0] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[0])))) >> 14)
		pred_Q13[1] = int32((int(int32(state.Smth_width_Q14)) * int(int32(int16(pred_Q13[1])))) >> 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = int32(state.Smth_width_Q14)
	}
	if int(*mid_only_flag) == 1 {
		state.Silent_side_len += int16(frame_length - STEREO_INTERP_LEN_MS*fs_kHz)
		if int(state.Silent_side_len) < LA_SHAPE_MS*fs_kHz {
			*mid_only_flag = 0
		} else {
			state.Silent_side_len = 10000
		}
	} else {
		state.Silent_side_len = 0
	}
	if int(*mid_only_flag) == 0 && int(mid_side_rates_bps[1]) < 1 {
		mid_side_rates_bps[1] = 1
		mid_side_rates_bps[0] = int32(silk_max_int(1, int(total_rate_bps)-int(mid_side_rates_bps[1])))
	}
	pred0_Q13 = int32(-state.Pred_prev_Q13[0])
	pred1_Q13 = int32(-state.Pred_prev_Q13[1])
	w_Q24 = int32(int(uint32(state.Width_prev_Q14)) << 10)
	denom_Q16 = int(int32((1 << 16) / (STEREO_INTERP_LEN_MS * fs_kHz)))
	if 16 == 1 {
		delta0_Q13 = -(((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) >> 1) + ((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) & 1))
	} else {
		delta0_Q13 = -((((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) >> (16 - 1)) + 1) >> 1)
	}
	if 16 == 1 {
		delta1_Q13 = -(((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) >> 1) + ((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) & 1))
	} else {
		delta1_Q13 = -((((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) >> (16 - 1)) + 1) >> 1)
	}
	deltaw_Q24 = int32(int(uint32(int32(((int(width_Q14)-int(state.Width_prev_Q14))*int(int64(int16(denom_Q16))))>>16))) << 10)
	for n = 0; n < STEREO_INTERP_LEN_MS*fs_kHz; n++ {
		pred0_Q13 += int32(delta0_Q13)
		pred1_Q13 += int32(delta1_Q13)
		w_Q24 += deltaw_Q24
		sum = int32(int(uint32(int32((int(mid[n])+int(int32(mid[n+2])))+int(int32(int(uint32(mid[n+1]))<<1))))) << 9)
		sum = int32(int64(int32((int64(w_Q24)*int64(side[n+1]))>>16)) + ((int64(sum) * int64(int16(pred0_Q13))) >> 16))
		sum = int32(int64(sum) + ((int64(int32(int(uint32(int32(mid[n+1])))<<11)) * int64(int16(pred1_Q13))) >> 16))
		if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			x2[n-1] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			x2[n-1] = math.MinInt16
		} else if 8 == 1 {
			x2[n-1] = int16((int(sum) >> 1) + (int(sum) & 1))
		} else {
			x2[n-1] = int16(((int(sum) >> (8 - 1)) + 1) >> 1)
		}
	}
	pred0_Q13 = -pred_Q13[0]
	pred1_Q13 = -pred_Q13[1]
	w_Q24 = int32(int(uint32(width_Q14)) << 10)
	for n = STEREO_INTERP_LEN_MS * fs_kHz; n < frame_length; n++ {
		sum = int32(int(uint32(int32((int(mid[n])+int(int32(mid[n+2])))+int(int32(int(uint32(mid[n+1]))<<1))))) << 9)
		sum = int32(int64(int32((int64(w_Q24)*int64(side[n+1]))>>16)) + ((int64(sum) * int64(int16(pred0_Q13))) >> 16))
		sum = int32(int64(sum) + ((int64(int32(int(uint32(int32(mid[n+1])))<<11)) * int64(int16(pred1_Q13))) >> 16))
		if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			x2[n-1] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			x2[n-1] = math.MinInt16
		} else if 8 == 1 {
			x2[n-1] = int16((int(sum) >> 1) + (int(sum) & 1))
		} else {
			x2[n-1] = int16(((int(sum) >> (8 - 1)) + 1) >> 1)
		}
	}
	state.Pred_prev_Q13[0] = int16(pred_Q13[0])
	state.Pred_prev_Q13[1] = int16(pred_Q13[1])
	state.Width_prev_Q14 = int16(width_Q14)
}
