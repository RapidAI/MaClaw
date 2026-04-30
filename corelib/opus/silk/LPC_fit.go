package silk

import "math"

func silk_LPC_fit(a_QOUT []int16, a_QIN []int32, QOUT int, QIN int, d int) {
	var (
		i         int
		idx       int = 0
		maxabs    int32
		absval    int32
		chirp_Q16 int32
	)
	for i = 0; i < 10; i++ {
		maxabs = 0
		for k := 0; k < d; k++ {
			if int(a_QIN[k]) > 0 {
				absval = a_QIN[k]
			} else {
				absval = -(a_QIN[k])
			}
			if int(absval) > int(maxabs) {
				maxabs = absval
				idx = k
			}
		}
		if (QIN - QOUT) == 1 {
			maxabs = int32((int(maxabs) >> 1) + (int(maxabs) & 1))
		} else {
			maxabs = int32(((int(maxabs) >> ((QIN - QOUT) - 1)) + 1) >> 1)
		}
		if int(maxabs) > math.MaxInt16 {
			if int(maxabs) < 163838 {
				maxabs = maxabs
			} else {
				maxabs = 163838
			}
			chirp_Q16 = int32(int(int32(math.Floor(0.999*(1<<16)+0.5))) - int(int32(int(int32(int(uint32(int32(int(maxabs)-math.MaxInt16)))<<14))/((int(maxabs)*(idx+1))>>2))))
			BwExpander32(a_QIN, d, chirp_Q16)
		} else {
			break
		}
	}
	if i == 10 {
		for k := 0; k < d; k++ {
			if (func() int {
				if (QIN - QOUT) == 1 {
					return (int(a_QIN[k]) >> 1) + (int(a_QIN[k]) & 1)
				}
				return ((int(a_QIN[k]) >> ((QIN - QOUT) - 1)) + 1) >> 1
			}()) > math.MaxInt16 {
				a_QOUT[k] = math.MaxInt16
			} else if (func() int {
				if (QIN - QOUT) == 1 {
					return (int(a_QIN[k]) >> 1) + (int(a_QIN[k]) & 1)
				}
				return ((int(a_QIN[k]) >> ((QIN - QOUT) - 1)) + 1) >> 1
			}()) < int(math.MinInt16) {
				a_QOUT[k] = math.MinInt16
			} else if (QIN - QOUT) == 1 {
				a_QOUT[k] = int16((int(a_QIN[k]) >> 1) + (int(a_QIN[k]) & 1))
			} else {
				a_QOUT[k] = int16(((int(a_QIN[k]) >> ((QIN - QOUT) - 1)) + 1) >> 1)
			}
			a_QIN[k] = int32(int(uint32(int32(a_QOUT[k]))) << (QIN - QOUT))
		}
	} else {
		for k := 0; k < d; k++ {
			if (QIN - QOUT) == 1 {
				a_QOUT[k] = int16((int(a_QIN[k]) >> 1) + (int(a_QIN[k]) & 1))
			} else {
				a_QOUT[k] = int16(((int(a_QIN[k]) >> ((QIN - QOUT) - 1)) + 1) >> 1)
			}
		}
	}
}
