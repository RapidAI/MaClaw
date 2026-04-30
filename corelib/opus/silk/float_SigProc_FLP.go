package silk

import "math"

const PI = math.Pi

func silk_sigmoid(x float32) float32 {
	return float32(1.0 / (math.Exp(float64(-x)) + 1.0))
}
func silk_float2int(x float32) int32 {
	return int32(int(math.Floor(float64(x + 0.5))))
}
func silk_float2short_array(out []int16, in []float32, length int32) {
	var k int32
	for k = int32(int(length) - 1); int(k) >= 0; k-- {
		if int(int32(int(math.Floor(float64(in[k]+0.5))))) > math.MaxInt16 {
			out[k] = math.MaxInt16
		} else if int(int32(int(math.Floor(float64(in[k]+0.5))))) < int(math.MinInt16) {
			out[k] = math.MinInt16
		} else {
			out[k] = int16(int32(int(math.Floor(float64(in[k] + 0.5)))))
		}
	}
}
func silk_short2float_array(out []float32, in []int16, length int32) {
	var k int32
	for k = int32(int(length) - 1); int(k) >= 0; k-- {
		out[k] = float32(in[k])
	}
}
func silk_log2(x float64) float32 {
	return float32(math.Log10(x) * 3.32192809488736)
}
