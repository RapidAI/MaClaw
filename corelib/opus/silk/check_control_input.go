package silk

func CheckControlInput(encControl *EncControlStruct) int {
	if int(encControl.API_sampleRate) != 8000 && int(encControl.API_sampleRate) != 12000 && int(encControl.API_sampleRate) != 16000 && int(encControl.API_sampleRate) != 24000 && int(encControl.API_sampleRate) != 32000 && int(encControl.API_sampleRate) != 44100 && int(encControl.API_sampleRate) != 48000 || int(encControl.DesiredInternalSampleRate) != 8000 && int(encControl.DesiredInternalSampleRate) != 12000 && int(encControl.DesiredInternalSampleRate) != 16000 || int(encControl.MaxInternalSampleRate) != 8000 && int(encControl.MaxInternalSampleRate) != 12000 && int(encControl.MaxInternalSampleRate) != 16000 || int(encControl.MinInternalSampleRate) != 8000 && int(encControl.MinInternalSampleRate) != 12000 && int(encControl.MinInternalSampleRate) != 16000 || int(encControl.MinInternalSampleRate) > int(encControl.DesiredInternalSampleRate) || int(encControl.MaxInternalSampleRate) < int(encControl.DesiredInternalSampleRate) || int(encControl.MinInternalSampleRate) > int(encControl.MaxInternalSampleRate) {
		return -102
	}
	if encControl.PayloadSize_ms != 10 && encControl.PayloadSize_ms != 20 && encControl.PayloadSize_ms != 40 && encControl.PayloadSize_ms != 60 {
		return -103
	}
	if encControl.PacketLossPercentage < 0 || encControl.PacketLossPercentage > 100 {
		return -105
	}
	if encControl.UseDTX < 0 || encControl.UseDTX > 1 {
		return -108
	}
	if encControl.UseCBR < 0 || encControl.UseCBR > 1 {
		return -109
	}
	if encControl.UseInBandFEC < 0 || encControl.UseInBandFEC > 1 {
		return -107
	}
	if int(encControl.NChannelsAPI) < 1 || int(encControl.NChannelsAPI) > ENCODER_NUM_CHANNELS {
		return -111
	}
	if int(encControl.NChannelsInternal) < 1 || int(encControl.NChannelsInternal) > ENCODER_NUM_CHANNELS {
		return -111
	}
	if int(encControl.NChannelsInternal) > int(encControl.NChannelsAPI) {
		return -111
	}
	if encControl.Complexity < 0 || encControl.Complexity > 10 {
		return -106
	}
	return SILK_NO_ERROR
}
