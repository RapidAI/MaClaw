package silk

func (psDec *DecoderState) Init() {
	*psDec = DecoderState{}
	psDec.First_frame_after_reset = 1
	psDec.Prev_gain_Q16 = 65536
	psDec.Arch = opus_select_arch()
	CNG_Reset(psDec)
	PLC_Reset(psDec)
}
