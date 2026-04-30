package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func silk_LPC_analysis_filter16_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int) {
	for ix := 16; ix < length; ix++ {
		i := ix - 1
		LPC_pred := s[i+0]*PredCoef[0] + s[i-1]*PredCoef[1] + s[i-2]*PredCoef[2] + s[i-3]*PredCoef[3] + s[i-4]*PredCoef[4] + s[i-5]*PredCoef[5] + s[i-6]*PredCoef[6] + s[i-7]*PredCoef[7] + s[i-8]*PredCoef[8] + s[i-9]*PredCoef[9] + s[i-10]*PredCoef[10] + s[i-11]*PredCoef[11] + s[i-12]*PredCoef[12] + s[i-13]*PredCoef[13] + s[i-14]*PredCoef[14] + s[i-15]*PredCoef[15]
		r_LPC[ix] = s[i+1] - LPC_pred
	}
}
func silk_LPC_analysis_filter12_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int) {
	for ix := 12; ix < length; ix++ {
		i := ix - 1
		LPC_pred := s[i+0]*PredCoef[0] + s[i-1]*PredCoef[1] + s[i-2]*PredCoef[2] + s[i-3]*PredCoef[3] + s[i-4]*PredCoef[4] + s[i-5]*PredCoef[5] + s[i-6]*PredCoef[6] + s[i-7]*PredCoef[7] + s[i-8]*PredCoef[8] + s[i-9]*PredCoef[9] + s[i-10]*PredCoef[10] + s[i-11]*PredCoef[11]
		r_LPC[ix] = s[i+1] - LPC_pred
	}
}
func silk_LPC_analysis_filter10_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int) {
	for ix := 10; ix < length; ix++ {
		i := ix - 1
		LPC_pred := s[i+0]*PredCoef[0] + s[i-1]*PredCoef[1] + s[i-2]*PredCoef[2] + s[i-3]*PredCoef[3] + s[i-4]*PredCoef[4] + s[i-5]*PredCoef[5] + s[i-6]*PredCoef[6] + s[i-7]*PredCoef[7] + s[i-8]*PredCoef[8] + s[i-9]*PredCoef[9]
		r_LPC[ix] = s[i+1] - LPC_pred
	}
}
func silk_LPC_analysis_filter8_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int) {
	for ix := 8; ix < length; ix++ {
		i := ix - 1
		LPC_pred := s[i+0]*PredCoef[0] + s[i-1]*PredCoef[1] + s[i-2]*PredCoef[2] + s[i-3]*PredCoef[3] + s[i-4]*PredCoef[4] + s[i-5]*PredCoef[5] + s[i-6]*PredCoef[6] + s[i-7]*PredCoef[7]
		r_LPC[ix] = s[i+1] - LPC_pred
	}
}
func silk_LPC_analysis_filter6_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int) {
	for ix := 6; ix < length; ix++ {
		i := ix - 1
		LPC_pred := s[i+0]*PredCoef[0] + s[i-1]*PredCoef[1] + s[i-2]*PredCoef[2] + s[i-3]*PredCoef[3] + s[i-4]*PredCoef[4] + s[i-5]*PredCoef[5]
		r_LPC[ix] = s[i+1] - LPC_pred
	}
}
func LPC_analysis_filter_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int, Order int) {
	switch Order {
	case 6:
		silk_LPC_analysis_filter6_FLP(r_LPC, PredCoef, s, length)
	case 8:
		silk_LPC_analysis_filter8_FLP(r_LPC, PredCoef, s, length)
	case 10:
		silk_LPC_analysis_filter10_FLP(r_LPC, PredCoef, s, length)
	case 12:
		silk_LPC_analysis_filter12_FLP(r_LPC, PredCoef, s, length)
	case 16:
		silk_LPC_analysis_filter16_FLP(r_LPC, PredCoef, s, length)
	default:
	}
	libc.MemSet(unsafe.Pointer(&r_LPC[0]), 0, Order*int(unsafe.Sizeof(float32(0))))
}
