package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

type OpusDecoder struct {
	Celt_dec_offset      int
	Silk_dec_offset      int
	Channels             int
	Fs                   int32
	DecControl           silk_DecControlStruct
	Decode_gain          int
	Arch                 int
	Stream_channels      int
	Bandwidth            int
	Mode                 int
	Prev_mode            int
	Frame_size           int
	Prev_redundancy      int
	Last_packet_duration int
	Softclip_mem         [2]opus_val16
	RangeFinal           uint32
}

func opus_decoder_get_size(channels int) int {
	var (
		silkDecSizeBytes int
		celtDecSizeBytes int
		ret              int
	)
	if channels < 1 || channels > 2 {
		return 0
	}
	ret = silk_Get_Decoder_Size(&silkDecSizeBytes)
	if ret != 0 {
		return 0
	}
	silkDecSizeBytes = align(silkDecSizeBytes)
	celtDecSizeBytes = celt_decoder_get_size(channels)
	return align(int(unsafe.Sizeof(OpusDecoder{}))) + silkDecSizeBytes + celtDecSizeBytes
}
func opus_decoder_init(st *OpusDecoder, Fs int32, channels int) int {
	var (
		silk_dec         unsafe.Pointer
		celt_dec         *OpusCustomDecoder
		ret              int
		silkDecSizeBytes int
	)
	if int(Fs) != 48000 && int(Fs) != 24000 && int(Fs) != 16000 && int(Fs) != 12000 && int(Fs) != 8000 || channels != 1 && channels != 2 {
		return -1
	}
	libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(st))), 0, opus_decoder_get_size(channels)*int(unsafe.Sizeof(byte(0))))
	ret = silk_Get_Decoder_Size(&silkDecSizeBytes)
	if ret != 0 {
		return -3
	}
	silkDecSizeBytes = align(silkDecSizeBytes)
	st.Silk_dec_offset = align(int(unsafe.Sizeof(OpusDecoder{})))
	st.Celt_dec_offset = st.Silk_dec_offset + silkDecSizeBytes
	silk_dec = unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Silk_dec_offset)
	celt_dec = (*OpusCustomDecoder)(unsafe.Pointer((*byte)(unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Celt_dec_offset))))
	st.Stream_channels = func() int {
		p := &st.Channels
		st.Channels = channels
		return *p
	}()
	st.Fs = Fs
	st.DecControl.API_sampleRate = st.Fs
	st.DecControl.NChannelsAPI = int32(st.Channels)
	ret = silk_InitDecoder(silk_dec)
	if ret != 0 {
		return -3
	}
	ret = celt_decoder_init(celt_dec, Fs, channels)
	if ret != OPUS_OK {
		return -3
	}
	opus_custom_decoder_ctl(celt_dec, CELT_SET_SIGNALLING_REQUEST, func() int {
		return 0
	}())
	st.Prev_mode = 0
	st.Frame_size = int(Fs) / 400
	st.Arch = opus_select_arch()
	return OPUS_OK
}
func opus_decoder_create(Fs int32, channels int, error *int) *OpusDecoder {
	var (
		ret int
		st  *OpusDecoder
	)
	if int(Fs) != 48000 && int(Fs) != 24000 && int(Fs) != 16000 && int(Fs) != 12000 && int(Fs) != 8000 || channels != 1 && channels != 2 {
		if error != nil {
			*error = -1
		}
		return nil
	}
	st = (*OpusDecoder)(libc.Malloc(opus_decoder_get_size(channels)))
	if st == nil {
		if error != nil {
			*error = -7
		}
		return nil
	}
	ret = opus_decoder_init(st, Fs, channels)
	if error != nil {
		*error = ret
	}
	if ret != OPUS_OK {
		libc.Free(unsafe.Pointer(st))
		st = nil
	}
	return st
}
func smooth_fade(in1 *opus_val16, in2 *opus_val16, out *opus_val16, overlap int, channels int, window *opus_val16, Fs int32) {
	var (
		i   int
		c   int
		inc int = 48000 / int(Fs)
	)
	for c = 0; c < channels; c++ {
		for i = 0; i < overlap; i++ {
			var w opus_val16 = ((*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i*inc)))) * (*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i*inc)))))
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(opus_val16(0))*uintptr(i*channels+c))) = opus_val16((opus_val32(w) * opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(in2), unsafe.Sizeof(opus_val16(0))*uintptr(i*channels+c))))) + opus_val32(Q15ONE-w)*opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(in1), unsafe.Sizeof(opus_val16(0))*uintptr(i*channels+c)))))
		}
	}
}
func opus_packet_get_mode(data *uint8) int {
	var mode int
	if int(*data)&0x80 != 0 {
		mode = MODE_CELT_ONLY
	} else if (int(*data) & 0x60) == 0x60 {
		mode = MODE_HYBRID
	} else {
		mode = MODE_SILK_ONLY
	}
	return mode
}
func opus_decode_frame(st *OpusDecoder, data *uint8, len_ int32, pcm *opus_val16, frame_size int, decode_fec int) int {
	var (
		silk_dec                 unsafe.Pointer
		celt_dec                 *OpusCustomDecoder
		i                        int
		silk_ret                 int = 0
		celt_ret                 int = 0
		dec                      ec_dec
		silk_frame_size          int32
		pcm_silk_size            int
		pcm_silk                 *int16
		pcm_transition_silk_size int
		pcm_transition_silk      *opus_val16
		pcm_transition_celt_size int
		pcm_transition_celt      *opus_val16
		pcm_transition           *opus_val16 = nil
		redundant_audio_size     int
		redundant_audio          *opus_val16
		audiosize                int
		mode                     int
		bandwidth                int
		transition               int = 0
		start_band               int
		redundancy               int = 0
		redundancy_bytes         int = 0
		celt_to_silk             int = 0
		c                        int
		F2_5                     int
		F5                       int
		F10                      int
		F20                      int
		window                   *opus_val16
		redundant_rng            uint32 = 0
		celt_accum               int
	)
	silk_dec = unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Silk_dec_offset)
	celt_dec = (*OpusCustomDecoder)(unsafe.Pointer((*byte)(unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Celt_dec_offset))))
	F20 = int(st.Fs) / 50
	F10 = F20 >> 1
	F5 = F10 >> 1
	F2_5 = F5 >> 1
	if frame_size < F2_5 {
		return -2
	}
	if frame_size < (int(st.Fs) / 25 * 3) {
		frame_size = frame_size
	} else {
		frame_size = int(st.Fs) / 25 * 3
	}
	if int(len_) <= 1 {
		data = nil
		if frame_size < st.Frame_size {
			frame_size = frame_size
		} else {
			frame_size = st.Frame_size
		}
	}
	if data != nil {
		audiosize = st.Frame_size
		mode = st.Mode
		bandwidth = st.Bandwidth
		ec_dec_init(&dec, data, uint32(len_))
	} else {
		audiosize = frame_size
		if st.Prev_redundancy != 0 {
			mode = MODE_CELT_ONLY
		} else {
			mode = st.Prev_mode
		}
		bandwidth = 0
		if mode == 0 {
			for i = 0; i < audiosize*st.Channels; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = 0
			}
			return audiosize
		}
		if audiosize > F20 {
			for {
				{
					var ret int = opus_decode_frame(st, nil, 0, pcm, func() int {
						if audiosize < F20 {
							return audiosize
						}
						return F20
					}(), 0)
					if ret < 0 {
						return ret
					}
					pcm = (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(ret*st.Channels)))
					audiosize -= ret
				}
				if audiosize <= 0 {
					break
				}
			}
			return frame_size
		} else if audiosize < F20 {
			if audiosize > F10 {
				audiosize = F10
			} else if mode != MODE_SILK_ONLY && audiosize > F5 && audiosize < F10 {
				audiosize = F5
			}
		}
	}
	celt_accum = 0
	pcm_transition_silk_size = ALLOC_NONE
	pcm_transition_celt_size = ALLOC_NONE
	if data != nil && st.Prev_mode > 0 && (mode == MODE_CELT_ONLY && st.Prev_mode != MODE_CELT_ONLY && st.Prev_redundancy == 0 || mode != MODE_CELT_ONLY && st.Prev_mode == MODE_CELT_ONLY) {
		transition = 1
		if mode == MODE_CELT_ONLY {
			pcm_transition_celt_size = F5 * st.Channels
		} else {
			pcm_transition_silk_size = F5 * st.Channels
		}
	}
	pcm_transition_celt = (*opus_val16)(libc.Malloc(pcm_transition_celt_size * int(unsafe.Sizeof(opus_val16(0)))))
	if transition != 0 && mode == MODE_CELT_ONLY {
		pcm_transition = pcm_transition_celt
		opus_decode_frame(st, nil, 0, pcm_transition, func() int {
			if F5 < audiosize {
				return F5
			}
			return audiosize
		}(), 0)
	}
	if audiosize > frame_size {
		return -1
	} else {
		frame_size = audiosize
	}
	if mode != MODE_CELT_ONLY && celt_accum == 0 {
		pcm_silk_size = (func() int {
			if F10 > frame_size {
				return F10
			}
			return frame_size
		}()) * st.Channels
	} else {
		pcm_silk_size = ALLOC_NONE
	}
	pcm_silk = (*int16)(libc.Malloc(pcm_silk_size * int(unsafe.Sizeof(int16(0)))))
	if mode != MODE_CELT_ONLY {
		var (
			lost_flag       int
			decoded_samples int
			pcm_ptr         *int16
		)
		pcm_ptr = pcm_silk
		if st.Prev_mode == MODE_CELT_ONLY {
			silk_InitDecoder(silk_dec)
		}
		if 10 > (audiosize * 1000 / int(st.Fs)) {
			st.DecControl.PayloadSize_ms = 10
		} else {
			st.DecControl.PayloadSize_ms = audiosize * 1000 / int(st.Fs)
		}
		if data != nil {
			st.DecControl.NChannelsInternal = int32(st.Stream_channels)
			if mode == MODE_SILK_ONLY {
				if bandwidth == OPUS_BANDWIDTH_NARROWBAND {
					st.DecControl.InternalSampleRate = 8000
				} else if bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
					st.DecControl.InternalSampleRate = 12000
				} else if bandwidth == OPUS_BANDWIDTH_WIDEBAND {
					st.DecControl.InternalSampleRate = 16000
				} else {
					st.DecControl.InternalSampleRate = 16000
				}
			} else {
				st.DecControl.InternalSampleRate = 16000
			}
		}
		if data == nil {
			lost_flag = 1
		} else {
			lost_flag = decode_fec * 2
		}
		decoded_samples = 0
		for {
			{
				var first_frame int = int(libc.BoolToInt(decoded_samples == 0))
				silk_ret = silk_Decode(silk_dec, &st.DecControl, lost_flag, first_frame, &dec, unsafe.Slice(pcm_ptr, frame_size*st.Channels), &silk_frame_size, st.Arch)
				if silk_ret != 0 {
					if lost_flag != 0 {
						silk_frame_size = int32(frame_size)
						for i = 0; i < frame_size*st.Channels; i++ {
							*(*int16)(unsafe.Add(unsafe.Pointer(pcm_ptr), unsafe.Sizeof(int16(0))*uintptr(i))) = 0
						}
					} else {
						return -3
					}
				}
				pcm_ptr = (*int16)(unsafe.Add(unsafe.Pointer(pcm_ptr), unsafe.Sizeof(int16(0))*uintptr(int(silk_frame_size)*st.Channels)))
				decoded_samples += int(silk_frame_size)
			}
			if decoded_samples >= frame_size {
				break
			}
		}
	}
	start_band = 0
	if decode_fec == 0 && mode != MODE_CELT_ONLY && data != nil && ec_tell((*ec_ctx)(unsafe.Pointer(&dec)))+17+int(libc.BoolToInt(mode == MODE_HYBRID))*20 <= int(len_)*8 {
		if mode == MODE_HYBRID {
			redundancy = ec_dec_bit_logp(&dec, 12)
		} else {
			redundancy = 1
		}
		if redundancy != 0 {
			celt_to_silk = ec_dec_bit_logp(&dec, 1)
			if mode == MODE_HYBRID {
				redundancy_bytes = int(int32(ec_dec_uint(&dec, 256))) + 2
			} else {
				redundancy_bytes = int(len_) - ((ec_tell((*ec_ctx)(unsafe.Pointer(&dec))) + 7) >> 3)
			}
			len_ -= int32(redundancy_bytes)
			if int(len_)*8 < ec_tell((*ec_ctx)(unsafe.Pointer(&dec))) {
				len_ = 0
				redundancy_bytes = 0
				redundancy = 0
			}
			dec.Storage -= uint32(int32(redundancy_bytes))
		}
	}
	if mode != MODE_CELT_ONLY {
		start_band = 17
	}
	if redundancy != 0 {
		transition = 0
		pcm_transition_silk_size = ALLOC_NONE
	}
	pcm_transition_silk = (*opus_val16)(libc.Malloc(pcm_transition_silk_size * int(unsafe.Sizeof(opus_val16(0)))))
	if transition != 0 && mode != MODE_CELT_ONLY {
		pcm_transition = pcm_transition_silk
		opus_decode_frame(st, nil, 0, pcm_transition, func() int {
			if F5 < audiosize {
				return F5
			}
			return audiosize
		}(), 0)
	}
	if bandwidth != 0 {
		var endband int = 21
		switch bandwidth {
		case OPUS_BANDWIDTH_NARROWBAND:
			endband = 13
		case OPUS_BANDWIDTH_MEDIUMBAND:
			fallthrough
		case OPUS_BANDWIDTH_WIDEBAND:
			endband = 17
		case OPUS_BANDWIDTH_SUPERWIDEBAND:
			endband = 19
		case OPUS_BANDWIDTH_FULLBAND:
			endband = 21
		default:
		}
		for {
			if opus_custom_decoder_ctl(celt_dec, CELT_SET_END_BAND_REQUEST, func() int32 {
				_ = endband == 0
				return int32(endband)
			}()) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
	}
	for {
		if opus_custom_decoder_ctl(celt_dec, CELT_SET_CHANNELS_REQUEST, func() int32 {
			_ = st.Stream_channels == 0
			return int32(st.Stream_channels)
		}()) != OPUS_OK {
			return -3
		}
		if true {
			break
		}
	}
	if redundancy != 0 {
		redundant_audio_size = F5 * st.Channels
	} else {
		redundant_audio_size = ALLOC_NONE
	}
	redundant_audio = (*opus_val16)(libc.Malloc(redundant_audio_size * int(unsafe.Sizeof(opus_val16(0)))))
	if redundancy != 0 && celt_to_silk != 0 {
		for {
			if opus_custom_decoder_ctl(celt_dec, CELT_SET_START_BAND_REQUEST, func() int {
				return 0
			}()) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
		celt_decode_with_ec(celt_dec, (*uint8)(unsafe.Add(unsafe.Pointer(data), len_)), redundancy_bytes, redundant_audio, F5, nil, 0)
		for {
			if opus_custom_decoder_ctl(celt_dec, OPUS_GET_FINAL_RANGE_REQUEST, (*uint32)(unsafe.Add(unsafe.Pointer(&redundant_rng), unsafe.Sizeof(uint32(0))*uintptr(int64(uintptr(unsafe.Pointer(&redundant_rng))-uintptr(unsafe.Pointer(&redundant_rng))))))) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
	}
	for {
		if opus_custom_decoder_ctl(celt_dec, CELT_SET_START_BAND_REQUEST, func() int32 {
			_ = start_band == 0
			return int32(start_band)
		}()) != OPUS_OK {
			return -3
		}
		if true {
			break
		}
	}
	if mode != MODE_SILK_ONLY {
		var celt_frame_size int = (func() int {
			if F20 < frame_size {
				return F20
			}
			return frame_size
		}())
		if mode != st.Prev_mode && st.Prev_mode > 0 && st.Prev_redundancy == 0 {
			for {
				if opus_custom_decoder_ctl(celt_dec, OPUS_RESET_STATE) != OPUS_OK {
					return -3
				}
				if true {
					break
				}
			}
		}
		celt_ret = celt_decode_with_ec(celt_dec, func() *uint8 {
			if decode_fec != 0 {
				return nil
			}
			return data
		}(), int(len_), pcm, celt_frame_size, &dec, celt_accum)
	} else {
		var silence [2]uint8 = [2]uint8{math.MaxUint8, math.MaxUint8}
		if celt_accum == 0 {
			for i = 0; i < frame_size*st.Channels; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = 0
			}
		}
		if st.Prev_mode == MODE_HYBRID && (redundancy == 0 || celt_to_silk == 0 || st.Prev_redundancy == 0) {
			for {
				if opus_custom_decoder_ctl(celt_dec, CELT_SET_START_BAND_REQUEST, func() int {
					return 0
				}()) != OPUS_OK {
					return -3
				}
				if true {
					break
				}
			}
			celt_decode_with_ec(celt_dec, &silence[0], 2, pcm, F2_5, nil, celt_accum)
		}
	}
	if mode != MODE_CELT_ONLY && celt_accum == 0 {
		for i = 0; i < frame_size*st.Channels; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) + opus_val16(float64(*(*int16)(unsafe.Add(unsafe.Pointer(pcm_silk), unsafe.Sizeof(int16(0))*uintptr(i))))*(1.0/32768.0))
		}
	}
	{
		var celt_mode *OpusCustomMode
		for {
			if opus_custom_decoder_ctl(celt_dec, CELT_GET_MODE_REQUEST, (**OpusCustomMode)(unsafe.Add(unsafe.Pointer(&celt_mode), unsafe.Sizeof((*OpusCustomMode)(nil))*uintptr(int64(uintptr(unsafe.Pointer(&celt_mode))-uintptr(unsafe.Pointer(&celt_mode))))))) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
		window = celt_mode.Window
	}
	if redundancy != 0 && celt_to_silk == 0 {
		for {
			if opus_custom_decoder_ctl(celt_dec, OPUS_RESET_STATE) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
		for {
			if opus_custom_decoder_ctl(celt_dec, CELT_SET_START_BAND_REQUEST, func() int {
				return 0
			}()) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
		celt_decode_with_ec(celt_dec, (*uint8)(unsafe.Add(unsafe.Pointer(data), len_)), redundancy_bytes, redundant_audio, F5, nil, 0)
		for {
			if opus_custom_decoder_ctl(celt_dec, OPUS_GET_FINAL_RANGE_REQUEST, (*uint32)(unsafe.Add(unsafe.Pointer(&redundant_rng), unsafe.Sizeof(uint32(0))*uintptr(int64(uintptr(unsafe.Pointer(&redundant_rng))-uintptr(unsafe.Pointer(&redundant_rng))))))) != OPUS_OK {
				return -3
			}
			if true {
				break
			}
		}
		smooth_fade((*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*(frame_size-F2_5)))), (*opus_val16)(unsafe.Add(unsafe.Pointer(redundant_audio), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*(frame_size-F2_5)))), F2_5, st.Channels, window, st.Fs)
	}
	if redundancy != 0 && celt_to_silk != 0 && (st.Prev_mode != MODE_SILK_ONLY || st.Prev_redundancy != 0) {
		for c = 0; c < st.Channels; c++ {
			for i = 0; i < F2_5; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*i+c))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(redundant_audio), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*i+c)))
			}
		}
		smooth_fade((*opus_val16)(unsafe.Add(unsafe.Pointer(redundant_audio), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), F2_5, st.Channels, window, st.Fs)
	}
	if transition != 0 {
		if audiosize >= F5 {
			for i = 0; i < st.Channels*F2_5; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm_transition), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
			smooth_fade((*opus_val16)(unsafe.Add(unsafe.Pointer(pcm_transition), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*F2_5))), F2_5, st.Channels, window, st.Fs)
		} else {
			smooth_fade(pcm_transition, pcm, pcm, F2_5, st.Channels, window, st.Fs)
		}
	}
	if st.Decode_gain != 0 {
		var gain opus_val32
		gain = opus_val32(float32(math.Exp((float64(st.Decode_gain) * 0.000648814081) * 0.6931471805599453)))
		for i = 0; i < frame_size*st.Channels; i++ {
			var x opus_val32
			x = opus_val32((*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * opus_val16(gain))
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(x)
		}
	}
	if int(len_) <= 1 {
		st.RangeFinal = 0
	} else {
		st.RangeFinal = uint32(int32(int(dec.Rng) ^ int(redundant_rng)))
	}
	st.Prev_mode = mode
	st.Prev_redundancy = int(libc.BoolToInt(redundancy != 0 && celt_to_silk == 0))
	if celt_ret >= 0 {
		if _opus_false() != 0 {
			for {
				if true {
					break
				}
			}
		}
	}
	if celt_ret < 0 {
		return celt_ret
	}
	return audiosize
}
func opus_decode_native(st *OpusDecoder, data *uint8, len_ int32, pcm *opus_val16, frame_size int, decode_fec int, self_delimited int, packet_offset *int32, soft_clip int) int {
	var (
		i                      int
		nb_samples             int
		count                  int
		offset                 int
		toc                    uint8
		packet_frame_size      int
		packet_bandwidth       int
		packet_mode            int
		packet_stream_channels int
		size                   [48]int16
	)
	if decode_fec < 0 || decode_fec > 1 {
		return -1
	}
	if (decode_fec != 0 || int(len_) == 0 || data == nil) && frame_size%(int(st.Fs)/400) != 0 {
		return -1
	}
	if int(len_) == 0 || data == nil {
		var pcm_count int = 0
		for {
			{
				var ret int
				ret = opus_decode_frame(st, nil, 0, (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(pcm_count*st.Channels))), frame_size-pcm_count, 0)
				if ret < 0 {
					return ret
				}
				pcm_count += ret
			}
			if pcm_count >= frame_size {
				break
			}
		}
		if _opus_false() != 0 {
			for {
				if true {
					break
				}
			}
		}
		st.Last_packet_duration = pcm_count
		return pcm_count
	} else if int(len_) < 0 {
		return -1
	}
	packet_mode = opus_packet_get_mode(data)
	packet_bandwidth = opus_packet_get_bandwidth(data)
	packet_frame_size = opus_packet_get_samples_per_frame(data, st.Fs)
	packet_stream_channels = opus_packet_get_nb_channels(data)
	count = opus_packet_parse_impl(data, len_, self_delimited, &toc, [48]*uint8{}, size, &offset, packet_offset)
	if count < 0 {
		return count
	}
	data = (*uint8)(unsafe.Add(unsafe.Pointer(data), offset))
	if decode_fec != 0 {
		var (
			duration_copy int
			ret           int
		)
		if frame_size < packet_frame_size || packet_mode == MODE_CELT_ONLY || st.Mode == MODE_CELT_ONLY {
			return opus_decode_native(st, nil, 0, pcm, frame_size, 0, 0, nil, soft_clip)
		}
		duration_copy = st.Last_packet_duration
		if frame_size-packet_frame_size != 0 {
			ret = opus_decode_native(st, nil, 0, pcm, frame_size-packet_frame_size, 0, 0, nil, soft_clip)
			if ret < 0 {
				st.Last_packet_duration = duration_copy
				return ret
			}
		}
		st.Mode = packet_mode
		st.Bandwidth = packet_bandwidth
		st.Frame_size = packet_frame_size
		st.Stream_channels = packet_stream_channels
		ret = opus_decode_frame(st, data, int32(size[0]), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*(frame_size-packet_frame_size)))), packet_frame_size, 1)
		if ret < 0 {
			return ret
		} else {
			if _opus_false() != 0 {
				for {
					if true {
						break
					}
				}
			}
			st.Last_packet_duration = frame_size
			return frame_size
		}
	}
	if count*packet_frame_size > frame_size {
		return -2
	}
	st.Mode = packet_mode
	st.Bandwidth = packet_bandwidth
	st.Frame_size = packet_frame_size
	st.Stream_channels = packet_stream_channels
	nb_samples = 0
	for i = 0; i < count; i++ {
		var ret int
		ret = opus_decode_frame(st, data, int32(size[i]), (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(nb_samples*st.Channels))), frame_size-nb_samples, 0)
		if ret < 0 {
			return ret
		}
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), size[i]))
		nb_samples += ret
	}
	st.Last_packet_duration = nb_samples
	if _opus_false() != 0 {
		for {
			if true {
				break
			}
		}
	}
	if soft_clip != 0 {
		opus_pcm_soft_clip((*float32)(unsafe.Pointer(pcm)), nb_samples, st.Channels, (*float32)(unsafe.Pointer(&st.Softclip_mem[0])))
	} else {
		st.Softclip_mem[0] = func() opus_val16 {
			p := &st.Softclip_mem[1]
			st.Softclip_mem[1] = 0
			return *p
		}()
	}
	return nb_samples
}
func opus_decode(st *OpusDecoder, data *uint8, len_ int32, pcm *int16, frame_size int, decode_fec int) int {
	var (
		out        *float32
		ret        int
		i          int
		nb_samples int
	)
	if frame_size <= 0 {
		return -1
	}
	if data != nil && int(len_) > 0 && decode_fec == 0 {
		nb_samples = opus_decoder_get_nb_samples(st, unsafe.Slice(data, int(len_)), len_)
		if nb_samples > 0 {
			if frame_size < nb_samples {
				frame_size = frame_size
			} else {
				frame_size = nb_samples
			}
		} else {
			return -4
		}
	}
	out = (*float32)(libc.Malloc((frame_size * st.Channels) * int(unsafe.Sizeof(float32(0)))))
	ret = opus_decode_native(st, data, len_, (*opus_val16)(unsafe.Pointer(out)), frame_size, decode_fec, 0, nil, 1)
	if ret > 0 {
		for i = 0; i < ret*st.Channels; i++ {
			*(*int16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(int16(0))*uintptr(i))) = FLOAT2INT16(*(*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(i))))
		}
	}
	return ret
}
func opus_decode_float(st *OpusDecoder, data *uint8, len_ int32, pcm *opus_val16, frame_size int, decode_fec int) int {
	if frame_size <= 0 {
		return -1
	}
	return opus_decode_native(st, data, len_, pcm, frame_size, decode_fec, 0, nil, 0)
}
func opus_decoder_ctl(st *OpusDecoder, request int, _rest ...interface{}) int {
	var (
		ret      int = OPUS_OK
		ap       libc.ArgList
		silk_dec unsafe.Pointer
		celt_dec *OpusCustomDecoder
	)
	silk_dec = unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Silk_dec_offset)
	celt_dec = (*OpusCustomDecoder)(unsafe.Pointer((*byte)(unsafe.Add(unsafe.Pointer((*byte)(unsafe.Pointer(st))), st.Celt_dec_offset))))
	ap.Start(request, _rest)
	switch request {
	case OPUS_GET_BANDWIDTH_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Bandwidth)
	case OPUS_GET_FINAL_RANGE_REQUEST:
		var value *uint32 = ap.Arg().(*uint32)
		if value == nil {
			goto bad_arg
		}
		*value = st.RangeFinal
	case OPUS_RESET_STATE:
		libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Stream_channels))), 0, int((unsafe.Sizeof(OpusDecoder{})-uintptr(int64(uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Stream_channels))))-uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(st)))))))*unsafe.Sizeof(byte(0))))
		opus_custom_decoder_ctl(celt_dec, OPUS_RESET_STATE)
		silk_InitDecoder(silk_dec)
		st.Stream_channels = st.Channels
		st.Frame_size = int(st.Fs) / 400
	case OPUS_GET_SAMPLE_RATE_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = st.Fs
	case OPUS_GET_PITCH_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		if st.Prev_mode == MODE_CELT_ONLY {
			ret = opus_custom_decoder_ctl(celt_dec, OPUS_GET_PITCH_REQUEST, (*int32)(unsafe.Add(unsafe.Pointer(value), unsafe.Sizeof(int32(0))*uintptr(int64(uintptr(unsafe.Pointer(value))-uintptr(unsafe.Pointer(value)))))))
		} else {
			*value = int32(st.DecControl.PrevPitchLag)
		}
	case OPUS_GET_GAIN_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Decode_gain)
	case OPUS_SET_GAIN_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < math.MinInt16 || int(value) > math.MaxInt16 {
			goto bad_arg
		}
		st.Decode_gain = int(value)
	case OPUS_GET_LAST_PACKET_DURATION_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Last_packet_duration)
	case OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 0 || int(value) > 1 {
			goto bad_arg
		}
		ret = opus_custom_decoder_ctl(celt_dec, OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST, func() int32 {
			_ = int(value) == 0
			return value
		}())
	case OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		ret = opus_custom_decoder_ctl(celt_dec, OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST, (*int32)(unsafe.Add(unsafe.Pointer(value), unsafe.Sizeof(int32(0))*uintptr(int64(uintptr(unsafe.Pointer(value))-uintptr(unsafe.Pointer(value)))))))
	default:
		ret = -5
	}
	ap.End()
	return ret
bad_arg:
	ap.End()
	return -1
}
func opus_decoder_destroy(st *OpusDecoder) {
	libc.Free(unsafe.Pointer(st))
}
func opus_packet_get_bandwidth(data *uint8) int {
	var bandwidth int
	if int(*data)&0x80 != 0 {
		bandwidth = OPUS_BANDWIDTH_MEDIUMBAND + ((int(*data) >> 5) & 0x3)
		if bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			bandwidth = OPUS_BANDWIDTH_NARROWBAND
		}
	} else if (int(*data) & 0x60) == 0x60 {
		if (int(*data) & 0x10) != 0 {
			bandwidth = OPUS_BANDWIDTH_FULLBAND
		} else {
			bandwidth = OPUS_BANDWIDTH_SUPERWIDEBAND
		}
	} else {
		bandwidth = OPUS_BANDWIDTH_NARROWBAND + ((int(*data) >> 5) & 0x3)
	}
	return bandwidth
}
func opus_packet_get_nb_channels(data *uint8) int {
	if (int(*data) & 0x4) != 0 {
		return 2
	}
	return 1
}
func opus_packet_get_nb_frames(packet []uint8, len_ int32) int {
	var count int
	if int(len_) < 1 {
		return -1
	}
	count = int(packet[0]) & 0x3
	if count == 0 {
		return 1
	} else if count != 3 {
		return 2
	} else if int(len_) < 2 {
		return -4
	} else {
		return int(packet[1]) & 0x3F
	}
}
func opus_packet_get_nb_samples(packet []uint8, len_ int32, Fs int32) int {
	var (
		samples int
		count   int = opus_packet_get_nb_frames(packet, len_)
	)
	if count < 0 {
		return count
	}
	samples = count * opus_packet_get_samples_per_frame(&packet[0], Fs)
	if samples*25 > int(Fs)*3 {
		return -4
	} else {
		return samples
	}
}
func opus_decoder_get_nb_samples(dec *OpusDecoder, packet []uint8, len_ int32) int {
	return opus_packet_get_nb_samples(packet, len_, dec.Fs)
}
