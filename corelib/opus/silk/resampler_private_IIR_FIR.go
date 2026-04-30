package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func resamplerPrivateIIR_FIR_INTERPOL(out []int16, buf []int16, max_index_Q16 int32, index_increment_Q16 int32) []int16 {
	var (
		index_Q16   int32
		res_Q15     int32
		table_index int32
	)
	for index_Q16 = 0; int(index_Q16) < int(max_index_Q16); index_Q16 += index_increment_Q16 {
		table_index = int32(((int(index_Q16) & math.MaxUint16) * 12) >> 16)
		buf_ptr := buf[int(index_Q16)>>16:]
		res_Q15 = int32(int(int32(buf_ptr[0])) * int(int32(silk_resampler_frac_FIR_12[table_index][0])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[1]))*int(int32(silk_resampler_frac_FIR_12[table_index][1])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[2]))*int(int32(silk_resampler_frac_FIR_12[table_index][2])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[3]))*int(int32(silk_resampler_frac_FIR_12[table_index][3])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[4]))*int(int32(silk_resampler_frac_FIR_12[11-int(table_index)][3])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[5]))*int(int32(silk_resampler_frac_FIR_12[11-int(table_index)][2])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[6]))*int(int32(silk_resampler_frac_FIR_12[11-int(table_index)][1])))
		res_Q15 = int32(int(res_Q15) + int(int32(buf_ptr[7]))*int(int32(silk_resampler_frac_FIR_12[11-int(table_index)][0])))
		out[0] = int16(func() int {
			if (func() int {
				if 15 == 1 {
					return (int(res_Q15) >> 1) + (int(res_Q15) & 1)
				}
				return ((int(res_Q15) >> (15 - 1)) + 1) >> 1
			}()) > math.MaxInt16 {
				return math.MaxInt16
			}
			if (func() int {
				if 15 == 1 {
					return (int(res_Q15) >> 1) + (int(res_Q15) & 1)
				}
				return ((int(res_Q15) >> (15 - 1)) + 1) >> 1
			}()) < int(math.MinInt16) {
				return math.MinInt16
			}
			if 15 == 1 {
				return (int(res_Q15) >> 1) + (int(res_Q15) & 1)
			}
			return ((int(res_Q15) >> (15 - 1)) + 1) >> 1
		}())
		out = out[1:]
	}
	return out
}
func ResamplerPrivateIIR_FIR(S *ResamplerState, out []int16, in []int16, inLen int32) {
	var (
		nSamplesIn          int32
		max_index_Q16       int32
		index_increment_Q16 int32
	)
	buf := make([]int16, S.BatchSize*2+RESAMPLER_ORDER_FIR_12)
	libc.MemCpy(unsafe.Pointer(&buf[0]), unsafe.Pointer(&S.SFIR.I16[0]), int(RESAMPLER_ORDER_FIR_12*unsafe.Sizeof(int16(0))))
	index_increment_Q16 = S.InvRatio_Q16
	for {
		if int(inLen) < S.BatchSize {
			nSamplesIn = inLen
		} else {
			nSamplesIn = int32(S.BatchSize)
		}
		resamplerPrivateUp2HQ(S.SIIR[:], buf[RESAMPLER_ORDER_FIR_12:], in, nSamplesIn)
		max_index_Q16 = int32(int(uint32(nSamplesIn)) << (16 + 1))
		out = resamplerPrivateIIR_FIR_INTERPOL(out, []int16(buf), max_index_Q16, index_increment_Q16)
		in = in[nSamplesIn:]
		inLen -= nSamplesIn
		if int(inLen) > 0 {
			libc.MemCpy(unsafe.Pointer(&buf[0]), unsafe.Pointer(&buf[int(nSamplesIn)<<1]), int(RESAMPLER_ORDER_FIR_12*unsafe.Sizeof(int16(0))))
		} else {
			break
		}
	}
	libc.MemCpy(unsafe.Pointer(&S.SFIR.I16[0]), unsafe.Pointer(&buf[int(nSamplesIn)<<1]), int(RESAMPLER_ORDER_FIR_12*unsafe.Sizeof(int16(0))))
}
