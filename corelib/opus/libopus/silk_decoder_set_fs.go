package libopus

func silk_decoder_set_fs(psDec *silk_decoder_state, fs_kHz int, fs_API_Hz int32) int {
	return psDec.SetFS(fs_kHz, fs_API_Hz)
}
