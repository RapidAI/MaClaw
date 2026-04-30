package silk

import (
	"math"
)

const ORDER_FIR = 4

func ResamplerDown2_3(S []int32, out []int16, in []int16, inLen int32) {
	var (
		nSamplesIn int32
		counter    int32
		res_Q6     int32
		buf_ptr    []int32
	)
	buf := make([]int32, (RESAMPLER_MAX_BATCH_SIZE_MS*RESAMPLER_MAX_FS_KHZ)+ORDER_FIR)
	copy(buf, S[:ORDER_FIR])
	for {
		if int(inLen) < (int(RESAMPLER_MAX_BATCH_SIZE_MS * RESAMPLER_MAX_FS_KHZ)) {
			nSamplesIn = inLen
		} else {
			nSamplesIn = int32(int(RESAMPLER_MAX_BATCH_SIZE_MS * RESAMPLER_MAX_FS_KHZ))
		}
		resampler_private_AR2(S[ORDER_FIR:], buf[ORDER_FIR:], in, silk_Resampler_2_3_COEFS_LQ[:], nSamplesIn)
		buf_ptr = buf
		counter = nSamplesIn
		for int(counter) > 2 {
			res_Q6 = int32((int64(buf_ptr[0]) * int64(silk_Resampler_2_3_COEFS_LQ[2])) >> 16)
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[1]) * int64(silk_Resampler_2_3_COEFS_LQ[3])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[2]) * int64(silk_Resampler_2_3_COEFS_LQ[5])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[3]) * int64(silk_Resampler_2_3_COEFS_LQ[4])) >> 16))
			out[0] = int16(func() int {
				if (func() int {
					if 6 == 1 {
						return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
					}
					return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
				}()) > math.MaxInt16 {
					return math.MaxInt16
				}
				if (func() int {
					if 6 == 1 {
						return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
					}
					return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
				}()) < int(math.MinInt16) {
					return math.MinInt16
				}
				if 6 == 1 {
					return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
				}
				return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
			}())
			out = out[1:]
			res_Q6 = int32((int64(buf_ptr[1]) * int64(silk_Resampler_2_3_COEFS_LQ[4])) >> 16)
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[2]) * int64(silk_Resampler_2_3_COEFS_LQ[5])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[3]) * int64(silk_Resampler_2_3_COEFS_LQ[3])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[4]) * int64(silk_Resampler_2_3_COEFS_LQ[2])) >> 16))
			out[0] = int16(func() int {
				if (func() int {
					if 6 == 1 {
						return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
					}
					return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
				}()) > math.MaxInt16 {
					return math.MaxInt16
				}
				if (func() int {
					if 6 == 1 {
						return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
					}
					return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
				}()) < int(math.MinInt16) {
					return math.MinInt16
				}
				if 6 == 1 {
					return (int(res_Q6) >> 1) + (int(res_Q6) & 1)
				}
				return ((int(res_Q6) >> (6 - 1)) + 1) >> 1
			}())
			out = out[1:]
			buf_ptr = buf_ptr[3:]
			counter -= 3
		}
		in = in[nSamplesIn:]
		inLen -= nSamplesIn
		if int(inLen) > 0 {
			copy(buf[:ORDER_FIR], buf[nSamplesIn:])
		} else {
			break
		}
	}
	copy(S[:ORDER_FIR], buf[nSamplesIn:])
}
