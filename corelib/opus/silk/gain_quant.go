package silk

const OFFSET = 2090
const SCALE_Q16 = 2251
const INV_SCALE_Q16 = 0x1D1C71

func GainsQuant(ind [4]int8, gain_Q16 [4]int32, prev_ind *int8, conditional int, nb_subfr int) {
	for k := 0; k < nb_subfr; k++ {
		ind[k] = int8(int32(((((int(N_LEVELS_QGAIN - 1)) * 65536) / (((int(MAX_QGAIN_DB - MIN_QGAIN_DB)) * 128) / 6)) * int(int64(int16(int(silk_lin2log(gain_Q16[k]))-((int(MIN_QGAIN_DB*128))/6+16*128))))) >> 16))
		if int(ind[k]) < int(*prev_ind) {
			ind[k]++
		}
		if 0 > (int(N_LEVELS_QGAIN - 1)) {
			if int(ind[k]) > 0 {
				ind[k] = 0
			} else if int(ind[k]) < (int(N_LEVELS_QGAIN - 1)) {
				ind[k] = int8(int(N_LEVELS_QGAIN - 1))
			} else {
				ind[k] = ind[k]
			}
		} else if int(ind[k]) > (int(N_LEVELS_QGAIN - 1)) {
			ind[k] = int8(int(N_LEVELS_QGAIN - 1))
		} else if int(ind[k]) < 0 {
			ind[k] = 0
		} else {
			ind[k] = ind[k]
		}
		if k == 0 && conditional == 0 {
			if (int(*prev_ind) + (-4)) > (int(N_LEVELS_QGAIN - 1)) {
				if int(ind[k]) > (int(*prev_ind) + (-4)) {
					ind[k] = int8(int(*prev_ind) + (-4))
				} else if int(ind[k]) < (int(N_LEVELS_QGAIN - 1)) {
					ind[k] = int8(int(N_LEVELS_QGAIN - 1))
				} else {
					ind[k] = ind[k]
				}
			} else if int(ind[k]) > (int(N_LEVELS_QGAIN - 1)) {
				ind[k] = int8(int(N_LEVELS_QGAIN - 1))
			} else if int(ind[k]) < (int(*prev_ind) + (-4)) {
				ind[k] = int8(int(*prev_ind) + (-4))
			} else {
				ind[k] = ind[k]
			}
			*prev_ind = ind[k]
		} else {
			ind[k] = int8(int(ind[k]) - int(*prev_ind))
			double_step_size_threshold := int(MAX_DELTA_GAIN_QUANT*2) - N_LEVELS_QGAIN + int(*prev_ind)
			if int(ind[k]) > double_step_size_threshold {
				ind[k] = int8(double_step_size_threshold + ((int(ind[k]) - double_step_size_threshold + 1) >> 1))
			}
			if int(-4) > MAX_DELTA_GAIN_QUANT {
				if int(ind[k]) > int(-4) {
					ind[k] = -4
				} else if int(ind[k]) < MAX_DELTA_GAIN_QUANT {
					ind[k] = MAX_DELTA_GAIN_QUANT
				} else {
					ind[k] = ind[k]
				}
			} else if int(ind[k]) > MAX_DELTA_GAIN_QUANT {
				ind[k] = MAX_DELTA_GAIN_QUANT
			} else if int(ind[k]) < int(-4) {
				ind[k] = -4
			} else {
				ind[k] = ind[k]
			}
			if int(ind[k]) > double_step_size_threshold {
				*prev_ind += int8(int(int32(int(uint32(ind[k]))<<1)) - double_step_size_threshold)
				*prev_ind = int8(silk_min_int(int(*prev_ind), int(N_LEVELS_QGAIN-1)))
			} else {
				*prev_ind += ind[k]
			}
			ind[k] -= -4
		}
		gain_Q16[k] = silk_log2lin(silk_min_32(int32(int(int32(((((((int(MAX_QGAIN_DB-MIN_QGAIN_DB))*128)/6)*65536)/(int(N_LEVELS_QGAIN-1)))*int(int64(int16(*prev_ind))))>>16))+((int(MIN_QGAIN_DB*128))/6+16*128)), 3967))
	}
}
func GainsDequant(gain_Q16 [4]int32, ind [4]int8, prev_ind *int8, conditional int, nb_subfr int) {
	for k := 0; k < nb_subfr; k++ {
		if k == 0 && conditional == 0 {
			*prev_ind = int8(silk_max_int(int(ind[k]), int(*prev_ind)-16))
		} else {
			ind_tmp := int(ind[k]) + (-4)
			double_step_size_threshold := int(MAX_DELTA_GAIN_QUANT*2) - N_LEVELS_QGAIN + int(*prev_ind)
			if ind_tmp > double_step_size_threshold {
				*prev_ind += int8(int(int32(int(uint32(int32(ind_tmp)))<<1)) - double_step_size_threshold)
			} else {
				*prev_ind += int8(ind_tmp)
			}
		}
		if 0 > (int(N_LEVELS_QGAIN - 1)) {
			if int(*prev_ind) > 0 {
				*prev_ind = 0
			} else if int(*prev_ind) < (int(N_LEVELS_QGAIN - 1)) {
				*prev_ind = int8(int(N_LEVELS_QGAIN - 1))
			} else {
				*prev_ind = *prev_ind
			}
		} else if int(*prev_ind) > (int(N_LEVELS_QGAIN - 1)) {
			*prev_ind = int8(int(N_LEVELS_QGAIN - 1))
		} else if int(*prev_ind) < 0 {
			*prev_ind = 0
		} else {
			*prev_ind = *prev_ind
		}
		gain_Q16[k] = silk_log2lin(silk_min_32(int32(int(int32(((((((int(MAX_QGAIN_DB-MIN_QGAIN_DB))*128)/6)*65536)/(int(N_LEVELS_QGAIN-1)))*int(int64(int16(*prev_ind))))>>16))+((int(MIN_QGAIN_DB*128))/6+16*128)), 3967))
	}
}
func GainsID(ind [4]int8, nb_subfr int) int32 {
	var gainsID int32
	for k := 0; k < nb_subfr; k++ {
		gainsID = int32(int(ind[k]) + int(int32(int(uint32(gainsID))<<8)))
	}
	return gainsID
}
