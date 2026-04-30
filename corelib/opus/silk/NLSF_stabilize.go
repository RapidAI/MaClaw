package silk

import "math"

func NLSF_stabilize(NLSF_Q15 []int16, NDeltaMin_Q15 []int16, L int) {
	const MAX_LOOPS = 20
	var (
		I               int = 0
		loops           int
		center_freq_Q15 int16
		diff_Q15        int32
		min_diff_Q15    int32
		min_center_Q15  int32
		max_center_Q15  int32
	)
	for loops = 0; loops < MAX_LOOPS; loops++ {
		min_diff_Q15 = int32(int(NLSF_Q15[0]) - int(NDeltaMin_Q15[0]))
		I = 0
		for i := 1; i <= L-1; i++ {
			diff_Q15 = int32(int(NLSF_Q15[i]) - (int(NLSF_Q15[i-1]) + int(NDeltaMin_Q15[i])))
			if int(diff_Q15) < int(min_diff_Q15) {
				min_diff_Q15 = diff_Q15
				I = i
			}
		}
		diff_Q15 = int32((1 << 15) - (int(NLSF_Q15[L-1]) + int(NDeltaMin_Q15[L])))
		if int(diff_Q15) < int(min_diff_Q15) {
			min_diff_Q15 = diff_Q15
			I = L
		}
		if int(min_diff_Q15) >= 0 {
			return
		}
		if I == 0 {
			NLSF_Q15[0] = NDeltaMin_Q15[0]
		} else if I == L {
			NLSF_Q15[L-1] = int16((1 << 15) - int(NDeltaMin_Q15[L]))
		} else {
			min_center_Q15 = 0
			for k := 0; k < I; k++ {
				min_center_Q15 += int32(NDeltaMin_Q15[k])
			}
			min_center_Q15 += int32(int(NDeltaMin_Q15[I]) >> 1)
			max_center_Q15 = 1 << 15
			for k := L; k > I; k-- {
				max_center_Q15 -= int32(NDeltaMin_Q15[k])
			}
			max_center_Q15 -= int32(int(NDeltaMin_Q15[I]) >> 1)
			if int(min_center_Q15) > int(max_center_Q15) {
				if (func() int {
					if 1 == 1 {
						return ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1)
					}
					return (((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1
				}()) > int(min_center_Q15) {
					center_freq_Q15 = int16(min_center_Q15)
				} else if (func() int {
					if 1 == 1 {
						return ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1)
					}
					return (((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1
				}()) < int(max_center_Q15) {
					center_freq_Q15 = int16(max_center_Q15)
				} else if 1 == 1 {
					center_freq_Q15 = int16(((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1))
				} else {
					center_freq_Q15 = int16((((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1)
				}
			} else if (func() int {
				if 1 == 1 {
					return ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1)
				}
				return (((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1
			}()) > int(max_center_Q15) {
				center_freq_Q15 = int16(max_center_Q15)
			} else if (func() int {
				if 1 == 1 {
					return ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1)
				}
				return (((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1
			}()) < int(min_center_Q15) {
				center_freq_Q15 = int16(min_center_Q15)
			} else if 1 == 1 {
				center_freq_Q15 = int16(((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> 1) + ((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) & 1))
			} else {
				center_freq_Q15 = int16((((int(int32(NLSF_Q15[I-1])) + int(int32(NLSF_Q15[I]))) >> (1 - 1)) + 1) >> 1)
			}
			NLSF_Q15[I-1] = int16(int(center_freq_Q15) - (int(NDeltaMin_Q15[I]) >> 1))
			NLSF_Q15[I] = int16(int(NLSF_Q15[I-1]) + int(NDeltaMin_Q15[I]))
		}
	}
	if loops == MAX_LOOPS {
		silk_insertion_sort_increasing_all_values_int16(NLSF_Q15, L)
		NLSF_Q15[0] = int16(silk_max_int(int(NLSF_Q15[0]), int(NDeltaMin_Q15[0])))
		for i := 1; i < L; i++ {
			NLSF_Q15[i] = int16(silk_max_int(int(NLSF_Q15[i]), int(int16(func() int {
				if (int(int32(NLSF_Q15[i-1])) + int(NDeltaMin_Q15[i])) > math.MaxInt16 {
					return math.MaxInt16
				}
				if (int(int32(NLSF_Q15[i-1])) + int(NDeltaMin_Q15[i])) < math.MinInt16 {
					return math.MinInt16
				}
				return int(int32(NLSF_Q15[i-1])) + int(NDeltaMin_Q15[i])
			}()))))
		}
		NLSF_Q15[L-1] = int16(silk_min_int(int(NLSF_Q15[L-1]), (1<<15)-int(NDeltaMin_Q15[L])))
		for i := L - 2; i >= 0; i-- {
			NLSF_Q15[i] = int16(silk_min_int(int(NLSF_Q15[i]), int(NLSF_Q15[i+1])-int(NDeltaMin_Q15[i+1])))
		}
	}
}
