package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func NLSF_del_dec_quant(indices []int8, x_Q10 []int16, w_Q5 []int16, pred_coef_Q8 []uint8, ec_ix []int16, ec_rates_Q5 []uint8, quant_step_size_Q16 int, inv_quant_step_size_Q6 int16, mu_Q20 int32, order int16) int32 {
	var (
		i              int
		j              int
		nStates        int
		ind_tmp        int
		ind_min_max    int
		ind_max_min    int
		in_Q10         int
		res_Q10        int
		pred_Q10       int
		diff_Q10       int
		rate0_Q5       int
		rate1_Q5       int
		out0_Q10       int16
		out1_Q10       int16
		RD_tmp_Q25     int32
		min_Q25        int32
		min_max_Q25    int32
		max_min_Q25    int32
		ind_sort       [4]int
		ind            [4][16]int8
		prev_out_Q10   [8]int16
		RD_Q25         [8]int32
		RD_min_Q25     [4]int32
		RD_max_Q25     [4]int32
		rates_Q5       []uint8
		out0_Q10_table [20]int
		out1_Q10_table [20]int
	)
	for i = -NLSF_QUANT_MAX_AMPLITUDE_EXT; i <= int(NLSF_QUANT_MAX_AMPLITUDE_EXT-1); i++ {
		out0_Q10 = int16(int32(int(uint32(int32(i))) << 10))
		out1_Q10 = int16(int(out0_Q10) + 1024)
		if i > 0 {
			out0_Q10 = int16(int(out0_Q10) - int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
			out1_Q10 = int16(int(out1_Q10) - int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
		} else if i == 0 {
			out1_Q10 = int16(int(out1_Q10) - int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
		} else if i == -1 {
			out0_Q10 = int16(int(out0_Q10) + int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
		} else {
			out0_Q10 = int16(int(out0_Q10) + int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
			out1_Q10 = int16(int(out1_Q10) + int(int32(math.Floor(NLSF_QUANT_LEVEL_ADJ*(1<<10)+0.5))))
		}
		out0_Q10_table[i+NLSF_QUANT_MAX_AMPLITUDE_EXT] = (int(int32(out0_Q10)) * int(int32(int16(quant_step_size_Q16)))) >> 16
		out1_Q10_table[i+NLSF_QUANT_MAX_AMPLITUDE_EXT] = (int(int32(out1_Q10)) * int(int32(int16(quant_step_size_Q16)))) >> 16
	}
	nStates = 1
	RD_Q25[0] = 0
	prev_out_Q10[0] = 0
	for i = int(order) - 1; i >= 0; i-- {
		rates_Q5 = ec_rates_Q5[ec_ix[i]:]
		in_Q10 = int(x_Q10[i])
		for j = 0; j < nStates; j++ {
			pred_Q10 = (int(int32(int16(pred_coef_Q8[i]))) * int(int32(prev_out_Q10[j]))) >> 8
			res_Q10 = in_Q10 - pred_Q10
			ind_tmp = (int(int32(inv_quant_step_size_Q6)) * int(int32(int16(res_Q10)))) >> 16
			if int(-NLSF_QUANT_MAX_AMPLITUDE_EXT) > (int(NLSF_QUANT_MAX_AMPLITUDE_EXT - 1)) {
				if ind_tmp > int(-NLSF_QUANT_MAX_AMPLITUDE_EXT) {
					ind_tmp = -NLSF_QUANT_MAX_AMPLITUDE_EXT
				} else if ind_tmp < (int(NLSF_QUANT_MAX_AMPLITUDE_EXT - 1)) {
					ind_tmp = int(NLSF_QUANT_MAX_AMPLITUDE_EXT - 1)
				} else {
					ind_tmp = ind_tmp
				}
			} else if ind_tmp > (int(NLSF_QUANT_MAX_AMPLITUDE_EXT - 1)) {
				ind_tmp = int(NLSF_QUANT_MAX_AMPLITUDE_EXT - 1)
			} else if ind_tmp < int(-NLSF_QUANT_MAX_AMPLITUDE_EXT) {
				ind_tmp = -NLSF_QUANT_MAX_AMPLITUDE_EXT
			} else {
				ind_tmp = ind_tmp
			}
			ind[j][i] = int8(ind_tmp)
			out0_Q10 = int16(out0_Q10_table[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE_EXT])
			out1_Q10 = int16(out1_Q10_table[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE_EXT])
			out0_Q10 = int16(int(out0_Q10) + pred_Q10)
			out1_Q10 = int16(int(out1_Q10) + pred_Q10)
			prev_out_Q10[j] = out0_Q10
			prev_out_Q10[j+nStates] = out1_Q10
			if ind_tmp+1 >= NLSF_QUANT_MAX_AMPLITUDE {
				if ind_tmp+1 == NLSF_QUANT_MAX_AMPLITUDE {
					rate0_Q5 = int(rates_Q5[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE])
					rate1_Q5 = 280
				} else {
					rate0_Q5 = (280 - int(NLSF_QUANT_MAX_AMPLITUDE*43)) + int(int32(int16(ind_tmp)))*43
					rate1_Q5 = rate0_Q5 + 43
				}
			} else if ind_tmp <= -NLSF_QUANT_MAX_AMPLITUDE {
				if ind_tmp == -NLSF_QUANT_MAX_AMPLITUDE {
					rate0_Q5 = 280
					rate1_Q5 = int(rates_Q5[ind_tmp+1+NLSF_QUANT_MAX_AMPLITUDE])
				} else {
					rate0_Q5 = (280 - int(NLSF_QUANT_MAX_AMPLITUDE*43)) + int(int32(int16(ind_tmp)))*int(int32(int16(-43)))
					rate1_Q5 = rate0_Q5 - 43
				}
			} else {
				rate0_Q5 = int(rates_Q5[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE])
				rate1_Q5 = int(rates_Q5[ind_tmp+1+NLSF_QUANT_MAX_AMPLITUDE])
			}
			RD_tmp_Q25 = RD_Q25[j]
			diff_Q10 = in_Q10 - int(out0_Q10)
			RD_Q25[j] = int32((int(RD_tmp_Q25) + (int(int32(int16(diff_Q10)))*int(int32(int16(diff_Q10))))*int(w_Q5[i])) + int(int32(int16(mu_Q20)))*int(int32(int16(rate0_Q5))))
			diff_Q10 = in_Q10 - int(out1_Q10)
			RD_Q25[j+nStates] = int32((int(RD_tmp_Q25) + (int(int32(int16(diff_Q10)))*int(int32(int16(diff_Q10))))*int(w_Q5[i])) + int(int32(int16(mu_Q20)))*int(int32(int16(rate1_Q5))))
		}
		if nStates <= (int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))/2 {
			for j = 0; j < nStates; j++ {
				ind[j+nStates][i] = int8(int(ind[j][i]) + 1)
			}
			nStates = int(int32(int(uint32(int32(nStates))) << 1))
			for j = nStates; j < (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2)); j++ {
				ind[j][i] = ind[j-nStates][i]
			}
		} else {
			for j = 0; j < (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2)); j++ {
				if int(RD_Q25[j]) > int(RD_Q25[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]) {
					RD_max_Q25[j] = RD_Q25[j]
					RD_min_Q25[j] = RD_Q25[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]
					RD_Q25[j] = RD_min_Q25[j]
					RD_Q25[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))] = RD_max_Q25[j]
					out0_Q10 = prev_out_Q10[j]
					prev_out_Q10[j] = prev_out_Q10[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]
					prev_out_Q10[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))] = out0_Q10
					ind_sort[j] = j + (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2))
				} else {
					RD_min_Q25[j] = RD_Q25[j]
					RD_max_Q25[j] = RD_Q25[j+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]
					ind_sort[j] = j
				}
			}
			for {
				min_max_Q25 = math.MaxInt32
				max_min_Q25 = 0
				ind_min_max = 0
				ind_max_min = 0
				for j = 0; j < (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2)); j++ {
					if int(min_max_Q25) > int(RD_max_Q25[j]) {
						min_max_Q25 = RD_max_Q25[j]
						ind_min_max = j
					}
					if int(max_min_Q25) < int(RD_min_Q25[j]) {
						max_min_Q25 = RD_min_Q25[j]
						ind_max_min = j
					}
				}
				if int(min_max_Q25) >= int(max_min_Q25) {
					break
				}
				ind_sort[ind_max_min] = ind_sort[ind_min_max] ^ (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2))
				RD_Q25[ind_max_min] = RD_Q25[ind_min_max+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]
				prev_out_Q10[ind_max_min] = prev_out_Q10[ind_min_max+(int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))]
				RD_min_Q25[ind_max_min] = 0
				RD_max_Q25[ind_min_max] = math.MaxInt32
				libc.MemCpy(unsafe.Pointer(&(ind[ind_max_min])[0]), unsafe.Pointer(&(ind[ind_min_max])[0]), int(MAX_LPC_ORDER*unsafe.Sizeof(int8(0))))
			}
			for j = 0; j < (int(1 << NLSF_QUANT_DEL_DEC_STATES_LOG2)); j++ {
				ind[j][i] += int8((ind_sort[j]) >> NLSF_QUANT_DEL_DEC_STATES_LOG2)
			}
		}
	}
	ind_tmp = 0
	min_Q25 = math.MaxInt32
	for j = 0; j < (int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))*2; j++ {
		if int(min_Q25) > int(RD_Q25[j]) {
			min_Q25 = RD_Q25[j]
			ind_tmp = j
		}
	}
	for j = 0; j < int(order); j++ {
		indices[j] = ind[ind_tmp&((int(1<<NLSF_QUANT_DEL_DEC_STATES_LOG2))-1)][j]
	}
	indices[0] += int8(ind_tmp >> NLSF_QUANT_DEL_DEC_STATES_LOG2)
	return min_Q25
}
