package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func QuantLTPGains(B_Q14 [20]int16, cbk_index [4]int8, periodicity_index *int8, sum_log_gain_Q7 *int32, pred_gain_dB_Q7 *int, XX_Q17 [100]int32, xX_Q17 [20]int32, subfr_len int, nb_subfr int, arch int) {
	var (
		j                    int
		k                    int
		cbk_size             int
		temp_idx             [4]int8
		cl_ptr_Q5            []uint8
		cbk_ptr_Q7           []int8
		cbk_gain_ptr_Q7      []uint8
		XX_Q17_ptr           []int32
		xX_Q17_ptr           []int32
		res_nrg_Q15_subfr    int32
		res_nrg_Q15          int32
		rate_dist_Q7_subfr   int32
		rate_dist_Q7         int32
		min_rate_dist_Q7     int32
		sum_log_gain_tmp_Q7  int32
		best_sum_log_gain_Q7 int32
		max_gain_Q7          int32
		gain_Q7              int
	)
	min_rate_dist_Q7 = math.MaxInt32
	best_sum_log_gain_Q7 = 0
	for k = 0; k < 3; k++ {
		var gain_safety int32 = (int32(math.Floor(0.4*(1<<7) + 0.5)))
		cl_ptr_Q5 = silk_LTP_gain_BITS_Q5_ptrs[k]
		cbk_ptr_Q7 = silk_LTP_vq_ptrs_Q7[k]
		cbk_gain_ptr_Q7 = silk_LTP_vq_gain_ptrs_Q7[k]
		cbk_size = int(silk_LTP_vq_sizes[k])
		XX_Q17_ptr = XX_Q17[:]
		xX_Q17_ptr = xX_Q17[:]
		res_nrg_Q15 = 0
		rate_dist_Q7 = 0
		sum_log_gain_tmp_Q7 = *sum_log_gain_Q7
		for j = 0; j < nb_subfr; j++ {
			max_gain_Q7 = int32(int(silk_log2lin(int32((int(int32(math.Floor((MAX_SUM_LOG_GAIN_DB/6.0)*(1<<7)+0.5)))-int(sum_log_gain_tmp_Q7))+int(int32(math.Floor(7*(1<<7)+0.5)))))) - int(gain_safety))
			_ = arch
			silk_VQ_WMat_EC_c(&temp_idx[j], &res_nrg_Q15_subfr, &rate_dist_Q7_subfr, &gain_Q7, XX_Q17_ptr, xX_Q17_ptr, cbk_ptr_Q7, cbk_gain_ptr_Q7, cl_ptr_Q5, subfr_len, max_gain_Q7, cbk_size)
			if ((int(uint32(res_nrg_Q15)) + int(uint32(res_nrg_Q15_subfr))) & 0x80000000) != 0 {
				res_nrg_Q15 = math.MaxInt32
			} else {
				res_nrg_Q15 = int32(int(res_nrg_Q15) + int(res_nrg_Q15_subfr))
			}
			if ((int(uint32(rate_dist_Q7)) + int(uint32(rate_dist_Q7_subfr))) & 0x80000000) != 0 {
				rate_dist_Q7 = math.MaxInt32
			} else {
				rate_dist_Q7 = int32(int(rate_dist_Q7) + int(rate_dist_Q7_subfr))
			}
			if 0 > (int(sum_log_gain_tmp_Q7) + int(silk_lin2log(int32(int(gain_safety)+gain_Q7))) - int(int32(math.Floor(7*(1<<7)+0.5)))) {
				sum_log_gain_tmp_Q7 = 0
			} else {
				sum_log_gain_tmp_Q7 = int32(int(sum_log_gain_tmp_Q7) + int(silk_lin2log(int32(int(gain_safety)+gain_Q7))) - int(int32(math.Floor(7*(1<<7)+0.5))))
			}
			XX_Q17_ptr = XX_Q17_ptr[LTP_ORDER*LTP_ORDER:]
			xX_Q17_ptr = xX_Q17_ptr[LTP_ORDER:]
		}
		if int(rate_dist_Q7) <= int(min_rate_dist_Q7) {
			min_rate_dist_Q7 = rate_dist_Q7
			*periodicity_index = int8(k)
			libc.MemCpy(unsafe.Pointer(&cbk_index[0]), unsafe.Pointer(&temp_idx[0]), nb_subfr*int(unsafe.Sizeof(int8(0))))
			best_sum_log_gain_Q7 = sum_log_gain_tmp_Q7
		}
	}
	cbk_ptr_Q7 = silk_LTP_vq_ptrs_Q7[*periodicity_index]
	for j = 0; j < nb_subfr; j++ {
		for k = 0; k < LTP_ORDER; k++ {
			B_Q14[j*LTP_ORDER+k] = int16(int32(int(uint32(cbk_ptr_Q7[int(cbk_index[j])*LTP_ORDER+k])) << 7))
		}
	}
	if nb_subfr == 2 {
		res_nrg_Q15 = int32(int(res_nrg_Q15) >> 1)
	} else {
		res_nrg_Q15 = int32(int(res_nrg_Q15) >> 2)
	}
	*sum_log_gain_Q7 = best_sum_log_gain_Q7
	*pred_gain_dB_Q7 = int(int32(int16(int(silk_lin2log(res_nrg_Q15))-(15<<7)))) * int(int32(int16(-3)))
}
