// corelib/audioconv/opus.go — OGG/Opus to WAV converter.
//
// Uses the built-in pure Go Opus decoder (corelib/opus/libopus, forked from
// gotranspile/opus) and the local OGG demuxer to extract packets.
// Feishu and Telegram voice messages use OGG/Opus encoding.
package audioconv

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/opus/libopus"
)

const maxNativeOpusDecodeSeconds = 10 * 60

// opusToWAV decodes OGG/Opus audio data to 16kHz mono 16-bit WAV.
func opusToWAV(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty opus data")
	}

	// Extract Opus packets from OGG container.
	packets, err := extractOggOpusPackets(data)
	if err != nil {
		return nil, fmt.Errorf("audioconv: ogg demux failed: %w", err)
	}

	// Create decoder at 48kHz mono (Opus native rate).
	// We'll resample to 16kHz after decoding.
	var errCode int
	dec := libopus.OpusDecoderCreate(48000, 1, &errCode)
	if errCode != 0 || dec == nil {
		return nil, fmt.Errorf("audioconv: opus decoder create failed: error %d", errCode)
	}
	defer libopus.OpusDecoderDestroy(dec)

	// Max frame: 60ms at 48kHz = 2880 samples.
	const maxFrameSamples = 2880
	frameBuf := make([]int16, maxFrameSamples)

	var allPCM []byte
	maxDecodedBytes := 48000 * 2 * maxNativeOpusDecodeSeconds

	for _, pkt := range packets {
		if len(pkt) == 0 {
			continue
		}
		// Skip OpusHead and OpusTags headers.
		if len(pkt) >= 8 && (string(pkt[:8]) == "OpusHead" || string(pkt[:8]) == "OpusTags") {
			continue
		}

		n := libopus.OpusDecode(dec, pkt, frameBuf, maxFrameSamples)
		if n <= 0 {
			continue // skip decode errors
		}

		// Convert int16 samples to S16LE bytes.
		for i := 0; i < n; i++ {
			s := frameBuf[i]
			allPCM = append(allPCM, byte(s), byte(s>>8))
		}
		if len(allPCM) > maxDecodedBytes {
			return nil, fmt.Errorf("audioconv: opus decoded PCM too large")
		}
	}

	if len(allPCM) == 0 {
		return nil, fmt.Errorf("audioconv: opus decode produced empty PCM")
	}

	// Resample from 48kHz to 16kHz.
	allPCM = resampleS16(allPCM, 48000, TargetSampleRate)

	return pcmToWAV(allPCM, TargetSampleRate, TargetChannels)
}
