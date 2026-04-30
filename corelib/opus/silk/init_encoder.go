package silk

func (psEnc *EncoderStateFLP) Init(arch int) int {
	var ret int = 0
	*psEnc = EncoderStateFLP{}
	psEnc.SCmn.Arch = arch
	ftmp := float64(int(VARIABLE_HP_MIN_CUTOFF_HZ*(1<<16))) + 0.5
	psEnc.SCmn.Variable_HP_smth1_Q15 = int32(int(uint32(int32(int(silk_lin2log(int32(ftmp)))-(16<<7)))) << 8)
	psEnc.SCmn.Variable_HP_smth2_Q15 = psEnc.SCmn.Variable_HP_smth1_Q15
	psEnc.SCmn.First_frame_after_reset = 1
	ret += VADInit(&psEnc.SCmn.SVAD)
	return ret
}
