package silk

import "math"

func silk_CLZ64(in int64) int32 {
	var in_upper int32
	in_upper = int32(in >> 32)
	if int(in_upper) == 0 {
		return int32(int(silk_CLZ32(int32(in))) + 32)
	} else {
		return silk_CLZ32(in_upper)
	}
}
func silk_CLZ_FRAC(in int32, lz *int32, frac_Q7 *int32) {
	var lzeros int32 = silk_CLZ32(in)
	*lz = lzeros
	*frac_Q7 = int32(int(silk_ROR32(in, 24-int(lzeros))) & math.MaxInt8)
}
func silk_SQRT_APPROX(x int32) int32 {
	var (
		y       int32
		lz      int32
		frac_Q7 int32
	)
	if int(x) <= 0 {
		return 0
	}
	silk_CLZ_FRAC(x, &lz, &frac_Q7)
	if int(lz)&1 != 0 {
		y = 32768
	} else {
		y = 46214
	}
	y >>= int32(int(lz) >> 1)
	y = int32(int64(y) + ((int64(y) * int64(int16(int(int32(int16(frac_Q7)))*213))) >> 16))
	return y
}
func silk_DIV32_varQ(a32 int32, b32 int32, Qres int) int32 {
	var (
		a_headrm int
		b_headrm int
		lshift   int
		b32_inv  int32
		a32_nrm  int32
		b32_nrm  int32
		result   int32
	)
	a_headrm = int(silk_CLZ32(int32(func() int {
		if int(a32) > 0 {
			return int(a32)
		}
		return int(-a32)
	}()))) - 1
	a32_nrm = int32(int(uint32(a32)) << a_headrm)
	b_headrm = int(silk_CLZ32(int32(func() int {
		if int(b32) > 0 {
			return int(b32)
		}
		return int(-b32)
	}()))) - 1
	b32_nrm = int32(int(uint32(b32)) << b_headrm)
	b32_inv = int32((int(math.MaxInt32 >> 2)) / (int(b32_nrm) >> 16))
	result = int32((int64(a32_nrm) * int64(int16(b32_inv))) >> 16)
	a32_nrm = int32(int(uint32(a32_nrm)) - int(uint32(int32(int(uint32(int32((int64(b32_nrm)*int64(result))>>32)))<<3))))
	result = int32(int64(result) + ((int64(a32_nrm) * int64(int16(b32_inv))) >> 16))
	lshift = a_headrm + 29 - b_headrm - Qres
	if lshift < 0 {
		return int32(int(uint32(int32(func() int {
			if (int(math.MinInt32) >> (-lshift)) > (math.MaxInt32 >> (-lshift)) {
				if int(result) > (int(math.MinInt32) >> (-lshift)) {
					return int(math.MinInt32) >> (-lshift)
				}
				if int(result) < (math.MaxInt32 >> (-lshift)) {
					return math.MaxInt32 >> (-lshift)
				}
				return int(result)
			}
			if int(result) > (math.MaxInt32 >> (-lshift)) {
				return math.MaxInt32 >> (-lshift)
			}
			if int(result) < (int(math.MinInt32) >> (-lshift)) {
				return int(math.MinInt32) >> (-lshift)
			}
			return int(result)
		}()))) << (-lshift))
	} else {
		if lshift < 32 {
			return int32(int(result) >> lshift)
		} else {
			return 0
		}
	}
}
func silk_INVERSE32_varQ(b32 int32, Qres int) int32 {
	var (
		b_headrm int
		lshift   int
		b32_inv  int32
		b32_nrm  int32
		err_Q32  int32
		result   int32
	)
	b_headrm = int(silk_CLZ32(int32(func() int {
		if int(b32) > 0 {
			return int(b32)
		}
		return int(-b32)
	}()))) - 1
	b32_nrm = int32(int(uint32(b32)) << b_headrm)
	b32_inv = int32((int(math.MaxInt32 >> 2)) / (int(b32_nrm) >> 16))
	result = int32(int(uint32(b32_inv)) << 16)
	err_Q32 = int32(int(uint32(int32((1<<29)-int(int32((int64(b32_nrm)*int64(int16(b32_inv)))>>16))))) << 3)
	result = int32(int64(result) + ((int64(err_Q32) * int64(b32_inv)) >> 16))
	lshift = 61 - b_headrm - Qres
	if lshift <= 0 {
		return int32(int(uint32(int32(func() int {
			if (int(math.MinInt32) >> (-lshift)) > (math.MaxInt32 >> (-lshift)) {
				if int(result) > (int(math.MinInt32) >> (-lshift)) {
					return int(math.MinInt32) >> (-lshift)
				}
				if int(result) < (math.MaxInt32 >> (-lshift)) {
					return math.MaxInt32 >> (-lshift)
				}
				return int(result)
			}
			if int(result) > (math.MaxInt32 >> (-lshift)) {
				return math.MaxInt32 >> (-lshift)
			}
			if int(result) < (int(math.MinInt32) >> (-lshift)) {
				return int(math.MinInt32) >> (-lshift)
			}
			return int(result)
		}()))) << (-lshift))
	} else {
		if lshift < 32 {
			return int32(int(result) >> lshift)
		} else {
			return 0
		}
	}
}
