package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"unsafe"
)

const LEAK_BANDS = 19
const PACKAGE_VERSION = "unknown"
const CELT_SET_PREDICTION_REQUEST = 10002
const CELT_SET_INPUT_CLIPPING_REQUEST = 10004
const CELT_GET_AND_CLEAR_ERROR_REQUEST = 10007
const CELT_SET_CHANNELS_REQUEST = 10008
const CELT_SET_START_BAND_REQUEST = 10010
const CELT_SET_END_BAND_REQUEST = 10012
const CELT_GET_MODE_REQUEST = 10015
const CELT_SET_SIGNALLING_REQUEST = 10016
const CELT_SET_TONALITY_REQUEST = 10018
const CELT_SET_TONALITY_SLOPE_REQUEST = 10020
const CELT_SET_ANALYSIS_REQUEST = 10022
const OPUS_SET_LFE_REQUEST = 10024
const OPUS_SET_ENERGY_MASK_REQUEST = 10026
const CELT_SET_SILK_INFO_REQUEST = 10028
const COMBFILTER_MAXPERIOD = 1024
const COMBFILTER_MINPERIOD = 15

type AnalysisInfo struct {
	Valid                int
	Tonality             float32
	Tonality_slope       float32
	Noisiness            float32
	Activity             float32
	Music_prob           float32
	Music_prob_min       float32
	Music_prob_max       float32
	Bandwidth            int
	Activity_probability float32
	Max_pitch_ratio      float32
	Leak_boost           [19]uint8
}
type SILKInfo struct {
	SignalType int
	Offset     int
}

var trim_icdf [11]uint8 = [11]uint8{126, 124, 119, 109, 87, 41, 19, 9, 4, 2, 0}
var spread_icdf [4]uint8 = [4]uint8{25, 23, 2, 0}
var tapset_icdf [3]uint8 = [3]uint8{2, 1, 0}

func resampling_factor(rate int32) int {
	var ret int
	switch rate {
	case 48000:
		ret = 1
	case 24000:
		ret = 2
	case 16000:
		ret = 3
	case 12000:
		ret = 4
	case 8000:
		ret = 6
	default:
		ret = 0
	}
	return ret
}
func comb_filter_const_c(y *opus_val32, x *opus_val32, T int, N int, g10 opus_val16, g11 opus_val16, g12 opus_val16) {
	var (
		x0 opus_val32
		x1 opus_val32
		x2 opus_val32
		x3 opus_val32
		x4 opus_val32
		i  int
	)
	x4 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T-2)))
	x3 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T-1)))
	x2 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), -int(unsafe.Sizeof(opus_val32(0))*uintptr(T))))
	x1 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T+1)))
	for i = 0; i < N; i++ {
		x0 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T+2)))
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i))) + opus_val32(g10*opus_val16(x2)) + opus_val32(g11*opus_val16(x1+x3)) + opus_val32(g12*opus_val16(x0+x4))
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i)))
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
}
func comb_filter(y *opus_val32, x *opus_val32, T0 int, T1 int, N int, g0 opus_val16, g1 opus_val16, tapset0 int, tapset1 int, window *opus_val16, overlap int, arch int) {
	var (
		i     int
		g00   opus_val16
		g01   opus_val16
		g02   opus_val16
		g10   opus_val16
		g11   opus_val16
		g12   opus_val16
		x0    opus_val32
		x1    opus_val32
		x2    opus_val32
		x3    opus_val32
		x4    opus_val32
		gains [3][3]opus_val16 = [3][3]opus_val16{{opus_val16(0.306640625), opus_val16(0.2170410156), opus_val16(0.1296386719)}, {opus_val16(0.4638671875), opus_val16(0.2680664062), opus_val16(0.0)}, {opus_val16(0.7998046875), opus_val16(0.1000976562), opus_val16(0.0)}}
	)
	if float32(g0) == 0 && float32(g1) == 0 {
		if x != y {
			libc.MemMove(unsafe.Pointer(y), unsafe.Pointer(x), N*int(unsafe.Sizeof(opus_val32(0)))+int((int64(uintptr(unsafe.Pointer(y))-uintptr(unsafe.Pointer(x))))*0))
		}
		return
	}
	if T0 > COMBFILTER_MINPERIOD {
		T0 = T0
	} else {
		T0 = COMBFILTER_MINPERIOD
	}
	if T1 > COMBFILTER_MINPERIOD {
		T1 = T1
	} else {
		T1 = COMBFILTER_MINPERIOD
	}
	g00 = g0 * (gains[tapset0][0])
	g01 = g0 * (gains[tapset0][1])
	g02 = g0 * (gains[tapset0][2])
	g10 = g1 * (gains[tapset1][0])
	g11 = g1 * (gains[tapset1][1])
	g12 = g1 * (gains[tapset1][2])
	x1 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T1+1)))
	x2 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), -int(unsafe.Sizeof(opus_val32(0))*uintptr(T1))))
	x3 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T1-1)))
	x4 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(-T1-2)))
	if g0 == g1 && T0 == T1 && tapset0 == tapset1 {
		overlap = 0
	}
	for i = 0; i < overlap; i++ {
		var f opus_val16
		x0 = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T1+2)))
		f = (*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) * (*(*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(i))))
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i))) + opus_val32(((Q15ONE-f)*g00)*opus_val16(*(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T0))))) + opus_val32(((Q15ONE-f)*g01)*opus_val16((*(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T0+1))))+(*(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T0-1)))))) + opus_val32(((Q15ONE-f)*g02)*opus_val16((*(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T0+2))))+(*(*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i-T0-2)))))) + opus_val32((f*g10)*opus_val16(x2)) + opus_val32((f*g11)*opus_val16(x1+x3)) + opus_val32((f*g12)*opus_val16(x0+x4))
		*(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i))) = *(*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i)))
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
	if float32(g1) == 0 {
		if x != y {
			libc.MemMove(unsafe.Pointer((*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(overlap)))), unsafe.Pointer((*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(overlap)))), (N-overlap)*int(unsafe.Sizeof(opus_val32(0)))+int((int64(uintptr(unsafe.Pointer((*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(overlap)))))-uintptr(unsafe.Pointer((*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(overlap)))))))*0))
		}
		return
	}
	_ = arch
	comb_filter_const_c((*opus_val32)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(opus_val32(0))*uintptr(i))), (*opus_val32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(opus_val32(0))*uintptr(i))), T1, N-i, g10, g11, g12)
}

var tf_select_table [4][8]int8 = [4][8]int8{{0, -1, 0, -1, 0, -1, 0, -1}, {0, -1, 0, -2, 1, 0, 1, -1}, {0, -2, 0, -3, 2, 0, 1, -1}, {0, -2, 0, -3, 3, 0, 1, -1}}

func init_caps(m *OpusCustomMode, cap_ *int, LM int, C int) {
	var i int
	for i = 0; i < m.NbEBands; i++ {
		var N int
		N = (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))) << LM
		*(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(i))) = (int(*(*uint8)(unsafe.Add(unsafe.Pointer(m.Cache.Caps), m.NbEBands*(LM*2+C-1)+i))) + 64) * C * N >> 2
	}
}
func opus_strerror(error int) *byte {
	var error_strings [8]*byte = [8]*byte{libc.CString("success"), libc.CString("invalid argument"), libc.CString("buffer too small"), libc.CString("internal error"), libc.CString("corrupted stream"), libc.CString("request not implemented"), libc.CString("invalid state"), libc.CString("memory allocation failed")}
	if error > 0 || error < -7 {
		return libc.CString("unknown error")
	} else {
		return error_strings[-error]
	}
}
func opus_get_version_string() *byte {
	return libc.CString("libopus unknown")
}
