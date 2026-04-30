package libopus

func silk_init_decoder(psDec *silk_decoder_state) int {
	psDec.Init()
	return 0
}
