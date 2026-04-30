package silk

const NLSF2A_QA = 16

func silk_NLSF2A_find_poly(out []int32, cLSF []int32, dd int) {
	var (
		k    int
		n    int
		ftmp int32
	)
	out[0] = int32(int(1 << NLSF2A_QA))
	out[1] = -cLSF[0]
	for k = 1; k < dd; k++ {
		ftmp = cLSF[k*2]
		out[k+1] = int32(int(int32(int(uint32(out[k-1]))<<1)) - int(int32(func() int64 {
			if NLSF2A_QA == 1 {
				return ((int64(ftmp) * int64(out[k])) >> 1) + ((int64(ftmp) * int64(out[k])) & 1)
			}
			return (((int64(ftmp) * int64(out[k])) >> int64(int(NLSF2A_QA-1))) + 1) >> 1
		}())))
		for n = k; n > 1; n-- {
			out[n] += int32(int(out[n-2]) - int(int32(func() int64 {
				if NLSF2A_QA == 1 {
					return ((int64(ftmp) * int64(out[n-1])) >> 1) + ((int64(ftmp) * int64(out[n-1])) & 1)
				}
				return (((int64(ftmp) * int64(out[n-1])) >> int64(int(NLSF2A_QA-1))) + 1) >> 1
			}())))
		}
		out[1] -= ftmp
	}
}
func NLSF2A(a_Q12 []int16, NLSF []int16, d int, arch int) {
	var ordering16 = [16]byte{0, 15, 8, 7, 4, 11, 12, 3, 2, 13, 10, 5, 6, 9, 14, 1}
	var ordering10 = [10]byte{0, 9, 6, 3, 4, 5, 8, 1, 2, 7}
	var ordering []byte
	var k int
	var i int
	var dd int
	var cos_LSF_NLSF2A_QA [24]int32
	var P [13]int32
	var Q [13]int32
	var Ptmp int32
	var Qtmp int32
	var f_int int32
	var f_frac int32
	var cos_val int32
	var delta int32
	var a32_NLSF2A_QA1 [24]int32
	ordering = func() []uint8 {
		if d == 16 {
			return ordering16[:]
		}
		return ordering10[:]
	}()
	for k = 0; k < d; k++ {
		f_int = int32(int(NLSF[k]) >> (15 - 7))
		f_frac = int32(int(NLSF[k]) - int(int32(int(uint32(f_int))<<(15-7))))
		cos_val = int32(silk_LSFCosTab_FIX_Q12[f_int])
		delta = int32(int(silk_LSFCosTab_FIX_Q12[int(f_int)+1]) - int(cos_val))
		if (int(20 - NLSF2A_QA)) == 1 {
			cos_LSF_NLSF2A_QA[ordering[k]] = int32(((int(int32(int(uint32(cos_val))<<8)) + int(delta)*int(f_frac)) >> 1) + ((int(int32(int(uint32(cos_val))<<8)) + int(delta)*int(f_frac)) & 1))
		} else {
			cos_LSF_NLSF2A_QA[ordering[k]] = int32((((int(int32(int(uint32(cos_val))<<8)) + int(delta)*int(f_frac)) >> ((int(20 - NLSF2A_QA)) - 1)) + 1) >> 1)
		}
	}
	dd = d >> 1
	silk_NLSF2A_find_poly(P[:], cos_LSF_NLSF2A_QA[0:], dd)
	silk_NLSF2A_find_poly(Q[:], cos_LSF_NLSF2A_QA[1:], dd)
	for k = 0; k < dd; k++ {
		Ptmp = int32(int(P[k+1]) + int(P[k]))
		Qtmp = int32(int(Q[k+1]) - int(Q[k]))
		a32_NLSF2A_QA1[k] = int32(int(-Qtmp) - int(Ptmp))
		a32_NLSF2A_QA1[d-k-1] = int32(int(Qtmp) - int(Ptmp))
	}
	silk_LPC_fit(a_Q12, a32_NLSF2A_QA1[:], 12, int(NLSF2A_QA+1), d)
	for i = 0; int(func() int32 {
		_ = arch
		return silk_LPC_inverse_pred_gain_c(a_Q12, d)
	}()) == 0 && i < MAX_LPC_STABILIZE_ITERATIONS; i++ {
		BwExpander32(a32_NLSF2A_QA1[:], d, int32(65536-int(int32(2<<i))))
		for k = 0; k < d; k++ {
			if (int(NLSF2A_QA+1) - 12) == 1 {
				a_Q12[k] = int16((int(a32_NLSF2A_QA1[k]) >> 1) + (int(a32_NLSF2A_QA1[k]) & 1))
			} else {
				a_Q12[k] = int16(((int(a32_NLSF2A_QA1[k]) >> ((int(NLSF2A_QA+1) - 12) - 1)) + 1) >> 1)
			}
		}
	}
}
