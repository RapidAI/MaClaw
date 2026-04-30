package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

type Decoder struct {
	Channel_state           [DECODER_NUM_CHANNELS]DecoderState
	SStereo                 StereoDecState
	NChannelsAPI            int
	NChannelsInternal       int
	Prev_decode_only_middle int
}

func GetDecoderSize() int {
	return int(unsafe.Sizeof(Decoder{}))
}
func (dec *Decoder) Init() int {
	for i := 0; i < DECODER_NUM_CHANNELS; i++ {
		dec.Channel_state[i].Init()
	}
	dec.SStereo = StereoDecState{}
	dec.Prev_decode_only_middle = 0
	return SILK_NO_ERROR
}
func (dec *Decoder) Decode(decControl *DecControlStruct, lostFlag int, newPacketFlag int, psRangeDec *entcode.Decoder, samplesOut []int16, nSamplesOut *int32, arch int) int {
	var (
		i                  int
		n                  int
		decode_only_middle int = 0
		ret                int = SILK_NO_ERROR
		nSamplesOutDec     int32
		LBRR_symbol        int32
		samplesOut1_tmp    [2][]int16
		MS_pred_Q13        [2]int32
		resample_out_ptr   []int16
		channel_state      = dec.Channel_state[:]
		has_side           int
		delay_stack_alloc  int
	)
	if newPacketFlag != 0 {
		for n = 0; n < int(decControl.NChannelsInternal); n++ {
			channel_state[n].NFramesDecoded = 0
		}
	}
	if int(decControl.NChannelsInternal) > dec.NChannelsInternal {
		channel_state[1].Init()
	}
	stereo_to_mono := decControl.NChannelsInternal == 1 && dec.NChannelsInternal == 2 && int(decControl.InternalSampleRate) == channel_state[0].Fs_kHz*1000
	if channel_state[0].NFramesDecoded == 0 {
		for n = 0; n < int(decControl.NChannelsInternal); n++ {
			var fs_kHz_dec int
			if decControl.PayloadSize_ms == 0 {
				channel_state[n].NFramesPerPacket = 1
				channel_state[n].Nb_subfr = 2
			} else if decControl.PayloadSize_ms == 10 {
				channel_state[n].NFramesPerPacket = 1
				channel_state[n].Nb_subfr = 2
			} else if decControl.PayloadSize_ms == 20 {
				channel_state[n].NFramesPerPacket = 1
				channel_state[n].Nb_subfr = 4
			} else if decControl.PayloadSize_ms == 40 {
				channel_state[n].NFramesPerPacket = 2
				channel_state[n].Nb_subfr = 4
			} else if decControl.PayloadSize_ms == 60 {
				channel_state[n].NFramesPerPacket = 3
				channel_state[n].Nb_subfr = 4
			} else {
				return -203
			}
			fs_kHz_dec = (int(decControl.InternalSampleRate) >> 10) + 1
			if fs_kHz_dec != 8 && fs_kHz_dec != 12 && fs_kHz_dec != 16 {
				return -200
			}
			ret += channel_state[n].SetFS(fs_kHz_dec, decControl.API_sampleRate)
		}
	}
	if int(decControl.NChannelsAPI) == 2 && int(decControl.NChannelsInternal) == 2 && (dec.NChannelsAPI == 1 || dec.NChannelsInternal == 1) {
		dec.SStereo.Pred_prev_Q13 = [2]int16{}
		dec.SStereo.SSide = [2]int16{}
		channel_state[1].Resampler_state = channel_state[0].Resampler_state
	}
	dec.NChannelsAPI = int(decControl.NChannelsAPI)
	dec.NChannelsInternal = int(decControl.NChannelsInternal)
	if int(decControl.API_sampleRate) > int(MAX_API_FS_KHZ*1000) || int(decControl.API_sampleRate) < 8000 {
		ret = -200
		return ret
	}
	if lostFlag != FLAG_PACKET_LOST && channel_state[0].NFramesDecoded == 0 {
		for n = 0; n < int(decControl.NChannelsInternal); n++ {
			for i = 0; i < channel_state[n].NFramesPerPacket; i++ {
				channel_state[n].VAD_flags[i] = psRangeDec.DecBitLogp(1)
			}
			channel_state[n].LBRR_flag = psRangeDec.DecBitLogp(1)
		}
		for n = 0; n < int(decControl.NChannelsInternal); n++ {
			*(*[3]int)(unsafe.Pointer(&channel_state[n].LBRR_flags[0])) = [3]int{}
			if channel_state[n].LBRR_flag != 0 {
				if channel_state[n].NFramesPerPacket == 1 {
					channel_state[n].LBRR_flags[0] = 1
				} else {
					LBRR_symbol = int32(psRangeDec.DecIcdf(silk_LBRR_flags_iCDF_ptr[channel_state[n].NFramesPerPacket-2], 8) + 1)
					for i = 0; i < channel_state[n].NFramesPerPacket; i++ {
						channel_state[n].LBRR_flags[i] = (int(LBRR_symbol) >> i) & 1
					}
				}
			}
		}
		if lostFlag == FLAG_DECODE_NORMAL {
			for i = 0; i < channel_state[0].NFramesPerPacket; i++ {
				for n = 0; n < int(decControl.NChannelsInternal); n++ {
					if channel_state[n].LBRR_flags[i] != 0 {
						var (
							pulses     [320]int16
							condCoding int
						)
						if int(decControl.NChannelsInternal) == 2 && n == 0 {
							StereoDecodePred(psRangeDec, MS_pred_Q13[:])
							if channel_state[1].LBRR_flags[i] == 0 {
								decode_only_middle = StereoDecodeMidOnly(psRangeDec)
							}
						}
						if i > 0 && channel_state[n].LBRR_flags[i-1] != 0 {
							condCoding = CODE_CONDITIONALLY
						} else {
							condCoding = CODE_INDEPENDENTLY
						}
						DecodeIndices(&channel_state[n], psRangeDec, i, true, condCoding)
						DecodePulses(psRangeDec, pulses[:], int(channel_state[n].Indices.SignalType), int(channel_state[n].Indices.QuantOffsetType), channel_state[n].Frame_length)
					}
				}
			}
		}
	}
	if int(decControl.NChannelsInternal) == 2 {
		if lostFlag == FLAG_DECODE_NORMAL || lostFlag == FLAG_DECODE_LBRR && channel_state[0].LBRR_flags[channel_state[0].NFramesDecoded] == 1 {
			StereoDecodePred(psRangeDec, MS_pred_Q13[:])
			if lostFlag == FLAG_DECODE_NORMAL && channel_state[1].VAD_flags[channel_state[0].NFramesDecoded] == 0 || lostFlag == FLAG_DECODE_LBRR && channel_state[1].LBRR_flags[channel_state[0].NFramesDecoded] == 0 {
				decode_only_middle = StereoDecodeMidOnly(psRangeDec)
			} else {
				decode_only_middle = 0
			}
		} else {
			for n = 0; n < 2; n++ {
				MS_pred_Q13[n] = int32(dec.SStereo.Pred_prev_Q13[n])
			}
		}
	}
	if int(decControl.NChannelsInternal) == 2 && decode_only_middle == 0 && dec.Prev_decode_only_middle == 1 {
		dec.Channel_state[1].OutBuf = [480]int16{}
		dec.Channel_state[1].SLPC_Q14_buf = [16]int32{}
		dec.Channel_state[1].LagPrev = 100
		dec.Channel_state[1].LastGainIndex = 10
		dec.Channel_state[1].PrevSignalType = TYPE_NO_VOICE_ACTIVITY
		dec.Channel_state[1].First_frame_after_reset = 1
	}
	delay_stack_alloc = int(libc.BoolToInt(int(decControl.InternalSampleRate)*int(decControl.NChannelsInternal) < int(decControl.API_sampleRate)*int(decControl.NChannelsAPI)))
	samplesOut1_tmp_storage1 := make([]int16, func() int {
		if delay_stack_alloc != 0 {
			return 0
		}
		return int(decControl.NChannelsInternal) * (channel_state[0].Frame_length + 2)
	}())
	if delay_stack_alloc != 0 {
		samplesOut1_tmp[0] = samplesOut
		samplesOut1_tmp[1] = samplesOut[channel_state[0].Frame_length+2:]
	} else {
		samplesOut1_tmp[0] = samplesOut1_tmp_storage1
		samplesOut1_tmp[1] = samplesOut1_tmp_storage1[channel_state[0].Frame_length+2:]
	}
	if lostFlag == FLAG_DECODE_NORMAL {
		has_side = int(libc.BoolToInt(decode_only_middle == 0))
	} else {
		has_side = int(libc.BoolToInt(dec.Prev_decode_only_middle == 0 || int(decControl.NChannelsInternal) == 2 && lostFlag == FLAG_DECODE_LBRR && channel_state[1].LBRR_flags[channel_state[1].NFramesDecoded] == 1))
	}
	for n = 0; n < int(decControl.NChannelsInternal); n++ {
		if n == 0 || has_side != 0 {
			var (
				FrameIndex int
				condCoding int
			)
			FrameIndex = channel_state[0].NFramesDecoded - n
			if FrameIndex <= 0 {
				condCoding = CODE_INDEPENDENTLY
			} else if lostFlag == FLAG_DECODE_LBRR {
				if channel_state[n].LBRR_flags[FrameIndex-1] != 0 {
					condCoding = CODE_CONDITIONALLY
				} else {
					condCoding = CODE_INDEPENDENTLY
				}
			} else if n > 0 && dec.Prev_decode_only_middle != 0 {
				condCoding = CODE_INDEPENDENTLY_NO_LTP_SCALING
			} else {
				condCoding = CODE_CONDITIONALLY
			}
			ret += DecodeFrame(&channel_state[n], psRangeDec, samplesOut1_tmp[n][2:], &nSamplesOutDec, lostFlag, condCoding, arch)
		} else {
			libc.MemSet(unsafe.Pointer(&samplesOut1_tmp[n][2]), 0, int(uintptr(nSamplesOutDec)*unsafe.Sizeof(int16(0))))
		}
		channel_state[n].NFramesDecoded++
	}
	if int(decControl.NChannelsAPI) == 2 && int(decControl.NChannelsInternal) == 2 {
		StereoMStoLR(&dec.SStereo, samplesOut1_tmp[0], samplesOut1_tmp[1], MS_pred_Q13[:], channel_state[0].Fs_kHz, int(nSamplesOutDec))
	} else {
		libc.MemCpy(unsafe.Pointer(&samplesOut1_tmp[0]), unsafe.Pointer(&dec.SStereo.SMid[0]), int(2*unsafe.Sizeof(int16(0))))
		libc.MemCpy(unsafe.Pointer(&dec.SStereo.SMid[0]), unsafe.Pointer(&samplesOut1_tmp[0][nSamplesOutDec]), int(2*unsafe.Sizeof(int16(0))))
	}
	*nSamplesOut = int32((int(nSamplesOutDec) * int(decControl.API_sampleRate)) / (int(int16(channel_state[0].Fs_kHz)) * 1000))
	samplesOut2_tmp := make([]int16, func() int {
		if int(decControl.NChannelsAPI) == 2 {
			return int(*nSamplesOut)
		}
		return 0
	}())
	if int(decControl.NChannelsAPI) == 2 {
		resample_out_ptr = samplesOut2_tmp
	} else {
		resample_out_ptr = samplesOut
	}
	samplesOut1_tmp_storage2 := make([]int16, func() int {
		if delay_stack_alloc != 0 {
			return int(decControl.NChannelsInternal) * (channel_state[0].Frame_length + 2)
		}
		return 0
	}())
	if delay_stack_alloc != 0 {
		libc.MemCpy(unsafe.Pointer(&samplesOut1_tmp_storage2[0]), unsafe.Pointer(&samplesOut[0]), int(decControl.NChannelsInternal)*(channel_state[0].Frame_length+2)*int(unsafe.Sizeof(int16(0))))
		samplesOut1_tmp[0] = samplesOut1_tmp_storage2
		samplesOut1_tmp[1] = samplesOut1_tmp_storage2[channel_state[0].Frame_length+2:]
	}
	for n = 0; n < (func() int {
		if int(decControl.NChannelsAPI) < int(decControl.NChannelsInternal) {
			return int(decControl.NChannelsAPI)
		}
		return int(decControl.NChannelsInternal)
	}()); n++ {
		ret += channel_state[n].Resampler_state.Resample(resample_out_ptr, samplesOut1_tmp[n][1:], nSamplesOutDec)
		if int(decControl.NChannelsAPI) == 2 {
			for i = 0; i < int(*nSamplesOut); i++ {
				samplesOut[n+i*2] = resample_out_ptr[i]
			}
		}
	}
	if int(decControl.NChannelsAPI) == 2 && int(decControl.NChannelsInternal) == 1 {
		if stereo_to_mono {
			ret += channel_state[1].Resampler_state.Resample(resample_out_ptr, samplesOut1_tmp[0][1:], nSamplesOutDec)
			for i = 0; i < int(*nSamplesOut); i++ {
				samplesOut[i*2+1] = resample_out_ptr[i]
			}
		} else {
			for i = 0; i < int(*nSamplesOut); i++ {
				samplesOut[i*2+1] = samplesOut[i*2+0]
			}
		}
	}
	if channel_state[0].PrevSignalType == TYPE_VOICED {
		var mult_tab = [3]int{6, 4, 3}
		decControl.PrevPitchLag = channel_state[0].LagPrev * mult_tab[(channel_state[0].Fs_kHz-8)>>2]
	} else {
		decControl.PrevPitchLag = 0
	}
	if lostFlag == FLAG_PACKET_LOST {
		for i = 0; i < dec.NChannelsInternal; i++ {
			dec.Channel_state[i].LastGainIndex = 10
		}
	} else {
		dec.Prev_decode_only_middle = decode_only_middle
	}
	return ret
}
