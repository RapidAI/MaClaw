package silk

import (
	"math"
)

func ControlEncoder(psEnc *EncoderStateFLP, encControl *EncControlStruct, allow_bw_switch int, channelNb int, force_fs_kHz int) int {
	var (
		fs_kHz int
		ret    int = 0
	)
	psEnc.SCmn.UseDTX = encControl.UseDTX
	psEnc.SCmn.UseCBR = encControl.UseCBR
	psEnc.SCmn.API_fs_Hz = encControl.API_sampleRate
	psEnc.SCmn.MaxInternal_fs_Hz = int(encControl.MaxInternalSampleRate)
	psEnc.SCmn.MinInternal_fs_Hz = int(encControl.MinInternalSampleRate)
	psEnc.SCmn.DesiredInternal_fs_Hz = int(encControl.DesiredInternalSampleRate)
	psEnc.SCmn.UseInBandFEC = encControl.UseInBandFEC
	psEnc.SCmn.NChannelsAPI = int(encControl.NChannelsAPI)
	psEnc.SCmn.NChannelsInternal = int(encControl.NChannelsInternal)
	psEnc.SCmn.Allow_bandwidth_switch = allow_bw_switch
	psEnc.SCmn.ChannelNb = channelNb
	if psEnc.SCmn.Controlled_since_last_payload != 0 && psEnc.SCmn.PrefillFlag == 0 {
		if int(psEnc.SCmn.API_fs_Hz) != int(psEnc.SCmn.Prev_API_fs_Hz) && psEnc.SCmn.Fs_kHz > 0 {
			ret += silk_setup_resamplers(psEnc, psEnc.SCmn.Fs_kHz)
		}
		return ret
	}
	fs_kHz = silk_control_audio_bandwidth(&psEnc.SCmn, encControl)
	if force_fs_kHz != 0 {
		fs_kHz = force_fs_kHz
	}
	ret += silk_setup_resamplers(psEnc, fs_kHz)
	ret += silk_setup_fs(psEnc, fs_kHz, encControl.PayloadSize_ms)
	ret += silk_setup_complexity(&psEnc.SCmn, encControl.Complexity)
	psEnc.SCmn.PacketLoss_perc = encControl.PacketLossPercentage
	ret += silk_setup_LBRR(&psEnc.SCmn, encControl)
	psEnc.SCmn.Controlled_since_last_payload = 1
	return ret
}
func silk_setup_resamplers(psEnc *EncoderStateFLP, fs_kHz int) int {
	var ret int = SILK_NO_ERROR
	if psEnc.SCmn.Fs_kHz != fs_kHz || int(psEnc.SCmn.Prev_API_fs_Hz) != int(psEnc.SCmn.API_fs_Hz) {
		if psEnc.SCmn.Fs_kHz == 0 {
			ret += psEnc.SCmn.Resampler_state.Init(psEnc.SCmn.API_fs_Hz, int32(fs_kHz*1000), 1)
		} else {
			var (
				temp_resampler_state *ResamplerState
				new_buf_samples      int32
				old_buf_samples      int32
				buf_length_ms        int32
			)
			buf_length_ms = int32(int(int32(int(uint32(int32(psEnc.SCmn.Nb_subfr*5)))<<1)) + LA_SHAPE_MS)
			old_buf_samples = int32(int(buf_length_ms) * psEnc.SCmn.Fs_kHz)
			new_buf_samples = int32(int(buf_length_ms) * fs_kHz)
			x_bufFIX := make([]int16, func() int {
				if int(old_buf_samples) > int(new_buf_samples) {
					return int(old_buf_samples)
				}
				return int(new_buf_samples)
			}())
			silk_float2short_array([]int16(x_bufFIX), psEnc.X_buf[:], old_buf_samples)
			temp_resampler_state = new(ResamplerState)
			ret += temp_resampler_state.Init(int32(int(int32(int16(psEnc.SCmn.Fs_kHz)))*1000), psEnc.SCmn.API_fs_Hz, 0)
			api_buf_samples := int(buf_length_ms) * int(int32(int(psEnc.SCmn.API_fs_Hz)/1000))
			x_buf_API_fs_Hz := make([]int16, api_buf_samples)
			ret += temp_resampler_state.Resample([]int16(x_buf_API_fs_Hz), []int16(x_bufFIX), old_buf_samples)
			ret += psEnc.SCmn.Resampler_state.Init(psEnc.SCmn.API_fs_Hz, int32(int(int32(int16(fs_kHz)))*1000), 1)
			ret += psEnc.SCmn.Resampler_state.Resample([]int16(x_bufFIX), []int16(x_buf_API_fs_Hz), int32(api_buf_samples))
			silk_short2float_array(psEnc.X_buf[:], []int16(x_bufFIX), new_buf_samples)
		}
	}
	psEnc.SCmn.Prev_API_fs_Hz = psEnc.SCmn.API_fs_Hz
	return ret
}
func silk_setup_fs(psEnc *EncoderStateFLP, fs_kHz int, PacketSize_ms int) int {
	var ret int = SILK_NO_ERROR
	if PacketSize_ms != psEnc.SCmn.PacketSize_ms {
		if PacketSize_ms != 10 && PacketSize_ms != 20 && PacketSize_ms != 40 && PacketSize_ms != 60 {
			ret = -103
		}
		if PacketSize_ms <= 10 {
			psEnc.SCmn.NFramesPerPacket = 1
			if PacketSize_ms == 10 {
				psEnc.SCmn.Nb_subfr = 2
			} else {
				psEnc.SCmn.Nb_subfr = 1
			}
			psEnc.SCmn.Frame_length = int(int32(int16(PacketSize_ms))) * int(int32(int16(fs_kHz)))
			psEnc.SCmn.Pitch_LPC_win_length = int(int32(int16((int(LA_PITCH_MS<<1))+10))) * int(int32(int16(fs_kHz)))
			if psEnc.SCmn.Fs_kHz == 8 {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:][:]
			} else {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:][:]
			}
		} else {
			psEnc.SCmn.NFramesPerPacket = int(int32(PacketSize_ms / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR))))
			psEnc.SCmn.Nb_subfr = MAX_NB_SUBFR
			psEnc.SCmn.Frame_length = int(int32(int16(fs_kHz))) * 20
			psEnc.SCmn.Pitch_LPC_win_length = int(int32(int16((int(LA_PITCH_MS<<1))+20))) * int(int32(int16(fs_kHz)))
			if psEnc.SCmn.Fs_kHz == 8 {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:][:]
			} else {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_iCDF[:][:]
			}
		}
		psEnc.SCmn.PacketSize_ms = PacketSize_ms
		psEnc.SCmn.TargetRate_bps = 0
	}
	if psEnc.SCmn.Fs_kHz != fs_kHz {
		psEnc.SShape = ShapeStateFLP{}
		psEnc.SCmn.SNSQ = NSQState{}
		psEnc.SCmn.Prev_NLSFq_Q15 = [16]int16{}
		psEnc.SCmn.SLP.In_LP_State = [2]int32{}
		psEnc.SCmn.InputBufIx = 0
		psEnc.SCmn.NFramesEncoded = 0
		psEnc.SCmn.TargetRate_bps = 0
		psEnc.SCmn.PrevLag = 100
		psEnc.SCmn.First_frame_after_reset = 1
		psEnc.SShape.LastGainIndex = 10
		psEnc.SCmn.SNSQ.LagPrev = 100
		psEnc.SCmn.SNSQ.Prev_gain_Q16 = 65536
		psEnc.SCmn.PrevSignalType = TYPE_NO_VOICE_ACTIVITY
		psEnc.SCmn.Fs_kHz = fs_kHz
		if psEnc.SCmn.Fs_kHz == 8 {
			if psEnc.SCmn.Nb_subfr == MAX_NB_SUBFR {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:][:]
			} else {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:][:]
			}
		} else {
			if psEnc.SCmn.Nb_subfr == MAX_NB_SUBFR {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_iCDF[:][:]
			} else {
				psEnc.SCmn.Pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:][:]
			}
		}
		if psEnc.SCmn.Fs_kHz == 8 || psEnc.SCmn.Fs_kHz == 12 {
			psEnc.SCmn.PredictLPCOrder = MIN_LPC_ORDER
			psEnc.SCmn.PsNLSF_CB = &silk_NLSF_CB_NB_MB
		} else {
			psEnc.SCmn.PredictLPCOrder = MAX_LPC_ORDER
			psEnc.SCmn.PsNLSF_CB = &silk_NLSF_CB_WB
		}
		psEnc.SCmn.Subfr_length = SUB_FRAME_LENGTH_MS * fs_kHz
		psEnc.SCmn.Frame_length = int(int32(int16(psEnc.SCmn.Subfr_length))) * int(int32(int16(psEnc.SCmn.Nb_subfr)))
		psEnc.SCmn.Ltp_mem_length = LTP_MEM_LENGTH_MS * int(int32(int16(fs_kHz)))
		psEnc.SCmn.La_pitch = LA_PITCH_MS * int(int32(int16(fs_kHz)))
		psEnc.SCmn.Max_pitch_lag = int(int32(int16(fs_kHz))) * 18
		if psEnc.SCmn.Nb_subfr == MAX_NB_SUBFR {
			psEnc.SCmn.Pitch_LPC_win_length = int(int32(int16((int(LA_PITCH_MS<<1))+20))) * int(int32(int16(fs_kHz)))
		} else {
			psEnc.SCmn.Pitch_LPC_win_length = int(int32(int16((int(LA_PITCH_MS<<1))+10))) * int(int32(int16(fs_kHz)))
		}
		if psEnc.SCmn.Fs_kHz == 16 {
			psEnc.SCmn.Pitch_lag_low_bits_iCDF = silk_uniform8_iCDF[:][:]
		} else if psEnc.SCmn.Fs_kHz == 12 {
			psEnc.SCmn.Pitch_lag_low_bits_iCDF = silk_uniform6_iCDF[:][:]
		} else {
			psEnc.SCmn.Pitch_lag_low_bits_iCDF = silk_uniform4_iCDF[:][:]
		}
	}
	return ret
}
func silk_setup_complexity(psEncC *EncoderState, Complexity int) int {
	var ret int = 0
	if Complexity < 1 {
		psEncC.PitchEstimationComplexity = SILK_PE_MIN_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.8*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 6
		psEncC.ShapingLPCOrder = 12
		psEncC.La_shape = psEncC.Fs_kHz * 3
		psEncC.NStatesDelayedDecision = 1
		psEncC.UseInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 2
		psEncC.Warping_Q16 = 0
	} else if Complexity < 2 {
		psEncC.PitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.76*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 8
		psEncC.ShapingLPCOrder = 14
		psEncC.La_shape = psEncC.Fs_kHz * 5
		psEncC.NStatesDelayedDecision = 1
		psEncC.UseInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 3
		psEncC.Warping_Q16 = 0
	} else if Complexity < 3 {
		psEncC.PitchEstimationComplexity = SILK_PE_MIN_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.8*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 6
		psEncC.ShapingLPCOrder = 12
		psEncC.La_shape = psEncC.Fs_kHz * 3
		psEncC.NStatesDelayedDecision = 2
		psEncC.UseInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 2
		psEncC.Warping_Q16 = 0
	} else if Complexity < 4 {
		psEncC.PitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.76*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 8
		psEncC.ShapingLPCOrder = 14
		psEncC.La_shape = psEncC.Fs_kHz * 5
		psEncC.NStatesDelayedDecision = 2
		psEncC.UseInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 4
		psEncC.Warping_Q16 = 0
	} else if Complexity < 6 {
		psEncC.PitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.74*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 10
		psEncC.ShapingLPCOrder = 16
		psEncC.La_shape = psEncC.Fs_kHz * 5
		psEncC.NStatesDelayedDecision = 2
		psEncC.UseInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 6
		psEncC.Warping_Q16 = psEncC.Fs_kHz * int(int32(math.Floor(WARPING_MULTIPLIER*(1<<16)+0.5)))
	} else if Complexity < 8 {
		psEncC.PitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.72*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 12
		psEncC.ShapingLPCOrder = 20
		psEncC.La_shape = psEncC.Fs_kHz * 5
		psEncC.NStatesDelayedDecision = 3
		psEncC.UseInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 8
		psEncC.Warping_Q16 = psEncC.Fs_kHz * int(int32(math.Floor(WARPING_MULTIPLIER*(1<<16)+0.5)))
	} else {
		psEncC.PitchEstimationComplexity = SILK_PE_MAX_COMPLEX
		psEncC.PitchEstimationThreshold_Q16 = int32(math.Floor(0.7*(1<<16) + 0.5))
		psEncC.PitchEstimationLPCOrder = 16
		psEncC.ShapingLPCOrder = 24
		psEncC.La_shape = psEncC.Fs_kHz * 5
		psEncC.NStatesDelayedDecision = MAX_DEL_DEC_STATES
		psEncC.UseInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 16
		psEncC.Warping_Q16 = psEncC.Fs_kHz * int(int32(math.Floor(WARPING_MULTIPLIER*(1<<16)+0.5)))
	}
	psEncC.PitchEstimationLPCOrder = silk_min_int(psEncC.PitchEstimationLPCOrder, psEncC.PredictLPCOrder)
	psEncC.ShapeWinLength = SUB_FRAME_LENGTH_MS*psEncC.Fs_kHz + psEncC.La_shape*2
	psEncC.Complexity = Complexity
	return ret
}
func silk_setup_LBRR(psEncC *EncoderState, encControl *EncControlStruct) int {
	var (
		LBRR_in_previous_packet int
		ret                     int = SILK_NO_ERROR
	)
	LBRR_in_previous_packet = psEncC.LBRR_enabled
	psEncC.LBRR_enabled = encControl.LBRR_coded
	if psEncC.LBRR_enabled != 0 {
		if LBRR_in_previous_packet == 0 {
			psEncC.LBRR_GainIncreases = 7
		} else {
			psEncC.LBRR_GainIncreases = silk_max_int(7-int(int32((int64(int32(psEncC.PacketLoss_perc))*int64(int16(int32(math.Floor(0.2*(1<<16)+0.5)))))>>16)), 3)
		}
	}
	return ret
}
