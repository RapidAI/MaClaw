package libopus

func silk_resampler_init(S *silk_resampler_state_struct, Fs_Hz_in int32, Fs_Hz_out int32, forEnc int) int {
	return S.Init(Fs_Hz_in, Fs_Hz_out, forEnc)
}
func silk_resampler(S *silk_resampler_state_struct, out []int16, in []int16, inLen int32) int {
	return S.Resample(out, in, inLen)
}
