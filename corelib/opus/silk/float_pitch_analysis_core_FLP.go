package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/celt"
)

const SCRATCH_SIZE = 22

func PitchAnalysisCore_FLP(frame []float32, pitch_out []int, lagIndex *int16, contourIndex *int8, LTPCorr *float32, prevLag int, search_thres1 float32, search_thres2 float32, Fs_kHz int, complexity int, nb_subfr int, arch int) int {
	var (
		i                  int
		k                  int
		d                  int
		j                  int
		frame_8kHz         [320]float32
		frame_4kHz         [160]float32
		frame_8_FIX        [320]int16
		frame_4_FIX        [160]int16
		filt_state         [6]int32
		threshold          float32
		contour_bias       float32
		C                  [4][149]float32
		xcorr              [65]opus_val32
		CC                 [11]float32
		target_ptr         []float32
		basis_ptr_ptr      []float32
		basis_ptr_i        int
		cross_corr         float64
		normalizer         float64
		energy             float64
		energy_tmp         float64
		d_srch             [24]int
		d_comp             [149]int16
		length_d_srch      int
		length_d_comp      int
		Cmax               float32
		CCmax              float32
		CCmax_b            float32
		CCmax_new_b        float32
		CCmax_new          float32
		CBimax             int
		CBimax_new         int
		lag                int
		start_lag          int
		end_lag            int
		lag_new            int
		cbk_size           int
		lag_log2           float32
		prevLag_log2       float32
		delta_lag_log2_sqr float32
		energies_st3       [4][34][5]float32
		cross_corr_st3     [4][34][5]float32
		lag_counter        int
		frame_length       int
		frame_length_8kHz  int
		frame_length_4kHz  int
		sf_length          int
		sf_length_8kHz     int
		sf_length_4kHz     int
		min_lag            int
		min_lag_8kHz       int
		min_lag_4kHz       int
		max_lag            int
		max_lag_8kHz       int
		max_lag_4kHz       int
		nb_cbk_search      int
		Lag_CB_ptr         []int8
	)
	frame_length = ((int(PE_SUBFR_LENGTH_MS * 4)) + nb_subfr*PE_SUBFR_LENGTH_MS) * Fs_kHz
	frame_length_4kHz = ((int(PE_SUBFR_LENGTH_MS * 4)) + nb_subfr*PE_SUBFR_LENGTH_MS) * 4
	frame_length_8kHz = ((int(PE_SUBFR_LENGTH_MS * 4)) + nb_subfr*PE_SUBFR_LENGTH_MS) * 8
	sf_length = PE_SUBFR_LENGTH_MS * Fs_kHz
	sf_length_4kHz = int(PE_SUBFR_LENGTH_MS * 4)
	sf_length_8kHz = int(PE_SUBFR_LENGTH_MS * 8)
	min_lag = PE_MIN_LAG_MS * Fs_kHz
	min_lag_4kHz = int(PE_MIN_LAG_MS * 4)
	min_lag_8kHz = int(PE_MIN_LAG_MS * 8)
	max_lag = PE_MAX_LAG_MS*Fs_kHz - 1
	max_lag_4kHz = int(PE_MAX_LAG_MS * 4)
	max_lag_8kHz = int(PE_MAX_LAG_MS*8) - 1
	if Fs_kHz == 16 {
		var frame_16_FIX [640]int16
		silk_float2short_array(frame_16_FIX[:], frame, int32(frame_length))
		libc.MemSet(unsafe.Pointer(&filt_state[0]), 0, int(2*unsafe.Sizeof(int32(0))))
		ResamplerDown2(filt_state[:], frame_8_FIX[:], frame_16_FIX[:], int32(frame_length))
		silk_short2float_array(frame_8kHz[:], frame_8_FIX[:], int32(frame_length_8kHz))
	} else if Fs_kHz == 12 {
		var frame_12_FIX [480]int16
		silk_float2short_array(frame_12_FIX[:], frame, int32(frame_length))
		libc.MemSet(unsafe.Pointer(&filt_state[0]), 0, int(6*unsafe.Sizeof(int32(0))))
		ResamplerDown2_3(filt_state[:], frame_8_FIX[:], frame_12_FIX[:], int32(frame_length))
		silk_short2float_array(frame_8kHz[:], frame_8_FIX[:], int32(frame_length_8kHz))
	} else {
		silk_float2short_array(frame_8_FIX[:], frame, int32(frame_length_8kHz))
	}
	libc.MemSet(unsafe.Pointer(&filt_state[0]), 0, int(2*unsafe.Sizeof(int32(0))))
	ResamplerDown2(filt_state[:], frame_4_FIX[:], frame_8_FIX[:], int32(frame_length_8kHz))
	silk_short2float_array(frame_4kHz[:], frame_4_FIX[:], int32(frame_length_4kHz))
	for i = frame_length_4kHz - 1; i > 0; i-- {
		if (float32(int32(frame_4kHz[i])) + (frame_4kHz[i-1])) > math.MaxInt16 {
			frame_4kHz[i] = math.MaxInt16
		} else if (float32(int32(frame_4kHz[i])) + (frame_4kHz[i-1])) < float32(math.MinInt16) {
			frame_4kHz[i] = float32(math.MinInt16)
		} else {
			frame_4kHz[i] = float32(int32(frame_4kHz[i])) + (frame_4kHz[i-1])
		}
	}
	libc.MemSet(unsafe.Pointer(&C[0][0]), 0, nb_subfr*int(unsafe.Sizeof(float32(0)))*(((int(PE_MAX_LAG_MS*PE_MAX_FS_KHZ))>>1)+5))
	target_ptr = frame_4kHz[int32(int(uint32(int32(sf_length_4kHz)))<<2):]
	for k = 0; k < nb_subfr>>1; k++ {
		basis_ptr_ptr, basis_ptr_i = target_ptr, -min_lag_4kHz
		celt.PitchXcorrC(target_ptr, sliceFromNeg(target_ptr, max_lag_4kHz), xcorr[:], sf_length_8kHz, max_lag_4kHz-min_lag_4kHz+1, arch)
		cross_corr = float64(xcorr[max_lag_4kHz-min_lag_4kHz])
		// basis_ptr_ptr[basis_ptr_i:] where basis_ptr_i is negative — use sliceFromNeg
		normalizer = silk_energy_FLP([]float32(target_ptr), sf_length_8kHz) + silk_energy_FLP(sliceFromNeg(basis_ptr_ptr, -basis_ptr_i), sf_length_8kHz) + float64(sf_length_8kHz)*4000.0
		C[0][min_lag_4kHz] += float32(cross_corr * 2 / normalizer)
		for d = min_lag_4kHz + 1; d <= max_lag_4kHz; d++ {
			basis_ptr_i--
			cross_corr = float64(xcorr[max_lag_4kHz-d])
			// basis_ptr_ptr[basis_ptr_i+0] and basis_ptr_ptr[basis_ptr_i+sf_length_8kHz] where basis_ptr_i is negative
			bpWide := sliceFromNeg(basis_ptr_ptr, -basis_ptr_i)
			normalizer += float64(bpWide[0])*float64(bpWide[0]) - float64(bpWide[sf_length_8kHz])*float64(bpWide[sf_length_8kHz])
			C[0][d] += float32(cross_corr * 2 / normalizer)
		}
		target_ptr = target_ptr[sf_length_8kHz:]
	}
	for i = max_lag_4kHz; i >= min_lag_4kHz; i-- {
		C[0][i] -= C[0][i] * float32(i) / 4096.0
	}
	length_d_srch = complexity*2 + 4
	silk_insertion_sort_decreasing_FLP(C[0][min_lag_4kHz:], d_srch[:], max_lag_4kHz-min_lag_4kHz+1, length_d_srch)
	Cmax = C[0][min_lag_4kHz]
	if Cmax < 0.2 {
		libc.MemSet(unsafe.Pointer(&pitch_out[0]), 0, nb_subfr*int(unsafe.Sizeof(int(0))))
		*LTPCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}
	threshold = search_thres1 * Cmax
	for i = 0; i < length_d_srch; i++ {
		if C[0][min_lag_4kHz+i] > threshold {
			d_srch[i] = int(int32(int(uint32(int32(d_srch[i]+min_lag_4kHz))) << 1))
		} else {
			length_d_srch = i
			break
		}
	}
	for i = min_lag_8kHz - 5; i < max_lag_8kHz+5; i++ {
		d_comp[i] = 0
	}
	for i = 0; i < length_d_srch; i++ {
		d_comp[d_srch[i]] = 1
	}
	for i = max_lag_8kHz + 3; i >= min_lag_8kHz; i-- {
		d_comp[i] += int16(int(d_comp[i-1]) + int(d_comp[i-2]))
	}
	length_d_srch = 0
	for i = min_lag_8kHz; i < max_lag_8kHz+1; i++ {
		if int(d_comp[i+1]) > 0 {
			d_srch[length_d_srch] = i
			length_d_srch++
		}
	}
	for i = max_lag_8kHz + 3; i >= min_lag_8kHz; i-- {
		d_comp[i] += int16(int(d_comp[i-1]) + int(d_comp[i-2]) + int(d_comp[i-3]))
	}
	length_d_comp = 0
	for i = min_lag_8kHz; i < max_lag_8kHz+4; i++ {
		if int(d_comp[i]) > 0 {
			d_comp[length_d_comp] = int16(i - 2)
			length_d_comp++
		}
	}
	libc.MemSet(unsafe.Pointer(&C[0][0]), 0, PE_MAX_NB_SUBFR*(((int(PE_MAX_LAG_MS*PE_MAX_FS_KHZ))>>1)+5)*int(unsafe.Sizeof(float32(0))))
	if Fs_kHz == 8 {
		target_ptr = frame[(int(PE_SUBFR_LENGTH_MS*4))*8:]
	} else {
		target_ptr = frame_8kHz[(int(PE_SUBFR_LENGTH_MS*4))*8:]
	}
	for k = 0; k < nb_subfr; k++ {
		energy_tmp = silk_energy_FLP([]float32(target_ptr), sf_length_8kHz) + 1.0
		for j = 0; j < length_d_comp; j++ {
			d = int(d_comp[j])
			basis_ptr_ptr, basis_ptr_i = target_ptr, -d
			bpSlice := sliceFromNeg(basis_ptr_ptr, -basis_ptr_i)
			cross_corr = silk_inner_product_FLP(bpSlice, []float32(target_ptr), sf_length_8kHz)
			if cross_corr > 0.0 {
				energy = silk_energy_FLP(bpSlice, sf_length_8kHz)
				C[k][d] = float32(cross_corr * 2 / (energy + energy_tmp))
			} else {
				C[k][d] = 0.0
			}
		}
		target_ptr = target_ptr[sf_length_8kHz:]
	}
	CCmax = 0.0
	CCmax_b = -1000.0
	CBimax = 0
	lag = -1
	if prevLag > 0 {
		if Fs_kHz == 12 {
			prevLag = int(int32(int(uint32(int32(prevLag)))<<1)) / 3
		} else if Fs_kHz == 16 {
			prevLag = prevLag >> 1
		}
		prevLag_log2 = silk_log2(float64(float32(prevLag)))
	} else {
		prevLag_log2 = 0
	}
	if nb_subfr == PE_MAX_NB_SUBFR {
		cbk_size = PE_NB_CBKS_STAGE2_EXT
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage2[0][0], 4*11)
		if Fs_kHz == 8 && complexity > SILK_PE_MIN_COMPLEX {
			nb_cbk_search = PE_NB_CBKS_STAGE2_EXT
		} else {
			nb_cbk_search = PE_NB_CBKS_STAGE2
		}
	} else {
		cbk_size = PE_NB_CBKS_STAGE2_10MS
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage2_10_ms[0][0], 2*3)
		nb_cbk_search = PE_NB_CBKS_STAGE2_10MS
	}
	for k = 0; k < length_d_srch; k++ {
		d = d_srch[k]
		for j = 0; j < nb_cbk_search; j++ {
			CC[j] = 0.0
			for i = 0; i < nb_subfr; i++ {
				CC[j] += C[i][d+int(Lag_CB_ptr[i*cbk_size+j])]
			}
		}
		CCmax_new = -1000.0
		CBimax_new = 0
		for i = 0; i < nb_cbk_search; i++ {
			if CC[i] > CCmax_new {
				CCmax_new = CC[i]
				CBimax_new = i
			}
		}
		lag_log2 = silk_log2(float64(float32(d)))
		CCmax_new_b = float32(float64(CCmax_new) - PE_SHORTLAG_BIAS*float64(nb_subfr)*float64(lag_log2))
		if prevLag > 0 {
			delta_lag_log2_sqr = lag_log2 - prevLag_log2
			delta_lag_log2_sqr *= delta_lag_log2_sqr
			CCmax_new_b -= float32(PE_PREVLAG_BIAS * float64(nb_subfr) * float64(*LTPCorr) * float64(delta_lag_log2_sqr) / float64(delta_lag_log2_sqr+0.5))
		}
		if CCmax_new_b > CCmax_b && CCmax_new > float32(nb_subfr)*search_thres2 {
			CCmax_b = CCmax_new_b
			CCmax = CCmax_new
			lag = d
			CBimax = CBimax_new
		}
	}
	if lag == -1 {
		libc.MemSet(unsafe.Pointer(&pitch_out[0]), 0, int(PE_MAX_NB_SUBFR*unsafe.Sizeof(int(0))))
		*LTPCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}
	*LTPCorr = CCmax / float32(nb_subfr)
	if Fs_kHz > 8 {
		if Fs_kHz == 12 {
			if 1 == 1 {
				lag = ((int(int32(int16(lag))) * 3) >> 1) + ((int(int32(int16(lag))) * 3) & 1)
			} else {
				lag = (((int(int32(int16(lag))) * 3) >> (1 - 1)) + 1) >> 1
			}
		} else {
			lag = int(int32(int(uint32(int32(lag))) << 1))
		}
		if min_lag > max_lag {
			if lag > min_lag {
				lag = min_lag
			} else if lag < max_lag {
				lag = max_lag
			} else {
				lag = lag
			}
		} else if lag > max_lag {
			lag = max_lag
		} else if lag < min_lag {
			lag = min_lag
		} else {
			lag = lag
		}
		start_lag = silk_max_int(lag-2, min_lag)
		end_lag = silk_min_int(lag+2, max_lag)
		lag_new = lag
		CBimax = 0
		CCmax = -1000.0
		silk_P_Ana_calc_corr_st3(cross_corr_st3, frame, start_lag, sf_length, nb_subfr, complexity, arch)
		silk_P_Ana_calc_energy_st3(energies_st3, frame, start_lag, sf_length, nb_subfr, complexity)
		lag_counter = 0
		contour_bias = float32(PE_FLATCONTOUR_BIAS / float64(lag))
		if nb_subfr == PE_MAX_NB_SUBFR {
			nb_cbk_search = int(silk_nb_cbk_searchs_stage3[complexity])
			cbk_size = PE_NB_CBKS_STAGE3_MAX
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3[0][0], 4*34)
		} else {
			nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
			cbk_size = PE_NB_CBKS_STAGE3_10MS
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3_10_ms[0][0], 2*12)
		}
		target_ptr = frame[(int(PE_SUBFR_LENGTH_MS*4))*Fs_kHz:]
		energy_tmp = silk_energy_FLP([]float32(target_ptr), nb_subfr*sf_length) + 1.0
		for d = start_lag; d <= end_lag; d++ {
			for j = 0; j < nb_cbk_search; j++ {
				cross_corr = 0.0
				energy = energy_tmp
				for k = 0; k < nb_subfr; k++ {
					cross_corr += float64(cross_corr_st3[k][j][lag_counter])
					energy += float64(energies_st3[k][j][lag_counter])
				}
				if cross_corr > 0.0 {
					CCmax_new = float32(cross_corr * 2 / energy)
					CCmax_new *= 1.0 - contour_bias*float32(j)
				} else {
					CCmax_new = 0.0
				}
				if CCmax_new > CCmax && (d+int(silk_CB_lags_stage3[0][j])) <= max_lag {
					CCmax = CCmax_new
					lag_new = d
					CBimax = j
				}
			}
			lag_counter++
		}
		for k = 0; k < nb_subfr; k++ {
			pitch_out[k] = lag_new + int(Lag_CB_ptr[k*cbk_size+CBimax])
			if min_lag > (PE_MAX_LAG_MS * Fs_kHz) {
				if (pitch_out[k]) > min_lag {
					pitch_out[k] = min_lag
				} else if (pitch_out[k]) < (PE_MAX_LAG_MS * Fs_kHz) {
					pitch_out[k] = PE_MAX_LAG_MS * Fs_kHz
				} else {
					pitch_out[k] = pitch_out[k]
				}
			} else if (pitch_out[k]) > (PE_MAX_LAG_MS * Fs_kHz) {
				pitch_out[k] = PE_MAX_LAG_MS * Fs_kHz
			} else if (pitch_out[k]) < min_lag {
				pitch_out[k] = min_lag
			} else {
				pitch_out[k] = pitch_out[k]
			}
		}
		*lagIndex = int16(lag_new - min_lag)
		*contourIndex = int8(CBimax)
	} else {
		for k = 0; k < nb_subfr; k++ {
			pitch_out[k] = lag + int(Lag_CB_ptr[k*cbk_size+CBimax])
			if min_lag_8kHz > (int(PE_MAX_LAG_MS * 8)) {
				if (pitch_out[k]) > min_lag_8kHz {
					pitch_out[k] = min_lag_8kHz
				} else if (pitch_out[k]) < (int(PE_MAX_LAG_MS * 8)) {
					pitch_out[k] = int(PE_MAX_LAG_MS * 8)
				} else {
					pitch_out[k] = pitch_out[k]
				}
			} else if (pitch_out[k]) > (int(PE_MAX_LAG_MS * 8)) {
				pitch_out[k] = int(PE_MAX_LAG_MS * 8)
			} else if (pitch_out[k]) < min_lag_8kHz {
				pitch_out[k] = min_lag_8kHz
			} else {
				pitch_out[k] = pitch_out[k]
			}
		}
		*lagIndex = int16(lag - min_lag_8kHz)
		*contourIndex = int8(CBimax)
	}
	return 0
}
func silk_P_Ana_calc_corr_st3(cross_corr_st3 [4][34][5]float32, frame []float32, start_lag int, sf_length int, nb_subfr int, complexity int, arch int) {
	var (
		target_ptr    []float32
		i             int
		j             int
		k             int
		lag_counter   int
		lag_low       int
		lag_high      int
		nb_cbk_search int
		delta         int
		idx           int
		cbk_size      int
		scratch_mem   [22]float32
		xcorr         [22]opus_val32
		Lag_range_ptr []int8
		Lag_CB_ptr    []int8
	)
	if nb_subfr == PE_MAX_NB_SUBFR {
		Lag_range_ptr = unsafe.Slice(&silk_Lag_range_stage3[complexity][0][0], 4*2)
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3[0][0], 4*34)
		nb_cbk_search = int(silk_nb_cbk_searchs_stage3[complexity])
		cbk_size = PE_NB_CBKS_STAGE3_MAX
	} else {
		Lag_range_ptr = unsafe.Slice(&silk_Lag_range_stage3_10_ms[0][0], 2*2)
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3_10_ms[0][0], 2*12)
		nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
		cbk_size = PE_NB_CBKS_STAGE3_10MS
	}
	target_ptr = frame[int32(int(uint32(int32(sf_length)))<<2):]
	for k = 0; k < nb_subfr; k++ {
		lag_counter = 0
		lag_low = int(Lag_range_ptr[k*2+0])
		lag_high = int(Lag_range_ptr[k*2+1])
		celt.PitchXcorrC(target_ptr, sliceFromNeg(target_ptr, start_lag+lag_high), xcorr[:], sf_length, lag_high-lag_low+1, arch)
		for j = lag_low; j <= lag_high; j++ {
			scratch_mem[lag_counter] = float32(xcorr[lag_high-j])
			lag_counter++
		}
		delta = int(Lag_range_ptr[k*2+0])
		for i = 0; i < nb_cbk_search; i++ {
			idx = int(Lag_CB_ptr[k*cbk_size+i]) - delta
			for j = 0; j < PE_NB_STAGE3_LAGS; j++ {
				cross_corr_st3[k][i][j] = scratch_mem[idx+j]
			}
		}
		target_ptr = target_ptr[sf_length:]
	}
}
func silk_P_Ana_calc_energy_st3(energies_st3 [4][34][5]float32, frame []float32, start_lag int, sf_length int, nb_subfr int, complexity int) {
	var (
		target_ptr    []float32
		basis_ptr     []float32
		energy        float64
		k             int
		i             int
		j             int
		lag_counter   int
		nb_cbk_search int
		delta         int
		idx           int
		cbk_size      int
		lag_diff      int
		scratch_mem   [22]float32
		Lag_range_ptr []int8
		Lag_CB_ptr    []int8
	)
	if nb_subfr == PE_MAX_NB_SUBFR {
		Lag_range_ptr = unsafe.Slice(&silk_Lag_range_stage3[complexity][0][0], 4*2)
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3[0][0], 4*34)
		nb_cbk_search = int(silk_nb_cbk_searchs_stage3[complexity])
		cbk_size = PE_NB_CBKS_STAGE3_MAX
	} else {
		Lag_range_ptr = unsafe.Slice(&silk_Lag_range_stage3_10_ms[0][0], 2*2)
		Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3_10_ms[0][0], 2*12)
		nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
		cbk_size = PE_NB_CBKS_STAGE3_10MS
	}
	target_ptr = frame[int32(int(uint32(int32(sf_length)))<<2):]
	for k = 0; k < nb_subfr; k++ {
		lag_counter = 0
		basis_ptr = sliceFromNeg(target_ptr, start_lag+int(Lag_range_ptr[k*2+0]))
		energy = silk_energy_FLP(basis_ptr, sf_length) + 0.001
		scratch_mem[lag_counter] = float32(energy)
		lag_counter++
		lag_diff = int(Lag_range_ptr[k*2+1]) - int(Lag_range_ptr[k*2+0]) + 1
		for i = 1; i < lag_diff; i++ {
			energy -= float64(basis_ptr[sf_length-i]) * float64(basis_ptr[sf_length-i])
			energy += float64(elemFromNeg(basis_ptr, i)) * float64(elemFromNeg(basis_ptr, i))
			scratch_mem[lag_counter] = float32(energy)
			lag_counter++
		}
		delta = int(Lag_range_ptr[k*2+0])
		for i = 0; i < nb_cbk_search; i++ {
			idx = int(Lag_CB_ptr[k*cbk_size+i]) - delta
			for j = 0; j < PE_NB_STAGE3_LAGS; j++ {
				energies_st3[k][i][j] = scratch_mem[idx+j]
			}
		}
		target_ptr = target_ptr[sf_length:]
	}
}
