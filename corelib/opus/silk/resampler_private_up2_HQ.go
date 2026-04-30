package silk

import (
	"math"
)

func resamplerPrivateUp2HQ(S []int32, out []int16, in []int16, len_ int32) {
	var (
		k       int32
		in32    int32
		out32_1 int32
		out32_2 int32
		Y       int32
		X       int32
	)
	for k = 0; int(k) < int(len_); k++ {
		in32 = int32(int(uint32(int32(in[k]))) << 10)
		Y = int32(int(in32) - int(S[0]))
		X = int32((int64(Y) * int64(silk_resampler_up2_hq_0[0])) >> 16)
		out32_1 = int32(int(S[0]) + int(X))
		S[0] = int32(int(in32) + int(X))
		Y = int32(int(out32_1) - int(S[1]))
		X = int32((int64(Y) * int64(silk_resampler_up2_hq_0[1])) >> 16)
		out32_2 = int32(int(S[1]) + int(X))
		S[1] = int32(int(out32_1) + int(X))
		Y = int32(int(out32_2) - int(S[2]))
		X = int32(int64(Y) + ((int64(Y) * int64(silk_resampler_up2_hq_0[2])) >> 16))
		out32_1 = int32(int(S[2]) + int(X))
		S[2] = int32(int(out32_2) + int(X))
		if (func() int {
			if 10 == 1 {
				return (int(out32_1) >> 1) + (int(out32_1) & 1)
			}
			return ((int(out32_1) >> (10 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			out[int(k)*2] = math.MaxInt16
		} else if (func() int {
			if 10 == 1 {
				return (int(out32_1) >> 1) + (int(out32_1) & 1)
			}
			return ((int(out32_1) >> (10 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			out[int(k)*2] = math.MinInt16
		} else if 10 == 1 {
			out[int(k)*2] = int16((int(out32_1) >> 1) + (int(out32_1) & 1))
		} else {
			out[int(k)*2] = int16(((int(out32_1) >> (10 - 1)) + 1) >> 1)
		}
		Y = int32(int(in32) - int(S[3]))
		X = int32((int64(Y) * int64(silk_resampler_up2_hq_1[0])) >> 16)
		out32_1 = int32(int(S[3]) + int(X))
		S[3] = int32(int(in32) + int(X))
		Y = int32(int(out32_1) - int(S[4]))
		X = int32((int64(Y) * int64(silk_resampler_up2_hq_1[1])) >> 16)
		out32_2 = int32(int(S[4]) + int(X))
		S[4] = int32(int(out32_1) + int(X))
		Y = int32(int(out32_2) - int(S[5]))
		X = int32(int64(Y) + ((int64(Y) * int64(silk_resampler_up2_hq_1[2])) >> 16))
		out32_1 = int32(int(S[5]) + int(X))
		S[5] = int32(int(out32_2) + int(X))
		if (func() int {
			if 10 == 1 {
				return (int(out32_1) >> 1) + (int(out32_1) & 1)
			}
			return ((int(out32_1) >> (10 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			out[int(k)*2+1] = math.MaxInt16
		} else if (func() int {
			if 10 == 1 {
				return (int(out32_1) >> 1) + (int(out32_1) & 1)
			}
			return ((int(out32_1) >> (10 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			out[int(k)*2+1] = math.MinInt16
		} else if 10 == 1 {
			out[int(k)*2+1] = int16((int(out32_1) >> 1) + (int(out32_1) & 1))
		} else {
			out[int(k)*2+1] = int16(((int(out32_1) >> (10 - 1)) + 1) >> 1)
		}
	}
}
func ResamplerPrivateUp2HQWrapper(S *ResamplerState, out []int16, in []int16, len_ int32) {
	resamplerPrivateUp2HQ(S.SIIR[:], out, in, len_)
}
