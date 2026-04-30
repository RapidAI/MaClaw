package silk

import "unsafe"

func DecodePitch(lagIndex int16, contourIndex int8, pitch_lags []int, Fs_kHz int, nb_subfr int) {
	var (
		lag        int
		k          int
		min_lag    int
		max_lag    int
		cbk_size   int
		Lag_CB_ptr []int8
	)
	if Fs_kHz == 8 {
		if nb_subfr == PE_MAX_NB_SUBFR {
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage2[0][0], 4*11)
			cbk_size = PE_NB_CBKS_STAGE2_EXT
		} else {
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage2_10_ms[0][0], 2*3)
			cbk_size = PE_NB_CBKS_STAGE2_10MS
		}
	} else {
		if nb_subfr == PE_MAX_NB_SUBFR {
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3[0][0], 4*34)
			cbk_size = PE_NB_CBKS_STAGE3_MAX
		} else {
			Lag_CB_ptr = unsafe.Slice(&silk_CB_lags_stage3_10_ms[0][0], 2*12)
			cbk_size = PE_NB_CBKS_STAGE3_10MS
		}
	}
	min_lag = PE_MIN_LAG_MS * int(int32(int16(Fs_kHz)))
	max_lag = PE_MAX_LAG_MS * int(int32(int16(Fs_kHz)))
	lag = min_lag + int(lagIndex)
	for k = 0; k < nb_subfr; k++ {
		pitch_lags[k] = lag + int(Lag_CB_ptr[k*cbk_size+int(contourIndex)])
		if min_lag > max_lag {
			if (pitch_lags[k]) > min_lag {
				pitch_lags[k] = min_lag
			} else if (pitch_lags[k]) < max_lag {
				pitch_lags[k] = max_lag
			} else {
				pitch_lags[k] = pitch_lags[k]
			}
		} else if (pitch_lags[k]) > max_lag {
			pitch_lags[k] = max_lag
		} else if (pitch_lags[k]) < min_lag {
			pitch_lags[k] = min_lag
		} else {
			pitch_lags[k] = pitch_lags[k]
		}
	}
}
