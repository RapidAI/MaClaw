package silk

func (psDec *DecoderState) SetFS(fs_kHz int, fs_API_Hz int32) int {
	var (
		frame_length int
		ret          int = 0
	)
	psDec.Subfr_length = SUB_FRAME_LENGTH_MS * int(int32(int16(fs_kHz)))
	frame_length = int(int32(int16(psDec.Nb_subfr))) * int(int32(int16(psDec.Subfr_length)))
	if psDec.Fs_kHz != fs_kHz || int(psDec.Fs_API_hz) != int(fs_API_Hz) {
		ret += psDec.Resampler_state.Init(int32(int(int32(int16(fs_kHz)))*1000), fs_API_Hz, 0)
		psDec.Fs_API_hz = fs_API_Hz
	}
	if psDec.Fs_kHz != fs_kHz || frame_length != psDec.Frame_length {
		if fs_kHz == 8 {
			if psDec.Nb_subfr == MAX_NB_SUBFR {
				psDec.Pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:][:]
			} else {
				psDec.Pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:][:]
			}
		} else {
			if psDec.Nb_subfr == MAX_NB_SUBFR {
				psDec.Pitch_contour_iCDF = silk_pitch_contour_iCDF[:][:]
			} else {
				psDec.Pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:][:]
			}
		}
		if psDec.Fs_kHz != fs_kHz {
			psDec.Ltp_mem_length = LTP_MEM_LENGTH_MS * int(int32(int16(fs_kHz)))
			if fs_kHz == 8 || fs_kHz == 12 {
				psDec.LPC_order = MIN_LPC_ORDER
				psDec.PsNLSF_CB = &silk_NLSF_CB_NB_MB
			} else {
				psDec.LPC_order = MAX_LPC_ORDER
				psDec.PsNLSF_CB = &silk_NLSF_CB_WB
			}
			if fs_kHz == 16 {
				psDec.Pitch_lag_low_bits_iCDF = silk_uniform8_iCDF[:][:]
			} else if fs_kHz == 12 {
				psDec.Pitch_lag_low_bits_iCDF = silk_uniform6_iCDF[:][:]
			} else if fs_kHz == 8 {
				psDec.Pitch_lag_low_bits_iCDF = silk_uniform4_iCDF[:][:]
			} else {
			}
			psDec.First_frame_after_reset = 1
			psDec.LagPrev = 100
			psDec.LastGainIndex = 10
			psDec.PrevSignalType = TYPE_NO_VOICE_ACTIVITY
			psDec.OutBuf = [480]int16{}
			psDec.SLPC_Q14_buf = [16]int32{}
		}
		psDec.Fs_kHz = fs_kHz
		psDec.Frame_length = frame_length
	}
	return ret
}
