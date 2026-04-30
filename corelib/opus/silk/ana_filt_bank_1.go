package silk

import "math"

var A_fb1_20 int16 = 5394 << 1
var A_fb1_21 int16 = -24290

func silk_ana_filt_bank_1(in []int16, S []int32, outL []int16, outH []int16, N int32) {
	var (
		k     int
		N2    int = (int(N) >> 1)
		in32  int32
		X     int32
		Y     int32
		out_1 int32
		out_2 int32
	)
	for k = 0; k < N2; k++ {
		in32 = int32(int(uint32(int32(in[k*2]))) << 10)
		Y = int32(int(in32) - int(S[0]))
		X = int32(int64(Y) + ((int64(Y) * int64(A_fb1_21)) >> 16))
		out_1 = int32(int(S[0]) + int(X))
		S[0] = int32(int(in32) + int(X))
		in32 = int32(int(uint32(int32(in[k*2+1]))) << 10)
		Y = int32(int(in32) - int(S[1]))
		X = int32((int64(Y) * int64(A_fb1_20)) >> 16)
		out_2 = int32(int(S[1]) + int(X))
		S[1] = int32(int(in32) + int(X))
		if (func() int {
			if 11 == 1 {
				return ((int(out_2) + int(out_1)) >> 1) + ((int(out_2) + int(out_1)) & 1)
			}
			return (((int(out_2) + int(out_1)) >> (11 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			outL[k] = math.MaxInt16
		} else if (func() int {
			if 11 == 1 {
				return ((int(out_2) + int(out_1)) >> 1) + ((int(out_2) + int(out_1)) & 1)
			}
			return (((int(out_2) + int(out_1)) >> (11 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			outL[k] = math.MinInt16
		} else if 11 == 1 {
			outL[k] = int16(((int(out_2) + int(out_1)) >> 1) + ((int(out_2) + int(out_1)) & 1))
		} else {
			outL[k] = int16((((int(out_2) + int(out_1)) >> (11 - 1)) + 1) >> 1)
		}
		if (func() int {
			if 11 == 1 {
				return ((int(out_2) - int(out_1)) >> 1) + ((int(out_2) - int(out_1)) & 1)
			}
			return (((int(out_2) - int(out_1)) >> (11 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			outH[k] = math.MaxInt16
		} else if (func() int {
			if 11 == 1 {
				return ((int(out_2) - int(out_1)) >> 1) + ((int(out_2) - int(out_1)) & 1)
			}
			return (((int(out_2) - int(out_1)) >> (11 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			outH[k] = math.MinInt16
		} else if 11 == 1 {
			outH[k] = int16(((int(out_2) - int(out_1)) >> 1) + ((int(out_2) - int(out_1)) & 1))
		} else {
			outH[k] = int16((((int(out_2) - int(out_1)) >> (11 - 1)) + 1) >> 1)
		}
	}
}
