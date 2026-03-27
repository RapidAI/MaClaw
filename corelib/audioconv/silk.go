// corelib/audioconv/silk.go — Silk v3 to WAV converter (pure Go).
//
// Uses github.com/wdvxdr1123/go-silk (ccgo-transpiled, no CGo needed).
// WeChat and QQ voice messages use Silk v3 encoding (sometimes with a 0x02
// prefix before the #!SILK_V3 header).
// The decoded PCM is resampled to 16kHz mono for ASR.
package audioconv

import (
	"bytes"
	"fmt"

	silk "github.com/wdvxdr1123/go-silk"
)

const silkDecodeSampleRate = 24000

// silkToWAV decodes Silk v3 audio data to 16kHz mono 16-bit WAV.
// The input may have an optional 0x02 prefix (WeChat format).
func silkToWAV(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty silk data")
	}

	// Strip optional 0x02 prefix that WeChat adds before #!SILK_V3 header.
	raw := data
	if len(raw) > 1 && raw[0] == 0x02 {
		raw = raw[1:]
	}

	// Verify SILK_V3 header.
	if !bytes.HasPrefix(raw, []byte("#!SILK_V3")) {
		return nil, fmt.Errorf("audioconv: not a valid silk v3 file (missing header)")
	}

	// Decode silk to PCM (S16LE).
	pcm, err := silk.DecodeSilkBuffToPcm(data, silkDecodeSampleRate)
	if err != nil {
		return nil, fmt.Errorf("audioconv: silk decode failed: %w", err)
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("audioconv: silk decode produced empty PCM")
	}

	// Resample 24kHz → 16kHz for ASR.
	pcm = resampleS16(pcm, silkDecodeSampleRate, TargetSampleRate)

	return pcmToWAV(pcm, TargetSampleRate, TargetChannels)
}
