package libopus

// api.go provides exported Go-friendly wrappers around the transpiled
// C-style opus functions. This is the public API for the opus encoder.

// OpusEncoderCreate creates a new Opus encoder.
// Fs: sample rate (48000, 24000, 16000, 12000, or 8000).
// channels: 1 (mono) or 2 (stereo).
// application: OPUS_APPLICATION_VOIP, OPUS_APPLICATION_AUDIO, or OPUS_APPLICATION_RESTRICTED_LOWDELAY.
// Returns the encoder and sets errCode to 0 on success, negative on error.
func OpusEncoderCreate(Fs int32, channels int, application int, errCode *int) *OpusEncoder {
	return opus_encoder_create(Fs, channels, application, errCode)
}

// OpusEncoderDestroy frees an Opus encoder.
func OpusEncoderDestroy(enc *OpusEncoder) {
	opus_encoder_destroy(enc)
}

// OpusEncodeFloat encodes a frame of float32 PCM audio.
// pcm: input audio (frame_size * channels samples).
// frame_size: number of samples per channel (must be 2.5, 5, 10, 20, 40, or 60 ms worth).
// data: output buffer for the encoded packet.
// maxDataBytes: size of the output buffer.
// Returns the number of bytes written to data, or a negative error code.
func OpusEncodeFloat(enc *OpusEncoder, pcm *float32, frame_size int, data *uint8, maxDataBytes int32) int32 {
	return opus_encode_float(enc, pcm, frame_size, data, maxDataBytes)
}

// OpusEncoderCtl sets encoder parameters.
// request: the CTL request constant (e.g., OPUS_SET_BITRATE_REQUEST).
// value: the parameter value.
func OpusEncoderCtl(enc *OpusEncoder, request int, value int32) int {
	// The top-level opus_encoder_ctl uses ap.Arg().(int32) for set requests.
	return opus_encoder_ctl(enc, request, value)
}

// ---------------------------------------------------------------------------
// Decoder API
// ---------------------------------------------------------------------------

// OpusDecoderCreate creates a new Opus decoder.
// Fs: sample rate (48000, 24000, 16000, 12000, or 8000).
// channels: 1 (mono) or 2 (stereo).
func OpusDecoderCreate(Fs int32, channels int, errCode *int) *OpusDecoder {
	return opus_decoder_create(Fs, channels, errCode)
}

// OpusDecoderDestroy frees an Opus decoder.
func OpusDecoderDestroy(dec *OpusDecoder) {
	opus_decoder_destroy(dec)
}

// OpusDecode decodes an Opus packet to S16LE PCM.
// data: encoded Opus packet.
// pcm: output buffer (frame_size * channels int16 samples).
// frame_size: max number of samples per channel to decode.
// Returns the number of decoded samples per channel, or a negative error code.
func OpusDecode(dec *OpusDecoder, data []byte, pcm []int16, frame_size int) int {
	var dataPtr *uint8
	var dataLen int32
	if len(data) > 0 {
		dataPtr = &data[0]
		dataLen = int32(len(data))
	}
	var pcmPtr *int16
	if len(pcm) > 0 {
		pcmPtr = &pcm[0]
	}
	return opus_decode(dec, dataPtr, dataLen, pcmPtr, frame_size, 0)
}

// OpusGetNbSamples returns the number of samples in an Opus packet.
func OpusGetNbSamples(dec *OpusDecoder, data []byte) int {
	if len(data) == 0 {
		return -1
	}
	return opus_decoder_get_nb_samples(dec, data, int32(len(data)))
}
