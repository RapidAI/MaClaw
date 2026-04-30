package silk

func BwExpander32(ar []int32, d int, chirp_Q16 int32) {
	var (
		i                   int
		chirp_minus_one_Q16 int32 = int32(int(chirp_Q16) - 65536)
	)
	for i = 0; i < d-1; i++ {
		ar[i] = int32((int64(chirp_Q16) * int64(ar[i])) >> 16)
		if 16 == 1 {
			chirp_Q16 += int32(((int(chirp_Q16) * int(chirp_minus_one_Q16)) >> 1) + ((int(chirp_Q16) * int(chirp_minus_one_Q16)) & 1))
		} else {
			chirp_Q16 += int32((((int(chirp_Q16) * int(chirp_minus_one_Q16)) >> (16 - 1)) + 1) >> 1)
		}
	}
	ar[d-1] = int32((int64(chirp_Q16) * int64(ar[d-1])) >> 16)
}
