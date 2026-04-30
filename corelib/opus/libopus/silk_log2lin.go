package libopus

import "math"

func silk_log2lin(inLog_Q7 int32) int32 {
	var (
		out     int32
		frac_Q7 int32
	)
	if int(inLog_Q7) < 0 {
		return 0
	} else if int(inLog_Q7) >= 3967 {
		return silk_int32_MAX
	}
	out = int32(1 << (int(inLog_Q7) >> 7))
	frac_Q7 = int32(int(inLog_Q7) & math.MaxInt8)
	if int(inLog_Q7) < 2048 {
		out = int32(int(out) + ((int(out) * int(int32(int(frac_Q7)+(((int(int32(int16(frac_Q7)))*int(int32(int16(128-int(frac_Q7)))))*int(int64(-174)))>>16)))) >> 7))
	} else {
		out = int32(int(out) + (int(out)>>7)*int(int32(int(frac_Q7)+(((int(int32(int16(frac_Q7)))*int(int32(int16(128-int(frac_Q7)))))*int(int64(-174)))>>16))))
	}
	return out
}
