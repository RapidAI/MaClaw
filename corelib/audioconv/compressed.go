package audioconv

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	gomp3 "github.com/hajimehoshi/go-mp3"
)

const CompressedAudioDecoderName = "native-go-mp3"

var compressedAudioToWAV = nativeCompressedAudioToWAV

const maxNativeMP3DecodeSeconds = 10 * 60

func HasCompressedAudioDecoder() bool {
	return true
}

func nativeCompressedAudioToWAV(data []byte, format string) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty compressed audio data")
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatMP3, "mpeg", "audio/mpeg", "audio/mp3":
		return mp3ToWAV(data)
	case FormatM4A, "mp4", "audio/mp4", "audio/x-m4a", FormatAAC, "audio/aac":
		return nil, fmt.Errorf("audioconv: native %s decode is not supported", strings.TrimSpace(format))
	default:
		return nil, fmt.Errorf("audioconv: unsupported compressed format %q", format)
	}
}

func mp3ToWAV(data []byte) ([]byte, error) {
	decoder, err := gomp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("audioconv: mp3 decode init failed: %w", err)
	}
	maxDecodedBytes := int64(decoder.SampleRate() * 4 * maxNativeMP3DecodeSeconds)
	pcmStereo, err := io.ReadAll(io.LimitReader(decoder, maxDecodedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("audioconv: mp3 decode failed: %w", err)
	}
	if len(pcmStereo) == 0 {
		return nil, fmt.Errorf("audioconv: mp3 decode produced empty PCM")
	}
	if int64(len(pcmStereo)) > maxDecodedBytes {
		return nil, fmt.Errorf("audioconv: mp3 decoded PCM too large")
	}
	if len(pcmStereo)%4 != 0 {
		return nil, fmt.Errorf("audioconv: malformed mp3 decoded PCM size %d", len(pcmStereo))
	}

	// go-mp3 always emits S16LE stereo frames, even for mono sources.
	pcmMono := stereoToMono(pcmStereo)
	if decoder.SampleRate() != TargetSampleRate {
		pcmMono = resampleS16(pcmMono, decoder.SampleRate(), TargetSampleRate)
	}
	return pcmToWAV(pcmMono, TargetSampleRate, TargetChannels)
}
