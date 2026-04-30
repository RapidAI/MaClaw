package libopus

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/cmath"
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

type OpusCustomEncoder struct {
	Mode             *OpusCustomMode
	Channels         int
	Stream_channels  int
	Force_intra      int
	Clip             int
	Disable_pf       int
	Complexity       int
	Upsample         int
	Start            int
	End              int
	Bitrate          int32
	Vbr              int
	Signalling       int
	Constrained_vbr  int
	Loss_rate        int
	Lsb_depth        int
	Lfe              int
	Disable_inv      int
	Arch             int
	Rng              uint32
	Spread_decision  int
	DelayedIntra     opus_val32
	Tonal_average    int
	LastCodedBands   int
	Hf_average       int
	Tapset_decision  int
	Prefilter_period int
	Prefilter_gain   opus_val16
	Prefilter_tapset int
	Consec_transient int
	Analysis         AnalysisInfo
	Silk_info        SILKInfo
	Preemph_memE     [2]opus_val32
	Preemph_memD     [2]opus_val32
	Vbr_reservoir    int32
	Vbr_drift        int32
	Vbr_offset       int32
	Vbr_count        int32
	Overlap_max      opus_val32
	Stereo_saving    opus_val16
	Intensity        int
	Energy_mask      *opus_val16
	Spec_avg         opus_val16
	// In_mem is a C flexible array member. In C, the struct is allocated with
	// extra bytes: malloc(sizeof(OpusCustomEncoder) + extra). Code accesses
	// In_mem[i] for i up to channels*(overlap+COMBFILTER_MAXPERIOD)+channels*4*NbEBands.
	// Max: 2*(120+1024)+2*4*21 = 2456. We use 8192 for safety.
	In_mem           [8192]celt_sig
}

func celt_encoder_get_size(channels int) int {
	var mode *OpusCustomMode = opus_custom_mode_create(48000, 960, nil)
	return opus_custom_encoder_get_size(mode, channels)
}
func opus_custom_encoder_get_size(mode *OpusCustomMode, channels int) int {
	var size int = (channels*mode.Overlap-1)*int(unsafe.Sizeof(celt_sig(0))) + int(unsafe.Sizeof(OpusCustomEncoder{})) + channels*COMBFILTER_MAXPERIOD*int(unsafe.Sizeof(celt_sig(0))) + channels*4*mode.NbEBands*int(unsafe.Sizeof(opus_val16(0)))
	return size
}
func opus_custom_encoder_init_arch(st *OpusCustomEncoder, mode *OpusCustomMode, channels int, arch int) int {
	if channels < 0 || channels > 2 {
		return -1
	}
	if st == nil || mode == nil {
		return -7
	}
	libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(st))), 0, opus_custom_encoder_get_size(mode, channels)*int(unsafe.Sizeof(byte(0))))
	st.Mode = mode
	st.Stream_channels = func() int {
		p := &st.Channels
		st.Channels = channels
		return *p
	}()
	st.Upsample = 1
	st.Start = 0
	st.End = st.Mode.EffEBands
	st.Signalling = 1
	st.Arch = arch
	st.Constrained_vbr = 1
	st.Clip = 1
	st.Bitrate = -1
	st.Vbr = 0
	st.Force_intra = 0
	st.Complexity = 5
	st.Lsb_depth = 24
	opus_custom_encoder_ctl(st, OPUS_RESET_STATE)
	return OPUS_OK
}
func celt_encoder_init(st *OpusCustomEncoder, sampling_rate int32, channels int, arch int) int {
	var ret int
	ret = opus_custom_encoder_init_arch(st, opus_custom_mode_create(48000, 960, nil), channels, arch)
	if ret != OPUS_OK {
		return ret
	}
	st.Upsample = resampling_factor(sampling_rate)
	return OPUS_OK
}
func transient_analysis(in *opus_val32, len_ int, C int, tf_estimate *opus_val16, tf_chan *int, allow_weak_transients int, weak_transient *int) int {
	var (
		i             int
		tmp           *opus_val16
		mem0          opus_val32
		mem1          opus_val32
		is_transient  int   = 0
		mask_metric   int32 = 0
		c             int
		tf_max        opus_val16
		len2          int
		forward_decay opus_val16 = opus_val16(0.0625)
		inv_table     [128]uint8 = [128]uint8{math.MaxUint8, math.MaxUint8, 156, 110, 86, 70, 59, 51, 45, 40, 37, 33, 31, 28, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 16, 15, 15, 14, 13, 13, 12, 12, 12, 12, 11, 11, 11, 10, 10, 10, 9, 9, 9, 9, 9, 9, 8, 8, 8, 8, 8, 7, 7, 7, 7, 7, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 2}
	)
	tmp = (*opus_val16)(libc.Malloc(len_ * int(unsafe.Sizeof(opus_val16(0)))))
	*weak_transient = 0
	if allow_weak_transients != 0 {
		forward_decay = opus_val16(0.03125)
	}
	len2 = len_ / 2
	for c = 0; c < C; c++ {
		var (
			mean   opus_val32
			unmask int32 = 0
			norm   opus_val32
			maxE   opus_val16
		)
		mem0 = 0
		mem1 = 0
		for i = 0; i < len_; i++ {
			var (
				x opus_val32
				y opus_val32
			)
			x = *(*opus_val32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(opus_val32(0))*uintptr(i+c*len_)))
			y = mem0 + x
			mem0 = mem1 + y - opus_val32(float32(x)*2)
			mem1 = x - y*opus_val32(0.5)
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(y)
		}
		libc.MemSet(unsafe.Pointer(tmp), 0, int(12*unsafe.Sizeof(opus_val16(0))))
		mean = 0
		mem0 = 0
		for i = 0; i < len2; i++ {
			var x2 opus_val16 = opus_val16((opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i*2)))) * opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i*2))))) + opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i*2+1))))*opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i*2+1)))))
			mean += opus_val32(x2)
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(mem0 + opus_val32(forward_decay*(x2-opus_val16(mem0))))
			mem0 = opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i))))
		}
		mem0 = 0
		maxE = 0
		for i = len2 - 1; i >= 0; i-- {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(mem0 + opus_val32((*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i)))-opus_val16(mem0))*opus_val16(0.125)))
			mem0 = opus_val32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i))))
			if maxE > opus_val16(mem0) {
				maxE = maxE
			} else {
				maxE = opus_val16(mem0)
			}
		}
		mean = opus_val32(float32(math.Sqrt(float64(float32(mean*opus_val32(maxE)*opus_val32(0.5)) * float32(len2)))))
		norm = opus_val32(float32(len2) / float32(EPSILON+mean))
		unmask = 0
		for i = 12; i < len2-5; i += 4 {
			var id int
			if 0 > (func() float64 {
				if math.MaxInt8 < math.Floor(float64(float32(norm)*64*float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i)))+EPSILON))) {
					return math.MaxInt8
				}
				return math.Floor(float64(float32(norm) * 64 * float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i)))+EPSILON)))
			}()) {
				id = 0
			} else if math.MaxInt8 < math.Floor(float64(float32(norm)*64*float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i)))+EPSILON))) {
				id = math.MaxInt8
			} else {
				id = int(math.Floor(float64(float32(norm) * 64 * float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val16(0))*uintptr(i)))+EPSILON))))
			}
			unmask += int32(inv_table[id])
		}
		unmask = int32(int(unmask) * 64 * 4 / ((len2 - 17) * 6))
		if int(unmask) > int(mask_metric) {
			*tf_chan = c
			mask_metric = unmask
		}
	}
	is_transient = int(libc.BoolToInt(int(mask_metric) > 200))
	if allow_weak_transients != 0 && is_transient != 0 && int(mask_metric) < 600 {
		is_transient = 0
		*weak_transient = 1
	}
	if 0 > ((float32(math.Sqrt(float64(int(mask_metric) * 27)))) - 42) {
		tf_max = 0
	} else {
		tf_max = opus_val16((float32(math.Sqrt(float64(int(mask_metric) * 27)))) - 42)
	}
	*tf_estimate = opus_val16(float32(math.Sqrt(float64(func() opus_val32 {
		if 0 > float32((opus_val32(func() opus_val16 {
			if 163 < float32(tf_max) {
				return 163
			}
			return tf_max
		}())*opus_val32(0.0069))-opus_val32(0.139)) {
			return 0
		}
		return (opus_val32(func() opus_val16 {
			if 163 < float32(tf_max) {
				return 163
			}
			return tf_max
		}()) * opus_val32(0.0069)) - opus_val32(0.139)
	}()))))
	return is_transient
}
func patch_transient_decision(newE *opus_val16, oldE *opus_val16, nbEBands int, start int, end int, C int) int {
	var (
		i          int
		c          int
		mean_diff  opus_val32 = 0
		spread_old [26]opus_val16
	)
	if C == 1 {
		spread_old[start] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(start)))
		for i = start + 1; i < end; i++ {
			if (spread_old[i-1] - opus_val16(1.0)) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
				spread_old[i] = spread_old[i-1] - opus_val16(1.0)
			} else {
				spread_old[i] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
		}
	} else {
		if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(start)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(start+nbEBands)))) {
			spread_old[start] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(start)))
		} else {
			spread_old[start] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(start+nbEBands)))
		}
		for i = start + 1; i < end; i++ {
			if (spread_old[i-1] - opus_val16(1.0)) > (func() opus_val16 {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands)))) {
					return *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}
				return *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands)))
			}()) {
				spread_old[i] = spread_old[i-1] - opus_val16(1.0)
			} else if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands)))) {
				spread_old[i] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				spread_old[i] = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands)))
			}
		}
	}
	for i = end - 2; i >= start; i-- {
		if (spread_old[i]) > (spread_old[i+1] - opus_val16(1.0)) {
			spread_old[i] = spread_old[i]
		} else {
			spread_old[i] = spread_old[i+1] - opus_val16(1.0)
		}
	}
	c = 0
	for {
		for func() {
			if 2 > start {
				i = 2
			} else {
				i = start
			}
		}(); i < end-1; i++ {
			var (
				x1 opus_val16
				x2 opus_val16
			)
			if 0 > float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(newE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))) {
				x1 = 0
			} else {
				x1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(newE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))
			}
			if 0 > float32(spread_old[i]) {
				x2 = 0
			} else {
				x2 = spread_old[i]
			}
			mean_diff = mean_diff + opus_val32(func() opus_val16 {
				if 0 > float32(x1-x2) {
					return 0
				}
				return x1 - x2
			}())
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
	mean_diff = mean_diff / opus_val32(C*(end-1-(func() int {
		if 2 > start {
			return 2
		}
		return start
	}())))
	return int(libc.BoolToInt(mean_diff > opus_val32(1.0)))
}
func compute_mdcts(mode *OpusCustomMode, shortBlocks int, in *celt_sig, out *celt_sig, C int, CC int, LM int, upsample int, arch int) {
	var (
		overlap int = mode.Overlap
		N       int
		B       int
		shift   int
		i       int
		b       int
		c       int
	)
	if shortBlocks != 0 {
		B = shortBlocks
		N = mode.ShortMdctSize
		shift = mode.MaxLM
	} else {
		B = 1
		N = mode.ShortMdctSize << LM
		shift = mode.MaxLM - LM
	}
	c = 0
	for {
		for b = 0; b < B; b++ {
			clt_mdct_forward_c(&mode.Mdct, (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(B*N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(b*N))))), (*float32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(b+c*N*B))))), mode.Window, overlap, shift, B, arch)
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	if CC == 2 && C == 1 {
		for i = 0; i < B*N; i++ {
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(i))) = ((*(*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(i)))) * celt_sig(0.5)) + (*(*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(B*N+i))))*celt_sig(0.5)
		}
	}
	if upsample != 1 {
		c = 0
		for {
			{
				var bound int = B * N / upsample
				for i = 0; i < bound; i++ {
					*(*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(c*B*N+i))) *= celt_sig(upsample)
				}
				libc.MemSet(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(celt_sig(0))*uintptr(c*B*N+bound)))), 0, (B*N-bound)*int(unsafe.Sizeof(celt_sig(0))))
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
}
func celt_preemphasis(pcmp *opus_val16, inp *celt_sig, N int, CC int, upsample int, coef *opus_val16, mem *celt_sig, clip int) {
	var (
		i     int
		coef0 opus_val16
		m     celt_sig
		Nu    int
	)
	coef0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(coef), unsafe.Sizeof(opus_val16(0))*0))
	m = *mem
	if float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(coef), unsafe.Sizeof(opus_val16(0))*1))) == 0 && upsample == 1 && clip == 0 {
		for i = 0; i < N; i++ {
			var x opus_val16
			x = (*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcmp), unsafe.Sizeof(opus_val16(0))*uintptr(CC*i)))) * CELT_SIG_SCALE
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i))) = celt_sig(x - opus_val16(m))
			m = celt_sig(opus_val32(coef0) * opus_val32(x))
		}
		*mem = m
		return
	}
	Nu = N / upsample
	if upsample != 1 {
		libc.MemSet(unsafe.Pointer(inp), 0, N*int(unsafe.Sizeof(celt_sig(0))))
	}
	for i = 0; i < Nu; i++ {
		*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample))) = celt_sig((*(*opus_val16)(unsafe.Add(unsafe.Pointer(pcmp), unsafe.Sizeof(opus_val16(0))*uintptr(CC*i)))) * CELT_SIG_SCALE)
	}
	if clip != 0 {
		for i = 0; i < Nu; i++ {
			if (-65536.0) > (func() celt_sig {
				if celt_sig(65536.0) < (*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample)))) {
					return celt_sig(65536.0)
				}
				return *(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample)))
			}()) {
				*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample))) = celt_sig(-65536.0)
			} else if celt_sig(65536.0) < (*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample)))) {
				*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample))) = celt_sig(65536.0)
			} else {
				*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample))) = *(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i*upsample)))
			}
		}
	}
	{
		for i = 0; i < N; i++ {
			var x opus_val16
			x = opus_val16(*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i))))
			*(*celt_sig)(unsafe.Add(unsafe.Pointer(inp), unsafe.Sizeof(celt_sig(0))*uintptr(i))) = celt_sig(x - opus_val16(m))
			m = celt_sig(opus_val32(coef0) * opus_val32(x))
		}
	}
	*mem = m
}
func l1_metric(tmp *celt_norm, N int, LM int, bias opus_val16) opus_val32 {
	var (
		i  int
		L1 opus_val32
	)
	L1 = 0
	for i = 0; i < N; i++ {
		L1 += opus_val32(float32(math.Abs(float64(*(*celt_norm)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(celt_norm(0))*uintptr(i)))))))
	}
	L1 = L1 + opus_val32((float32(LM)*float32(bias))*float32(L1))
	return L1
}
func tf_analysis(m *OpusCustomMode, len_ int, isTransient int, tf_res *int, lambda int, X *celt_norm, N0 int, LM int, tf_estimate opus_val16, tf_chan int, importance *int) int {
	var (
		i         int
		metric    *int
		cost0     int
		cost1     int
		path0     *int
		path1     *int
		tmp       *celt_norm
		tmp_1     *celt_norm
		sel       int
		selcost   [2]int
		tf_select int = 0
		bias      opus_val16
	)
	bias = (func() opus_val16 {
		if (-0.25) > (opus_val16(0.5) - tf_estimate) {
			return opus_val16(-0.25)
		}
		return opus_val16(0.5) - tf_estimate
	}()) * opus_val16(0.04)
	metric = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	tmp = (*celt_norm)(libc.Malloc(((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(len_)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(len_-1))))) << LM) * int(unsafe.Sizeof(celt_norm(0)))))
	tmp_1 = (*celt_norm)(libc.Malloc(((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(len_)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(len_-1))))) << LM) * int(unsafe.Sizeof(celt_norm(0)))))
	path0 = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	path1 = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	for i = 0; i < len_; i++ {
		var (
			k          int
			N          int
			narrow     int
			L1         opus_val32
			best_L1    opus_val32
			best_level int = 0
		)
		N = (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))) << LM
		narrow = int(libc.BoolToInt((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))) == 1))
		libc.MemCpy(unsafe.Pointer(tmp), unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(tf_chan*N0+(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM))))), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(tmp))-uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(tf_chan*N0+(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM))))))))*0))
		L1 = l1_metric(tmp, N, func() int {
			if isTransient != 0 {
				return LM
			}
			return 0
		}(), bias)
		best_L1 = L1
		if isTransient != 0 && narrow == 0 {
			libc.MemCpy(unsafe.Pointer(tmp_1), unsafe.Pointer(tmp), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(tmp_1))-uintptr(unsafe.Pointer(tmp))))*0))
			haar1(tmp_1, N>>LM, 1<<LM)
			L1 = l1_metric(tmp_1, N, LM+1, bias)
			if L1 < best_L1 {
				best_L1 = L1
				best_level = -1
			}
		}
		for k = 0; k < LM+int(libc.BoolToInt(isTransient == 0 && narrow == 0)); k++ {
			var B int
			if isTransient != 0 {
				B = LM - k - 1
			} else {
				B = k + 1
			}
			haar1(tmp, N>>k, 1<<k)
			L1 = l1_metric(tmp, N, B, bias)
			if L1 < best_L1 {
				best_L1 = L1
				best_level = k + 1
			}
		}
		if isTransient != 0 {
			*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i))) = best_level * 2
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i))) = best_level * (-2)
		}
		if narrow != 0 && (*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i))) == 0 || *(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i))) == LM*(-2)) {
			*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i))) -= 1
		}
	}
	tf_select = 0
	for sel = 0; sel < 2; sel++ {
		cost0 = *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*0)) * int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*0))-int(tf_select_table[LM][isTransient*4+sel*2+0])*2)))
		cost1 = *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*0))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*0))-int(tf_select_table[LM][isTransient*4+sel*2+1])*2))) + (func() int {
			if isTransient != 0 {
				return 0
			}
			return lambda
		}())
		for i = 1; i < len_; i++ {
			var (
				curr0 int
				curr1 int
			)
			if cost0 < (cost1 + lambda) {
				curr0 = cost0
			} else {
				curr0 = cost1 + lambda
			}
			if (cost0 + lambda) < cost1 {
				curr1 = cost0 + lambda
			} else {
				curr1 = cost1
			}
			cost0 = curr0 + *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i)))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i)))-int(tf_select_table[LM][isTransient*4+sel*2+0])*2)))
			cost1 = curr1 + *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i)))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i)))-int(tf_select_table[LM][isTransient*4+sel*2+1])*2)))
		}
		if cost0 < cost1 {
			cost0 = cost0
		} else {
			cost0 = cost1
		}
		selcost[sel] = cost0
	}
	if selcost[1] < selcost[0] && isTransient != 0 {
		tf_select = 1
	}
	cost0 = *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*0)) * int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*0))-int(tf_select_table[LM][isTransient*4+tf_select*2+0])*2)))
	cost1 = *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*0))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*0))-int(tf_select_table[LM][isTransient*4+tf_select*2+1])*2))) + (func() int {
		if isTransient != 0 {
			return 0
		}
		return lambda
	}())
	for i = 1; i < len_; i++ {
		var (
			curr0 int
			curr1 int
			from0 int
			from1 int
		)
		from0 = cost0
		from1 = cost1 + lambda
		if from0 < from1 {
			curr0 = from0
			*(*int)(unsafe.Add(unsafe.Pointer(path0), unsafe.Sizeof(int(0))*uintptr(i))) = 0
		} else {
			curr0 = from1
			*(*int)(unsafe.Add(unsafe.Pointer(path0), unsafe.Sizeof(int(0))*uintptr(i))) = 1
		}
		from0 = cost0 + lambda
		from1 = cost1
		if from0 < from1 {
			curr1 = from0
			*(*int)(unsafe.Add(unsafe.Pointer(path1), unsafe.Sizeof(int(0))*uintptr(i))) = 0
		} else {
			curr1 = from1
			*(*int)(unsafe.Add(unsafe.Pointer(path1), unsafe.Sizeof(int(0))*uintptr(i))) = 1
		}
		cost0 = curr0 + *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i)))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i)))-int(tf_select_table[LM][isTransient*4+tf_select*2+0])*2)))
		cost1 = curr1 + *(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i)))*int(cmath.Abs(int64(*(*int)(unsafe.Add(unsafe.Pointer(metric), unsafe.Sizeof(int(0))*uintptr(i)))-int(tf_select_table[LM][isTransient*4+tf_select*2+1])*2)))
	}
	if cost0 < cost1 {
		*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(len_-1))) = 0
	} else {
		*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(len_-1))) = 1
	}
	for i = len_ - 2; i >= 0; i-- {
		if *(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i+1))) == 1 {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = *(*int)(unsafe.Add(unsafe.Pointer(path1), unsafe.Sizeof(int(0))*uintptr(i+1)))
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = *(*int)(unsafe.Add(unsafe.Pointer(path0), unsafe.Sizeof(int(0))*uintptr(i+1)))
		}
	}
	return tf_select
}
func tf_encode(start int, end int, isTransient int, tf_res *int, LM int, tf_select int, enc *ec_enc) {
	var (
		curr          int
		i             int
		tf_select_rsv int
		tf_changed    int
		logp          int
		budget        uint32
		tell          uint32
	)
	budget = uint32(int32(int(enc.Storage) * 8))
	tell = uint32(int32(ec_tell((*ec_ctx)(unsafe.Pointer(enc)))))
	if isTransient != 0 {
		logp = 2
	} else {
		logp = 4
	}
	tf_select_rsv = int(libc.BoolToInt(LM > 0 && int(tell)+logp+1 <= int(budget)))
	budget -= uint32(int32(tf_select_rsv))
	curr = func() int {
		tf_changed = 0
		return tf_changed
	}()
	for i = start; i < end; i++ {
		if int(tell)+logp <= int(budget) {
			ec_enc_bit_logp(enc, *(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i)))^curr, uint(logp))
			tell = uint32(int32(ec_tell((*ec_ctx)(unsafe.Pointer(enc)))))
			curr = *(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i)))
			tf_changed |= curr
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = curr
		}
		if isTransient != 0 {
			logp = 4
		} else {
			logp = 5
		}
	}
	if tf_select_rsv != 0 && int(tf_select_table[LM][isTransient*4+0+tf_changed]) != int(tf_select_table[LM][isTransient*4+2+tf_changed]) {
		ec_enc_bit_logp(enc, tf_select, 1)
	} else {
		tf_select = 0
	}
	for i = start; i < end; i++ {
		*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = int(tf_select_table[LM][isTransient*4+tf_select*2+*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i)))])
	}
}
func alloc_trim_analysis(m *OpusCustomMode, X *celt_norm, bandLogE *opus_val16, end int, LM int, C int, N0 int, analysis *AnalysisInfo, stereo_saving *opus_val16, tf_estimate opus_val16, intensity int, surround_trim opus_val16, equiv_rate int32, arch int) int {
	var (
		i          int
		diff       opus_val32 = 0
		c          int
		trim_index int
		trim       opus_val16 = opus_val16(5.0)
		logXC      opus_val16
		logXC2     opus_val16
	)
	if int(equiv_rate) < 64000 {
		trim = opus_val16(4.0)
	} else if int(equiv_rate) < 80000 {
		var frac int32 = int32((int(equiv_rate) - 64000) >> 10)
		trim = opus_val16(float64(frac)*(1.0/16.0) + 4.0)
	}
	if C == 2 {
		var (
			sum   opus_val16 = 0
			minXC opus_val16
		)
		for i = 0; i < 8; i++ {
			var partial opus_val32
			partial = func() opus_val32 {
				_ = arch
				return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM))))), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM), unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N0+(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM)))))), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM)
			}()
			sum = sum + opus_val16(partial)
		}
		sum = sum * (1.0 / 8)
		if 1.0 < (float32(math.Abs(float64(sum)))) {
			sum = opus_val16(1.0)
		} else {
			sum = opus_val16(float32(math.Abs(float64(sum))))
		}
		minXC = sum
		for i = 8; i < intensity; i++ {
			var partial opus_val32
			partial = func() opus_val32 {
				_ = arch
				return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM))))), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM), unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N0+(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM)))))), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM), (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM)
			}()
			if minXC < opus_val16(float32(math.Abs(float64(partial)))) {
				minXC = minXC
			} else {
				minXC = opus_val16(float32(math.Abs(float64(partial))))
			}
		}
		if 1.0 < (float32(math.Abs(float64(minXC)))) {
			minXC = opus_val16(1.0)
		} else {
			minXC = opus_val16(float32(math.Abs(float64(minXC))))
		}
		logXC = opus_val16(float32(math.Log(float64(opus_val32(1.001)-opus_val32(sum)*opus_val32(sum))) * 1.4426950408889634))
		if (logXC * opus_val16(0.5)) > opus_val16(float32(math.Log(float64(opus_val32(1.001)-opus_val32(minXC)*opus_val32(minXC)))*1.4426950408889634)) {
			logXC2 = logXC * opus_val16(0.5)
		} else {
			logXC2 = opus_val16(float32(math.Log(float64(opus_val32(1.001)-opus_val32(minXC)*opus_val32(minXC))) * 1.4426950408889634))
		}
		if (-4.0) > (logXC * opus_val16(0.75)) {
			trim += opus_val16(-4.0)
		} else {
			trim += logXC * opus_val16(0.75)
		}
		if (*stereo_saving + opus_val16(0.25)) < opus_val16(float32(-(logXC2 * opus_val16(0.5)))) {
			*stereo_saving = *stereo_saving + opus_val16(0.25)
		} else {
			*stereo_saving = opus_val16(float32(-(logXC2 * opus_val16(0.5))))
		}
	}
	c = 0
	for {
		for i = 0; i < end-1; i++ {
			diff += opus_val32(float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*m.NbEBands)))) * float32(int32(i*2+2-end)))
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
	diff /= opus_val32(C * (end - 1))
	if (-2.0) > (func() float32 {
		if 2.0 < (float32(diff+opus_val32(1.0)) / 6) {
			return 2.0
		}
		return float32(diff+opus_val32(1.0)) / 6
	}()) {
		trim -= opus_val16(-2.0)
	} else if 2.0 < (float32(diff+opus_val32(1.0)) / 6) {
		trim -= opus_val16(2.0)
	} else {
		trim -= opus_val16(float32(diff+opus_val32(1.0)) / 6)
	}
	trim -= surround_trim
	trim -= opus_val16(float32(tf_estimate) * 2)
	if analysis.Valid != 0 {
		if (-2.0) > (func() opus_val16 {
			if opus_val16(2.0) < (opus_val16((analysis.Tonality_slope + 0.05) * 2.0)) {
				return opus_val16(2.0)
			}
			return opus_val16((analysis.Tonality_slope + 0.05) * 2.0)
		}()) {
			trim -= opus_val16(-2.0)
		} else if opus_val16(2.0) < (opus_val16((analysis.Tonality_slope + 0.05) * 2.0)) {
			trim -= opus_val16(2.0)
		} else {
			trim -= opus_val16((analysis.Tonality_slope + 0.05) * 2.0)
		}
	}
	trim_index = int(math.Floor(float64(trim + opus_val16(0.5))))
	if 0 > (func() int {
		if 10 < trim_index {
			return 10
		}
		return trim_index
	}()) {
		trim_index = 0
	} else if 10 < trim_index {
		trim_index = 10
	} else {
		trim_index = trim_index
	}
	return trim_index
}
func stereo_analysis(m *OpusCustomMode, X *celt_norm, LM int, N0 int) int {
	var (
		i      int
		thetas int
		sumLR  opus_val32 = EPSILON
		sumMS  opus_val32 = EPSILON
	)
	for i = 0; i < 13; i++ {
		var j int
		for j = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))) << LM; j < int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))<<LM; j++ {
			var (
				L opus_val32
				R opus_val32
				M opus_val32
				S opus_val32
			)
			L = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))))
			R = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N0+j))))
			M = L + R
			S = L - R
			sumLR = sumLR + opus_val32((float32(math.Abs(float64(L))))+(float32(math.Abs(float64(R)))))
			sumMS = sumMS + opus_val32((float32(math.Abs(float64(M))))+(float32(math.Abs(float64(S)))))
		}
	}
	sumMS = sumMS * opus_val32(0.707107)
	thetas = 13
	if LM <= 1 {
		thetas -= 8
	}
	return int(libc.BoolToInt((float32((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*13)))<<(LM+1))+thetas) * float32(sumMS)) > (float32(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*13)))<<(LM+1)) * float32(sumLR))))
}
func median_of_5(x *opus_val16) opus_val16 {
	var (
		t0 opus_val16
		t1 opus_val16
		t2 opus_val16
		t3 opus_val16
		t4 opus_val16
	)
	t2 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*2))
	if *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0)) > *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1)) {
		t0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1))
		t1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0))
	} else {
		t0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0))
		t1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1))
	}
	if *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*3)) > *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*4)) {
		t3 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*4))
		t4 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*3))
	} else {
		t3 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*3))
		t4 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*4))
	}
	if t0 > t3 {
		for {
			{
				var tmp opus_val16 = t0
				t0 = t3
				t3 = tmp
			}
			if true {
				break
			}
		}
		for {
			{
				var tmp opus_val16 = t1
				t1 = t4
				t4 = tmp
			}
			if true {
				break
			}
		}
	}
	if t2 > t1 {
		if t1 < t3 {
			if t2 < t3 {
				return t2
			}
			return t3
		} else {
			if t4 < t1 {
				return t4
			}
			return t1
		}
	} else {
		if t2 < t3 {
			if t1 < t3 {
				return t1
			}
			return t3
		} else {
			if t2 < t4 {
				return t2
			}
			return t4
		}
	}
}
func median_of_3(x *opus_val16) opus_val16 {
	var (
		t0 opus_val16
		t1 opus_val16
		t2 opus_val16
	)
	if *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0)) > *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1)) {
		t0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1))
		t1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0))
	} else {
		t0 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*0))
		t1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*1))
	}
	t2 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val16(0))*2))
	if t1 < t2 {
		return t1
	} else if t0 < t2 {
		return t2
	} else {
		return t0
	}
}
func dynalloc_analysis(bandLogE *opus_val16, bandLogE2 *opus_val16, nbEBands int, start int, end int, C int, offsets *int, lsb_depth int, logN *int16, isTransient int, vbr int, constrained_vbr int, eBands *int16, LM int, effectiveBytes int, tot_boost_ *int32, lfe int, surround_dynalloc *opus_val16, analysis *AnalysisInfo, importance *int, spread_weight *int) opus_val16 {
	var (
		i           int
		c           int
		tot_boost   int32 = 0
		maxDepth    opus_val16
		follower    *opus_val16
		noise_floor *opus_val16
	)
	follower = (*opus_val16)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(opus_val16(0)))))
	noise_floor = (*opus_val16)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(opus_val16(0)))))
	libc.MemSet(unsafe.Pointer(offsets), 0, nbEBands*int(unsafe.Sizeof(int(0))))
	maxDepth = opus_val16(-31.9)
	for i = 0; i < end; i++ {
		*(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(float32((opus_val32(*(*int16)(unsafe.Add(unsafe.Pointer(logN), unsafe.Sizeof(int16(0))*uintptr(i))))*opus_val32(0.0625))+opus_val32(0.5)) + float32(9-lsb_depth) - float32(eMeans[i]) + float32(opus_val32((i+5)*(i+5))*opus_val32(0.0062)))
	}
	c = 0
	for {
		for i = 0; i < end; i++ {
			if maxDepth > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
				maxDepth = maxDepth
			} else {
				maxDepth = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
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
	{
		var (
			mask *opus_val16
			sig  *opus_val16
		)
		mask = (*opus_val16)(libc.Malloc(nbEBands * int(unsafe.Sizeof(opus_val16(0)))))
		sig = (*opus_val16)(libc.Malloc(nbEBands * int(unsafe.Sizeof(opus_val16(0)))))
		for i = 0; i < end; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
		}
		if C == 2 {
			for i = 0; i < end; i++ {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}
			}
		}
		libc.MemCpy(unsafe.Pointer(sig), unsafe.Pointer(mask), end*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(sig))-uintptr(unsafe.Pointer(mask))))*0))
		for i = 1; i < end; i++ {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i-1))) - opus_val16(2.0)) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i-1))) - opus_val16(2.0)
			}
		}
		for i = end - 2; i >= 0; i-- {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) - opus_val16(3.0)) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) - opus_val16(3.0)
			}
		}
		for i = 0; i < end; i++ {
			var (
				smr opus_val16 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(sig), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - (func() opus_val16 {
					if (func() opus_val16 {
						if 0 > float32(maxDepth-opus_val16(12.0)) {
							return 0
						}
						return maxDepth - opus_val16(12.0)
					}()) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
						if 0 > float32(maxDepth-opus_val16(12.0)) {
							return 0
						}
						return maxDepth - opus_val16(12.0)
					}
					return *(*opus_val16)(unsafe.Add(unsafe.Pointer(mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}())
				shift int = (func() int {
					if 5 < (func() int {
						if 0 > (-int(math.Floor(float64(smr + opus_val16(0.5))))) {
							return 0
						}
						return -int(math.Floor(float64(smr + opus_val16(0.5))))
					}()) {
						return 5
					}
					if 0 > (-int(math.Floor(float64(smr + opus_val16(0.5))))) {
						return 0
					}
					return -int(math.Floor(float64(smr + opus_val16(0.5))))
				}())
			)
			*(*int)(unsafe.Add(unsafe.Pointer(spread_weight), unsafe.Sizeof(int(0))*uintptr(i))) = 32 >> shift
		}
	}
	if effectiveBytes > 50 && LM >= 1 && lfe == 0 {
		var last int = 0
		c = 0
		for {
			{
				var (
					offset opus_val16
					tmp    opus_val16
					f      *opus_val16
				)
				f = (*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands)))
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*0)) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands)))
				for i = 1; i < end; i++ {
					if *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i))) > *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i-1)))+opus_val16(0.5) {
						last = i
					}
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i-1))) + opus_val16(1.5)) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i-1))) + opus_val16(1.5)
					} else {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
					}
				}
				for i = last - 1; i >= 0; i-- {
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) < (func() opus_val16 {
						if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) + opus_val16(2.0)) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))) {
							return *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) + opus_val16(2.0)
						}
						return *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
					}()) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
					} else if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) + opus_val16(2.0)) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i+1))) + opus_val16(2.0)
					} else {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i)))
					}
				}
				offset = opus_val16(1.0)
				for i = 2; i < end-2; i++ {
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (median_of_5((*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i-2)))) - offset) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
					} else {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = median_of_5((*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+i-2)))) - offset
					}
				}
				tmp = median_of_3((*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands)))) - offset
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*0))) > tmp {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*0)) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*0))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*0)) = tmp
				}
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*1))) > tmp {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*1)) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*1))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*1)) = tmp
				}
				tmp = median_of_3((*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(c*nbEBands+end-3)))) - offset
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-2)))) > tmp {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-2))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-2)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-2))) = tmp
				}
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-1)))) > tmp {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-1))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-1)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(end-1))) = tmp
				}
				for i = 0; i < end; i++ {
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
					} else {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(f), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(noise_floor), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
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
		if C == 2 {
			for i = start; i < end; i++ {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - opus_val16(4.0)) {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - opus_val16(4.0)
				}
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) - opus_val16(4.0)) {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) - opus_val16(4.0)
				}
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = ((func() opus_val16 {
					if 0 > float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))-*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
						return 0
					}
					return *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}()) + (func() opus_val16 {
					if 0 > float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))-*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))) {
						return 0
					}
					return *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))
				}())) * opus_val16(0.5)
			}
		} else {
			for i = start; i < end; i++ {
				if 0 > float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))-*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = 0
				} else {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}
			}
		}
		for i = start; i < end; i++ {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
		}
		for i = start; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i))) = int(math.Floor(float64((float32(math.Exp(float64((func() opus_val16 {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) < opus_val16(4.0) {
					return *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				}
				return opus_val16(4.0)
			}())*opus_val16(0.6931471805599453)))))*13 + 0.5)))
		}
		if (vbr == 0 || constrained_vbr != 0) && isTransient == 0 {
			for i = start; i < end; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * opus_val16(0.5)
			}
		}
		for i = start; i < end; i++ {
			if i < 8 {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) *= 2
			}
			if i >= 12 {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = (*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * opus_val16(0.5)
			}
		}
		if analysis.Valid != 0 {
			for i = start; i < (func() int {
				if LEAK_BANDS < end {
					return LEAK_BANDS
				}
				return end
			}()); i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(float64(*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) + float64(analysis.Leak_boost[i])*(1.0/64.0))
			}
		}
		for i = start; i < end; i++ {
			var (
				width      int
				boost      int
				boost_bits int
			)
			if float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) < 4 {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = 4
			}
			width = C * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))) << LM
			if width < 6 {
				boost = int(*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i))))
				boost_bits = boost * width << BITRES
			} else if width > 48 {
				boost = int(float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * 8)
				boost_bits = (boost * width << BITRES) / 8
			} else {
				boost = int(float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(follower), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * float32(width) / 6)
				boost_bits = boost * 6 << BITRES
			}
			if (vbr == 0 || constrained_vbr != 0 && isTransient == 0) && (int(tot_boost)+boost_bits)>>BITRES>>3 > effectiveBytes*2/3 {
				var cap_ int32 = int32((effectiveBytes * 2 / 3) << BITRES << 3)
				*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(i))) = int(cap_) - int(tot_boost)
				tot_boost = cap_
				break
			} else {
				*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(i))) = boost
				tot_boost += int32(boost_bits)
			}
		}
	} else {
		for i = start; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(importance), unsafe.Sizeof(int(0))*uintptr(i))) = 13
		}
	}
	*tot_boost_ = tot_boost
	return maxDepth
}
func run_prefilter(st *OpusCustomEncoder, in *celt_sig, prefilter_mem *celt_sig, CC int, N int, prefilter_tapset int, pitch *int, gain *opus_val16, qgain *int, enabled int, nbAvailableBytes int, analysis *AnalysisInfo) int {
	var (
		c            int
		_pre         *celt_sig
		pre          [2]*celt_sig
		mode         *OpusCustomMode
		pitch_index  int
		pitch_index_slice [1]int
		gain1        opus_val16
		pf_threshold opus_val16
		pf_on        int
		qg           int
		overlap      int
	)
	mode = st.Mode
	overlap = mode.Overlap
	_pre = (*celt_sig)(libc.Malloc((CC * (N + COMBFILTER_MAXPERIOD)) * int(unsafe.Sizeof(celt_sig(0)))))
	pre[0] = _pre
	pre[1] = (*celt_sig)(unsafe.Add(unsafe.Pointer(_pre), unsafe.Sizeof(celt_sig(0))*uintptr(N+COMBFILTER_MAXPERIOD)))
	c = 0
	for {
		libc.MemCpy(unsafe.Pointer(pre[c]), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), int(COMBFILTER_MAXPERIOD*unsafe.Sizeof(celt_sig(0))+uintptr((int64(uintptr(unsafe.Pointer(pre[c]))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))))))*0)))
		libc.MemCpy(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(overlap)))), N*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(overlap)))))))*0))
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	if enabled != 0 {
		var pitch_buf *opus_val16
		pitch_buf = (*opus_val16)(libc.Malloc(((COMBFILTER_MAXPERIOD + N) >> 1) * int(unsafe.Sizeof(opus_val16(0)))))
		pitch_downsample(pre[:], unsafe.Slice(pitch_buf, (COMBFILTER_MAXPERIOD+N)>>1), COMBFILTER_MAXPERIOD+N, CC, st.Arch)
		pitch_search(unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(pitch_buf), unsafe.Sizeof(opus_val16(0))*uintptr(int(COMBFILTER_MAXPERIOD>>1)))), N>>1), unsafe.Slice(pitch_buf, (COMBFILTER_MAXPERIOD+N)>>1), N, COMBFILTER_MAXPERIOD-int(COMBFILTER_MINPERIOD*3), pitch_index_slice[:], st.Arch)
		pitch_index = pitch_index_slice[0]
		pitch_index = COMBFILTER_MAXPERIOD - pitch_index
		gain1 = remove_doubling(unsafe.Slice(pitch_buf, (COMBFILTER_MAXPERIOD+N)>>1), COMBFILTER_MAXPERIOD, COMBFILTER_MINPERIOD, N, &pitch_index, st.Prefilter_period, st.Prefilter_gain, st.Arch)
		if pitch_index > int(COMBFILTER_MAXPERIOD-2) {
			pitch_index = int(COMBFILTER_MAXPERIOD - 2)
		}
		gain1 = gain1 * opus_val16(0.7)
		if st.Loss_rate > 2 {
			gain1 = gain1 * opus_val16(0.5)
		}
		if st.Loss_rate > 4 {
			gain1 = gain1 * opus_val16(0.5)
		}
		if st.Loss_rate > 8 {
			gain1 = 0
		}
	} else {
		gain1 = 0
		pitch_index = COMBFILTER_MINPERIOD
	}
	if analysis.Valid != 0 {
		gain1 = gain1 * opus_val16(analysis.Max_pitch_ratio)
	}
	pf_threshold = opus_val16(0.2)
	if cmath.Abs(int64(pitch_index-st.Prefilter_period))*10 > int64(pitch_index) {
		pf_threshold += opus_val16(0.2)
	}
	if nbAvailableBytes < 25 {
		pf_threshold += opus_val16(0.1)
	}
	if nbAvailableBytes < 35 {
		pf_threshold += opus_val16(0.1)
	}
	if st.Prefilter_gain > opus_val16(0.4) {
		pf_threshold -= opus_val16(0.1)
	}
	if st.Prefilter_gain > opus_val16(0.55) {
		pf_threshold -= opus_val16(0.1)
	}
	if pf_threshold > opus_val16(0.2) {
		pf_threshold = pf_threshold
	} else {
		pf_threshold = opus_val16(0.2)
	}
	if gain1 < pf_threshold {
		gain1 = 0
		pf_on = 0
		qg = 0
	} else {
		if (float32(math.Abs(float64(gain1 - st.Prefilter_gain)))) < 0.1 {
			gain1 = st.Prefilter_gain
		}
		qg = int(math.Floor(float64(float32(gain1)*32/3+0.5))) - 1
		if 0 > (func() int {
			if 7 < qg {
				return 7
			}
			return qg
		}()) {
			qg = 0
		} else if 7 < qg {
			qg = 7
		} else {
			qg = qg
		}
		gain1 = opus_val16(float64(qg+1) * 0.09375)
		pf_on = 1
	}
	c = 0
	for {
		{
			var offset int = mode.ShortMdctSize - overlap
			if st.Prefilter_period > COMBFILTER_MINPERIOD {
				st.Prefilter_period = st.Prefilter_period
			} else {
				st.Prefilter_period = COMBFILTER_MINPERIOD
			}
			libc.MemCpy(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Pointer(&st.In_mem[c*overlap]), overlap*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))))-uintptr(unsafe.Pointer(&st.In_mem[c*overlap]))))*0))
			if offset != 0 {
				comb_filter((*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(overlap))))), (*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD))))), st.Prefilter_period, st.Prefilter_period, offset, -st.Prefilter_gain, -st.Prefilter_gain, st.Prefilter_tapset, st.Prefilter_tapset, nil, 0, st.Arch)
			}
			comb_filter((*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(overlap)))), unsafe.Sizeof(celt_sig(0))*uintptr(offset))))), (*opus_val32)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))), unsafe.Sizeof(celt_sig(0))*uintptr(offset))))), st.Prefilter_period, pitch_index, N-offset, -st.Prefilter_gain, -gain1, st.Prefilter_tapset, prefilter_tapset, mode.Window, overlap, st.Arch)
			libc.MemCpy(unsafe.Pointer(&st.In_mem[c*overlap]), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), overlap*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer(&st.In_mem[c*overlap]))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0))
			if N > COMBFILTER_MAXPERIOD {
				libc.MemCpy(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), int(COMBFILTER_MAXPERIOD*unsafe.Sizeof(celt_sig(0))+uintptr((int64(uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0)))
			} else {
				libc.MemMove(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Sizeof(celt_sig(0))*uintptr(N)))), (COMBFILTER_MAXPERIOD-N)*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Sizeof(celt_sig(0))*uintptr(N)))))))*0))
				libc.MemCpy(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N))))), unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))), N*int(unsafe.Sizeof(celt_sig(0)))+int((int64(uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(prefilter_mem), unsafe.Sizeof(celt_sig(0))*uintptr(c*COMBFILTER_MAXPERIOD)))), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))), -int(unsafe.Sizeof(celt_sig(0))*uintptr(N))))))-uintptr(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(pre[c]), unsafe.Sizeof(celt_sig(0))*uintptr(COMBFILTER_MAXPERIOD)))))))*0))
			}
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	*gain = gain1
	*pitch = pitch_index
	*qgain = qg
	return pf_on
}
func compute_vbr(mode *OpusCustomMode, analysis *AnalysisInfo, base_target int32, LM int, bitrate int32, lastCodedBands int, C int, intensity int, constrained_vbr int, stereo_saving opus_val16, tot_boost int, tf_estimate opus_val16, pitch_change int, maxDepth opus_val16, lfe int, has_surround_mask int, surround_masking opus_val16, temporal_vbr opus_val16) int {
	var (
		target         int32
		coded_bins     int
		coded_bands    int
		tf_calibration opus_val16
		nbEBands       int
		eBands         *int16
	)
	nbEBands = mode.NbEBands
	eBands = mode.EBands
	if lastCodedBands != 0 {
		coded_bands = lastCodedBands
	} else {
		coded_bands = nbEBands
	}
	coded_bins = int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(coded_bands)))) << LM
	if C == 2 {
		coded_bins += int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(func() int {
			if intensity < coded_bands {
				return intensity
			}
			return coded_bands
		}())))) << LM
	}
	target = base_target
	if analysis.Valid != 0 && analysis.Activity < 0.4 {
		target -= int32(float32(coded_bins<<BITRES) * (0.4 - analysis.Activity))
	}
	if C == 2 {
		var (
			coded_stereo_bands int
			coded_stereo_dof   int
			max_frac           opus_val16
		)
		if intensity < coded_bands {
			coded_stereo_bands = intensity
		} else {
			coded_stereo_bands = coded_bands
		}
		coded_stereo_dof = (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(coded_stereo_bands)))) << LM) - coded_stereo_bands
		max_frac = opus_val16((opus_val32(coded_stereo_dof) * opus_val32(0.8)) / opus_val32(opus_val16(coded_bins)))
		if stereo_saving < opus_val16(1.0) {
			stereo_saving = stereo_saving
		} else {
			stereo_saving = opus_val16(1.0)
		}
		if (float32(max_frac) * float32(target)) < float32(opus_val32(stereo_saving-opus_val16(0.1))*opus_val32(coded_stereo_dof<<BITRES)) {
			target -= int32(float32(max_frac) * float32(target))
		} else {
			target -= int32(opus_val32(stereo_saving-opus_val16(0.1)) * opus_val32(coded_stereo_dof<<BITRES))
		}
	}
	target += int32(tot_boost - (19 << LM))
	tf_calibration = opus_val16(0.044)
	target += int32(float32(tf_estimate-tf_calibration) * float32(target))
	if analysis.Valid != 0 && lfe == 0 {
		var (
			tonal_target int32
			tonal        float32
		)
		tonal = (func() float32 {
			if 0.0 > (analysis.Tonality - 0.15) {
				return 0.0
			}
			return analysis.Tonality - 0.15
		}()) - 0.12
		tonal_target = int32(int(target) + int(int32(float64(coded_bins<<BITRES)*1.2*float64(tonal))))
		if pitch_change != 0 {
			tonal_target += int32(float64(coded_bins<<BITRES) * 0.8)
		}
		target = tonal_target
	}
	if has_surround_mask != 0 && lfe == 0 {
		var surround_target int32 = int32(int(target) + int(int32(opus_val32(surround_masking)*opus_val32(coded_bins<<BITRES))))
		if (int(target) / 4) > int(surround_target) {
			target = int32(int(target) / 4)
		} else {
			target = surround_target
		}
	}
	{
		var (
			floor_depth int32
			bins        int
		)
		bins = int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(nbEBands-2)))) << LM
		floor_depth = int32(opus_val32(C*bins<<BITRES) * opus_val32(maxDepth))
		if int(floor_depth) > (int(target) >> 2) {
			floor_depth = floor_depth
		} else {
			floor_depth = int32(int(target) >> 2)
		}
		if int(target) < int(floor_depth) {
			target = target
		} else {
			target = floor_depth
		}
	}
	if (has_surround_mask == 0 || lfe != 0) && constrained_vbr != 0 {
		target = int32(int(base_target) + int(int32(float64(int(target)-int(base_target))*0.67)))
	}
	if has_surround_mask == 0 && tf_estimate < opus_val16(0.2) {
		var (
			amount      opus_val16
			tvbr_factor opus_val16
		)
		amount = opus_val16(float64(func() int {
			if 0 > (func() int {
				if 32000 < (96000 - int(bitrate)) {
					return 32000
				}
				return 96000 - int(bitrate)
			}()) {
				return 0
			}
			if 32000 < (96000 - int(bitrate)) {
				return 32000
			}
			return 96000 - int(bitrate)
		}()) * 3.1e-06)
		tvbr_factor = opus_val16(opus_val32(temporal_vbr) * opus_val32(amount))
		target += int32(float32(tvbr_factor) * float32(target))
	}
	if (int(base_target) * 2) < int(target) {
		target = int32(int(base_target) * 2)
	} else {
		target = target
	}
	return int(target)
}
func celt_encode_with_ec(st *OpusCustomEncoder, pcm *opus_val16, frame_size int, compressed *uint8, nbCompressedBytes int, enc *ec_enc) int {
	var (
		i                      int
		c                      int
		N                      int
		bits                   int32
		_enc                   ec_enc
		in                     *celt_sig
		freq                   *celt_sig
		X                      *celt_norm
		bandE                  *celt_ener
		bandLogE               *opus_val16
		bandLogE2              *opus_val16
		fine_quant             *int
		error                  *opus_val16
		pulses                 *int
		cap_                   *int
		offsets                *int
		importance             *int
		spread_weight          *int
		fine_priority          *int
		tf_res                 *int
		collapse_masks         *uint8
		prefilter_mem          *celt_sig
		oldBandE               *opus_val16
		oldLogE                *opus_val16
		oldLogE2               *opus_val16
		energyError            *opus_val16
		shortBlocks            int = 0
		isTransient            int = 0
		CC                     int = st.Channels
		C                      int = st.Stream_channels
		LM                     int
		M                      int
		tf_select              int
		nbFilledBytes          int
		nbAvailableBytes       int
		start                  int
		end                    int
		effEnd                 int
		codedBands             int
		alloc_trim             int
		pitch_index            int        = COMBFILTER_MINPERIOD
		gain1                  opus_val16 = 0
		dual_stereo            int        = 0
		effectiveBytes         int
		dynalloc_logp          int
		vbr_rate               int32
		total_bits             int32
		total_boost            int32
		balance                int32
		tell                   int32
		tell0_frac             int32
		prefilter_tapset       int = 0
		pf_on                  int
		anti_collapse_rsv      int
		anti_collapse_on       int = 0
		silence                int = 0
		tf_chan                int = 0
		tf_estimate            opus_val16
		pitch_change           int = 0
		tot_boost              int32
		sample_max             opus_val32
		maxDepth               opus_val16
		mode                   *OpusCustomMode
		nbEBands               int
		overlap                int
		eBands                 *int16
		secondMdct             int
		signalBandwidth        int
		transient_got_disabled int        = 0
		surround_masking       opus_val16 = 0
		temporal_vbr           opus_val16 = 0
		surround_trim          opus_val16 = 0
		equiv_rate             int32
		hybrid                 int
		weak_transient         int = 0
		enable_tf_analysis     int
		surround_dynalloc      *opus_val16
	)
	mode = st.Mode
	nbEBands = mode.NbEBands
	overlap = mode.Overlap
	eBands = mode.EBands
	start = st.Start
	end = st.End
	hybrid = int(libc.BoolToInt(start != 0))
	tf_estimate = 0
	if nbCompressedBytes < 2 || pcm == nil {
		return -1
	}
	frame_size *= st.Upsample
	for LM = 0; LM <= mode.MaxLM; LM++ {
		if mode.ShortMdctSize<<LM == frame_size {
			break
		}
	}
	if LM > mode.MaxLM {
		return -1
	}
	M = 1 << LM
	N = M * mode.ShortMdctSize
	prefilter_mem = &st.In_mem[CC*overlap]
	oldBandE = (*opus_val16)(unsafe.Pointer(&st.In_mem[CC*(overlap+COMBFILTER_MAXPERIOD)]))
	oldLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(CC*nbEBands)))
	oldLogE2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(CC*nbEBands)))
	energyError = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(CC*nbEBands)))
	if enc == nil {
		tell0_frac = func() int32 {
			tell = 1
			return tell
		}()
		nbFilledBytes = 0
	} else {
		tell0_frac = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(enc))))
		tell = int32(ec_tell((*ec_ctx)(unsafe.Pointer(enc))))
		nbFilledBytes = (int(tell) + 4) >> 3
	}
	if nbCompressedBytes < 1275 {
		nbCompressedBytes = nbCompressedBytes
	} else {
		nbCompressedBytes = 1275
	}
	nbAvailableBytes = nbCompressedBytes - nbFilledBytes
	if st.Vbr != 0 && int(st.Bitrate) != -1 {
		var den int32 = int32(int(mode.Fs) >> BITRES)
		vbr_rate = int32((int(st.Bitrate)*frame_size + (int(den) >> 1)) / int(den))
		effectiveBytes = int(vbr_rate) >> (int(BITRES + 3))
	} else {
		var tmp int32
		vbr_rate = 0
		tmp = int32(int(st.Bitrate) * frame_size)
		if int(tell) > 1 {
			tmp += tell
		}
		if int(st.Bitrate) != -1 {
			if 2 > (func() int {
				if nbCompressedBytes < ((int(tmp)+int(mode.Fs)*4)/(int(mode.Fs)*8) - int(libc.BoolToInt(st.Signalling != 0))) {
					return nbCompressedBytes
				}
				return (int(tmp)+int(mode.Fs)*4)/(int(mode.Fs)*8) - int(libc.BoolToInt(st.Signalling != 0))
			}()) {
				nbCompressedBytes = 2
			} else if nbCompressedBytes < ((int(tmp)+int(mode.Fs)*4)/(int(mode.Fs)*8) - int(libc.BoolToInt(st.Signalling != 0))) {
				nbCompressedBytes = nbCompressedBytes
			} else {
				nbCompressedBytes = (int(tmp)+int(mode.Fs)*4)/(int(mode.Fs)*8) - int(libc.BoolToInt(st.Signalling != 0))
			}
		}
		effectiveBytes = nbCompressedBytes - nbFilledBytes
	}
	equiv_rate = int32((int(int32(nbCompressedBytes)) * 8 * 50 << (3 - LM)) - (C*40+20)*((400>>LM)-50))
	if int(st.Bitrate) != -1 {
		if int(equiv_rate) < (int(st.Bitrate) - (C*40+20)*((400>>LM)-50)) {
			equiv_rate = equiv_rate
		} else {
			equiv_rate = int32(int(st.Bitrate) - (C*40+20)*((400>>LM)-50))
		}
	}
	if enc == nil {
		ec_enc_init(&_enc, compressed, uint32(int32(nbCompressedBytes)))
		enc = &_enc
	}
	if int(vbr_rate) > 0 {
		if st.Constrained_vbr != 0 {
			var (
				vbr_bound   int32
				max_allowed int32
			)
			vbr_bound = vbr_rate
			if (func() int {
				if (func() int {
					if int(tell) == 1 {
						return 2
					}
					return 0
				}()) > ((int(vbr_rate) + int(vbr_bound) - int(st.Vbr_reservoir)) >> (int(BITRES + 3))) {
					if int(tell) == 1 {
						return 2
					}
					return 0
				}
				return (int(vbr_rate) + int(vbr_bound) - int(st.Vbr_reservoir)) >> (int(BITRES + 3))
			}()) < nbAvailableBytes {
				if (func() int {
					if int(tell) == 1 {
						return 2
					}
					return 0
				}()) > ((int(vbr_rate) + int(vbr_bound) - int(st.Vbr_reservoir)) >> (int(BITRES + 3))) {
					if int(tell) == 1 {
						max_allowed = 2
					} else {
						max_allowed = 0
					}
				} else {
					max_allowed = int32((int(vbr_rate) + int(vbr_bound) - int(st.Vbr_reservoir)) >> (int(BITRES + 3)))
				}
			} else {
				max_allowed = int32(nbAvailableBytes)
			}
			if int(max_allowed) < nbAvailableBytes {
				nbCompressedBytes = nbFilledBytes + int(max_allowed)
				nbAvailableBytes = int(max_allowed)
				ec_enc_shrink(enc, uint32(int32(nbCompressedBytes)))
			}
		}
	}
	total_bits = int32(nbCompressedBytes * 8)
	effEnd = end
	if effEnd > mode.EffEBands {
		effEnd = mode.EffEBands
	}
	in = (*celt_sig)(libc.Malloc((CC * (N + overlap)) * int(unsafe.Sizeof(celt_sig(0)))))
	if st.Overlap_max > celt_maxabs16(unsafe.Slice(pcm, C*(N-overlap)/st.Upsample), C*(N-overlap)/st.Upsample) {
		sample_max = st.Overlap_max
	} else {
		sample_max = celt_maxabs16(unsafe.Slice(pcm, C*(N-overlap)/st.Upsample), C*(N-overlap)/st.Upsample)
	}
	st.Overlap_max = celt_maxabs16(unsafe.Slice((*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(C*(N-overlap)/st.Upsample))), C*overlap/st.Upsample), C*overlap/st.Upsample)
	if sample_max > st.Overlap_max {
		sample_max = sample_max
	} else {
		sample_max = st.Overlap_max
	}
	silence = int(libc.BoolToInt(sample_max <= opus_val32(1.0/float32(int(1)<<st.Lsb_depth))))
	if int(tell) == 1 {
		ec_enc_bit_logp(enc, silence, 15)
	} else {
		silence = 0
	}
	if silence != 0 {
		if int(vbr_rate) > 0 {
			effectiveBytes = func() int {
				nbCompressedBytes = func() int {
					if nbCompressedBytes < (nbFilledBytes + 2) {
						return nbCompressedBytes
					}
					return nbFilledBytes + 2
				}()
				return nbCompressedBytes
			}()
			total_bits = int32(nbCompressedBytes * 8)
			nbAvailableBytes = 2
			ec_enc_shrink(enc, uint32(int32(nbCompressedBytes)))
		}
		tell = int32(nbCompressedBytes * 8)
		enc.Nbits_total += int(tell) - ec_tell((*ec_ctx)(unsafe.Pointer(enc)))
	}
	c = 0
	for {
		{
			var need_clip int = 0
			need_clip = int(libc.BoolToInt(st.Clip != 0 && sample_max > opus_val32(65536.0)))
			celt_preemphasis((*opus_val16)(unsafe.Add(unsafe.Pointer(pcm), unsafe.Sizeof(opus_val16(0))*uintptr(c))), (*celt_sig)(unsafe.Add(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(celt_sig(0))*uintptr(c*(N+overlap))))), unsafe.Sizeof(celt_sig(0))*uintptr(overlap))), N, CC, st.Upsample, &mode.Preemph[0], (*celt_sig)(unsafe.Pointer(&st.Preemph_memE[c])), need_clip)
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= CC {
			break
		}
	}
	{
		var (
			enabled int
			qg      int
		)
		enabled = int(libc.BoolToInt((st.Lfe != 0 && nbAvailableBytes > 3 || nbAvailableBytes > C*12) && hybrid == 0 && silence == 0 && st.Disable_pf == 0 && st.Complexity >= 5))
		prefilter_tapset = st.Tapset_decision
		pf_on = run_prefilter(st, in, prefilter_mem, CC, N, prefilter_tapset, &pitch_index, &gain1, &qg, enabled, nbAvailableBytes, &st.Analysis)
		if (gain1 > opus_val16(0.4) || st.Prefilter_gain > opus_val16(0.4)) && (st.Analysis.Valid == 0 || st.Analysis.Tonality > 0.3) && (float64(pitch_index) > float64(st.Prefilter_period)*1.26 || float64(pitch_index) < float64(st.Prefilter_period)*0.79) {
			pitch_change = 1
		}
		if pf_on == 0 {
			if hybrid == 0 && int(tell)+16 <= int(total_bits) {
				ec_enc_bit_logp(enc, 0, 1)
			}
		} else {
			var octave int
			ec_enc_bit_logp(enc, 1, 1)
			pitch_index += 1
			octave = ec_ilog(uint32(int32(pitch_index))) - 5
			ec_enc_uint(enc, uint32(int32(octave)), 6)
			ec_enc_bits(enc, uint32(int32(pitch_index-(16<<octave))), uint(octave+4))
			pitch_index -= 1
			ec_enc_bits(enc, uint32(int32(qg)), 3)
			ec_enc_icdf(enc, prefilter_tapset, tapset_icdf[:], 2)
		}
	}
	isTransient = 0
	shortBlocks = 0
	if st.Complexity >= 1 && st.Lfe == 0 {
		var allow_weak_transients int = int(libc.BoolToInt(hybrid != 0 && effectiveBytes < 15 && st.Silk_info.SignalType != 2))
		isTransient = transient_analysis((*opus_val32)(unsafe.Pointer(in)), N+overlap, CC, &tf_estimate, &tf_chan, allow_weak_transients, &weak_transient)
	}
	if LM > 0 && ec_tell((*ec_ctx)(unsafe.Pointer(enc)))+3 <= int(total_bits) {
		if isTransient != 0 {
			shortBlocks = M
		}
	} else {
		isTransient = 0
		transient_got_disabled = 1
	}
	freq = (*celt_sig)(libc.Malloc((CC * N) * int(unsafe.Sizeof(celt_sig(0)))))
	bandE = (*celt_ener)(libc.Malloc((nbEBands * CC) * int(unsafe.Sizeof(celt_ener(0)))))
	bandLogE = (*opus_val16)(libc.Malloc((nbEBands * CC) * int(unsafe.Sizeof(opus_val16(0)))))
	secondMdct = int(libc.BoolToInt(shortBlocks != 0 && st.Complexity >= 8))
	bandLogE2 = (*opus_val16)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(opus_val16(0)))))
	if secondMdct != 0 {
		compute_mdcts(mode, 0, in, freq, C, CC, LM, st.Upsample, st.Arch)
		compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.Arch)
		amp2Log2(mode, effEnd, end, bandE, bandLogE2, C)
		for c = 0; c < C; c++ {
			for i = 0; i < end; i++ {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i))) += opus_val16(float64(LM) * 0.5)
			}
		}
	}
	compute_mdcts(mode, shortBlocks, in, freq, C, CC, LM, st.Upsample, st.Arch)
	if CC == 2 && C == 1 {
		tf_chan = 0
	}
	compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.Arch)
	if st.Lfe != 0 {
		for i = 2; i < end; i++ {
			if (*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i)))) < ((*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*0))) * celt_ener(0.0001)) {
				*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))) = *(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i)))
			} else {
				*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))) = (*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*0))) * celt_ener(0.0001)
			}
			if (*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i)))) > EPSILON {
				*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))) = *(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i)))
			} else {
				*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))) = EPSILON
			}
		}
	}
	amp2Log2(mode, effEnd, end, bandE, bandLogE, C)
	surround_dynalloc = (*opus_val16)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(opus_val16(0)))))
	libc.MemSet(unsafe.Pointer(surround_dynalloc), 0, end*int(unsafe.Sizeof(opus_val16(0))))
	if hybrid == 0 && st.Energy_mask != nil && st.Lfe == 0 {
		var (
			mask_end       int
			midband        int
			count_dynalloc int
			mask_avg       opus_val32 = 0
			diff           opus_val32 = 0
			count          int        = 0
		)
		if 2 > st.LastCodedBands {
			mask_end = 2
		} else {
			mask_end = st.LastCodedBands
		}
		for c = 0; c < C; c++ {
			for i = 0; i < mask_end; i++ {
				var mask opus_val16
				if (func() opus_val16 {
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i)))) < opus_val16(0.25) {
						return *(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i)))
					}
					return opus_val16(0.25)
				}()) > (-2.0) {
					if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i)))) < opus_val16(0.25) {
						mask = *(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i)))
					} else {
						mask = opus_val16(0.25)
					}
				} else {
					mask = opus_val16(-2.0)
				}
				if float32(mask) > 0 {
					mask = mask * opus_val16(0.5)
				}
				mask_avg += opus_val32(mask) * opus_val32(int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))
				count += int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))
				diff += opus_val32(mask) * opus_val32(i*2+1-mask_end)
			}
		}
		mask_avg = mask_avg / opus_val32(opus_val16(count))
		mask_avg += opus_val32(0.2)
		diff = opus_val32(float32(diff) * 6 / float32(C*(mask_end-1)*(mask_end+1)*mask_end))
		diff = diff * opus_val32(0.5)
		if (func() opus_val32 {
			if diff < opus_val32(0.031) {
				return diff
			}
			return opus_val32(0.031)
		}()) > (-0.031) {
			if diff < opus_val32(0.031) {
				diff = diff
			} else {
				diff = opus_val32(0.031)
			}
		} else {
			diff = opus_val32(-0.031)
		}
		for midband = 0; int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(midband+1)))) < int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(mask_end))))/2; midband++ {
		}
		count_dynalloc = 0
		for i = 0; i < mask_end; i++ {
			var (
				lin    opus_val32
				unmask opus_val16
			)
			lin = mask_avg + opus_val32(float32(diff)*float32(i-midband))
			if C == 2 {
				if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))) {
					unmask = *(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				} else {
					unmask = *(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands+i)))
				}
			} else {
				unmask = *(*opus_val16)(unsafe.Add(unsafe.Pointer(st.Energy_mask), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
			if unmask < opus_val16(0.0) {
				unmask = unmask
			} else {
				unmask = opus_val16(0.0)
			}
			unmask -= opus_val16(lin)
			if unmask > opus_val16(0.25) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = unmask - opus_val16(0.25)
				count_dynalloc++
			}
		}
		if count_dynalloc >= 3 {
			mask_avg += opus_val32(0.25)
			if float32(mask_avg) > 0 {
				mask_avg = 0
				diff = 0
				libc.MemSet(unsafe.Pointer(surround_dynalloc), 0, mask_end*int(unsafe.Sizeof(opus_val16(0))))
			} else {
				for i = 0; i < mask_end; i++ {
					if 0 > float32(*(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i)))-opus_val16(0.25)) {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = 0
					} else {
						*(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(surround_dynalloc), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - opus_val16(0.25)
					}
				}
			}
		}
		mask_avg += opus_val32(0.2)
		surround_trim = opus_val16(float32(diff) * 64)
		surround_masking = opus_val16(mask_avg)
	}
	if st.Lfe == 0 {
		var (
			follow    opus_val16 = opus_val16(-10.0)
			frame_avg opus_val32 = 0
			offset    opus_val16
		)
		if shortBlocks != 0 {
			offset = opus_val16(float64(LM) * 0.5)
		} else {
			offset = 0
		}
		for i = start; i < end; i++ {
			if (follow - opus_val16(1.0)) > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - offset) {
				follow = follow - opus_val16(1.0)
			} else {
				follow = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) - offset
			}
			if C == 2 {
				if follow > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands))) - offset) {
					follow = follow
				} else {
					follow = *(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i+nbEBands))) - offset
				}
			}
			frame_avg += opus_val32(follow)
		}
		frame_avg /= opus_val32(end - start)
		temporal_vbr = opus_val16(frame_avg - opus_val32(st.Spec_avg))
		if opus_val16(3.0) < (func() opus_val16 {
			if (-1.5) > temporal_vbr {
				return opus_val16(-1.5)
			}
			return temporal_vbr
		}()) {
			temporal_vbr = opus_val16(3.0)
		} else if (-1.5) > temporal_vbr {
			temporal_vbr = opus_val16(-1.5)
		} else {
			temporal_vbr = temporal_vbr
		}
		st.Spec_avg += temporal_vbr * opus_val16(0.02)
	}
	if secondMdct == 0 {
		libc.MemCpy(unsafe.Pointer(bandLogE2), unsafe.Pointer(bandLogE), (C*nbEBands)*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(bandLogE2))-uintptr(unsafe.Pointer(bandLogE))))*0))
	}
	if LM > 0 && ec_tell((*ec_ctx)(unsafe.Pointer(enc)))+3 <= int(total_bits) && isTransient == 0 && st.Complexity >= 5 && st.Lfe == 0 && hybrid == 0 {
		if patch_transient_decision(bandLogE, oldBandE, nbEBands, start, end, C) != 0 {
			isTransient = 1
			shortBlocks = M
			compute_mdcts(mode, shortBlocks, in, freq, C, CC, LM, st.Upsample, st.Arch)
			compute_band_energies(mode, freq, bandE, effEnd, C, LM, st.Arch)
			amp2Log2(mode, effEnd, end, bandE, bandLogE, C)
			for c = 0; c < C; c++ {
				for i = 0; i < end; i++ {
					*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands*c+i))) += opus_val16(float64(LM) * 0.5)
				}
			}
			tf_estimate = opus_val16(0.2)
		}
	}
	if LM > 0 && ec_tell((*ec_ctx)(unsafe.Pointer(enc)))+3 <= int(total_bits) {
		ec_enc_bit_logp(enc, isTransient, 3)
	}
	X = (*celt_norm)(libc.Malloc((C * N) * int(unsafe.Sizeof(celt_norm(0)))))
	normalise_bands(mode, freq, X, bandE, effEnd, C, M)
	enable_tf_analysis = int(libc.BoolToInt(effectiveBytes >= C*15 && hybrid == 0 && st.Complexity >= 2 && st.Lfe == 0))
	offsets = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	importance = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	spread_weight = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	maxDepth = dynalloc_analysis(bandLogE, bandLogE2, nbEBands, start, end, C, offsets, st.Lsb_depth, mode.LogN, isTransient, st.Vbr, st.Constrained_vbr, eBands, LM, effectiveBytes, &tot_boost, st.Lfe, surround_dynalloc, &st.Analysis, importance, spread_weight)
	tf_res = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	if enable_tf_analysis != 0 {
		var lambda int
		if 80 > (20480/effectiveBytes + 2) {
			lambda = 80
		} else {
			lambda = 20480/effectiveBytes + 2
		}
		tf_select = tf_analysis(mode, effEnd, isTransient, tf_res, lambda, X, N, LM, tf_estimate, tf_chan, importance)
		for i = effEnd; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = *(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(effEnd-1)))
		}
	} else if hybrid != 0 && weak_transient != 0 {
		for i = 0; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = 1
		}
		tf_select = 0
	} else if hybrid != 0 && effectiveBytes < 15 && st.Silk_info.SignalType != 2 {
		for i = 0; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = 0
		}
		tf_select = isTransient
	} else {
		for i = 0; i < end; i++ {
			*(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i))) = isTransient
		}
		tf_select = 0
	}
	error = (*opus_val16)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(opus_val16(0)))))
	c = 0
	for {
		for i = start; i < end; i++ {
			if (float32(math.Abs(float64((*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))) - (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))))))) < 2.0 {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands))) -= (*(*opus_val16)(unsafe.Add(unsafe.Pointer(energyError), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))) * opus_val16(0.25)
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
	quant_coarse_energy(mode, start, end, effEnd, bandLogE, oldBandE, uint32(total_bits), error, enc, C, LM, nbAvailableBytes, st.Force_intra, &st.DelayedIntra, int(libc.BoolToInt(st.Complexity >= 4)), st.Loss_rate, st.Lfe)
	tf_encode(start, end, isTransient, tf_res, LM, tf_select, enc)
	if ec_tell((*ec_ctx)(unsafe.Pointer(enc)))+4 <= int(total_bits) {
		if st.Lfe != 0 {
			st.Tapset_decision = 0
			st.Spread_decision = 2
		} else if hybrid != 0 {
			if st.Complexity == 0 {
				st.Spread_decision = 0
			} else if isTransient != 0 {
				st.Spread_decision = 2
			} else {
				st.Spread_decision = 3
			}
		} else if shortBlocks != 0 || st.Complexity < 3 || nbAvailableBytes < C*10 {
			if st.Complexity == 0 {
				st.Spread_decision = 0
			} else {
				st.Spread_decision = 2
			}
		} else {
			st.Spread_decision = spreading_decision(mode, X, &st.Tonal_average, st.Spread_decision, &st.Hf_average, &st.Tapset_decision, int(libc.BoolToInt(pf_on != 0 && shortBlocks == 0)), effEnd, C, M, spread_weight)
		}
		ec_enc_icdf(enc, st.Spread_decision, spread_icdf[:], 5)
	}
	if st.Lfe != 0 {
		if 8 < (effectiveBytes / 3) {
			*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*0)) = 8
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*0)) = effectiveBytes / 3
		}
	}
	cap_ = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	init_caps(mode, cap_, LM, C)
	dynalloc_logp = 6
	total_bits <<= BITRES
	total_boost = 0
	tell = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(enc))))
	for i = start; i < end; i++ {
		var (
			width              int
			quanta             int
			dynalloc_loop_logp int
			boost              int
			j                  int
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
		for j = 0; int(tell)+(dynalloc_loop_logp<<BITRES) < int(total_bits)-int(total_boost) && boost < *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(i))); j++ {
			var flag int
			flag = int(libc.BoolToInt(j < *(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(i)))))
			ec_enc_bit_logp(enc, flag, uint(dynalloc_loop_logp))
			tell = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(enc))))
			if flag == 0 {
				break
			}
			boost += quanta
			total_boost += int32(quanta)
			dynalloc_loop_logp = 1
		}
		if j != 0 {
			if 2 > (dynalloc_logp - 1) {
				dynalloc_logp = 2
			} else {
				dynalloc_logp = dynalloc_logp - 1
			}
		}
		*(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(i))) = boost
	}
	if C == 2 {
		var (
			intensity_thresholds [21]opus_val16 = [21]opus_val16{1, 2, 3, 4, 5, 6, 7, 8, 16, 24, 36, 44, 50, 56, 62, 67, 72, 79, 88, 106, 134}
			intensity_histeresis [21]opus_val16 = [21]opus_val16{1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 4, 5, 6, 8, 8}
		)
		if LM != 0 {
			dual_stereo = stereo_analysis(mode, X, LM, N)
		}
		st.Intensity = hysteresis_decision(opus_val16(int(equiv_rate)/1000), &intensity_thresholds[0], &intensity_histeresis[0], 21, st.Intensity)
		if end < (func() int {
			if start > st.Intensity {
				return start
			}
			return st.Intensity
		}()) {
			st.Intensity = end
		} else if start > st.Intensity {
			st.Intensity = start
		} else {
			st.Intensity = st.Intensity
		}
	}
	alloc_trim = 5
	if int(tell)+(int(6<<BITRES)) <= int(total_bits)-int(total_boost) {
		if start > 0 || st.Lfe != 0 {
			st.Stereo_saving = 0
			alloc_trim = 5
		} else {
			alloc_trim = alloc_trim_analysis(mode, X, bandLogE, end, LM, C, N, &st.Analysis, &st.Stereo_saving, tf_estimate, st.Intensity, surround_trim, equiv_rate, st.Arch)
		}
		ec_enc_icdf(enc, alloc_trim, trim_icdf[:], 7)
		tell = int32(ec_tell_frac((*ec_ctx)(unsafe.Pointer(enc))))
	}
	if int(vbr_rate) > 0 {
		var (
			alpha       opus_val16
			delta       int32
			target      int32
			base_target int32
			min_allowed int32
			lm_diff     int = mode.MaxLM - LM
		)
		if nbCompressedBytes < (1275 >> (3 - LM)) {
			nbCompressedBytes = nbCompressedBytes
		} else {
			nbCompressedBytes = 1275 >> (3 - LM)
		}
		if hybrid == 0 {
			base_target = int32(int(vbr_rate) - ((C*40 + 20) << BITRES))
		} else {
			if 0 > (int(vbr_rate) - ((C*9 + 4) << BITRES)) {
				base_target = 0
			} else {
				base_target = int32(int(vbr_rate) - ((C*9 + 4) << BITRES))
			}
		}
		if st.Constrained_vbr != 0 {
			base_target += int32(int(st.Vbr_offset) >> lm_diff)
		}
		if hybrid == 0 {
			target = int32(compute_vbr(mode, &st.Analysis, base_target, LM, equiv_rate, st.LastCodedBands, C, st.Intensity, st.Constrained_vbr, st.Stereo_saving, int(tot_boost), tf_estimate, pitch_change, maxDepth, st.Lfe, int(libc.BoolToInt(st.Energy_mask != nil)), surround_masking, temporal_vbr))
		} else {
			target = base_target
			if st.Silk_info.Offset < 100 {
				target += int32(int(12<<BITRES) >> (3 - LM))
			}
			if st.Silk_info.Offset > 100 {
				target -= int32(int(18<<BITRES) >> (3 - LM))
			}
			target += int32(float32(tf_estimate-opus_val16(0.25)) * float32(int(50<<BITRES)))
			if tf_estimate > opus_val16(0.7) {
				if int(target) > (int(50 << BITRES)) {
					target = target
				} else {
					target = int32(int(50 << BITRES))
				}
			}
		}
		target = int32(int(target) + int(tell))
		min_allowed = int32(((int(tell) + int(total_boost) + (1 << (int(BITRES + 3))) - 1) >> (int(BITRES + 3))) + 2)
		if hybrid != 0 {
			if int(min_allowed) > ((int(tell0_frac) + (int(37 << BITRES)) + int(total_boost) + (1 << (int(BITRES + 3))) - 1) >> (int(BITRES + 3))) {
				min_allowed = min_allowed
			} else {
				min_allowed = int32((int(tell0_frac) + (int(37 << BITRES)) + int(total_boost) + (1 << (int(BITRES + 3))) - 1) >> (int(BITRES + 3)))
			}
		}
		nbAvailableBytes = (int(target) + (1 << (int(BITRES + 2)))) >> (int(BITRES + 3))
		if int(min_allowed) > nbAvailableBytes {
			nbAvailableBytes = int(min_allowed)
		} else {
			nbAvailableBytes = nbAvailableBytes
		}
		if nbCompressedBytes < nbAvailableBytes {
			nbAvailableBytes = nbCompressedBytes
		} else {
			nbAvailableBytes = nbAvailableBytes
		}
		delta = int32(int(target) - int(vbr_rate))
		target = int32(nbAvailableBytes << (int(BITRES + 3)))
		if silence != 0 {
			nbAvailableBytes = 2
			target = int32(int(2 * 8 << BITRES))
			delta = 0
		}
		if int(st.Vbr_count) < 970 {
			st.Vbr_count++
			alpha = opus_val16(1.0 / float64(int(st.Vbr_count)+20))
		} else {
			alpha = opus_val16(0.001)
		}
		if st.Constrained_vbr != 0 {
			st.Vbr_reservoir += int32(int(target) - int(vbr_rate))
		}
		if st.Constrained_vbr != 0 {
			st.Vbr_drift += int32(float32(alpha) * float32((int(delta)*(1<<lm_diff))-int(st.Vbr_offset)-int(st.Vbr_drift)))
			st.Vbr_offset = -st.Vbr_drift
		}
		if st.Constrained_vbr != 0 && int(st.Vbr_reservoir) < 0 {
			var adjust int = int(-st.Vbr_reservoir) / (int(8 << BITRES))
			if silence != 0 {
				nbAvailableBytes += 0
			} else {
				nbAvailableBytes += adjust
			}
			st.Vbr_reservoir = 0
		}
		if nbCompressedBytes < nbAvailableBytes {
			nbCompressedBytes = nbCompressedBytes
		} else {
			nbCompressedBytes = nbAvailableBytes
		}
		ec_enc_shrink(enc, uint32(int32(nbCompressedBytes)))
	}
	fine_quant = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	pulses = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	fine_priority = (*int)(libc.Malloc(nbEBands * int(unsafe.Sizeof(int(0)))))
	bits = int32(((int(int32(nbCompressedBytes)) * 8) << BITRES) - int(ec_tell_frac((*ec_ctx)(unsafe.Pointer(enc)))) - 1)
	if isTransient != 0 && LM >= 2 && int(bits) >= ((LM+2)<<BITRES) {
		anti_collapse_rsv = int(1 << BITRES)
	} else {
		anti_collapse_rsv = 0
	}
	bits -= int32(anti_collapse_rsv)
	signalBandwidth = end - 1
	if st.Analysis.Valid != 0 {
		var min_bandwidth int
		if int(equiv_rate) < C*32000 {
			min_bandwidth = 13
		} else if int(equiv_rate) < C*48000 {
			min_bandwidth = 16
		} else if int(equiv_rate) < C*60000 {
			min_bandwidth = 18
		} else if int(equiv_rate) < C*80000 {
			min_bandwidth = 19
		} else {
			min_bandwidth = 20
		}
		if st.Analysis.Bandwidth > min_bandwidth {
			signalBandwidth = st.Analysis.Bandwidth
		} else {
			signalBandwidth = min_bandwidth
		}
	}
	if st.Lfe != 0 {
		signalBandwidth = 1
	}
	codedBands = clt_compute_allocation(mode, start, end, offsets, cap_, alloc_trim, &st.Intensity, &dual_stereo, bits, &balance, pulses, fine_quant, fine_priority, C, LM, (*ec_ctx)(unsafe.Pointer(enc)), 1, st.LastCodedBands, signalBandwidth)
	if st.LastCodedBands != 0 {
		if (st.LastCodedBands + 1) < (func() int {
			if (st.LastCodedBands - 1) > codedBands {
				return st.LastCodedBands - 1
			}
			return codedBands
		}()) {
			st.LastCodedBands = st.LastCodedBands + 1
		} else if (st.LastCodedBands - 1) > codedBands {
			st.LastCodedBands = st.LastCodedBands - 1
		} else {
			st.LastCodedBands = codedBands
		}
	} else {
		st.LastCodedBands = codedBands
	}
	quant_fine_energy(mode, start, end, oldBandE, error, fine_quant, enc, C)
	collapse_masks = (*uint8)(libc.Malloc((C * nbEBands) * int(unsafe.Sizeof(uint8(0)))))
	quant_all_bands(1, mode, start, end, X, func() *celt_norm {
		if C == 2 {
			return (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N)))
		}
		return nil
	}(), collapse_masks, bandE, pulses, shortBlocks, st.Spread_decision, dual_stereo, st.Intensity, tf_res, int32(nbCompressedBytes*(int(8<<BITRES))-anti_collapse_rsv), balance, (*ec_ctx)(unsafe.Pointer(enc)), LM, codedBands, &st.Rng, st.Complexity, st.Arch, st.Disable_inv)
	if anti_collapse_rsv > 0 {
		anti_collapse_on = int(libc.BoolToInt(st.Consec_transient < 2))
		ec_enc_bits(enc, uint32(int32(anti_collapse_on)), 1)
	}
	quant_energy_finalise(mode, start, end, oldBandE, error, fine_quant, fine_priority, nbCompressedBytes*8-ec_tell((*ec_ctx)(unsafe.Pointer(enc))), enc, C)
	libc.MemSet(unsafe.Pointer(energyError), 0, (nbEBands*CC)*int(unsafe.Sizeof(opus_val16(0))))
	c = 0
	for {
		for i = start; i < end; i++ {
			if (-0.5) > (func() opus_val16 {
				if opus_val16(0.5) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(error), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))) {
					return opus_val16(0.5)
				}
				return *(*opus_val16)(unsafe.Add(unsafe.Pointer(error), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))
			}()) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(energyError), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands))) = opus_val16(-0.5)
			} else if opus_val16(0.5) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(error), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(energyError), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands))) = opus_val16(0.5)
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(energyError), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(error), unsafe.Sizeof(opus_val16(0))*uintptr(i+c*nbEBands)))
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
	if silence != 0 {
		for i = 0; i < C*nbEBands; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(-28.0)
		}
	}
	st.Prefilter_period = pitch_index
	st.Prefilter_gain = gain1
	st.Prefilter_tapset = prefilter_tapset
	if CC == 2 && C == 1 {
		libc.MemCpy(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands)))), unsafe.Pointer(oldBandE), nbEBands*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(nbEBands)))))-uintptr(unsafe.Pointer(oldBandE))))*0))
	}
	if isTransient == 0 {
		libc.MemCpy(unsafe.Pointer(oldLogE2), unsafe.Pointer(oldLogE), (CC*nbEBands)*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(oldLogE2))-uintptr(unsafe.Pointer(oldLogE))))*0))
		libc.MemCpy(unsafe.Pointer(oldLogE), unsafe.Pointer(oldBandE), (CC*nbEBands)*int(unsafe.Sizeof(opus_val16(0)))+int((int64(uintptr(unsafe.Pointer(oldLogE))-uintptr(unsafe.Pointer(oldBandE))))*0))
	} else {
		for i = 0; i < CC*nbEBands; i++ {
			if (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) < (*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			} else {
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = *(*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
			}
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
		}() >= CC {
			break
		}
	}
	if isTransient != 0 || transient_got_disabled != 0 {
		st.Consec_transient++
	} else {
		st.Consec_transient = 0
	}
	st.Rng = enc.Rng
	ec_enc_done(enc)
	if ec_get_error((*ec_ctx)(unsafe.Pointer(enc))) != 0 {
		return -3
	} else {
		return nbCompressedBytes
	}
}
func opus_custom_encoder_ctl(st *OpusCustomEncoder, request int, _rest ...interface{}) int {
	var ap libc.ArgList
	ap.Start(request, _rest)
	switch request {
	case OPUS_SET_COMPLEXITY_REQUEST:
		var value int = int(ap.Arg().(int32))
		if value < 0 || value > 10 {
			goto bad_arg
		}
		st.Complexity = value
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
	case CELT_SET_PREDICTION_REQUEST:
		var value int = int(ap.Arg().(int32))
		if value < 0 || value > 2 {
			goto bad_arg
		}
		st.Disable_pf = int(libc.BoolToInt(value <= 1))
		st.Force_intra = int(libc.BoolToInt(value == 0))
	case OPUS_SET_PACKET_LOSS_PERC_REQUEST:
		var value int = int(ap.Arg().(int32))
		if value < 0 || value > 100 {
			goto bad_arg
		}
		st.Loss_rate = value
	case OPUS_SET_VBR_CONSTRAINT_REQUEST:
		var value int32 = ap.Arg().(int32)
		st.Constrained_vbr = int(value)
	case OPUS_SET_VBR_REQUEST:
		var value int32 = ap.Arg().(int32)
		st.Vbr = int(value)
	case OPUS_SET_BITRATE_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) <= 500 && int(value) != -1 {
			goto bad_arg
		}
		if int(value) < (st.Channels * 260000) {
			value = value
		} else {
			value = int32(st.Channels * 260000)
		}
		st.Bitrate = value
	case CELT_SET_CHANNELS_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 1 || int(value) > 2 {
			goto bad_arg
		}
		st.Stream_channels = int(value)
	case OPUS_SET_LSB_DEPTH_REQUEST:
		var value int32 = ap.Arg().(int32)
		if int(value) < 8 || int(value) > 24 {
			goto bad_arg
		}
		st.Lsb_depth = int(value)
	case OPUS_GET_LSB_DEPTH_REQUEST:
		var value *int32 = ap.Arg().(*int32)
		*value = int32(st.Lsb_depth)
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
	case OPUS_RESET_STATE:
		var (
			i        int
			oldBandE *opus_val16
			oldLogE  *opus_val16
			oldLogE2 *opus_val16
		)
		oldBandE = (*opus_val16)(unsafe.Pointer(&st.In_mem[st.Channels*(st.Mode.Overlap+COMBFILTER_MAXPERIOD)]))
		oldLogE = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldBandE), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*st.Mode.NbEBands)))
		oldLogE2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(st.Channels*st.Mode.NbEBands)))
		libc.MemSet(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Rng))), 0, (opus_custom_encoder_get_size(st.Mode, st.Channels)-int(int64(uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(&st.Rng))))-uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(st)))))))*int(unsafe.Sizeof(byte(0))))
		for i = 0; i < st.Channels*st.Mode.NbEBands; i++ {
			*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = func() opus_val16 {
				p := (*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(i)))
				*(*opus_val16)(unsafe.Add(unsafe.Pointer(oldLogE2), unsafe.Sizeof(opus_val16(0))*uintptr(i))) = opus_val16(-28.0)
				return *p
			}()
		}
		st.Vbr_offset = 0
		st.DelayedIntra = 1
		st.Spread_decision = 2
		st.Tonal_average = 256
		st.Hf_average = 0
		st.Tapset_decision = 0
	case CELT_SET_SIGNALLING_REQUEST:
		var value int32 = ap.Arg().(int32)
		st.Signalling = int(value)
	case CELT_SET_ANALYSIS_REQUEST:
		var info *AnalysisInfo = ap.Arg().(*AnalysisInfo)
		if info != nil {
			libc.MemCpy(unsafe.Pointer(&st.Analysis), unsafe.Pointer(info), int((int64(uintptr(unsafe.Pointer(&st.Analysis))-uintptr(unsafe.Pointer(info))))*0+int64(1*unsafe.Sizeof(AnalysisInfo{}))))
		}
	case CELT_SET_SILK_INFO_REQUEST:
		var info *SILKInfo = ap.Arg().(*SILKInfo)
		if info != nil {
			libc.MemCpy(unsafe.Pointer(&st.Silk_info), unsafe.Pointer(info), int((int64(uintptr(unsafe.Pointer(&st.Silk_info))-uintptr(unsafe.Pointer(info))))*0+int64(1*unsafe.Sizeof(SILKInfo{}))))
		}
	case CELT_GET_MODE_REQUEST:
		var value **OpusCustomMode = ap.Arg().(**OpusCustomMode)
		if value == nil {
			goto bad_arg
		}
		*value = st.Mode
	case OPUS_GET_FINAL_RANGE_REQUEST:
		var value *uint32 = ap.Arg().(*uint32)
		if value == nil {
			goto bad_arg
		}
		*value = st.Rng
	case OPUS_SET_LFE_REQUEST:
		var value int32 = ap.Arg().(int32)
		st.Lfe = int(value)
	case OPUS_SET_ENERGY_MASK_REQUEST:
		var value *opus_val16 = ap.Arg().(*opus_val16)
		st.Energy_mask = value
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
