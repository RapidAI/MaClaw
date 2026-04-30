package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

const NB_FRAMES = 8
const NB_TBANDS = 18
const ANALYSIS_BUF_SIZE = 720
const ANALYSIS_COUNT_MAX = 10000
const DETECT_SIZE = 100
const TRANSITION_PENALTY = 10
const NB_TONAL_SKIP_BANDS = 9
const LEAKAGE_OFFSET = 2.5
const LEAKAGE_SLOPE = 2.0

type TonalityAnalysisState struct {
	Arch               int
	Application        int
	Fs                 int32
	Angle              [240]float32
	D_angle            [240]float32
	D2_angle           [240]float32
	Inmem              [720]opus_val32
	Mem_fill           int
	Prev_band_tonality [18]float32
	Prev_tonality      float32
	Prev_bandwidth     int
	E                  [8][18]float32
	LogE               [8][18]float32
	LowE               [18]float32
	HighE              [18]float32
	MeanE              [19]float32
	Mem                [32]float32
	Cmean              [8]float32
	Std                [9]float32
	Etracker           float32
	LowECount          float32
	E_count            int
	Count              int
	Analysis_offset    int
	Write_pos          int
	Read_pos           int
	Read_subframe      int
	Hp_ener_accum      float32
	Initialized        int
	Rnn_state          [32]float32
	Downmix_state      [3]opus_val32
	Info               [100]AnalysisInfo
}

var dct_table [128]float32 = [128]float32{0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.25, 0.351851, 0.33833, 0.311806, 0.2733, 0.224292, 0.166664, 0.102631, 0.034654, -0.034654, -0.102631, -0.166664, -0.224292, -0.2733, -0.311806, -0.33833, -0.351851, 0.34676, 0.293969, 0.196424, 0.068975, -0.068975, -0.196424, -0.293969, -0.34676, -0.34676, -0.293969, -0.196424, -0.068975, 0.068975, 0.196424, 0.293969, 0.34676, 0.33833, 0.224292, 0.034654, -0.166664, -0.311806, -0.351851, -0.2733, -0.102631, 0.102631, 0.2733, 0.351851, 0.311806, 0.166664, -0.034654, -0.224292, -0.33833, 0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641, 0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641, 0.311806, 0.034654, -0.2733, -0.33833, -0.102631, 0.224292, 0.351851, 0.166664, -0.166664, -0.351851, -0.224292, 0.102631, 0.33833, 0.2733, -0.034654, -0.311806, 0.293969, -0.068975, -0.34676, -0.196424, 0.196424, 0.34676, 0.068975, -0.293969, -0.293969, 0.068975, 0.34676, 0.196424, -0.196424, -0.34676, -0.068975, 0.293969, 0.2733, -0.166664, -0.33833, 0.034654, 0.351851, 0.102631, -0.311806, -0.224292, 0.224292, 0.311806, -0.102631, -0.351851, -0.034654, 0.33833, 0.166664, -0.2733}
var analysis_window [240]float32 = [240]float32{4.3e-05, 0.000171, 0.000385, 0.000685, 0.001071, 0.001541, 0.002098, 0.002739, 0.003466, 0.004278, 0.005174, 0.006156, 0.007222, 0.008373, 0.009607, 0.010926, 0.012329, 0.013815, 0.015385, 0.017037, 0.018772, 0.02059, 0.02249, 0.024472, 0.026535, 0.028679, 0.030904, 0.03321, 0.035595, 0.03806, 0.040604, 0.043227, 0.045928, 0.048707, 0.051564, 0.054497, 0.057506, 0.060591, 0.063752, 0.066987, 0.070297, 0.07368, 0.077136, 0.080665, 0.084265, 0.087937, 0.091679, 0.095492, 0.099373, 0.103323, 0.107342, 0.111427, 0.115579, 0.119797, 0.12408, 0.128428, 0.132839, 0.137313, 0.141849, 0.146447, 0.151105, 0.155823, 0.1606, 0.165435, 0.170327, 0.175276, 0.18028, 0.18534, 0.190453, 0.195619, 0.200838, 0.206107, 0.211427, 0.216797, 0.222215, 0.22768, 0.233193, 0.238751, 0.244353, 0.25, 0.255689, 0.261421, 0.267193, 0.273005, 0.278856, 0.284744, 0.29067, 0.296632, 0.302628, 0.308658, 0.314721, 0.320816, 0.326941, 0.333097, 0.33928, 0.345492, 0.351729, 0.357992, 0.36428, 0.37059, 0.376923, 0.383277, 0.389651, 0.396044, 0.402455, 0.408882, 0.415325, 0.421783, 0.428254, 0.434737, 0.441231, 0.447736, 0.454249, 0.46077, 0.467298, 0.473832, 0.48037, 0.486912, 0.493455, 0.5, 0.506545, 0.513088, 0.51963, 0.526168, 0.532702, 0.53923, 0.545751, 0.552264, 0.558769, 0.565263, 0.571746, 0.578217, 0.584675, 0.591118, 0.597545, 0.603956, 0.610349, 0.616723, 0.623077, 0.62941, 0.63572, 0.642008, 0.648271, 0.654508, 0.66072, 0.666903, 0.673059, 0.679184, 0.685279, 0.691342, 0.697372, 0.703368, 0.70933, 0.715256, 0.721144, 0.726995, 0.732807, 0.738579, 0.744311, 0.75, 0.755647, 0.761249, 0.766807, 0.77232, 0.777785, 0.783203, 0.788573, 0.793893, 0.799162, 0.804381, 0.809547, 0.81466, 0.81972, 0.824724, 0.829673, 0.834565, 0.8394, 0.844177, 0.848895, 0.853553, 0.858151, 0.862687, 0.867161, 0.871572, 0.87592, 0.880203, 0.884421, 0.888573, 0.892658, 0.896677, 0.900627, 0.904508, 0.908321, 0.912063, 0.915735, 0.919335, 0.922864, 0.92632, 0.929703, 0.933013, 0.936248, 0.939409, 0.942494, 0.945503, 0.948436, 0.951293, 0.954072, 0.956773, 0.959396, 0.96194, 0.964405, 0.96679, 0.969096, 0.971321, 0.973465, 0.975528, 0.97751, 0.97941, 0.981228, 0.982963, 0.984615, 0.986185, 0.987671, 0.989074, 0.990393, 0.991627, 0.992778, 0.993844, 0.994826, 0.995722, 0.996534, 0.997261, 0.997902, 0.998459, 0.998929, 0.999315, 0.999615, 0.999829, 0.999957, 1.0}
var tbands [19]int = [19]int{4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 136, 160, 192, 240}

func silk_resampler_down2_hp(S *opus_val32, out *opus_val32, in *opus_val32, inLen int) opus_val32 {
	var (
		k        int
		len2     int = inLen / 2
		in32     opus_val32
		out32    opus_val32
		out32_hp opus_val32
		Y        opus_val32
		X        opus_val32
		hp_ener  opus_val64 = 0
	)
	for k = 0; k < len2; k++ {
		in32 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(opus_val32(0))*uintptr(k*2)))
		Y = in32 - (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*0)))
		X = Y * opus_val32(0.6074371)
		out32 = (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*0))) + X
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*0)) = in32 + X
		out32_hp = out32
		in32 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(opus_val32(0))*uintptr(k*2+1)))
		Y = in32 - (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*1)))
		X = Y * opus_val32(0.15063)
		out32 = out32 + (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*1)))
		out32 = out32 + X
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*1)) = in32 + X
		Y = (-in32) - (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*2)))
		X = Y * opus_val32(0.15063)
		out32_hp = out32_hp + (*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*2)))
		out32_hp = out32_hp + X
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(S), unsafe.Sizeof(opus_val32(0))*2)) = (-in32) + X
		hp_ener += opus_val64(out32_hp * opus_val32(opus_val64(out32_hp)))
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(opus_val32(0))*uintptr(k))) = out32 * opus_val32(0.5)
	}
	return opus_val32(hp_ener)
}
func downmix_and_resample(downmix downmix_func, _x unsafe.Pointer, y *opus_val32, S [3]opus_val32, subframe int, offset int, c1 int, c2 int, C int, Fs int) opus_val32 {
	var (
		tmp   *opus_val32
		scale opus_val32
		j     int
		ret   opus_val32 = 0
	)
	if subframe == 0 {
		return 0
	}
	if Fs == 48000 {
		subframe *= 2
		offset *= 2
	} else if Fs == 16000 {
		subframe = subframe * 2 / 3
		offset = offset * 2 / 3
	}
	tmp = (*opus_val32)(libc.Malloc(subframe * int(unsafe.Sizeof(opus_val32(0)))))
	downmix(_x, tmp, subframe, offset, c1, c2, C)
	scale = 1.0 / 32768
	if c2 == -2 {
		scale /= opus_val32(C)
	} else if c2 > -1 {
		scale /= 2
	}
	for j = 0; j < subframe; j++ {
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val32(0))*uintptr(j))) *= scale
	}
	if Fs == 48000 {
		ret = silk_resampler_down2_hp(&S[0], y, tmp, subframe)
	} else if Fs == 24000 {
		libc.MemCpy(unsafe.Pointer(y), unsafe.Pointer(tmp), subframe*int(unsafe.Sizeof(opus_val32(0)))+int((int64(uintptr(unsafe.Pointer(y))-uintptr(unsafe.Pointer(tmp))))*0))
	} else if Fs == 16000 {
		var tmp3x *opus_val32
		tmp3x = (*opus_val32)(libc.Malloc((subframe * 3) * int(unsafe.Sizeof(opus_val32(0)))))
		for j = 0; j < subframe; j++ {
			*(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp3x), unsafe.Sizeof(opus_val32(0))*uintptr(j*3))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val32(0))*uintptr(j)))
			*(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp3x), unsafe.Sizeof(opus_val32(0))*uintptr(j*3+1))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val32(0))*uintptr(j)))
			*(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp3x), unsafe.Sizeof(opus_val32(0))*uintptr(j*3+2))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(opus_val32(0))*uintptr(j)))
		}
		silk_resampler_down2_hp(&S[0], y, tmp3x, subframe*3)
	}
	return ret
}
func tonality_analysis_init(tonal *TonalityAnalysisState, Fs int32) {
	tonal.Arch = opus_select_arch()
	tonal.Fs = Fs
	tonality_analysis_reset(tonal)
}
func tonality_analysis_reset(tonal *TonalityAnalysisState) {
	var start *byte = (*byte)(unsafe.Pointer(&tonal.Angle[0]))
	libc.MemSet(unsafe.Pointer(start), 0, int((unsafe.Sizeof(TonalityAnalysisState{})-uintptr(int64(uintptr(unsafe.Pointer(start))-uintptr(unsafe.Pointer((*byte)(unsafe.Pointer(tonal)))))))*unsafe.Sizeof(byte(0))))
}
func tonality_get_info(tonal *TonalityAnalysisState, info_out *AnalysisInfo, len_ int) {
	var (
		pos            int
		curr_lookahead int
		tonality_max   float32
		tonality_avg   float32
		tonality_count int
		i              int
		pos0           int
		prob_avg       float32
		prob_count     float32
		prob_min       float32
		prob_max       float32
		vad_prob       float32
		mpos           int
		vpos           int
		bandwidth_span int
	)
	pos = tonal.Read_pos
	curr_lookahead = tonal.Write_pos - tonal.Read_pos
	if curr_lookahead < 0 {
		curr_lookahead += DETECT_SIZE
	}
	tonal.Read_subframe += len_ / (int(tonal.Fs) / 400)
	for tonal.Read_subframe >= 8 {
		tonal.Read_subframe -= 8
		tonal.Read_pos++
	}
	if tonal.Read_pos >= DETECT_SIZE {
		tonal.Read_pos -= DETECT_SIZE
	}
	if len_ > int(tonal.Fs)/50 && pos != tonal.Write_pos {
		pos++
		if pos == DETECT_SIZE {
			pos = 0
		}
	}
	if pos == tonal.Write_pos {
		pos--
	}
	if pos < 0 {
		pos = int(DETECT_SIZE - 1)
	}
	pos0 = pos
	libc.MemCpy(unsafe.Pointer(info_out), unsafe.Pointer(&tonal.Info[pos]), int((int64(uintptr(unsafe.Pointer(info_out))-uintptr(unsafe.Pointer(&tonal.Info[pos]))))*0+int64(1*unsafe.Sizeof(AnalysisInfo{}))))
	if info_out.Valid == 0 {
		return
	}
	tonality_max = func() float32 {
		tonality_avg = info_out.Tonality
		return tonality_avg
	}()
	tonality_count = 1
	bandwidth_span = 6
	for i = 0; i < 3; i++ {
		pos++
		if pos == DETECT_SIZE {
			pos = 0
		}
		if pos == tonal.Write_pos {
			break
		}
		if tonality_max > tonal.Info[pos].Tonality {
			tonality_max = tonality_max
		} else {
			tonality_max = tonal.Info[pos].Tonality
		}
		tonality_avg += tonal.Info[pos].Tonality
		tonality_count++
		if info_out.Bandwidth > tonal.Info[pos].Bandwidth {
			info_out.Bandwidth = info_out.Bandwidth
		} else {
			info_out.Bandwidth = tonal.Info[pos].Bandwidth
		}
		bandwidth_span--
	}
	pos = pos0
	for i = 0; i < bandwidth_span; i++ {
		pos--
		if pos < 0 {
			pos = int(DETECT_SIZE - 1)
		}
		if pos == tonal.Write_pos {
			break
		}
		if info_out.Bandwidth > tonal.Info[pos].Bandwidth {
			info_out.Bandwidth = info_out.Bandwidth
		} else {
			info_out.Bandwidth = tonal.Info[pos].Bandwidth
		}
	}
	if (tonality_avg / float32(tonality_count)) > (tonality_max - 0.2) {
		info_out.Tonality = tonality_avg / float32(tonality_count)
	} else {
		info_out.Tonality = tonality_max - 0.2
	}
	mpos = func() int {
		vpos = pos0
		return vpos
	}()
	if curr_lookahead > 15 {
		mpos += 5
		if mpos >= DETECT_SIZE {
			mpos -= DETECT_SIZE
		}
		vpos += 1
		if vpos >= DETECT_SIZE {
			vpos -= DETECT_SIZE
		}
	}
	prob_min = 1.0
	prob_max = 0.0
	vad_prob = tonal.Info[vpos].Activity_probability
	if 0.1 > vad_prob {
		prob_count = 0.1
	} else {
		prob_count = vad_prob
	}
	prob_avg = (func() float32 {
		if 0.1 > vad_prob {
			return 0.1
		}
		return vad_prob
	}()) * tonal.Info[mpos].Music_prob
	for {
		var pos_vad float32
		mpos++
		if mpos == DETECT_SIZE {
			mpos = 0
		}
		if mpos == tonal.Write_pos {
			break
		}
		vpos++
		if vpos == DETECT_SIZE {
			vpos = 0
		}
		if vpos == tonal.Write_pos {
			break
		}
		pos_vad = tonal.Info[vpos].Activity_probability
		if ((prob_avg - TRANSITION_PENALTY*(vad_prob-pos_vad)) / prob_count) < prob_min {
			prob_min = (prob_avg - TRANSITION_PENALTY*(vad_prob-pos_vad)) / prob_count
		} else {
			prob_min = prob_min
		}
		if ((prob_avg + TRANSITION_PENALTY*(vad_prob-pos_vad)) / prob_count) > prob_max {
			prob_max = (prob_avg + TRANSITION_PENALTY*(vad_prob-pos_vad)) / prob_count
		} else {
			prob_max = prob_max
		}
		if 0.1 > pos_vad {
			prob_count += 0.1
		} else {
			prob_count += pos_vad
		}
		prob_avg += (func() float32 {
			if 0.1 > pos_vad {
				return 0.1
			}
			return pos_vad
		}()) * tonal.Info[mpos].Music_prob
	}
	info_out.Music_prob = prob_avg / prob_count
	if (prob_avg / prob_count) < prob_min {
		prob_min = prob_avg / prob_count
	} else {
		prob_min = prob_min
	}
	if (prob_avg / prob_count) > prob_max {
		prob_max = prob_avg / prob_count
	} else {
		prob_max = prob_max
	}
	if prob_min > 0.0 {
		prob_min = prob_min
	} else {
		prob_min = 0.0
	}
	if prob_max < 1.0 {
		prob_max = prob_max
	} else {
		prob_max = 1.0
	}
	if curr_lookahead < 10 {
		var (
			pmin float32
			pmax float32
		)
		pmin = prob_min
		pmax = prob_max
		pos = pos0
		for i = 0; i < (func() int {
			if (tonal.Count - 1) < 15 {
				return tonal.Count - 1
			}
			return 15
		}()); i++ {
			pos--
			if pos < 0 {
				pos = int(DETECT_SIZE - 1)
			}
			if pmin < tonal.Info[pos].Music_prob {
				pmin = pmin
			} else {
				pmin = tonal.Info[pos].Music_prob
			}
			if pmax > tonal.Info[pos].Music_prob {
				pmax = pmax
			} else {
				pmax = tonal.Info[pos].Music_prob
			}
		}
		if 0.0 > (pmin - vad_prob*0.1) {
			pmin = 0.0
		} else {
			pmin = pmin - vad_prob*0.1
		}
		if 1.0 < (pmax + vad_prob*0.1) {
			pmax = 1.0
		} else {
			pmax = pmax + vad_prob*0.1
		}
		prob_min += float32((1.0 - float64(curr_lookahead)*0.1) * float64(pmin-prob_min))
		prob_max += float32((1.0 - float64(curr_lookahead)*0.1) * float64(pmax-prob_max))
	}
	info_out.Music_prob_min = prob_min
	info_out.Music_prob_max = prob_max
}

var std_feature_bias [9]float32 = [9]float32{5.684947, 3.475288, 1.770634, 1.599784, 3.773215, 2.163313, 1.260756, 1.116868, 1.918795}

func tonality_analysis(tonal *TonalityAnalysisState, celt_mode *OpusCustomMode, x unsafe.Pointer, len_ int, offset int, c1 int, c2 int, C int, lsb_depth int, downmix downmix_func) {
	var (
		i                  int
		b                  int
		kfft               *kiss_fft_state
		in                 *kiss_fft_cpx
		out                *kiss_fft_cpx
		N                  int      = 480
		N2                 int      = 240
		A                  *float32 = &tonal.Angle[0]
		dA                 *float32 = &tonal.D_angle[0]
		d2A                *float32 = &tonal.D2_angle[0]
		tonality           *float32
		noisiness          *float32
		band_tonality      [18]float32
		logE               [18]float32
		BFCC               [8]float32
		features           [25]float32
		frame_tonality     float32
		max_frame_tonality float32
		frame_noisiness    float32
		pi4                float32 = float32(math.Pi * math.Pi * math.Pi * math.Pi)
		slope              float32 = 0
		frame_stationarity float32
		relativeE          float32
		frame_probs        [2]float32
		alpha              float32
		alphaE             float32
		alphaE2            float32
		frame_loudness     float32
		bandwidth_mask     float32
		is_masked          [19]int
		bandwidth          int     = 0
		maxE               float32 = 0
		noise_floor        float32
		remaining          int
		info               *AnalysisInfo
		hp_ener            float32
		tonality2          [240]float32
		midE               [8]float32
		spec_variability   float32 = 0
		band_log2          [19]float32
		leakage_from       [19]float32
		leakage_to         [19]float32
		layer_out          [32]float32
		below_max_pitch    float32
		above_max_pitch    float32
		is_silence         int
	)
	if tonal.Initialized == 0 {
		tonal.Mem_fill = 240
		tonal.Initialized = 1
	}
	alpha = float32(1.0 / float64(func() int {
		if 10 < (tonal.Count + 1) {
			return 10
		}
		return tonal.Count + 1
	}()))
	alphaE = float32(1.0 / float64(func() int {
		if 25 < (tonal.Count + 1) {
			return 25
		}
		return tonal.Count + 1
	}()))
	alphaE2 = float32(1.0 / float64(func() int {
		if 100 < (tonal.Count + 1) {
			return 100
		}
		return tonal.Count + 1
	}()))
	if tonal.Count <= 1 {
		alphaE2 = 1
	}
	if int(tonal.Fs) == 48000 {
		len_ /= 2
		offset /= 2
	} else if int(tonal.Fs) == 16000 {
		len_ = len_ * 3 / 2
		offset = offset * 3 / 2
	}
	kfft = celt_mode.Mdct.Kfft[0]
	tonal.Hp_ener_accum += float32(downmix_and_resample(downmix, x, &tonal.Inmem[tonal.Mem_fill], tonal.Downmix_state, func() int {
		if len_ < (ANALYSIS_BUF_SIZE - tonal.Mem_fill) {
			return len_
		}
		return ANALYSIS_BUF_SIZE - tonal.Mem_fill
	}(), offset, c1, c2, C, int(tonal.Fs)))
	if tonal.Mem_fill+len_ < ANALYSIS_BUF_SIZE {
		tonal.Mem_fill += len_
		return
	}
	hp_ener = tonal.Hp_ener_accum
	info = &tonal.Info[func() int {
		p := &tonal.Write_pos
		x := *p
		*p++
		return x
	}()]
	if tonal.Write_pos >= DETECT_SIZE {
		tonal.Write_pos -= DETECT_SIZE
	}
	is_silence = is_digital_silence((*[ANALYSIS_BUF_SIZE]opus_val16)(unsafe.Pointer(&tonal.Inmem))[:], ANALYSIS_BUF_SIZE, 1, lsb_depth)
	in = (*kiss_fft_cpx)(libc.Malloc(int(unsafe.Sizeof(kiss_fft_cpx{}) * 480)))
	out = (*kiss_fft_cpx)(libc.Malloc(int(unsafe.Sizeof(kiss_fft_cpx{}) * 480)))
	tonality = (*float32)(libc.Malloc(int(unsafe.Sizeof(float32(0)) * 240)))
	noisiness = (*float32)(libc.Malloc(int(unsafe.Sizeof(float32(0)) * 240)))
	for i = 0; i < N2; i++ {
		var w float32 = analysis_window[i]
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R = w * float32(tonal.Inmem[i])
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I = w * float32(tonal.Inmem[N2+i])
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i-1)))).R = w * float32(tonal.Inmem[N-i-1])
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i-1)))).I = w * float32(tonal.Inmem[N+N2-i-1])
	}
	libc.MemMove(unsafe.Pointer(&tonal.Inmem[0]), unsafe.Pointer(&tonal.Inmem[ANALYSIS_BUF_SIZE-240]), int((int64(uintptr(unsafe.Pointer(&tonal.Inmem[0]))-uintptr(unsafe.Pointer(&tonal.Inmem[ANALYSIS_BUF_SIZE-240]))))*0+int64(240*unsafe.Sizeof(opus_val32(0)))))
	remaining = len_ - (ANALYSIS_BUF_SIZE - tonal.Mem_fill)
	tonal.Hp_ener_accum = float32(downmix_and_resample(downmix, x, &tonal.Inmem[240], tonal.Downmix_state, remaining, offset+ANALYSIS_BUF_SIZE-tonal.Mem_fill, c1, c2, C, int(tonal.Fs)))
	tonal.Mem_fill = remaining + 240
	if is_silence != 0 {
		var prev_pos int = tonal.Write_pos - 2
		if prev_pos < 0 {
			prev_pos += DETECT_SIZE
		}
		libc.MemCpy(unsafe.Pointer(info), unsafe.Pointer(&tonal.Info[prev_pos]), int((int64(uintptr(unsafe.Pointer(info))-uintptr(unsafe.Pointer(&tonal.Info[prev_pos]))))*0+int64(1*unsafe.Sizeof(AnalysisInfo{}))))
		return
	}
	_ = tonal.Arch
	opus_fft_c(kfft, in, out)
	if (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*0))).R != (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*0))).R {
		info.Valid = 0
		return
	}
	for i = 1; i < N2; i++ {
		var (
			X1r       float32
			X2r       float32
			X1i       float32
			X2i       float32
			angle     float32
			d_angle   float32
			d2_angle  float32
			angle2    float32
			d_angle2  float32
			d2_angle2 float32
			mod1      float32
			mod2      float32
			avg_mod   float32
		)
		X1r = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R
		X1i = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I
		X2r = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I
		X2i = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R
		angle = float32(0.5/math.Pi) * fast_atan2f(X1i, X1r)
		d_angle = angle - *(*float32)(unsafe.Add(unsafe.Pointer(A), unsafe.Sizeof(float32(0))*uintptr(i)))
		d2_angle = d_angle - *(*float32)(unsafe.Add(unsafe.Pointer(dA), unsafe.Sizeof(float32(0))*uintptr(i)))
		angle2 = float32(0.5/math.Pi) * fast_atan2f(X2i, X2r)
		d_angle2 = angle2 - angle
		d2_angle2 = d_angle2 - d_angle
		mod1 = d2_angle - float32(int(math.Floor(float64(d2_angle+0.5))))
		*(*float32)(unsafe.Add(unsafe.Pointer(noisiness), unsafe.Sizeof(float32(0))*uintptr(i))) = float32(math.Abs(float64(mod1)))
		mod1 *= mod1
		mod1 *= mod1
		mod2 = d2_angle2 - float32(int(math.Floor(float64(d2_angle2+0.5))))
		*(*float32)(unsafe.Add(unsafe.Pointer(noisiness), unsafe.Sizeof(float32(0))*uintptr(i))) += float32(math.Abs(float64(mod2)))
		mod2 *= mod2
		mod2 *= mod2
		avg_mod = (*(*float32)(unsafe.Add(unsafe.Pointer(d2A), unsafe.Sizeof(float32(0))*uintptr(i))) + mod1 + mod2*2) * 0.25
		*(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i))) = 1.0/(pi4*(40.0*16.0)*avg_mod+1.0) - 0.015
		tonality2[i] = 1.0/(pi4*(40.0*16.0)*mod2+1.0) - 0.015
		*(*float32)(unsafe.Add(unsafe.Pointer(A), unsafe.Sizeof(float32(0))*uintptr(i))) = angle2
		*(*float32)(unsafe.Add(unsafe.Pointer(dA), unsafe.Sizeof(float32(0))*uintptr(i))) = d_angle2
		*(*float32)(unsafe.Add(unsafe.Pointer(d2A), unsafe.Sizeof(float32(0))*uintptr(i))) = mod2
	}
	for i = 2; i < N2-1; i++ {
		var tt float32 = (func() float32 {
			if (tonality2[i]) < (func() float32 {
				if (tonality2[i-1]) > (tonality2[i+1]) {
					return tonality2[i-1]
				}
				return tonality2[i+1]
			}()) {
				return tonality2[i]
			}
			if (tonality2[i-1]) > (tonality2[i+1]) {
				return tonality2[i-1]
			}
			return tonality2[i+1]
		}())
		*(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i))) = (func() float32 {
			if (*(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i)))) > (tt - 0.1) {
				return *(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i)))
			}
			return tt - 0.1
		}()) * 0.9
	}
	frame_tonality = 0
	max_frame_tonality = 0
	info.Activity = 0
	frame_noisiness = 0
	frame_stationarity = 0
	if tonal.Count == 0 {
		for b = 0; b < NB_TBANDS; b++ {
			tonal.LowE[b] = 1e+10
			tonal.HighE[b] = -1e+10
		}
	}
	relativeE = 0
	frame_loudness = 0
	{
		var (
			E   float32 = 0
			X1r float32
			X2r float32
		)
		X1r = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*0))).R * 2
		X2r = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*0))).I * 2
		E = X1r*X1r + X2r*X2r
		for i = 1; i < 4; i++ {
			var binE float32 = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I
			E += binE
		}
		E = E
		band_log2[0] = float32(math.Log(float64(E+1e-10))) * (0.5 * 1.442695)
	}
	for b = 0; b < NB_TBANDS; b++ {
		var (
			E            float32 = 0
			tE           float32 = 0
			nE           float32 = 0
			L1           float32
			L2           float32
			stationarity float32
		)
		for i = tbands[b]; i < tbands[b+1]; i++ {
			var binE float32 = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I
			binE = binE
			E += binE
			tE += binE * (func() float32 {
				if 0 > (*(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i)))) {
					return 0
				}
				return *(*float32)(unsafe.Add(unsafe.Pointer(tonality), unsafe.Sizeof(float32(0))*uintptr(i)))
			}())
			nE += binE * 2.0 * (0.5 - *(*float32)(unsafe.Add(unsafe.Pointer(noisiness), unsafe.Sizeof(float32(0))*uintptr(i))))
		}
		if E >= 1e+09 || E != E {
			info.Valid = 0
			return
		}
		tonal.E[tonal.E_count][b] = E
		frame_noisiness += nE / (E + 1e-15)
		frame_loudness += float32(math.Sqrt(float64(E + 1e-10)))
		logE[b] = float32(math.Log(float64(E + 1e-10)))
		band_log2[b+1] = float32(math.Log(float64(E+1e-10))) * (0.5 * 1.442695)
		tonal.LogE[tonal.E_count][b] = logE[b]
		if tonal.Count == 0 {
			tonal.HighE[b] = func() float32 {
				p := &tonal.LowE[b]
				tonal.LowE[b] = logE[b]
				return *p
			}()
		}
		if tonal.HighE[b] > tonal.LowE[b]+7.5 {
			if tonal.HighE[b]-logE[b] > logE[b]-tonal.LowE[b] {
				tonal.HighE[b] -= 0.01
			} else {
				tonal.LowE[b] += 0.01
			}
		}
		if logE[b] > tonal.HighE[b] {
			tonal.HighE[b] = logE[b]
			if (tonal.HighE[b] - 15) > (tonal.LowE[b]) {
				tonal.LowE[b] = tonal.HighE[b] - 15
			} else {
				tonal.LowE[b] = tonal.LowE[b]
			}
		} else if logE[b] < tonal.LowE[b] {
			tonal.LowE[b] = logE[b]
			if (tonal.LowE[b] + 15) < (tonal.HighE[b]) {
				tonal.HighE[b] = tonal.LowE[b] + 15
			} else {
				tonal.HighE[b] = tonal.HighE[b]
			}
		}
		relativeE += (logE[b] - tonal.LowE[b]) / ((tonal.HighE[b] - tonal.LowE[b]) + 1e-05)
		L1 = func() float32 {
			L2 = 0
			return L2
		}()
		for i = 0; i < NB_FRAMES; i++ {
			L1 += float32(math.Sqrt(float64(tonal.E[i][b])))
			L2 += tonal.E[i][b]
		}
		if 0.99 < (L1 / float32(math.Sqrt(float64(NB_FRAMES*L2+1e-15)))) {
			stationarity = 0.99
		} else {
			stationarity = L1 / float32(math.Sqrt(float64(NB_FRAMES*L2+1e-15)))
		}
		stationarity *= stationarity
		stationarity *= stationarity
		frame_stationarity += stationarity
		if (tE / (E + 1e-15)) > (stationarity * tonal.Prev_band_tonality[b]) {
			band_tonality[b] = tE / (E + 1e-15)
		} else {
			band_tonality[b] = stationarity * tonal.Prev_band_tonality[b]
		}
		frame_tonality += band_tonality[b]
		if b >= int(NB_TBANDS-NB_TONAL_SKIP_BANDS) {
			frame_tonality -= band_tonality[b-NB_TBANDS+NB_TONAL_SKIP_BANDS]
		}
		if float64(max_frame_tonality) > ((float64(b-NB_TBANDS)*0.03 + 1.0) * float64(frame_tonality)) {
			max_frame_tonality = max_frame_tonality
		} else {
			max_frame_tonality = float32((float64(b-NB_TBANDS)*0.03 + 1.0) * float64(frame_tonality))
		}
		slope += band_tonality[b] * float32(b-8)
		tonal.Prev_band_tonality[b] = band_tonality[b]
	}
	leakage_from[0] = band_log2[0]
	leakage_to[0] = band_log2[0] - LEAKAGE_OFFSET
	for b = 1; b < int(NB_TBANDS+1); b++ {
		var leak_slope float32 = float32(LEAKAGE_SLOPE * float64(tbands[b]-tbands[b-1]) / 4)
		if (leakage_from[b-1] + leak_slope) < (band_log2[b]) {
			leakage_from[b] = leakage_from[b-1] + leak_slope
		} else {
			leakage_from[b] = band_log2[b]
		}
		if (leakage_to[b-1] - leak_slope) > (band_log2[b] - LEAKAGE_OFFSET) {
			leakage_to[b] = leakage_to[b-1] - leak_slope
		} else {
			leakage_to[b] = band_log2[b] - LEAKAGE_OFFSET
		}
	}
	for b = int(NB_TBANDS - 2); b >= 0; b-- {
		var leak_slope float32 = float32(LEAKAGE_SLOPE * float64(tbands[b+1]-tbands[b]) / 4)
		if (leakage_from[b+1] + leak_slope) < (leakage_from[b]) {
			leakage_from[b] = leakage_from[b+1] + leak_slope
		} else {
			leakage_from[b] = leakage_from[b]
		}
		if (leakage_to[b+1] - leak_slope) > (leakage_to[b]) {
			leakage_to[b] = leakage_to[b+1] - leak_slope
		} else {
			leakage_to[b] = leakage_to[b]
		}
	}
	for b = 0; b < int(NB_TBANDS+1); b++ {
		var boost float32 = (func() float32 {
			if 0 > (leakage_to[b] - band_log2[b]) {
				return 0
			}
			return leakage_to[b] - band_log2[b]
		}()) + (func() float32 {
			if 0 > (band_log2[b] - (leakage_from[b] + LEAKAGE_OFFSET)) {
				return 0
			}
			return band_log2[b] - (leakage_from[b] + LEAKAGE_OFFSET)
		}())
		if math.MaxUint8 < (int(math.Floor(float64(boost*64.0 + 0.5)))) {
			info.Leak_boost[b] = math.MaxUint8
		} else {
			info.Leak_boost[b] = uint8(int8(int(math.Floor(float64(boost*64.0 + 0.5)))))
		}
	}
	for ; b < LEAK_BANDS; b++ {
		info.Leak_boost[b] = 0
	}
	for i = 0; i < NB_FRAMES; i++ {
		var (
			j       int
			mindist float32 = 1e+15
		)
		for j = 0; j < NB_FRAMES; j++ {
			var (
				k    int
				dist float32 = 0
			)
			for k = 0; k < NB_TBANDS; k++ {
				var tmp float32
				tmp = tonal.LogE[i][k] - tonal.LogE[j][k]
				dist += tmp * tmp
			}
			if j != i {
				if mindist < dist {
					mindist = mindist
				} else {
					mindist = dist
				}
			}
		}
		spec_variability += mindist
	}
	spec_variability = float32(math.Sqrt(float64(spec_variability / NB_FRAMES / NB_TBANDS)))
	bandwidth_mask = 0
	bandwidth = 0
	maxE = 0
	noise_floor = float32(0.00057 / float64(int(1)<<(func() int {
		if 0 > (lsb_depth - 8) {
			return 0
		}
		return lsb_depth - 8
	}())))
	noise_floor *= noise_floor
	below_max_pitch = 0
	above_max_pitch = 0
	for b = 0; b < NB_TBANDS; b++ {
		var (
			E          float32 = 0
			Em         float32
			band_start int
			band_end   int
		)
		band_start = tbands[b]
		band_end = tbands[b+1]
		for i = band_start; i < band_end; i++ {
			var binE float32 = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I*(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(N-i)))).I
			E += binE
		}
		E = E
		if maxE > E {
			maxE = maxE
		} else {
			maxE = E
		}
		if band_start < 64 {
			below_max_pitch += E
		} else {
			above_max_pitch += E
		}
		if ((1 - alphaE2) * tonal.MeanE[b]) > E {
			tonal.MeanE[b] = (1 - alphaE2) * tonal.MeanE[b]
		} else {
			tonal.MeanE[b] = E
		}
		if E > (tonal.MeanE[b]) {
			Em = E
		} else {
			Em = tonal.MeanE[b]
		}
		if E*1e+09 > maxE && (Em > noise_floor*3*float32(band_end-band_start) || E > noise_floor*float32(band_end-band_start)) {
			bandwidth = b + 1
		}
		is_masked[b] = int(libc.BoolToInt(E < float32(func() float64 {
			if tonal.Prev_bandwidth >= b+1 {
				return 0.01
			}
			return 0.05
		}())*bandwidth_mask))
		if (bandwidth_mask * 0.05) > E {
			bandwidth_mask = bandwidth_mask * 0.05
		} else {
			bandwidth_mask = E
		}
	}
	if int(tonal.Fs) == 48000 {
		var (
			noise_ratio float32
			Em          float32
			E           float32 = hp_ener * (1.0 / (60 * 60))
		)
		if tonal.Prev_bandwidth == 20 {
			noise_ratio = 10.0
		} else {
			noise_ratio = 30.0
		}
		above_max_pitch += E
		if ((1 - alphaE2) * tonal.MeanE[b]) > E {
			tonal.MeanE[b] = (1 - alphaE2) * tonal.MeanE[b]
		} else {
			tonal.MeanE[b] = E
		}
		if E > (tonal.MeanE[b]) {
			Em = E
		} else {
			Em = tonal.MeanE[b]
		}
		if Em > noise_ratio*3*noise_floor*160 || E > noise_ratio*noise_floor*160 {
			bandwidth = 20
		}
		is_masked[b] = int(libc.BoolToInt(E < float32(func() float64 {
			if tonal.Prev_bandwidth == 20 {
				return 0.01
			}
			return 0.05
		}())*bandwidth_mask))
	}
	if above_max_pitch > below_max_pitch {
		info.Max_pitch_ratio = below_max_pitch / above_max_pitch
	} else {
		info.Max_pitch_ratio = 1
	}
	if bandwidth == 20 && is_masked[NB_TBANDS] != 0 {
		bandwidth -= 2
	} else if bandwidth > 0 && bandwidth <= NB_TBANDS && is_masked[bandwidth-1] != 0 {
		bandwidth--
	}
	if tonal.Count <= 2 {
		bandwidth = 20
	}
	frame_loudness = float32(math.Log10(float64(frame_loudness))) * 20
	if (tonal.Etracker - 0.003) > frame_loudness {
		tonal.Etracker = tonal.Etracker - 0.003
	} else {
		tonal.Etracker = frame_loudness
	}
	tonal.LowECount *= 1 - alphaE
	if frame_loudness < tonal.Etracker-30 {
		tonal.LowECount += alphaE
	}
	for i = 0; i < 8; i++ {
		var sum float32 = 0
		for b = 0; b < 16; b++ {
			sum += dct_table[i*16+b] * logE[b]
		}
		BFCC[i] = sum
	}
	for i = 0; i < 8; i++ {
		var sum float32 = 0
		for b = 0; b < 16; b++ {
			sum += dct_table[i*16+b] * 0.5 * (tonal.HighE[b] + tonal.LowE[b])
		}
		midE[i] = sum
	}
	frame_stationarity /= NB_TBANDS
	relativeE /= NB_TBANDS
	if tonal.Count < 10 {
		relativeE = 0.5
	}
	frame_noisiness /= NB_TBANDS
	info.Activity = frame_noisiness + (1-frame_noisiness)*relativeE
	frame_tonality = max_frame_tonality / float32(int(NB_TBANDS-NB_TONAL_SKIP_BANDS))
	if frame_tonality > (tonal.Prev_tonality * 0.8) {
		frame_tonality = frame_tonality
	} else {
		frame_tonality = tonal.Prev_tonality * 0.8
	}
	tonal.Prev_tonality = frame_tonality
	slope /= 8 * 8
	info.Tonality_slope = slope
	tonal.E_count = (tonal.E_count + 1) % NB_FRAMES
	if (tonal.Count + 1) < ANALYSIS_COUNT_MAX {
		tonal.Count = tonal.Count + 1
	} else {
		tonal.Count = ANALYSIS_COUNT_MAX
	}
	info.Tonality = frame_tonality
	for i = 0; i < 4; i++ {
		features[i] = (BFCC[i]+tonal.Mem[i+24])*(-0.12299) + (tonal.Mem[i]+tonal.Mem[i+16])*0.49195 + tonal.Mem[i+8]*0.69693 - tonal.Cmean[i]*1.4349
	}
	for i = 0; i < 4; i++ {
		tonal.Cmean[i] = (1-alpha)*tonal.Cmean[i] + alpha*BFCC[i]
	}
	for i = 0; i < 4; i++ {
		features[i+4] = (BFCC[i]-tonal.Mem[i+24])*0.63246 + (tonal.Mem[i]-tonal.Mem[i+16])*0.31623
	}
	for i = 0; i < 3; i++ {
		features[i+8] = (BFCC[i]+tonal.Mem[i+24])*0.53452 - (tonal.Mem[i]+tonal.Mem[i+16])*0.26726 - tonal.Mem[i+8]*0.53452
	}
	if tonal.Count > 5 {
		for i = 0; i < 9; i++ {
			tonal.Std[i] = (1-alpha)*tonal.Std[i] + alpha*features[i]*features[i]
		}
	}
	for i = 0; i < 4; i++ {
		features[i] = BFCC[i] - midE[i]
	}
	for i = 0; i < 8; i++ {
		tonal.Mem[i+24] = tonal.Mem[i+16]
		tonal.Mem[i+16] = tonal.Mem[i+8]
		tonal.Mem[i+8] = tonal.Mem[i]
		tonal.Mem[i] = BFCC[i]
	}
	for i = 0; i < 9; i++ {
		features[i+11] = float32(math.Sqrt(float64(tonal.Std[i]))) - std_feature_bias[i]
	}
	features[18] = spec_variability - 0.78
	features[20] = info.Tonality - 0.154723
	features[21] = info.Activity - 0.724643
	features[22] = frame_stationarity - 0.743717
	features[23] = info.Tonality_slope + 0.069216
	features[24] = tonal.LowECount - 0.06793
	compute_dense(&layer0, layer_out[:], features[:])
	compute_gru(&layer1, tonal.Rnn_state[:], layer_out[:])
	compute_dense(&layer2, frame_probs[:], tonal.Rnn_state[:])
	info.Activity_probability = frame_probs[1]
	info.Music_prob = frame_probs[0]
	info.Bandwidth = bandwidth
	tonal.Prev_bandwidth = bandwidth
	info.Noisiness = frame_noisiness
	info.Valid = 1
}
func run_analysis(analysis *TonalityAnalysisState, celt_mode *OpusCustomMode, analysis_pcm unsafe.Pointer, analysis_frame_size int, frame_size int, c1 int, c2 int, C int, Fs int32, lsb_depth int, downmix downmix_func, analysis_info *AnalysisInfo) {
	var (
		offset  int
		pcm_len int
	)
	analysis_frame_size -= analysis_frame_size & 1
	if analysis_pcm != nil {
		if ((int(DETECT_SIZE - 5)) * int(Fs) / 50) < analysis_frame_size {
			analysis_frame_size = (int(DETECT_SIZE - 5)) * int(Fs) / 50
		} else {
			analysis_frame_size = analysis_frame_size
		}
		pcm_len = analysis_frame_size - analysis.Analysis_offset
		offset = analysis.Analysis_offset
		for pcm_len > 0 {
			tonality_analysis(analysis, celt_mode, analysis_pcm, func() int {
				if (int(Fs) / 50) < pcm_len {
					return int(Fs) / 50
				}
				return pcm_len
			}(), offset, c1, c2, C, lsb_depth, downmix)
			offset += int(Fs) / 50
			pcm_len -= int(Fs) / 50
		}
		analysis.Analysis_offset = analysis_frame_size
		analysis.Analysis_offset -= frame_size
	}
	tonality_get_info(analysis, analysis_info, frame_size)
}
