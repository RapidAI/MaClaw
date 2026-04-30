package libopus

func silk_lin2log(inLin int32) int32 {
	var (
		lz      int32
		frac_Q7 int32
	)
	silk_CLZ_FRAC(inLin, &lz, &frac_Q7)
	return int32(int(int32(int(frac_Q7)+(((int(frac_Q7)*(128-int(frac_Q7)))*179)>>16))) + int(int32(int(uint32(int32(31-int(lz))))<<7)))
}
