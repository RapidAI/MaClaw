package silk

import "math"

const BIN_DIV_STEPS_A2NLSF_FIX = 3
const MAX_ITERATIONS_A2NLSF_FIX = 16

func A2NLSF_trans_poly(p []int32, dd int) {
	var (
		k int
		n int
	)
	for k = 2; k <= dd; k++ {
		for n = dd; n > k; n-- {
			p[n-2] -= p[n]
		}
		p[k-2] -= int32(int(uint32(p[k])) << 1)
	}
}
func A2NLSF_eval_poly(p []int32, x int32, dd int) int32 {
	var (
		n     int
		x_Q16 int32
		y32   int32
	)
	y32 = p[dd]
	x_Q16 = int32(int(uint32(x)) << 4)
	if dd == 8 {
		y32 = int32(int64(p[7]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[6]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[5]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[4]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[3]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[2]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[1]) + ((int64(y32) * int64(x_Q16)) >> 16))
		y32 = int32(int64(p[0]) + ((int64(y32) * int64(x_Q16)) >> 16))
	} else {
		for n = dd - 1; n >= 0; n-- {
			y32 = int32(int64(p[n]) + ((int64(y32) * int64(x_Q16)) >> 16))
		}
	}
	return y32
}
func A2NLSF_init(a_Q16 []int32, P []int32, Q []int32, dd int) {
	var k int
	P[dd] = 1 << 16
	Q[dd] = 1 << 16
	for k = 0; k < dd; k++ {
		P[k] = int32(int(-a_Q16[dd-k-1]) - int(a_Q16[dd+k]))
		Q[k] = int32(int(-a_Q16[dd-k-1]) + int(a_Q16[dd+k]))
	}
	for k = dd; k > 0; k-- {
		P[k-1] -= P[k]
		Q[k-1] += Q[k]
	}
	A2NLSF_trans_poly(P, dd)
	A2NLSF_trans_poly(Q, dd)
}
func A2NLSF(NLSF []int16, a_Q16 []int32, d int) {
	var (
		i       int
		k       int
		m       int
		dd      int
		root_ix int
		ffrac   int
		xlo     int32
		xhi     int32
		xmid    int32
		ylo     int32
		yhi     int32
		ymid    int32
		thr     int32
		nom     int32
		den     int32
		P       [13]int32
		Q       [13]int32
		PQ      [2][]int32
	)
	PQ[0] = P[:]
	PQ[1] = Q[:]
	dd = d >> 1
	A2NLSF_init(a_Q16, P[:], Q[:], dd)
	p := P[:]
	xlo = int32(silk_LSFCosTab_FIX_Q12[0])
	ylo = A2NLSF_eval_poly([]int32(p), xlo, dd)
	if int(ylo) < 0 {
		NLSF[0] = 0
		p = Q[:]
		ylo = A2NLSF_eval_poly([]int32(p), xlo, dd)
		root_ix = 1
	} else {
		root_ix = 0
	}
	k = 1
	i = 0
	thr = 0
	for {
		xhi = int32(silk_LSFCosTab_FIX_Q12[k])
		yhi = A2NLSF_eval_poly([]int32(p), xhi, dd)
		if int(ylo) <= 0 && int(yhi) >= int(thr) || int(ylo) >= 0 && int(yhi) <= int(-thr) {
			if int(yhi) == 0 {
				thr = 1
			} else {
				thr = 0
			}
			ffrac = -256
			for m = 0; m < BIN_DIV_STEPS_A2NLSF_FIX; m++ {
				if 1 == 1 {
					xmid = int32(((int(xlo) + int(xhi)) >> 1) + ((int(xlo) + int(xhi)) & 1))
				} else {
					xmid = int32((((int(xlo) + int(xhi)) >> (1 - 1)) + 1) >> 1)
				}
				ymid = A2NLSF_eval_poly([]int32(p), xmid, dd)
				if int(ylo) <= 0 && int(ymid) >= 0 || int(ylo) >= 0 && int(ymid) <= 0 {
					xhi = xmid
					yhi = ymid
				} else {
					xlo = xmid
					ylo = ymid
					ffrac = ffrac + (128 >> m)
				}
			}
			if (func() int {
				if int(ylo) > 0 {
					return int(ylo)
				}
				return int(-ylo)
			}()) < 65536 {
				den = int32(int(ylo) - int(yhi))
				nom = int32(int(int32(int(uint32(ylo))<<(int(8-BIN_DIV_STEPS_A2NLSF_FIX)))) + (int(den) >> 1))
				if int(den) != 0 {
					ffrac += int(int32(int(nom) / int(den)))
				}
			} else {
				ffrac += int(int32(int(ylo) / ((int(ylo) - int(yhi)) >> (int(8 - BIN_DIV_STEPS_A2NLSF_FIX)))))
			}
			NLSF[root_ix] = int16(silk_min_32(int32(int(int32(int(uint32(int32(k)))<<8))+ffrac), math.MaxInt16))
			root_ix++
			if root_ix >= d {
				break
			}
			p = PQ[root_ix&1]
			xlo = int32(silk_LSFCosTab_FIX_Q12[k-1])
			ylo = int32(int(uint32(int32(1-(root_ix&2)))) << 12)
		} else {
			k++
			xlo = xhi
			ylo = yhi
			thr = 0
			if k > LSF_COS_TAB_SZ_FIX {
				i++
				if i > MAX_ITERATIONS_A2NLSF_FIX {
					NLSF[0] = int16(int32((1 << 15) / (d + 1)))
					for k = 1; k < d; k++ {
						NLSF[k] = int16(int(NLSF[k-1]) + int(NLSF[0]))
					}
					return
				}
				BwExpander32(a_Q16, d, int32(65536-int(int32(1<<i))))
				A2NLSF_init(a_Q16, P[:], Q[:], dd)
				p = P[:]
				xlo = int32(silk_LSFCosTab_FIX_Q12[0])
				ylo = A2NLSF_eval_poly([]int32(p), xlo, dd)
				if int(ylo) < 0 {
					NLSF[0] = 0
					p = Q[:]
					ylo = A2NLSF_eval_poly([]int32(p), xlo, dd)
					root_ix = 1
				} else {
					root_ix = 0
				}
				k = 1
			}
		}
	}
}
