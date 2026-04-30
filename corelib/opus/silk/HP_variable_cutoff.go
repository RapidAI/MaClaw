package silk

import "math"

func HP_variable_cutoff(state_Fxx []EncoderStateFLP) {
	var (
		quality_Q15       int
		pitch_freq_Hz_Q16 int32
		pitch_freq_log_Q7 int32
		delta_freq_Q7     int32
		psEncC1           = &state_Fxx[0].SCmn
	)
	if int(psEncC1.PrevSignalType) == TYPE_VOICED {
		pitch_freq_Hz_Q16 = int32(int(int32(int(uint32(int32(psEncC1.Fs_kHz*1000)))<<16)) / psEncC1.PrevLag)
		pitch_freq_log_Q7 = int32(int(silk_lin2log(pitch_freq_Hz_Q16)) - (16 << 7))
		quality_Q15 = psEncC1.Input_quality_bands_Q15[0]
		ftmp := float64(int(VARIABLE_HP_MIN_CUTOFF_HZ*(1<<16))) + 0.5
		pitch_freq_log_Q7 = int32(int64(pitch_freq_log_Q7) + ((int64(int32((int64(int32(int(uint32(int32(-quality_Q15)))<<2))*int64(int16(quality_Q15)))>>16)) * int64(int16(int(pitch_freq_log_Q7)-(int(silk_lin2log(int32(ftmp)))-(16<<7))))) >> 16))
		delta_freq_Q7 = int32(int(pitch_freq_log_Q7) - (int(psEncC1.Variable_HP_smth1_Q15) >> 8))
		if int(delta_freq_Q7) < 0 {
			delta_freq_Q7 = int32(int(delta_freq_Q7) * 3)
		}
		if int(-int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) > int(int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) {
			if int(delta_freq_Q7) > int(-int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) {
				delta_freq_Q7 = -int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7) + 0.5))
			} else if int(delta_freq_Q7) < int(int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) {
				delta_freq_Q7 = int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7) + 0.5))
			} else {
				delta_freq_Q7 = delta_freq_Q7
			}
		} else if int(delta_freq_Q7) > int(int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) {
			delta_freq_Q7 = int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7) + 0.5))
		} else if int(delta_freq_Q7) < int(-int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7)+0.5))) {
			delta_freq_Q7 = -int32(math.Floor(VARIABLE_HP_MAX_DELTA_FREQ*(1<<7) + 0.5))
		} else {
			delta_freq_Q7 = delta_freq_Q7
		}
		psEncC1.Variable_HP_smth1_Q15 = int32(int(psEncC1.Variable_HP_smth1_Q15) + (((int(int32(int16(psEncC1.Speech_activity_Q8))) * int(int32(int16(delta_freq_Q7)))) * int(int64(int16(int32(math.Floor(VARIABLE_HP_SMTH_COEF1*(1<<16)+0.5)))))) >> 16))
		if int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ)))<<8)) > int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ)))<<8)) {
			if int(psEncC1.Variable_HP_smth1_Q15) > int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ)))<<8)) {
				psEncC1.Variable_HP_smth1_Q15 = int32(int(uint32(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ))) << 8)
			} else if int(psEncC1.Variable_HP_smth1_Q15) < int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ)))<<8)) {
				psEncC1.Variable_HP_smth1_Q15 = int32(int(uint32(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ))) << 8)
			} else {
				psEncC1.Variable_HP_smth1_Q15 = psEncC1.Variable_HP_smth1_Q15
			}
		} else if int(psEncC1.Variable_HP_smth1_Q15) > int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ)))<<8)) {
			psEncC1.Variable_HP_smth1_Q15 = int32(int(uint32(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ))) << 8)
		} else if int(psEncC1.Variable_HP_smth1_Q15) < int(int32(int(uint32(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ)))<<8)) {
			psEncC1.Variable_HP_smth1_Q15 = int32(int(uint32(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ))) << 8)
		} else {
			psEncC1.Variable_HP_smth1_Q15 = psEncC1.Variable_HP_smth1_Q15
		}
	}
}
