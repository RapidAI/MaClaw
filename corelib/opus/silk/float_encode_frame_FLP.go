package silk

import (
	"math"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/cmath"
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func EncodeDoVAD_FLP(psEnc *EncoderStateFLP, activity int) {
	var activity_threshold int = int(int32(math.Floor(SPEECH_ACTIVITY_DTX_THRES*(1<<8) + 0.5)))
	_ = psEnc.SCmn.Arch
	VAD_GetSA_Q8_c(&psEnc.SCmn, psEnc.SCmn.InputBuf[1:])
	if activity == VAD_NO_ACTIVITY && psEnc.SCmn.Speech_activity_Q8 >= activity_threshold {
		psEnc.SCmn.Speech_activity_Q8 = activity_threshold - 1
	}
	if psEnc.SCmn.Speech_activity_Q8 < activity_threshold {
		psEnc.SCmn.Indices.SignalType = TYPE_NO_VOICE_ACTIVITY
		psEnc.SCmn.NoSpeechCounter++
		if psEnc.SCmn.NoSpeechCounter <= NB_SPEECH_FRAMES_BEFORE_DTX {
			psEnc.SCmn.InDTX = 0
		} else if psEnc.SCmn.NoSpeechCounter > int(MAX_CONSECUTIVE_DTX+NB_SPEECH_FRAMES_BEFORE_DTX) {
			psEnc.SCmn.NoSpeechCounter = NB_SPEECH_FRAMES_BEFORE_DTX
			psEnc.SCmn.InDTX = 0
		}
		psEnc.SCmn.VAD_flags[psEnc.SCmn.NFramesEncoded] = 0
	} else {
		psEnc.SCmn.NoSpeechCounter = 0
		psEnc.SCmn.InDTX = 0
		psEnc.SCmn.Indices.SignalType = TYPE_UNVOICED
		psEnc.SCmn.VAD_flags[psEnc.SCmn.NFramesEncoded] = 1
	}
}
func EncodeFrame_FLP(psEnc *EncoderStateFLP, pnBytesOut *int32, psRangeEnc *entcode.Encoder, condCoding int, maxBits int, useCBR int) int {
	var (
		sEncCtrl               EncoderControlFLP
		i                      int
		iter                   int
		maxIter                int
		found_upper            int
		found_lower            int
		ret                    int = 0
		x_frame                []float32
		res_pitch_frame        []float32
		res_pitch              [672]float32
		sRangeEnc_copy         entcode.Encoder
		sRangeEnc_copy2        entcode.Encoder
		sNSQ_copy              NSQState
		sNSQ_copy2             NSQState
		seed_copy              int32
		nBits                  int32
		nBits_lower            int32
		nBits_upper            int32
		gainMult_lower         int32
		gainMult_upper         int32
		gainsID                int32
		gainsID_lower          int32
		gainsID_upper          int32
		gainMult_Q8            int16
		ec_prevLagIndex_copy   int16
		ec_prevSignalType_copy int
		LastGainIndex_copy2    int8
		pGains_Q16             [4]int32
		ec_buf_copy            [1275]uint8
		gain_lock              [4]int = [4]int{}
		best_gain_mult         [4]int16
		best_sum               [4]int
	)
	LastGainIndex_copy2 = int8(func() int32 {
		nBits_lower = func() int32 {
			nBits_upper = func() int32 {
				gainMult_lower = func() int32 {
					gainMult_upper = 0
					return gainMult_upper
				}()
				return gainMult_lower
			}()
			return nBits_upper
		}()
		return nBits_lower
	}())
	psEnc.SCmn.Indices.Seed = int8(int(func() int32 {
		p := &psEnc.SCmn.FrameCounter
		x := *p
		*p++
		return x
	}()) & 3)
	x_frame = psEnc.X_buf[psEnc.SCmn.Ltp_mem_length:]
	res_pitch_frame = res_pitch[psEnc.SCmn.Ltp_mem_length:]
	LP_variable_cutoff(&psEnc.SCmn.SLP, psEnc.SCmn.InputBuf[1:], psEnc.SCmn.Frame_length)
	silk_short2float_array(x_frame[LA_SHAPE_MS*psEnc.SCmn.Fs_kHz:], psEnc.SCmn.InputBuf[1:], int32(psEnc.SCmn.Frame_length))
	for i = 0; i < 8; i++ {
		x_frame[LA_SHAPE_MS*psEnc.SCmn.Fs_kHz+i*(psEnc.SCmn.Frame_length>>3)] += float32(float64(1-(i&2)) * 1e-06)
	}
	if psEnc.SCmn.PrefillFlag == 0 {
		FindPitchLags_FLP(psEnc, &sEncCtrl, res_pitch[:], []float32(x_frame), psEnc.SCmn.Arch)
		NoiseShapeAnalysis_FLP(psEnc, &sEncCtrl, []float32(res_pitch_frame), []float32(x_frame))
		Find_pred_coefs_FLP(psEnc, &sEncCtrl, []float32(res_pitch_frame), []float32(x_frame), condCoding)
		Process_gains_FLP(psEnc, &sEncCtrl, condCoding)
		silk_LBRR_encode_FLP(psEnc, &sEncCtrl, []float32(x_frame), condCoding)
		maxIter = 6
		gainMult_Q8 = int16(int32(math.Floor(1*(1<<8) + 0.5)))
		found_lower = 0
		found_upper = 0
		gainsID = GainsID(psEnc.SCmn.Indices.GainsIndices, psEnc.SCmn.Nb_subfr)
		gainsID_lower = -1
		gainsID_upper = -1
		sRangeEnc_copy = *psRangeEnc
		sNSQ_copy = psEnc.SCmn.SNSQ
		seed_copy = int32(psEnc.SCmn.Indices.Seed)
		ec_prevLagIndex_copy = psEnc.SCmn.Ec_prevLagIndex
		ec_prevSignalType_copy = psEnc.SCmn.Ec_prevSignalType
		for iter = 0; ; iter++ {
			if int(gainsID) == int(gainsID_lower) {
				nBits = nBits_lower
			} else if int(gainsID) == int(gainsID_upper) {
				nBits = nBits_upper
			} else {
				if iter > 0 {
					*psRangeEnc = sRangeEnc_copy
					psEnc.SCmn.SNSQ = sNSQ_copy
					psEnc.SCmn.Indices.Seed = int8(seed_copy)
					psEnc.SCmn.Ec_prevLagIndex = ec_prevLagIndex_copy
					psEnc.SCmn.Ec_prevSignalType = ec_prevSignalType_copy
				}
				NSQ_wrapper_FLP(psEnc, &sEncCtrl, &psEnc.SCmn.Indices, &psEnc.SCmn.SNSQ, psEnc.SCmn.Pulses[:], []float32(x_frame))
				if iter == maxIter && found_lower == 0 {
					sRangeEnc_copy2 = *psRangeEnc
				}
				EncodeIndices(&psEnc.SCmn, psRangeEnc, psEnc.SCmn.NFramesEncoded, 0, condCoding)
				EncodePulses(psRangeEnc, int(psEnc.SCmn.Indices.SignalType), int(psEnc.SCmn.Indices.QuantOffsetType), psEnc.SCmn.Pulses[:], psEnc.SCmn.Frame_length)
				nBits = int32(psRangeEnc.Tell())
				if iter == maxIter && found_lower == 0 && int(nBits) > maxBits {
					*psRangeEnc = sRangeEnc_copy2
					psEnc.SShape.LastGainIndex = sEncCtrl.LastGainIndexPrev
					for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
						psEnc.SCmn.Indices.GainsIndices[i] = 4
					}
					if condCoding != CODE_CONDITIONALLY {
						psEnc.SCmn.Indices.GainsIndices[0] = sEncCtrl.LastGainIndexPrev
					}
					psEnc.SCmn.Ec_prevLagIndex = ec_prevLagIndex_copy
					psEnc.SCmn.Ec_prevSignalType = ec_prevSignalType_copy
					for i = 0; i < psEnc.SCmn.Frame_length; i++ {
						psEnc.SCmn.Pulses[i] = 0
					}
					EncodeIndices(&psEnc.SCmn, psRangeEnc, psEnc.SCmn.NFramesEncoded, 0, condCoding)
					EncodePulses(psRangeEnc, int(psEnc.SCmn.Indices.SignalType), int(psEnc.SCmn.Indices.QuantOffsetType), psEnc.SCmn.Pulses[:], psEnc.SCmn.Frame_length)
					nBits = int32(psRangeEnc.Tell())
				}
				if useCBR == 0 && iter == 0 && int(nBits) <= maxBits {
					break
				}
			}
			if iter == maxIter {
				if found_lower != 0 && (int(gainsID) == int(gainsID_lower) || int(nBits) > maxBits) {
					*psRangeEnc = sRangeEnc_copy2
					copy(psRangeEnc.Buf[:sRangeEnc_copy2.Offs], ec_buf_copy[:sRangeEnc_copy2.Offs])
					psEnc.SCmn.SNSQ = sNSQ_copy2
					psEnc.SShape.LastGainIndex = LastGainIndex_copy2
				}
				break
			}
			if int(nBits) > maxBits {
				if found_lower == 0 && iter >= 2 {
					if (sEncCtrl.Lambda * 1.5) > 1.5 {
						sEncCtrl.Lambda = sEncCtrl.Lambda * 1.5
					} else {
						sEncCtrl.Lambda = 1.5
					}
					psEnc.SCmn.Indices.QuantOffsetType = 0
					found_upper = 0
					gainsID_upper = -1
				} else {
					found_upper = 1
					nBits_upper = nBits
					gainMult_upper = int32(gainMult_Q8)
					gainsID_upper = gainsID
				}
			} else if int(nBits) < maxBits-5 {
				found_lower = 1
				nBits_lower = nBits
				gainMult_lower = int32(gainMult_Q8)
				if int(gainsID) != int(gainsID_lower) {
					gainsID_lower = gainsID
					sRangeEnc_copy2 = *psRangeEnc
					copy(ec_buf_copy[:psRangeEnc.Offs], psRangeEnc.Buf[:psRangeEnc.Offs])
					sNSQ_copy2 = psEnc.SCmn.SNSQ
					LastGainIndex_copy2 = psEnc.SShape.LastGainIndex
				}
			} else {
				break
			}
			if found_lower == 0 && int(nBits) > maxBits {
				var j int
				for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
					var sum int = 0
					for j = i * psEnc.SCmn.Subfr_length; j < (i+1)*psEnc.SCmn.Subfr_length; j++ {
						sum += int(cmath.Abs(int64(psEnc.SCmn.Pulses[j])))
					}
					if iter == 0 || sum < best_sum[i] && gain_lock[i] == 0 {
						best_sum[i] = sum
						best_gain_mult[i] = gainMult_Q8
					} else {
						gain_lock[i] = 1
					}
				}
			}
			if (found_lower & found_upper) == 0 {
				if int(nBits) > maxBits {
					if int(gainMult_Q8) < 16384 {
						gainMult_Q8 *= 2
					} else {
						gainMult_Q8 = math.MaxInt16
					}
				} else {
					var gain_factor_Q16 int32
					gain_factor_Q16 = silk_log2lin(int32(int(int32(int(uint32(int32(int(nBits)-maxBits)))<<7))/psEnc.SCmn.Frame_length + int(int32(math.Floor(16*(1<<7)+0.5)))))
					gainMult_Q8 = int16(int32((int64(gain_factor_Q16) * int64(gainMult_Q8)) >> 16))
				}
			} else {
				gainMult_Q8 = int16(int(gainMult_lower) + ((int(gainMult_upper)-int(gainMult_lower))*(maxBits-int(nBits_lower)))/(int(nBits_upper)-int(nBits_lower)))
				if int(gainMult_Q8) > (int(gainMult_lower) + ((int(gainMult_upper) - int(gainMult_lower)) >> 2)) {
					gainMult_Q8 = int16(int(gainMult_lower) + ((int(gainMult_upper) - int(gainMult_lower)) >> 2))
				} else if int(gainMult_Q8) < (int(gainMult_upper) - ((int(gainMult_upper) - int(gainMult_lower)) >> 2)) {
					gainMult_Q8 = int16(int(gainMult_upper) - ((int(gainMult_upper) - int(gainMult_lower)) >> 2))
				}
			}
			for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
				var tmp int16
				if gain_lock[i] != 0 {
					tmp = best_gain_mult[i]
				} else {
					tmp = gainMult_Q8
				}
				pGains_Q16[i] = int32(int(uint32(int32(func() int {
					if (int(math.MinInt32) >> 8) > (int(math.MaxInt32 >> 8)) {
						if int(int32((int64(sEncCtrl.GainsUnq_Q16[i])*int64(tmp))>>16)) > (int(math.MinInt32) >> 8) {
							return int(math.MinInt32) >> 8
						}
						if int(int32((int64(sEncCtrl.GainsUnq_Q16[i])*int64(tmp))>>16)) < (int(math.MaxInt32 >> 8)) {
							return int(math.MaxInt32 >> 8)
						}
						return int(int32((int64(sEncCtrl.GainsUnq_Q16[i]) * int64(tmp)) >> 16))
					}
					if int(int32((int64(sEncCtrl.GainsUnq_Q16[i])*int64(tmp))>>16)) > (int(math.MaxInt32 >> 8)) {
						return int(math.MaxInt32 >> 8)
					}
					if int(int32((int64(sEncCtrl.GainsUnq_Q16[i])*int64(tmp))>>16)) < (int(math.MinInt32) >> 8) {
						return int(math.MinInt32) >> 8
					}
					return int(int32((int64(sEncCtrl.GainsUnq_Q16[i]) * int64(tmp)) >> 16))
				}()))) << 8)
			}
			psEnc.SShape.LastGainIndex = sEncCtrl.LastGainIndexPrev
			GainsQuant(psEnc.SCmn.Indices.GainsIndices, pGains_Q16, &psEnc.SShape.LastGainIndex, int(libc.BoolToInt(condCoding == CODE_CONDITIONALLY)), psEnc.SCmn.Nb_subfr)
			gainsID = GainsID(psEnc.SCmn.Indices.GainsIndices, psEnc.SCmn.Nb_subfr)
			for i = 0; i < psEnc.SCmn.Nb_subfr; i++ {
				sEncCtrl.Gains[i] = float32(float64(pGains_Q16[i]) / 65536.0)
			}
		}
	}
	copy(psEnc.X_buf[:psEnc.SCmn.Ltp_mem_length+LA_SHAPE_MS*psEnc.SCmn.Fs_kHz], psEnc.X_buf[psEnc.SCmn.Frame_length:])
	if psEnc.SCmn.PrefillFlag != 0 {
		*pnBytesOut = 0
		return ret
	}
	psEnc.SCmn.PrevLag = sEncCtrl.PitchL[psEnc.SCmn.Nb_subfr-1]
	psEnc.SCmn.PrevSignalType = psEnc.SCmn.Indices.SignalType
	psEnc.SCmn.First_frame_after_reset = 0
	*pnBytesOut = int32((psRangeEnc.Tell() + 7) >> 3)
	return ret
}
func silk_LBRR_encode_FLP(psEnc *EncoderStateFLP, psEncCtrl *EncoderControlFLP, xfw []float32, condCoding int) {
	var (
		k              int
		Gains_Q16      [4]int32
		TempGains      [4]float32
		psIndices_LBRR = &psEnc.SCmn.Indices_LBRR[psEnc.SCmn.NFramesEncoded]
		sNSQ_LBRR      NSQState
	)
	if psEnc.SCmn.LBRR_enabled != 0 && psEnc.SCmn.Speech_activity_Q8 > int(int32(math.Floor(LBRR_SPEECH_ACTIVITY_THRES*(1<<8)+0.5))) {
		psEnc.SCmn.LBRR_flags[psEnc.SCmn.NFramesEncoded] = 1
		sNSQ_LBRR = psEnc.SCmn.SNSQ
		*psIndices_LBRR = psEnc.SCmn.Indices
		copy(TempGains[:psEnc.SCmn.Nb_subfr], psEncCtrl.Gains[:psEnc.SCmn.Nb_subfr])
		if psEnc.SCmn.NFramesEncoded == 0 || psEnc.SCmn.LBRR_flags[psEnc.SCmn.NFramesEncoded-1] == 0 {
			psEnc.SCmn.LBRRprevLastGainIndex = psEnc.SShape.LastGainIndex
			psIndices_LBRR.GainsIndices[0] += int8(psEnc.SCmn.LBRR_GainIncreases)
			psIndices_LBRR.GainsIndices[0] = int8(silk_min_int(int(psIndices_LBRR.GainsIndices[0]), int(N_LEVELS_QGAIN-1)))
		}
		GainsDequant(Gains_Q16, psIndices_LBRR.GainsIndices, &psEnc.SCmn.LBRRprevLastGainIndex, int(libc.BoolToInt(condCoding == CODE_CONDITIONALLY)), psEnc.SCmn.Nb_subfr)
		for k = 0; k < psEnc.SCmn.Nb_subfr; k++ {
			psEncCtrl.Gains[k] = float32(float64(Gains_Q16[k]) * (1.0 / 65536.0))
		}
		NSQ_wrapper_FLP(psEnc, psEncCtrl, psIndices_LBRR, &sNSQ_LBRR, psEnc.SCmn.Pulses_LBRR[psEnc.SCmn.NFramesEncoded][:], xfw)
		copy(psEncCtrl.Gains[:psEnc.SCmn.Nb_subfr], TempGains[:psEnc.SCmn.Nb_subfr])
	}
}
