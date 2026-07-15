// corelib/audioconv/convert.go - Unified IM voice to WAV converter.
//
// Converts voice messages from various IM platforms to 16kHz mono 16-bit WAV
// suitable for ASR (speech recognition).
//
// Supported input formats:
//   - silk / silk_v3: WeChat, QQ voice messages
//   - ogg / opus:     Feishu, Telegram voice messages
//   - mp3:             Common browser and gateway audio
//   - wav:            Already WAV, returned as-is (or resampled if needed)
//
// M4A/AAC are recognized so callers get a precise unsupported-format error
// (ErrNativeDecodeUnsupported). Wrong extensions that actually contain a
// supported format are re-detected from magic bytes before failing.
package audioconv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/audioformat"
)

// Format constants for the format parameter.
const (
	FormatSilk = "silk"
	FormatOGG  = "ogg"
	FormatOpus = "opus"
	FormatWAV  = "wav"
	FormatMP3  = "mp3"
	FormatM4A  = "m4a"
	FormatAAC  = "aac"
)

// ToWAV converts voice audio data to 16kHz mono 16-bit WAV.
// The format parameter hints at the source format ("silk", "ogg", "opus", "wav", "mp3", "m4a", "aac").
// M4A/AAC hints are rejected with ErrNativeDecodeUnsupported (typed).
// If format is empty, auto-detection is attempted.
func ToWAV(data []byte, format string) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty input data")
	}
	if len(data) > MaxNativeAudioInputBytes {
		return nil, fmt.Errorf("audioconv: input audio too large")
	}

	format = NormalizeFormatHint(format)
	if format == "" {
		format = detectFormat(data)
	}

	switch format {
	case FormatSilk:
		return silkToWAV(data)
	case FormatOGG, FormatOpus:
		return opusToWAV(data)
	case FormatWAV:
		return ensureWAVFormat(data)
	case FormatMP3:
		return compressedAudioToWAV(data, format)
	case FormatM4A, FormatAAC:
		// Wrong extension safety: if bytes are actually a supported format, decode that.
		if auto := detectFormat(data); auto != "" && auto != FormatM4A && auto != FormatAAC {
			return ToWAV(data, auto)
		}
		return nil, NewNativeDecodeUnsupported(format)
	default:
		return nil, fmt.Errorf("audioconv: unsupported format %q", format)
	}
}

// detectFormat tries to identify the audio format from magic bytes.
func detectFormat(data []byte) string {
	if bytes.HasPrefix(data, []byte("#!SILK_V3")) {
		return FormatSilk
	}
	if len(data) > 1 && data[0] == 0x02 && bytes.HasPrefix(data[1:], []byte("#!SILK_V3")) {
		return FormatSilk
	}
	if bytes.HasPrefix(data, []byte("OggS")) {
		return FormatOGG
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && len(data) > 11 && string(data[8:12]) == "WAVE" {
		return FormatWAV
	}
	if audioformat.LooksLikeADTS(data) {
		return FormatAAC
	}
	if audioformat.LooksLikeMP3(data) {
		return FormatMP3
	}
	if len(data) > 12 && string(data[4:8]) == "ftyp" {
		brand := strings.ToLower(string(data[8:12]))
		if strings.Contains(brand, "m4a") || strings.Contains(brand, "mp4") || strings.Contains(brand, "isom") {
			return FormatM4A
		}
	}
	return ""
}

// ensureWAVFormat checks if the WAV is already 16kHz/mono/16bit. If not,
// it extracts the PCM, resamples, and re-wraps.
func ensureWAVFormat(data []byte) ([]byte, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("audioconv: WAV too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("audioconv: not a valid WAV file")
	}

	audioFormat, channels, sampleRate, bitsPerSample, err := parseWAVFmtChunk(data)
	if err != nil {
		return nil, err
	}
	if audioFormat != 1 {
		return nil, fmt.Errorf("audioconv: unsupported WAV format %d (want PCM)", audioFormat)
	}
	if sampleRate <= 0 {
		return nil, fmt.Errorf("audioconv: invalid WAV sample rate %d", sampleRate)
	}

	pcm, err := extractWAVData(data)
	if err != nil {
		return nil, err
	}
	if sampleRate == TargetSampleRate && channels == TargetChannels && bitsPerSample == TargetBitsPerSamp {
		if err := validatePCMFrameSize(pcm, channels, bitsPerSample); err != nil {
			return nil, err
		}
		return data, nil
	}

	pcm, err = pcmToMonoS16(pcm, channels, bitsPerSample)
	if err != nil {
		return nil, err
	}
	if sampleRate != TargetSampleRate {
		pcm = resampleS16(pcm, sampleRate, TargetSampleRate)
	}

	log.Printf("[audioconv] WAV resampled: %dHz/%dch -> %dHz/1ch", sampleRate, channels, TargetSampleRate)
	return pcmToWAV(pcm, TargetSampleRate, TargetChannels)
}

// parseFmtChunk searches for the "fmt " chunk in a WAV file and returns
// channels, sampleRate, and bitsPerSample.
func parseFmtChunk(data []byte) (channels, sampleRate, bitsPerSample int, err error) {
	_, channels, sampleRate, bitsPerSample, err = parseWAVFmtChunk(data)
	return channels, sampleRate, bitsPerSample, err
}

func parseWAVFmtChunk(data []byte) (audioFormat, channels, sampleRate, bitsPerSample int, err error) {
	for i := 12; i+8 <= len(data); {
		chunkID, chunkStart, chunkEnd, next, err := nextWAVChunk(data, i)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		chunkSize := chunkEnd - chunkStart
		if chunkID == "fmt " && chunkSize >= 16 {
			audioFormat = int(binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2]))
			channels = int(binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16]))
			return audioFormat, channels, sampleRate, bitsPerSample, nil
		}
		i = next
	}
	return 0, 0, 0, 0, fmt.Errorf("audioconv: WAV fmt chunk not found")
}

// extractWAVData finds and returns the raw PCM data from a WAV file.
func extractWAVData(data []byte) ([]byte, error) {
	for i := 12; i+8 <= len(data); {
		chunkID, chunkStart, chunkEnd, next, err := nextWAVChunk(data, i)
		if err != nil {
			return nil, err
		}
		if chunkID == "data" {
			return data[chunkStart:chunkEnd], nil
		}
		i = next
	}
	return nil, fmt.Errorf("audioconv: WAV data chunk not found")
}

func nextWAVChunk(data []byte, offset int) (chunkID string, chunkStart, chunkEnd, next int, err error) {
	if offset < 0 || offset+8 > len(data) {
		return "", 0, 0, 0, fmt.Errorf("audioconv: malformed WAV chunk offset %d", offset)
	}
	chunkID = string(data[offset : offset+4])
	chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
	chunkStart = offset + 8
	if chunkSize > len(data)-chunkStart {
		return "", 0, 0, 0, fmt.Errorf("audioconv: malformed WAV %s chunk size %d", strings.TrimSpace(chunkID), chunkSize)
	}
	chunkEnd = chunkStart + chunkSize
	next = chunkEnd
	if chunkSize%2 != 0 {
		next++
	}
	if next > len(data) {
		return "", 0, 0, 0, fmt.Errorf("audioconv: malformed WAV %s chunk padding", strings.TrimSpace(chunkID))
	}
	return chunkID, chunkStart, chunkEnd, next, nil
}

func validatePCMFrameSize(pcm []byte, channels, bitsPerSample int) error {
	if channels < 1 || bitsPerSample <= 0 || bitsPerSample%8 != 0 {
		return fmt.Errorf("audioconv: malformed WAV PCM format %dch/%dbit", channels, bitsPerSample)
	}
	frameSize := channels * (bitsPerSample / 8)
	if frameSize <= 0 || len(pcm)%frameSize != 0 {
		return fmt.Errorf("audioconv: malformed WAV PCM size %d for %dch/%dbit", len(pcm), channels, bitsPerSample)
	}
	if len(pcm) == 0 {
		return fmt.Errorf("audioconv: empty WAV PCM data")
	}
	return nil
}

// stereoToMono converts interleaved stereo S16LE to mono by averaging.
func stereoToMono(pcm []byte) []byte {
	samples := len(pcm) / 4
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		l := int(readS16LE(pcm, i*2))
		r := int(readS16LE(pcm, i*2+1))
		avg := int16((l + r) / 2)
		writeS16LE(out, i, avg)
	}
	return out
}

func pcmToMonoS16(pcm []byte, channels, bitsPerSample int) ([]byte, error) {
	if channels < 1 || channels > 2 {
		return nil, fmt.Errorf("audioconv: unsupported WAV channels %d", channels)
	}
	bytesPerSample := bitsPerSample / 8
	switch bitsPerSample {
	case 8, 16, 24, 32:
	default:
		return nil, fmt.Errorf("audioconv: unsupported WAV bit depth %d", bitsPerSample)
	}
	if err := validatePCMFrameSize(pcm, channels, bitsPerSample); err != nil {
		return nil, err
	}
	frameSize := channels * bytesPerSample
	frames := len(pcm) / frameSize
	out := make([]byte, frames*2)
	for frame := 0; frame < frames; frame++ {
		base := frame * frameSize
		sum := 0
		for ch := 0; ch < channels; ch++ {
			sum += decodePCMSampleToS16(pcm[base+ch*bytesPerSample:], bitsPerSample)
		}
		writeS16LE(out, frame, int16(sum/channels))
	}
	return out, nil
}

func decodePCMSampleToS16(sample []byte, bitsPerSample int) int {
	switch bitsPerSample {
	case 8:
		return (int(sample[0]) - 128) << 8
	case 16:
		return int(int16(binary.LittleEndian.Uint16(sample[:2])))
	case 24:
		v := int32(uint32(sample[0]) | uint32(sample[1])<<8 | uint32(sample[2])<<16)
		if v&0x800000 != 0 {
			v |= ^int32(0xffffff)
		}
		return int(v >> 8)
	case 32:
		return int(int32(binary.LittleEndian.Uint32(sample[:4])) >> 16)
	default:
		return 0
	}
}
