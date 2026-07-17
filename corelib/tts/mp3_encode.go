package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

// maxWAVToMP3EncodeSeconds bounds in-memory encode for short voice replies (TTS/IM).
const maxWAVToMP3EncodeSeconds = 10 * 60

// maxArchiveWAVToMP3EncodeSeconds bounds archival convert for desktop record_audio
// products (matches the 3h UI hard cap). Longer inputs should fall back to ffmpeg.
const maxArchiveWAVToMP3EncodeSeconds = 3 * 60 * 60

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
	return encodeWAVToMP3Context(ctx, wavData, maxWAVToMP3EncodeSeconds)
}

// maxArchiveWAVFileBytes is a coarse pre-read guard (~3h mono 16kHz 16-bit PCM
// + WAV header slack). Avoids loading multi-GB files into memory before decode.
const maxArchiveWAVFileBytes = int64(400 * 1024 * 1024)

// EncodeWAVFileToMP3Archive converts an on-disk product WAV into a sibling MP3
// for long-form recording archival. Uses a higher duration budget than TTS voice.
// Writes atomically via a temp file then rename. Caller should treat errors as
// best-effort (original WAV remains the source of truth).
func EncodeWAVFileToMP3Archive(ctx context.Context, wavPath, mp3Path string) error {
	wavPath = strings.TrimSpace(wavPath)
	mp3Path = strings.TrimSpace(mp3Path)
	if wavPath == "" || mp3Path == "" {
		return fmt.Errorf("mp3 archive: empty path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Fast path: source already MP3 (should not happen for record products).
	if strings.EqualFold(filepath.Ext(wavPath), ".mp3") {
		if absSrc, err := filepath.Abs(wavPath); err == nil {
			if absDst, err2 := filepath.Abs(mp3Path); err2 == nil && absSrc == absDst {
				return nil
			}
		}
		data, err := os.ReadFile(wavPath)
		if err != nil {
			return fmt.Errorf("mp3 archive: read source: %w", err)
		}
		return writeFileAtomic(mp3Path, data)
	}

	// Stat first: reject oversized inputs without a full ReadFile.
	fi, err := os.Stat(wavPath)
	if err != nil {
		return fmt.Errorf("mp3 archive: stat wav: %w", err)
	}
	if fi.Size() < 44 {
		return fmt.Errorf("mp3 archive: wav too short")
	}
	if fi.Size() > maxArchiveWAVFileBytes {
		return fmt.Errorf("mp3 archive: wav file too large (%d bytes)", fi.Size())
	}

	data, err := os.ReadFile(wavPath)
	if err != nil {
		return fmt.Errorf("mp3 archive: read wav: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mp3Data, err := encodeWAVToMP3Context(ctx, data, maxArchiveWAVToMP3EncodeSeconds)
	if err != nil {
		return err
	}
	return writeFileAtomic(mp3Path, mp3Data)
}

func encodeWAVToMP3Context(ctx context.Context, wavData []byte, maxSeconds int) ([]byte, error) {
	if len(wavData) == 0 {
		return nil, fmt.Errorf("mp3 encode: empty wav data")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decoded, err := decodeWAVPCM(wavData, maxSeconds)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// shine.Write is not cancelable mid-stream; run it in a goroutine so a parent
	// timeout can return promptly (caller can free/abandon without waiting for
	// the full encode). The goroutine may still finish and drop the result.
	type encodeResult struct {
		data []byte
		err  error
	}
	done := make(chan encodeResult, 1)
	go func() {
		var out bytes.Buffer
		encoder := mp3.NewEncoder(decoded.sampleRate, decoded.channels)
		if encoder == nil {
			done <- encodeResult{err: fmt.Errorf("mp3 encode: failed to initialize encoder")}
			return
		}
		if err := encoder.Write(&out, decoded.samples); err != nil {
			done <- encodeResult{err: fmt.Errorf("mp3 encode: %w", err)}
			return
		}
		// Drain the encoder bit-cache so the final frame is not truncated.
		if err := encoder.Flush(&out); err != nil {
			done <- encodeResult{err: fmt.Errorf("mp3 encode flush: %w", err)}
			return
		}
		mp3Data := out.Bytes()
		if len(mp3Data) == 0 {
			done <- encodeResult{err: fmt.Errorf("mp3 encode: empty mp3 output")}
			return
		}
		done <- encodeResult{data: mp3Data}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		return r.data, nil
	}
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mp3 archive: mkdir: %w", err)
	}
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("mp3 archive: create temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("mp3 archive: write temp: %w", err)
	}
	// Flush before rename so a crash cannot leave a truncated product that looks final.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("mp3 archive: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("mp3 archive: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Cross-volume fallback: copy then remove temp.
		if err2 := copyFileContents(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("mp3 archive: finalize: %w", err2)
		}
		_ = os.Remove(tmp)
	}
	return nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

type wavPCM struct {
	sampleRate int
	channels   int
	samples    []int16
}

func decodeWAVPCM(wavData []byte, maxSeconds int) (wavPCM, error) {
	if maxSeconds <= 0 {
		maxSeconds = maxWAVToMP3EncodeSeconds
	}
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
	if err := validateWAVPCMForMP3(decoder, maxSeconds); err != nil {
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

func validateWAVPCMForMP3(decoder *wav.Decoder, maxSeconds int) error {
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
	if maxSeconds <= 0 {
		maxSeconds = maxWAVToMP3EncodeSeconds
	}
	maxPCMBytes := int(decoder.SampleRate) * frameSize * maxSeconds
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
