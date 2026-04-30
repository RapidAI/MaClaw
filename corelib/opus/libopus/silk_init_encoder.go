package libopus



func silk_init_encoder(psEnc *silk_encoder_state_FLP, arch int) int {
	return psEnc.Init(arch)
}
