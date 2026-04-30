package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func resamplerPrivateDownFIR_INTERPOL(out []int16, buf []int32, FIR_Coefs []int16, FIR_Order int, FIR_Fracs int, max_index_Q16 int32, index_increment_Q16 int32) []int16 {
	var (
		index_Q16    int32
		res_Q6       int32
		interpol_ind int32
	)
	switch FIR_Order {
	case RESAMPLER_DOWN_ORDER_FIR0:
		for index_Q16 = 0; int(index_Q16) < int(max_index_Q16); index_Q16 += index_increment_Q16 {
			buf_ptr := buf[int(index_Q16)>>16:]
			interpol_ind = int32(((int(index_Q16) & math.MaxUint16) * int(int64(int16(FIR_Fracs)))) >> 16)
			interpol_ptr := FIR_Coefs[int(RESAMPLER_DOWN_ORDER_FIR0/2)*int(interpol_ind):]
			res_Q6 = int32((int64(buf_ptr[0]) * int64(interpol_ptr[0])) >> 16)
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[1]) * int64(interpol_ptr[1])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[2]) * int64(interpol_ptr[2])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[3]) * int64(interpol_ptr[3])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[4]) * int64(interpol_ptr[4])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[5]) * int64(interpol_ptr[5])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[6]) * int64(interpol_ptr[6])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[7]) * int64(interpol_ptr[7])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[8]) * int64(interpol_ptr[8])) >> 16))
			interpol_ptr = FIR_Coefs[int(RESAMPLER_DOWN_ORDER_FIR0/2)*(FIR_Fracs-1-int(interpol_ind)):]
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[17]) * int64(interpol_ptr[0])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[16]) * int64(interpol_ptr[1])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[15]) * int64(interpol_ptr[2])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[14]) * int64(interpol_ptr[3])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[13]) * int64(interpol_ptr[4])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[12]) * int64(interpol_ptr[5])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[11]) * int64(interpol_ptr[6])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[10]) * int64(interpol_ptr[7])) >> 16))
			res_Q6 = int32(int64(res_Q6) + ((int64(buf_ptr[9]) * int64(interpol_ptr[8])) >> 16))
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
		}
	case RESAMPLER_DOWN_ORDER_FIR1:
		for index_Q16 = 0; int(index_Q16) < int(max_index_Q16); index_Q16 += index_increment_Q16 {
			buf_ptr := buf[int(index_Q16)>>16:]
			res_Q6 = int32(((int(buf_ptr[0]) + int(buf_ptr[23])) * int(int64(FIR_Coefs[0]))) >> 16)
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[1]) + int(buf_ptr[22])) * int(int64(FIR_Coefs[1]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[2]) + int(buf_ptr[21])) * int(int64(FIR_Coefs[2]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[3]) + int(buf_ptr[20])) * int(int64(FIR_Coefs[3]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[4]) + int(buf_ptr[19])) * int(int64(FIR_Coefs[4]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[5]) + int(buf_ptr[18])) * int(int64(FIR_Coefs[5]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[6]) + int(buf_ptr[17])) * int(int64(FIR_Coefs[6]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[7]) + int(buf_ptr[16])) * int(int64(FIR_Coefs[7]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[8]) + int(buf_ptr[15])) * int(int64(FIR_Coefs[8]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[9]) + int(buf_ptr[14])) * int(int64(FIR_Coefs[9]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[10]) + int(buf_ptr[13])) * int(int64(FIR_Coefs[10]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[11]) + int(buf_ptr[12])) * int(int64(FIR_Coefs[11]))) >> 16))
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
		}
	case RESAMPLER_DOWN_ORDER_FIR2:
		for index_Q16 = 0; int(index_Q16) < int(max_index_Q16); index_Q16 += index_increment_Q16 {
			buf_ptr := buf[int(index_Q16)>>16:]
			res_Q6 = int32(((int(buf_ptr[0]) + int(buf_ptr[35])) * int(int64(FIR_Coefs[0]))) >> 16)
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[1]) + int(buf_ptr[34])) * int(int64(FIR_Coefs[1]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[2]) + int(buf_ptr[33])) * int(int64(FIR_Coefs[2]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[3]) + int(buf_ptr[32])) * int(int64(FIR_Coefs[3]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[4]) + int(buf_ptr[31])) * int(int64(FIR_Coefs[4]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[5]) + int(buf_ptr[30])) * int(int64(FIR_Coefs[5]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[6]) + int(buf_ptr[29])) * int(int64(FIR_Coefs[6]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[7]) + int(buf_ptr[28])) * int(int64(FIR_Coefs[7]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[8]) + int(buf_ptr[27])) * int(int64(FIR_Coefs[8]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[9]) + int(buf_ptr[26])) * int(int64(FIR_Coefs[9]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[10]) + int(buf_ptr[25])) * int(int64(FIR_Coefs[10]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[11]) + int(buf_ptr[24])) * int(int64(FIR_Coefs[11]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[12]) + int(buf_ptr[23])) * int(int64(FIR_Coefs[12]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[13]) + int(buf_ptr[22])) * int(int64(FIR_Coefs[13]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[14]) + int(buf_ptr[21])) * int(int64(FIR_Coefs[14]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[15]) + int(buf_ptr[20])) * int(int64(FIR_Coefs[15]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[16]) + int(buf_ptr[19])) * int(int64(FIR_Coefs[16]))) >> 16))
			res_Q6 = int32(int(res_Q6) + (((int(buf_ptr[17]) + int(buf_ptr[18])) * int(int64(FIR_Coefs[17]))) >> 16))
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
		}
	default:
	}
	return out
}
func ResamplerPrivateDownFIR(S *ResamplerState, out []int16, in []int16, inLen int32) {
	var (
		nSamplesIn          int32
		max_index_Q16       int32
		index_increment_Q16 int32
	)
	buf := make([]int32, S.BatchSize+S.FIR_Order)
	libc.MemCpy(unsafe.Pointer(&buf[0]), unsafe.Pointer(&S.SFIR.I32[0]), S.FIR_Order*int(unsafe.Sizeof(int32(0))))
	FIR_Coefs := S.Coefs[2:]
	index_increment_Q16 = S.InvRatio_Q16
	for {
		if int(inLen) < S.BatchSize {
			nSamplesIn = inLen
		} else {
			nSamplesIn = int32(S.BatchSize)
		}
		resampler_private_AR2(S.SIIR[:], buf[S.FIR_Order:], in, S.Coefs, nSamplesIn)
		max_index_Q16 = int32(int(uint32(nSamplesIn)) << 16)
		out = resamplerPrivateDownFIR_INTERPOL(out, []int32(buf), []int16(FIR_Coefs), S.FIR_Order, S.FIR_Fracs, max_index_Q16, index_increment_Q16)
		in = in[nSamplesIn:]
		inLen -= nSamplesIn
		if int(inLen) > 1 {
			libc.MemCpy(unsafe.Pointer(&buf[0]), unsafe.Pointer(&buf[nSamplesIn]), S.FIR_Order*int(unsafe.Sizeof(int32(0))))
		} else {
			break
		}
	}
	libc.MemCpy(unsafe.Pointer(&S.SFIR.I32[0]), unsafe.Pointer(&buf[nSamplesIn]), S.FIR_Order*int(unsafe.Sizeof(int32(0))))
}
