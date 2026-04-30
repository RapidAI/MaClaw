package silk

func resampler_private_AR2(S []int32, out_Q8 []int32, in []int16, A_Q14 []int16, len_ int32) {
	var (
		k     int32
		out32 int32
	)
	for k = 0; int(k) < int(len_); k++ {
		out32 = int32(int(S[0]) + int(int32(int(uint32(int32(in[k])))<<8)))
		out_Q8[k] = out32
		out32 = int32(int(uint32(out32)) << 2)
		S[0] = int32(int64(S[1]) + ((int64(out32) * int64(A_Q14[0])) >> 16))
		S[1] = int32((int64(out32) * int64(A_Q14[1])) >> 16)
	}
}
