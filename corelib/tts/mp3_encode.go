package tts

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/audioformat"
	"github.com/braheezy/shine-mp3/pkg/mp3"
	"github.com/go-audio/wav"
)

type PlayableVoiceFile struct {
	Data      []byte
	Name      string
	MIME      string
	Converted bool
}

const MP3EncoderName = "shine-mp3"

const maxWAVToMP3EncodeSeconds = 10 * 60

// HasMP3Encoder reports whether the built-in pure Go WAV-to-MP3 encoder is available.
func HasMP3Encoder() bool {
	return true
}

// PreparePlayableVoiceMP3 is the shared GUI/server voice reply fallback:
// pass through existing MP3 data, or convert WAV data to voice.mp3 in-process.
func PreparePlayableVoiceMP3(ctx context.Context, voiceFileName string, voiceBytes []byte) (PlayableVoiceFile, error) {
	if len(voiceBytes) == 0 {
		return PlayableVoiceFile{}, fmt.Errorf("empty voice data")
	}
	ext := strings.ToLower(filepath.Ext(voiceFileName))
	if audioformat.LooksLikeMP3(voiceBytes) {
		return PlayableVoiceFile{Data: voiceBytes, Name: "voice.mp3", MIME: "audio/mpeg"}, nil
	}
	if ext != ".wav" && !bytes.HasPrefix(voiceBytes, []byte("RIFF")) {
		return PlayableVoiceFile{}, fmt.Errorf("unsupported playable fallback source %q", voiceFileName)
	}
	mp3, err := EncodeWAVToMP3Context(ctx, voiceBytes)
	if err != nil {
		return PlayableVoiceFile{}, err
	}
	if len(mp3) == 0 {
		return PlayableVoiceFile{}, fmt.Errorf("mp3 encoder returned empty data")
	}
	return PlayableVoiceFile{Data: mp3, Name: "voice.mp3", MIME: "audio/mpeg", Converted: true}, nil
}

func HasMP3FrameHeader(data []byte) bool {
	return audioformat.LooksLikeMP3Frame(data)
}

// EncodeWAVToMP3 converts WAV bytes to MP3 using the built-in pure Go encoder.
func EncodeWAVToMP3(wavData []byte) ([]byte, error) {
	return EncodeWAVToMP3Context(context.Background(), wavData)
}

// EncodeWAVToMP3Context converts WAV bytes to MP3 fully in-process.
func EncodeWAVToMP3Context(ctx context.Context, wavData []byte) ([]byte, error) {
	if len(wavData) == 0 {
		return nil, fmt.Errorf("mp3 encode: empty wav data")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decoded, err := decodeWAVPCM(wavData)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := mp3.NewEncoder(decoded.sampleRate, decoded.channels)
	if encoder == nil {
		return nil, fmt.Errorf("mp3 encode: failed to initialize encoder")
	}
	if err := encoder.Write(&out, decoded.samples); err != nil {
		return nil, fmt.Errorf("mp3 encode: %w", err)
	}
	mp3Data := out.Bytes()
	if len(mp3Data) == 0 {
		return nil, fmt.Errorf("mp3 encode: empty mp3 output")
	}
	return mp3Data, nil
}

type wavPCM struct {
	sampleRate int
	channels   int
	samples    []int16
}

func decodeWAVPCM(wavData []byte) (wavPCM, error) {
	decoder := wav.NewDecoder(bytes.NewReader(wavData))
	if !decoder.IsValidFile() {
		return wavPCM{}, fmt.Errorf("mp3 encode: invalid wav data")
	}
	if decoder.WavAudioFormat != 1 {
		return wavPCM{}, fmt.Errorf("mp3 encode: unsupported wav encoding format %d", decoder.WavAudioFormat)
	}
	if decoder.NumChans < 1 || decoder.NumChans > 2 {
		return wavPCM{}, fmt.Errorf("mp3 encode: unsupported channel count %d", decoder.NumChans)
	}
	sampleRate := int(decoder.SampleRate)
	if !isSupportedMP3SampleRate(sampleRate) {
		return wavPCM{}, fmt.Errorf("mp3 encode: unsupported sample rate %d", sampleRate)
	}
	if err := validateWAVPCMForMP3(decoder); err != nil {
		return wavPCM{}, err
	}
	pcm, err := decoder.FullPCMBuffer()
	if err != nil {
		return wavPCM{}, fmt.Errorf("mp3 encode: decode wav: %w", err)
	}
	if pcm == nil || len(pcm.Data) == 0 {
		return wavPCM{}, fmt.Errorf("mp3 encode: empty wav pcm data")
	}
	if len(pcm.Data)%int(decoder.NumChans) != 0 {
		return wavPCM{}, fmt.Errorf("mp3 encode: malformed wav pcm sample count %d for %d channels", len(pcm.Data), decoder.NumChans)
	}
	samples := make([]int16, len(pcm.Data))
	for i, sample := range pcm.Data {
		samples[i] = pcmSampleToS16(sample, pcm.SourceBitDepth)
	}
	return wavPCM{
		sampleRate: sampleRate,
		channels:   int(decoder.NumChans),
		samples:    samples,
	}, nil
}

func validateWAVPCMForMP3(decoder *wav.Decoder) error {
	bitDepth := int(decoder.BitDepth)
	switch bitDepth {
	case 8, 16, 24, 32:
	default:
		return fmt.Errorf("mp3 encode: unsupported wav bit depth %d", bitDepth)
	}
	if err := decoder.FwdToPCM(); err != nil {
		return fmt.Errorf("mp3 encode: decode wav: %w", err)
	}
	if decoder.PCMSize <= 0 {
		return fmt.Errorf("mp3 encode: empty wav pcm data")
	}
	bytesPerSample := (bitDepth + 7) / 8
	frameSize := int(decoder.NumChans) * bytesPerSample
	if frameSize <= 0 || decoder.PCMSize%frameSize != 0 {
		return fmt.Errorf("mp3 encode: malformed wav pcm size %d for %dch/%dbit", decoder.PCMSize, decoder.NumChans, bitDepth)
	}
	maxPCMBytes := int(decoder.SampleRate) * frameSize * maxWAVToMP3EncodeSeconds
	if decoder.PCMSize > maxPCMBytes {
		return fmt.Errorf("mp3 encode: wav pcm too large")
	}
	return nil
}

func pcmSampleToS16(sample int, bitDepth int) int16 {
	switch bitDepth {
	case 8:
		return clampInt16((sample - 128) << 8)
	case 16:
		return clampInt16(sample)
	case 24:
		return clampInt16(sample >> 8)
	case 32:
		return clampInt16(sample >> 16)
	default:
		return clampInt16(sample)
	}
}

func clampInt16(v int) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

func isSupportedMP3SampleRate(sampleRate int) bool {
	switch sampleRate {
	case 16000, 22050, 24000, 32000, 44100, 48000:
		return true
	default:
		return false
	}
}
