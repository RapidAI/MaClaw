package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

const PLC_PITCH_LAG_MAX = 720
const PLC_PITCH_LAG_MIN = 100
const DECODE_BUFFER_SIZE = 2048

type OpusCustomDecoder struct {
	Mode                  *OpusCustomMode
	Overlap               int
	Channels              int
	Stream_channels       int
	Downsample            int
	Start                 int
	End                   int
	Signalling            int
	Disable_inv           int
	Arch                  int
	Rng                   uint32
	Error                 int
	Last_pitch_index      int
	Loss_duration         int
	Skip_plc              int
	Postfilter_period     int
	Postfilter_period_old int
	Postfilter_gain       opus_val16
	Postfilter_gain_old   opus_val16
	Postfilter_tapset     int
	Postfilter_tapset_old int
	Preemph_memD          [2]celt_sig
	// _decode_mem is a C flexible array member. In C, the struct is allocated
	// with extra bytes. Code accesses _decode_mem[i] for i up to
	// channels*(DECODE_BUFFER_SIZE+overlap)+channels*LPC_ORDER+NbEBands*8.
	// Max: 2*(2048+120)+2*24+21*8 = 4552. We use 8192 for safety.
	_decode_mem           [8192]celt_sig
}

func celt_decoder_get_size(channels int) int {
	var mode *OpusCustomMode = opus_custom_mode_create(48000, 960, nil)
	return opus_custom_decoder_get_size(mode, channels)
}
func opus_custom_decoder_get_size(mode *OpusCustomMode, channels int) int {
	var size int = (channels*(DECODE_BUFFER_SIZE+mode.Overlap)-1)*int(unsafe.Sizeof(celt_sig(0))) + int(unsafe.Sizeof(OpusCustomDecoder{})) + channels*LPC_ORDER*int(unsafe.Sizeof(opus_val16(0))) + mode.NbEBands*(4*2)*int(unsafe.Sizeof(opus_val16(0)))
	return size
}
func celt_decoder_init(st *OpusCustomDecoder, sampling_rate int32, channels int) int {
	var ret int
	ret = opus_custom_decoder_init(st, opus_custom_mode_create(48000, 960, nil), channels)
	if ret != OPUS_OK {
		return ret
	}
	st.Downsample = resampling_factor(sampling_rate)
	if st.Downsample == 0 {
		return -1
	} else {
		return OPUS_OK
	}
}
func opus_custom_decoder_init(st *OpusCustomDecoder, mode *OpusCustomMode, channels int) int {
	if channels < 0 || channels > 2 {
		return -1
	}
	if st == nil {
		return -7
	}
	libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(st))), 0, opus_custom_decoder_get_size(mode, channels)*int(unsafe.Sizeof(byte(0))))
	st.Mode = mode
	st.Overlap = mode.Overlap
	st.Stream_channels = func() int {
		p := &st.Channels
		st.Channels = channels
		return *p
	}()
	st.Downsample = 1
	st.Start = 0
	st.End = st.Mode.EffEBands
	st.Signalling = 1
	st.Disable_inv = int(libc.BoolToInt(channels == 1))
	st.Arch = opus_select_arch()
	opus_custom_decoder_ctl(st, OPUS_RESET_STATE)
	return OPUS_OK
}
func deemphasis_stereo_simple(in []*celt_sig, pcm *opus_val16, N int, coef0 opus_val16, mem *celt_sig) {
	var (
		x0 *celt_sig
		x1 *celt_sig
		m0 celt_sig
		m1 celt_sig
		j  int
	)
	x0 = in[0]
	x1 = in[1]
	m0 = *(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*0))
	m1 = *(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*1))
	for j = 0; j < N; j++ {
		var (
			tmp0 celt_sig
			tmp1 celt_sig
		)
		tmp0 = *(*celt_sig)(unsafe.Add(unsafe.Pointer(x0), unsafe.Sizeof(celt_sig(0))*uintptr(j))) + VERY_SMALL + m0
		tmp1 = *(*celt_sig)(unsafe.Add(unsafe.Pointer(x1), unsafe.Sizeof(celt_sig(0))*uintptr(j))) + VERY_SMALL + m1
		m0 = celt_sig(coef0 * opus_val16(tmp0))
		m1 = celt_sig(coef0 * opus_val16(tmp1))
		*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(j*2))) = opus_val16(tmp0 * (1 / CELT_SIG_SCALE))
		*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(j*2+1))) = opus_val16(tmp1 * (1 / CELT_SIG_SCALE))
	}
	*(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*0)) = m0
	*(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*1)) = m1
}
func deemphasis(in []*celt_sig, pcm *opus_val16, N int, C int, downsample int, coef *opus_val16, mem *celt_sig, accum int) {
	var (
		c                  int
		Nd                 int
		apply_downsampling int = 0
		coef0              opus_val16
		scratch            *celt_sig
	)
	if downsample == 1 && C == 2 && accum == 0 {
		deemphasis_stereo_simple(in, pcm, N, *(*opus_val16)(unsafe.Add(unsafe.Pointer(coef), unsafe.Sizeof(opus_val16(0))*0)), mem)
		return
	}
	_ = accum
	scratch = (*celt_sig)(libc.Malloc(N * int(unsafe.Sizeof(celt_sig(0)))))
	coef0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(coef), unsafe.Sizeof(opus_val16(0))*0))
	Nd = N / downsample
	c = 0
	for {
		{
			var (
				j int
				x *celt_sig
				y *opus_val16
				m celt_sig = *(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*uintptr(c)))
			)
			x = in[c]
			y = (*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(c)))
			if downsample > 1 {
				for j = 0; j < N; j++ {
					var tmp celt_sig = *(*celt_sig)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_sig(0))*uintptr(j))) + VERY_SMALL + m
					m = celt_sig(coef0 * opus_val16(tmp))
					*(*celt_sig)(unsafe.Add(unsafe.Pointer(scratch), unsafe.Sizeof(celt_sig(0))*uintptr(j))) = tmp
				}
				apply_downsampling = 1
			} else {
				for j = 0; j < N; j++ {
					var tmp celt_sig = *(*celt_sig)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_sig(0))*uintptr(j))) + VERY_SMALL + m
					m = celt_sig(coef0 * opus_val16(tmp))
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val16(0))*uintptr(j*C))) = opus_val16(tmp * (1 / CELT_SIG_SCALE))
				}
			}
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(mem), unsafe.Sizeof(celt_sig(0))*uintptr(c))) = m
			if apply_downsampling != 0 {
				for j = 0; j < Nd; j++ {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val16(0))*uintptr(j*C))) = opus_val16((*(*celt_sig)(unsafe.Add(unsafe.Pointer(scratch), unsafe.Sizeof(celt_sig(0))*uintptr(j*downsample)))) * (1 / CELT_SIG_SCALE))
				}
			}
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
}
func celt_synthesis(mode *OpusCustomMode, X *celt_norm, out_syn []*celt_sig, oldBandE *opus_val16, start int, effEnd int, C int, CC int, isTransient int, LM int, downsample int, silence int, arch int) {
	var (
		c        int
		i        int
		M        int
		b        int
		B        int
		N        int
		NB       int
		shift    int
		nbEBands int
		overlap  int
		freq     *celt_sig
	)
	overlap = mode.Overlap
	nbEBands = mode.NbEBands
	N = mode.ShortMdctSize << LM
	freq = (*celt_sig)(libc.Malloc(N * int(unsafe.Sizeof(celt_sig(0)))))
	M = 1 << LM
	if isTransient != 0 {
		B = M
		NB = mode.ShortMdctSize
		shift = mode.MaxLM
	} else {
		B = 1
		NB = mode.ShortMdctSize << LM
		shift = mode.MaxLM - LM
	}
	if CC == 2 && C == 1 {
		var freq2 *celt_sig
		denormalise_bands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		freq2 = (*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[1]), unsafe.Sizeof(celt_sig(0))*uintptr(overlap/2)))
		libc.MemCpy(unsafe.Pointer(freq2), unsafe.Pointer(freq), N*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer(freq2))-uintptr(unsafe.Pointer(freq))))*0))
		for b = 0; b < B; b++ {
			clt_mdct_backward_c(&mode.Mdct, (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(freq2), unsafe.Sizeof(celt_sig(0))*uintptr(b))))), (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[0]), unsafe.Sizeof(celt_sig(0))*uintptr(NB*b))))), mode.Window, overlap, shift, B, arch)
		}
		for b = 0; b < B; b++ {
			clt_mdct_backward_c(&mode.Mdct, (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(b))))), (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[1]), unsafe.Sizeof(celt_sig(0))*uintptr(NB*b))))), mode.Window, overlap, shift, B, arch)
		}
	} else if CC == 1 && C == 2 {
		var freq2 *celt_sig
		freq2 = (*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[0]), unsafe.Sizeof(celt_sig(0))*uintptr(overlap/2)))
		denormalise_bands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		denormalise_bands(mode, (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N))), freq2, (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands))), start, effEnd, M, downsample, silence)
		for i = 0; i < N; i++ {
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(i))) = ((*(*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(i)))) * celt_sig(0.5)) + (*(*celt_sig)(unsafe.Add(unsafe.Pointer(freq2), unsafe.Sizeof(celt_sig(0))*uintptr(i))))*celt_sig(0.5)
		}
		for b = 0; b < B; b++ {
			clt_mdct_backward_c(&mode.Mdct, (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(b))))), (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[0]), unsafe.Sizeof(celt_sig(0))*uintptr(NB*b))))), mode.Window, overlap, shift, B, arch)
		}
	} else {
		c = 0
		for {
			denormalise_bands(mode, (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(c*N))), freq, (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands))), start, effEnd, M, downsample, silence)
			for b = 0; b < B; b++ {
				clt_mdct_backward_c(&mode.Mdct, (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(b))))), (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[c]), unsafe.Sizeof(celt_sig(0))*uintptr(NB*b))))), mode.Window, overlap, shift, B, arch)
			}
			if func() int {
				p := &c
				*p++
				return *p
			}() >= CC {
				break
			}
		}
	}
	c = 0
	for {
		for i = 0; i < N; i++ {
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[c]), unsafe.Sizeof(celt_sig(0))*uintptr(i))) = *(*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[c]), unsafe.Sizeof(celt_sig(0))*uintptr(i)))
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
}
func tf_decode(start int, end int, isTransient int, tf_res *int, LM int, dec *ec_dec) {
	var (
		i             int
		curr          int
		tf_select     int
		tf_select_rsv int
		tf_changed    int
		logp          int
		budget        uint32
		tell          uint32
	)
	budget = uint32(int32(int(dec.Storage) * 8))
	tell = uint32(int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec)))))
	if isTransient != 0 {
		logp = 2
	} else {
		logp = 4
	}
	tf_select_rsv = int(libc.BoolToInt(LM > 0 && int(tell)+logp+1 <= int(budget)))
	budget -= uint32(int32(tf_select_rsv))
	tf_changed = func() int {
		curr = 0
		return curr
	}()
	for i = start; i < end; i++ {
		if int(tell)+logp <= int(budget) {
			curr ^= ec_dec_bit_logp(dec, uint(logp))
			tell = uint32(int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec)))))
			tf_changed |= curr
		}
		*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = curr
		if isTransient != 0 {
			logp = 4
		} else {
			logp = 5
		}
	}
	tf_select = 0
	if tf_select_rsv != 0 && int(tf_select_table[LM][isTransient*4+0+tf_changed]) != int(tf_select_table[LM][isTransient*4+2+tf_changed]) {
		tf_select = ec_dec_bit_logp(dec, 1)
	}
	for i = start; i < end; i++ {
		*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = int(tf_select_table[LM][isTransient*4+tf_select*2+*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i)))])
	}
}
func celt_plc_pitch_search(decode_mem [2]*celt_sig, C int, arch int) int {
	var (
		pitch_index  int
		lp_pitch_buf *opus_val16
		pitch_index_slice = []int{0}
	)
	lp_pitch_buf = (*opus_val16)(libc.Malloc((int(DECODE_BUFFER_SIZE >> 1)) * int(unsafe.Sizeof(opus_val16(0)))))
	pitch_downsample(decode_mem[:], unsafe.Slice(lp_pitch_buf, DECODE_BUFFER_SIZE>>1), DECODE_BUFFER_SIZE, C, arch)
	pitch_search(unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(lp_pitch_buf), unsafe.Sizeof(opus_val16(0))*(720>>1))), (DECODE_BUFFER_SIZE-720)>>1), unsafe.Slice(lp_pitch_buf, DECODE_BUFFER_SIZE>>1), int(DECODE_BUFFER_SIZE-720), 720-100, pitch_index_slice, arch)
	pitch_index = pitch_index_slice[0]
	pitch_index = 720 - pitch_index
	return pitch_index
}
func celt_decode_lost(st *OpusCustomDecoder, N int, LM int) {
	var (
		c              int
		i              int
		C              int = st.Channels
		decode_mem     [2]*celt_sig
		out_syn        [2]*celt_sig
		lpc            *opus_val16
		oldBandE       *opus_val16
		oldLogE        *opus_val16
		oldLogE2       *opus_val16
		backgroundLogE *opus_val16
		mode           *OpusCustomMode
		nbEBands       int
		overlap        int
		start          int
		loss_duration  int
		noise_based    int
		eBands         *int16
	)
	mode = st.Mode
	nbEBands = mode.NbEBands
	overlap = mode.Overlap
	eBands = mode.EBands
	c = 0
	for {
		decode_mem[c] = &st._decode_mem[c*(DECODE_BUFFER_SIZE+overlap)]
		out_syn[c] = (*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N))))
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
	lpc = (*opus_val16)(unsafe.Pointer(&st._decode_mem[(DECODE_BUFFER_SIZE+overlap)*C]))
	oldBandE = (*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(C*LPC_ORDER)))
	oldLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	oldLogE2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	backgroundLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	loss_duration = st.Loss_duration
	start = st.Start
	noise_based = int(libc.BoolToInt(loss_duration >= 40 || start != 0 || st.Skip_plc != 0))
	if noise_based != 0 {
		var (
			X      *celt_norm
			seed   uint32
			end    int
			effEnd int
			decay  opus_val16
		)
		end = st.End
		if start > (func() int {
			if end < mode.EffEBands {
				return end
			}
			return mode.EffEBands
		}()) {
			effEnd = start
		} else if end < mode.EffEBands {
			effEnd = end
		} else {
			effEnd = mode.EffEBands
		}
		X = (*celt_norm)(libc.Malloc((C * N) * int(unsafe.Sizeof(celt_norm(0)))))
		c = 0
		for {
			libc.MemMove(unsafe.Pointer(decode_mem[c]), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), (DECODE_BUFFER_SIZE-N+(overlap>>1))*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer(decode_mem[c]))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0))
			if func() int {
				p := &c
				*p++
				return *p
			}() >= C {
				break
			}
		}
		if loss_duration == 0 {
			decay = opus_val16(1.5)
		} else {
			decay = opus_val16(0.5)
		}
		c = 0
		for {
			for i = start; i < end; i++ {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) - decay) {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) - decay
				}
			}
			if func() int {
				p := &c
				*p++
				return *p
			}() >= C {
				break
			}
		}
		seed = st.Rng
		for c = 0; c < C; c++ {
			for i = start; i < effEnd; i++ {
				var (
					j     int
					boffs int
					blen  int
				)
				boffs = N*c + (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))) << LM)
				blen = (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))) << LM
				for j = 0; j < blen; j++ {
					seed = celt_lcg_rand(seed)
					*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(boffs+j))) = celt_norm(int(int32(seed)) >> 20)
				}
				renormalise_vector((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(boffs))), blen, Q15ONE, st.Arch)
			}
		}
		st.Rng = seed
		celt_synthesis(mode, X, out_syn[:], oldBandE, start, effEnd, C, C, 0, LM, st.Downsample, 0, st.Arch)
	} else {
		var (
			exc_length  int
			window      *opus_val16
			exc         *opus_val16
			fade        opus_val16 = Q15ONE
			pitch_index int
			etmp        *opus_val32
			_exc        *opus_val16
			fir_tmp     *opus_val16
		)
		if loss_duration == 0 {
			st.Last_pitch_index = func() int {
				pitch_index = celt_plc_pitch_search(decode_mem, C, st.Arch)
				return pitch_index
			}()
		} else {
			pitch_index = st.Last_pitch_index
			fade = opus_val16(0.8)
		}
		if (pitch_index * 2) < MAX_PERIOD {
			exc_length = pitch_index * 2
		} else {
			exc_length = MAX_PERIOD
		}
		etmp = (*opus_val32)(libc.Malloc(overlap * int(unsafe.Sizeof(opus_val32(0)))))
		_exc = (*opus_val16)(libc.Malloc((int(MAX_PERIOD + LPC_ORDER)) * int(unsafe.Sizeof(opus_val16(0)))))
		fir_tmp = (*opus_val16)(libc.Malloc(exc_length * int(unsafe.Sizeof(opus_val16(0)))))
		exc = (*opus_val16)(unsafe.Add(unsafe.Pointer(_exc), unsafe.Sizeof(opus_val16(0))*uintptr(LPC_ORDER)))
		window = mode.Window
		c = 0
		for {
			{
				var (
					decay                opus_val16
					attenuation          opus_val16
					S1                   opus_val32 = 0
					buf                  *celt_sig
					extrapolation_offset int
					extrapolation_len    int
					j                    int
				)
				buf = decode_mem[c]
				for i = 0; i < int(MAX_PERIOD+LPC_ORDER); i++ {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(i-LPC_ORDER))) = opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(int(DECODE_BUFFER_SIZE-MAX_PERIOD)-LPC_ORDER+i))))
				}
				if loss_duration == 0 {
					var ac [25]opus_val32
					_celt_autocorr(unsafe.Slice(exc, MAX_PERIOD), ac[:], unsafe.Slice(window, overlap), overlap, LPC_ORDER, MAX_PERIOD, st.Arch)
					ac[0] *= opus_val32(1.0001)
					for i = 1; i <= LPC_ORDER; i++ {
						ac[i] -= opus_val32(float32(ac[i]*(0.008*0.008)) * float32(i) * float32(i))
					}
					_celt_lpc(unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(c*LPC_ORDER))), LPC_ORDER), ac[:], LPC_ORDER)
				}
				{
					celt_fir_c(unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(MAX_PERIOD)))), -int(unsafe.Sizeof(opus_val16(0))*uintptr(exc_length)))), exc_length), unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(c*LPC_ORDER))), LPC_ORDER), unsafe.Slice(fir_tmp, exc_length), exc_length, LPC_ORDER, st.Arch)
					libc.MemCpy(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(MAX_PERIOD)))), -int(unsafe.Sizeof(opus_val16(0))*uintptr(exc_length))))), unsafe.Pointer(fir_tmp), exc_length*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(MAX_PERIOD)))), -int(unsafe.Sizeof(opus_val16(0))*uintptr(exc_length))))))-uintptr(unsafe.Pointer(fir_tmp))))*0))
				}
				{
					var (
						E1           opus_val32 = 1
						E2           opus_val32 = 1
						decay_length int
					)
					decay_length = exc_length >> 1
					for i = 0; i < decay_length; i++ {
						var e opus_val16
						e = *(*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(MAX_PERIOD-decay_length+i)))
						E1 += opus_val32(e) * opus_val32(e)
						e = *(*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(MAX_PERIOD-decay_length*2+i)))
						E2 += opus_val32(e) * opus_val32(e)
					}
					if E1 < E2 {
						E1 = E1
					} else {
						E1 = E2
					}
					decay = opus_val16(float32(math.Sqrt(float64(float32(E1) / float32(E2)))))
				}
				libc.MemMove(unsafe.Pointer(buf), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), (DECODE_BUFFER_SIZE-N)*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer(buf))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0))
				extrapolation_offset = MAX_PERIOD - pitch_index
				extrapolation_len = N + overlap
				attenuation = fade * decay
				for i = func() int {
					j = 0
					return j
				}(); i < extrapolation_len; func() int {
					i++
					return func() int {
						p := &j
						x := *p
						*p++
						return x
					}()
				}() {
					var tmp opus_val16
					if j >= pitch_index {
						j -= pitch_index
						attenuation = attenuation * decay
					}
					*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i))) = celt_sig(attenuation * (*(*opus_val16)(unsafe.Add(unsafe.Pointer(exc), unsafe.Sizeof(opus_val16(0))*uintptr(extrapolation_offset+j)))))
					tmp = opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(int(DECODE_BUFFER_SIZE-MAX_PERIOD)-N+extrapolation_offset+j))))
					S1 += opus_val32(tmp) * opus_val32(tmp)
				}
				{
					var lpc_mem [24]opus_val16
					for i = 0; i < LPC_ORDER; i++ {
						lpc_mem[i] = opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N-1-i))))
					}
					celt_iir(unsafe.Slice((*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N)))))), extrapolation_len), unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(c*LPC_ORDER))), LPC_ORDER), unsafe.Slice((*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N)))))), extrapolation_len), extrapolation_len, LPC_ORDER, lpc_mem[:], st.Arch)
				}
				{
					var S2 opus_val32 = 0
					for i = 0; i < extrapolation_len; i++ {
						var tmp opus_val16 = opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i))))
						S2 += opus_val32(tmp) * opus_val32(tmp)
					}
					if S1 <= S2*opus_val32(0.2) {
						for i = 0; i < extrapolation_len; i++ {
							*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i))) = 0
						}
					} else if S1 < S2 {
						var ratio opus_val16 = opus_val16(float32(math.Sqrt(float64((float32(S1) + 1) / (float32(S2) + 1)))))
						for i = 0; i < overlap; i++ {
							var tmp_g opus_val16 = Q15ONE - (*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i))))*(Q15ONE-ratio)
							*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i))) = celt_sig(tmp_g * opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i)))))
						}
						for i = overlap; i < extrapolation_len; i++ {
							*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i))) = celt_sig(ratio * opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE-N+i)))))
						}
					}
				}
				comb_filter(etmp, (*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE))))), st.Postfilter_period, st.Postfilter_period, overlap, -st.Postfilter_gain, -st.Postfilter_gain, st.Postfilter_tapset, st.Postfilter_tapset, nil, 0, st.Arch)
				for i = 0; i < overlap/2; i++ {
					*(*celt_sig)(unsafe.Add(unsafe.Pointer(buf), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE+i))) = celt_sig(((*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * opus_val16(*(*opus_val32)(unsafe.Add(unsafe.Pointer(etmp), unsafe.Sizeof(opus_val32(0))*uintptr(overlap-1-i))))) + (*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(overlap-i-1))))*opus_val16(*(*opus_val32)(unsafe.Add(unsafe.Pointer(etmp), unsafe.Sizeof(opus_val32(0))*uintptr(i)))))
				}
			}
			if func() int {
				p := &c
				*p++
				return *p
			}() >= C {
				break
			}
		}
	}
	if 10000 < (loss_duration + (1 << LM)) {
		st.Loss_duration = 10000
	} else {
		st.Loss_duration = loss_duration + (1 << LM)
	}
}
func celt_decode_with_ec(st *OpusCustomDecoder, data *uint8, len_ int, pcm *opus_val16, frame_size int, dec *ec_dec, accum int) int {
	var (
		c                       int
		i                       int
		N                       int
		spread_decision         int
		bits                    int32
		_dec                    ec_dec
		X                       *celt_norm
		fine_quant              *int
		pulses                  *int
		cap_                    *int
		offsets                 *int
		fine_priority           *int
		tf_res                  *int
		collapse_masks          *uint8
		decode_mem              [2]*celt_sig
		out_syn                 [2]*celt_sig
		lpc                     *opus_val16
		oldBandE                *opus_val16
		oldLogE                 *opus_val16
		oldLogE2                *opus_val16
		backgroundLogE          *opus_val16
		shortBlocks             int
		isTransient             int
		intra_ener              int
		CC                      int = st.Channels
		LM                      int
		M                       int
		start                   int
		end                     int
		effEnd                  int
		codedBands              int
		alloc_trim              int
		postfilter_pitch        int
		postfilter_gain         opus_val16
		intensity               int = 0
		dual_stereo             int = 0
		total_bits              int32
		balance                 int32
		tell                    int32
		dynalloc_logp           int
		postfilter_tapset       int
		anti_collapse_rsv       int
		anti_collapse_on        int = 0
		silence                 int
		C                       int = st.Stream_channels
		mode                    *OpusCustomMode
		nbEBands                int
		overlap                 int
		eBands                  *int16
		max_background_increase opus_val16
	)
	mode = st.Mode
	nbEBands = mode.NbEBands
	overlap = mode.Overlap
	eBands = mode.EBands
	start = st.Start
	end = st.End
	frame_size *= st.Downsample
	lpc = (*opus_val16)(unsafe.Pointer(&st._decode_mem[(DECODE_BUFFER_SIZE+overlap)*CC]))
	oldBandE = (*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(CC*LPC_ORDER)))
	oldLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	oldLogE2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	backgroundLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*2)))
	{
		for LM = 0; LM <= mode.MaxLM; LM++ {
			if mode.ShortMdctSize<<LM == frame_size {
				break
			}
		}
		if LM > mode.MaxLM {
			return -1
		}
	}
	M = 1 << LM
	if len_ < 0 || len_ > 1275 || pcm == nil {
		return -1
	}
	N = M * mode.ShortMdctSize
	c = 0
	for {
		decode_mem[c] = &st._decode_mem[c*(DECODE_BUFFER_SIZE+overlap)]
		out_syn[c] = (*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(DECODE_BUFFER_SIZE)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N))))
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	effEnd = end
	if effEnd > mode.EffEBands {
		effEnd = mode.EffEBands
	}
	if data == nil || len_ <= 1 {
		celt_decode_lost(st, N, LM)
		deemphasis(out_syn[:], pcm, N, CC, st.Downsample, &mode.Preemph[0], &st.Preemph_memD[0], accum)
		return frame_size / st.Downsample
	}
	st.Skip_plc = int(libc.BoolToInt(st.Loss_duration != 0))
	if dec == nil {
		ec_dec_init(&_dec, data, uint32(int32(len_)))
		dec = &_dec
	}
	if C == 1 {
		for i = 0; i < nbEBands; i++ {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))
			}
		}
	}
	total_bits = int32(len_ * 8)
	tell = int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec))))
	if int(tell) >= int(total_bits) {
		silence = 1
	} else if int(tell) == 1 {
		silence = ec_dec_bit_logp(dec, 15)
	} else {
		silence = 0
	}
	if silence != 0 {
		tell = int32(len_ * 8)
		dec.Nbits_total += int(tell) - ec_tell((*ec_ctx)(unsafe.Pointer(dec)))
	}
	postfilter_gain = 0
	postfilter_pitch = 0
	postfilter_tapset = 0
	if start == 0 && int(tell)+16 <= int(total_bits) {
		if ec_dec_bit_logp(dec, 1) != 0 {
			var (
				qg     int
				octave int
			)
			octave = int(ec_dec_uint(dec, 6))
			postfilter_pitch = (16 << octave) + int(ec_dec_bits(dec, uint(octave+4))) - 1
			qg = int(ec_dec_bits(dec, 3))
			if ec_tell((*ec_ctx)(unsafe.Pointer(dec)))+2 <= int(total_bits) {
				postfilter_tapset = ec_dec_icdf(dec, tapset_icdf[:], 2)
			}
			postfilter_gain = opus_val16(float64(qg+1) * 0.09375)
		}
		tell = int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec))))
	}
	if LM > 0 && int(tell)+3 <= int(total_bits) {
		isTransient = ec_dec_bit_logp(dec, 3)
		tell = int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec))))
	} else {
		isTransient = 0
	}
	if isTransient != 0 {
		shortBlocks = M
	} else {
		shortBlocks = 0
	}
	if int(tell)+3 <= int(total_bits) {
		intra_ener = ec_dec_bit_logp(dec, 3)
	} else {
		intra_ener = 0
	}
	unquant_coarse_energy(mode, start, end, oldBandE, intra_ener, dec, C, LM)
	tf_res = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	tf_decode(start, end, isTransient, tf_res, LM, dec)
	tell = int32(ec_tell((*ec_ctx)(unsafe.Pointer(dec))))
	spread_decision = 2
	if int(tell)+4 <= int(total_bits) {
		spread_decision = ec_dec_icdf(dec, spread_icdf[:], 5)
	}
	cap_ = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	init_caps(mode, cap_, LM, C)
	offsets = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	dynalloc_logp = 6
	total_bits <<= BITRES
	tell = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(dec))))
	for i = start; i < end; i++ {
		var (
			width              int
			quanta             int
			dynalloc_loop_logp int
			boost              int
		)
		width = C * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))) << LM
		if (width << BITRES) < (func() int {
			if (int(6 << BITRES)) > width {
				return int(6 << BITRES)
			}
			return width
		}()) {
			quanta = width << BITRES
		} else if (int(6 << BITRES)) > width {
			quanta = int(6 << BITRES)
		} else {
			quanta = width
		}
		dynalloc_loop_logp = dynalloc_logp
		boost = 0
		for int(tell)+(dynalloc_loop_logp<<BITRES) < int(total_bits) && boost < *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(i))) {
			var flag int
			flag = ec_dec_bit_logp(dec, uint(dynalloc_loop_logp))
			tell = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(dec))))
			if flag == 0 {
				break
			}
			boost += quanta
			total_bits -= int32(quanta)
			dynalloc_loop_logp = 1
		}
		*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(i))) = boost
		if boost > 0 {
			if 2 > (dynalloc_logp - 1) {
				dynalloc_logp = 2
			} else {
				dynalloc_logp = dynalloc_logp - 1
			}
		}
	}
	fine_quant = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	if int(tell)+(int(6<<BITRES)) <= int(total_bits) {
		alloc_trim = ec_dec_icdf(dec, trim_icdf[:], 7)
	} else {
		alloc_trim = 5
	}
	bits = int32(((int(int32(len_)) * 8) << BITRES) - int(ec_tell_frac((*ec_ctx)(unsafe.Pointer(dec)))) - 1)
	if isTransient != 0 && LM >= 2 && int(bits) >= ((LM+2)<<BITRES) {
		anti_collapse_rsv = int(1 << BITRES)
	} else {
		anti_collapse_rsv = 0
	}
	bits -= int32(anti_collapse_rsv)
	pulses = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	fine_priority = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	codedBands = clt_compute_allocation(mode, start, end, offsets, cap_, alloc_trim, &intensity, &dual_stereo, bits, &balance, pulses, fine_quant, fine_priority, C, LM, (*ec_ctx)(unsafe.Pointer(dec)), 0, 0, 0)
	unquant_fine_energy(mode, start, end, oldBandE, fine_quant, dec, C)
	c = 0
	for {
		libc.MemMove(unsafe.Pointer(decode_mem[c]), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), (DECODE_BUFFER_SIZE-N+overlap/2)*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer(decode_mem[c]))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(decode_mem[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0))
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	collapse_masks = (*uint8)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(uint8(0)))))
	X = (*celt_norm)(libc.Malloc((C * N) * int(unsafe.Sizeof(celt_norm(0)))))
	quant_all_bands(0, mode, start, end, X, func() *celt_norm {
		if C == 2 {
			return (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N)))
		}
		return nil
	}(), collapse_masks, nil, pulses, shortBlocks, spread_decision, dual_stereo, intensity, tf_res, int32(len_*(int(8<<BITRES))-anti_collapse_rsv), balance, (*ec_ctx)(unsafe.Pointer(dec)), LM, codedBands, &st.Rng, 0, st.Arch, st.Disable_inv)
	if anti_collapse_rsv > 0 {
		anti_collapse_on = int(ec_dec_bits(dec, 1))
	}
	unquant_energy_finalise(mode, start, end, oldBandE, fine_quant, fine_priority, len_*8-ec_tell((*ec_ctx)(unsafe.Pointer(dec))), dec, C)
	if anti_collapse_on != 0 {
		anti_collapse(mode, X, collapse_masks, LM, C, N, start, end, oldBandE, oldLogE, oldLogE2, pulses, st.Rng, st.Arch)
	}
	if silence != 0 {
		for i = 0; i < C*nbEBands; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(-28.0)
		}
	}
	celt_synthesis(mode, X, out_syn[:], oldBandE, start, effEnd, C, CC, isTransient, LM, st.Downsample, silence, st.Arch)
	c = 0
	for {
		if st.Postfilter_period > COMBFILTER_MINPERIOD {
			st.Postfilter_period = st.Postfilter_period
		} else {
			st.Postfilter_period = COMBFILTER_MINPERIOD
		}
		if st.Postfilter_period_old > COMBFILTER_MINPERIOD {
			st.Postfilter_period_old = st.Postfilter_period_old
		} else {
			st.Postfilter_period_old = COMBFILTER_MINPERIOD
		}
		comb_filter((*opus_val32)(unsafe.Pointer(out_syn[c])), (*opus_val32)(unsafe.Pointer(out_syn[c])), st.Postfilter_period_old, st.Postfilter_period, mode.ShortMdctSize, st.Postfilter_gain_old, st.Postfilter_gain, st.Postfilter_tapset_old, st.Postfilter_tapset, mode.Window, overlap, st.Arch)
		if LM != 0 {
			comb_filter((*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[c]), unsafe.Sizeof(celt_sig(0))*uintptr(mode.ShortMdctSize))))), (*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out_syn[c]), unsafe.Sizeof(celt_sig(0))*uintptr(mode.ShortMdctSize))))), st.Postfilter_period, postfilter_pitch, N-mode.ShortMdctSize, st.Postfilter_gain, postfilter_gain, st.Postfilter_tapset, postfilter_tapset, mode.Window, overlap, st.Arch)
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	st.Postfilter_period_old = st.Postfilter_period
	st.Postfilter_gain_old = st.Postfilter_gain
	st.Postfilter_tapset_old = st.Postfilter_tapset
	st.Postfilter_period = postfilter_pitch
	st.Postfilter_gain = postfilter_gain
	st.Postfilter_tapset = postfilter_tapset
	if LM != 0 {
		st.Postfilter_period_old = st.Postfilter_period
		st.Postfilter_gain_old = st.Postfilter_gain
		st.Postfilter_tapset_old = st.Postfilter_tapset
	}
	if C == 1 {
		libc.MemCpy(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands)))), unsafe.Pointer(oldBandE), nbEBands*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands)))))-uintptr(unsafe.Pointer(oldBandE))))*0))
	}
	if isTransient == 0 {
		libc.MemCpy(unsafe.Pointer(oldLogE2), unsafe.Pointer(oldLogE), (nbEBands*2)*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(oldLogE2))-uintptr(unsafe.Pointer(oldLogE))))*0))
		libc.MemCpy(unsafe.Pointer(oldLogE), unsafe.Pointer(oldBandE), (nbEBands*2)*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(oldLogE))-uintptr(unsafe.Pointer(oldBandE))))*0))
	} else {
		for i = 0; i < nbEBands*2; i++ {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
		}
	}
	max_background_increase = opus_val16(float64(func() int {
		if 160 < (st.Loss_duration + M) {
			return 160
		}
		return st.Loss_duration + M
	}()) * 0.001)
	for i = 0; i < nbEBands*2; i++ {
		if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) + max_background_increase) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) + max_background_increase
		} else {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(backgroundLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
		}
	}
	c = 0
	for {
		for i = 0; i < start; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = 0
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = func() opus_val16 {
				p := (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = opus_val16(-28.0)
				return *p
			}()
		}
		for i = end; i < nbEBands; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = 0
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = func() opus_val16 {
				p := (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) = opus_val16(-28.0)
				return *p
			}()
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= 2 {
			break
		}
	}
	st.Rng = dec.Rng
	deemphasis(out_syn[:], pcm, N, CC, st.Downsample, &mode.Preemph[0], &st.Preemph_memD[0], accum)
	st.Loss_duration = 0
	if ec_tell((*ec_ctx)(unsafe.Pointer(dec))) > len_*8 {
		return -3
	}
	if ec_get_error((*ec_ctx)(unsafe.Pointer(dec))) != 0 {
		st.Error = 1
	}
	return frame_size / st.Downsample
}
func opus_custom_decoder_ctl(st *OpusCustomDecoder, request int, _rest ...interface{}) int {
	var ap libc.ArgList
	ap.Start(request, _rest)
	switch request {
	case CELT_SET_START_BAND_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 0 || int(value) >= st.Mode.NbEBands {
			goto bad_arg
		}
		st.Start = int(value)
	case CELT_SET_END_BAND_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 1 || int(value) > st.Mode.NbEBands {
			goto bad_arg
		}
		st.End = int(value)
	case CELT_SET_CHANNELS_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 1 || int(value) > 2 {
			goto bad_arg
		}
		st.Stream_channels = int(value)
	case CELT_GET_AND_CLEAR_ERROR_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Error)
		st.Error = 0
	case OPUS_GET_LOOKAHEAD_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Overlap / st.Downsample)
	case OPUS_RESET_STATE:
		var (
			i        int
			lpc      *opus_val16
			oldBandE *opus_val16
			oldLogE  *opus_val16
			oldLogE2 *opus_val16
		)
		lpc = (*opus_val16)(unsafe.Pointer(&st._decode_mem[(DECODE_BUFFER_SIZE+st.Overlap)*st.Channels]))
		oldBandE = (*opus_val16)(unsafe.Add(unsafe.Pointer(lpc), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*LPC_ORDER)))
		oldLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(st.Mode.NbEBands*2)))
		oldLogE2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(st.Mode.NbEBands*2)))
		libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Rng))), 0, (opus_custom_decoder_get_size(st.Mode, st.Channels)-int(int64(uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Rng))))-uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(st)))))))*int(unsafe.Sizeof(byte(0))))
		for i = 0; i < st.Mode.NbEBands*2; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = func() opus_val16 {
				p := (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(-28.0)
				return *p
			}()
		}
		st.Skip_plc = 1
	case OPUS_GET_PITCH_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Postfilter_period)
	case CELT_GET_MODE_REQUEST:
		var value **OpusCustomMode = ap.Arg().(**OpusCustomMode)
		if value == nil {
			goto bad_arg
		}
		*value = st.Mode
	case CELT_SET_SIGNALLING_REQUEST:
		var value int32 = ap.Arg().(int32)
		st.Signalling = int(value)
	case OPUS_GET_FINAL_RANGE_REQUEST:
		var value *uint32 = ap.Arg().(*uint32)
		if value == nil {
			goto bad_arg
		}
		*value = st.Rng
	case OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 0 || int(value) > 1 {
			goto bad_arg
		}
		st.Disable_inv = int(value)
	case OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		if value == nil {
			goto bad_arg
		}
		*value = int32(st.Disable_inv)
	default:
		goto bad_request
	}
	ap.End()
	return OPUS_OK
bad_arg:
	ap.End()
	return -1
bad_request:
	ap.End()
	return -5
}
