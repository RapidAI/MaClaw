package silk

import "math"

func ResamplerDown2(S []int32, out []int16, in []int16, inLen int32) {
	var (
		k     int32
		len2  int32 = int32(int(inLen) >> 1)
		in32  int32
		out32 int32
		Y     int32
		X     int32
	)
	for k = 0; int(k) < int(len2); k++ {
		in32 = int32(int(uint32(int32(in[int(k)*2]))) << 10)
		Y = int32(int(in32) - int(S[0]))
		X = int32(int64(Y) + ((int64(Y) * int64(silk_resampler_down2_1)) >> 16))
		out32 = int32(int(S[0]) + int(X))
		S[0] = int32(int(in32) + int(X))
		in32 = int32(int(uint32(int32(in[int(k)*2+1]))) << 10)
		Y = int32(int(in32) - int(S[1]))
		X = int32((int64(Y) * int64(silk_resampler_down2_0)) >> 16)
		out32 = int32(int(out32) + int(S[1]))
		out32 = int32(int(out32) + int(X))
		S[1] = int32(int(in32) + int(X))
		if (func() int {
			if 11 == 1 {
				return (int(out32) >> 1) + (int(out32) & 1)
			}
			return ((int(out32) >> (11 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			out[k] = math.MaxInt16
		} else if (func() int {
			if 11 == 1 {
				return (int(out32) >> 1) + (int(out32) & 1)
			}
			return ((int(out32) >> (11 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			out[k] = math.MinInt16
		} else if 11 == 1 {
			out[k] = int16((int(out32) >> 1) + (int(out32) & 1))
		} else {
			out[k] = int16(((int(out32) >> (11 - 1)) + 1) >> 1)
		}
	}
}
