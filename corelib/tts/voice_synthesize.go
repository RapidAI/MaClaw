package tts

import (
	"fmt"
	"unicode/utf8"
)

// TextSynthesizer is the minimal TTS surface used for long-form synthesis.
// *Manager implements this; tests/fakes can implement SynthesizeText only.
type TextSynthesizer interface {
	SynthesizeText(text string) ([]byte, error)
}

// SynthesizeVoiceOGG is the high-level API for TTS → OGG Opus.
// Long text is split into semantic chunks, synthesized, and concatenated into
// one WAV/OGG so callers that expect a single payload keep working.
// maxChunkRunes controls per-segment size (0 = DefaultSpeechChunkRunes; clamped
// to MaxSafeSpeechChunkRunes). It is no longer a total-length truncate budget.
// Also returns the intermediate WAV for callers that need it (e.g. desktop playback).
func SynthesizeVoiceOGG(mgr TextSynthesizer, text string, maxChunkRunes int) (ogg []byte, wav []byte, err error) {
	wav, _, err = SynthesizeSpeechLongWAV(mgr, text, maxChunkRunes)
	if err != nil {
		return nil, nil, err
	}
	ogg, err = EncodeWAVToOpus(wav)
	if err != nil {
		// Return WAV as fallback — caller can use it for platforms that accept WAV.
		return nil, wav, fmt.Errorf("opus encode failed: %w", err)
	}
	return ogg, wav, nil
}

// SynthesizeSpeechLongWAV synthesizes long text into a single WAV by semantic
// chunking, per-chunk synthesis, and WAV concatenation with short silence gaps.
// Returns the number of semantic chunks used.
func SynthesizeSpeechLongWAV(mgr TextSynthesizer, text string, maxChunkRunes int) (wav []byte, chunks int, err error) {
	parts := SplitSpeechChunks(text, maxChunkRunes)
	return SynthesizeSpeechParts(mgr, parts)
}

// SynthesizeSpeechParts synthesizes pre-split semantic parts (avoids double-splitting).
func SynthesizeSpeechParts(mgr TextSynthesizer, parts []string) (wav []byte, chunks int, err error) {
	if mgr == nil {
		return nil, 0, fmt.Errorf("TTS manager not available")
	}
	if len(parts) == 0 {
		return nil, 0, fmt.Errorf("text is empty after cleaning")
	}

	// Prefer float PCM path when available (avoids WAV encode/decode per chunk).
	if audioSynth, ok := mgr.(interface {
		SynthesizeAudio(string) ([]float32, int, error)
	}); ok {
		return synthesizeSpeechLongFromPCM(audioSynth, parts)
	}

	wavs := make([][]byte, 0, len(parts))
	for i, part := range parts {
		w, synthErr := mgr.SynthesizeText(part)
		if synthErr != nil {
			return nil, len(parts), fmt.Errorf("synthesize chunk %d/%d: %w", i+1, len(parts), synthErr)
		}
		if len(w) > 0 {
			wavs = append(wavs, w)
		}
	}
	if len(wavs) == 0 {
		return nil, len(parts), fmt.Errorf("synthesize produced no audio")
	}
	joined, joinErr := ConcatenateWAVs(wavs, SpeechChunkSilenceMs)
	if joinErr != nil {
		return nil, len(parts), joinErr
	}
	return joined, len(parts), nil
}

type pcmSynthesizer interface {
	SynthesizeAudio(string) ([]float32, int, error)
}

func synthesizeSpeechLongFromPCM(mgr pcmSynthesizer, parts []string) ([]byte, int, error) {
	// Rough pre-size: ~0.12s/char * 24kHz for Chinese is overly optimistic; grow as needed.
	var allPCM []float32
	sampleRate := 0
	silenceSamples := 0
	for i, part := range parts {
		pcm, rate, synthErr := mgr.SynthesizeAudio(part)
		if synthErr != nil {
			return nil, len(parts), fmt.Errorf("synthesize chunk %d/%d: %w", i+1, len(parts), synthErr)
		}
		if sampleRate == 0 {
			sampleRate = rate
			silenceSamples = rate * SpeechChunkSilenceMs / 1000
			// Preallocate based on first chunk density.
			if len(pcm) > 0 && len(part) > 0 {
				est := len(pcm) * len(parts) * 12 / 10
				if silenceSamples > 0 {
					est += silenceSamples * (len(parts) - 1)
				}
				allPCM = make([]float32, 0, est)
			}
		}
		allPCM = append(allPCM, pcm...)
		if i < len(parts)-1 && silenceSamples > 0 {
			allPCM = append(allPCM, make([]float32, silenceSamples)...)
		}
	}
	if len(allPCM) == 0 {
		return nil, len(parts), fmt.Errorf("synthesize produced no audio")
	}
	return EncodeWAV(allPCM, sampleRate), len(parts), nil
}

// ConcatenateWAVs joins 16-bit PCM WAV segments with optional silence gaps (ms).
func ConcatenateWAVs(wavs [][]byte, gapMs int) ([]byte, error) {
	if len(wavs) == 0 {
		return nil, fmt.Errorf("no wav segments")
	}
	if len(wavs) == 1 {
		return wavs[0], nil
	}
	var all []int16
	sampleRate := 0
	for i, w := range wavs {
		pcm, rate, channels, err := parseWAVS16(w)
		if err != nil {
			return nil, fmt.Errorf("parse wav segment %d: %w", i+1, err)
		}
		if channels == 2 {
			pcm = downmixStereoS16(pcm)
		} else if channels != 1 {
			return nil, fmt.Errorf("unsupported channels %d in segment %d", channels, i+1)
		}
		if sampleRate == 0 {
			sampleRate = rate
			gap := 0
			if gapMs > 0 {
				gap = sampleRate * gapMs / 1000
			}
			capHint := len(pcm) * len(wavs)
			if gap > 0 {
				capHint += gap * (len(wavs) - 1)
			}
			all = make([]int16, 0, capHint)
		} else if rate != sampleRate {
			pcm = resampleS16Samples(pcm, rate, sampleRate)
		}
		all = append(all, pcm...)
		if i < len(wavs)-1 && gapMs > 0 && sampleRate > 0 {
			all = append(all, make([]int16, sampleRate*gapMs/1000)...)
		}
	}
	return encodeWAVS16(all, sampleRate), nil
}

// encodeWAVS16 writes mono 16-bit PCM WAV without a float conversion round-trip.
func encodeWAVS16(samples []int16, sampleRate int) []byte {
	nSamples := len(samples)
	dataSize := nSamples * 2
	fileSize := 44 + dataSize
	buf := make([]byte, fileSize)
	copy(buf[0:4], "RIFF")
	le32(buf[4:], uint32(fileSize-8))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	le32(buf[16:], 16)
	le16(buf[20:], 1) // PCM
	le16(buf[22:], 1) // mono
	le32(buf[24:], uint32(sampleRate))
	le32(buf[28:], uint32(sampleRate*2))
	le16(buf[32:], 2)
	le16(buf[34:], 16)
	copy(buf[36:40], "data")
	le32(buf[40:], uint32(dataSize))
	for i, s := range samples {
		le16(buf[44+i*2:], uint16(s))
	}
	return buf
}

// SynthesizeSpeechSegments returns one WAV per semantic chunk for sequential
// playback (desktop queue). Empty result means nothing to speak.
func SynthesizeSpeechSegments(mgr TextSynthesizer, text string, maxChunkRunes int) (wavs [][]byte, parts []string, err error) {
	if mgr == nil {
		return nil, nil, fmt.Errorf("TTS manager not available")
	}
	parts = SplitSpeechChunks(text, maxChunkRunes)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("text is empty after cleaning")
	}
	wavs = make([][]byte, 0, len(parts))
	for i, part := range parts {
		wav, synthErr := mgr.SynthesizeText(part)
		if synthErr != nil {
			return wavs, parts, fmt.Errorf("synthesize chunk %d/%d: %w", i+1, len(parts), synthErr)
		}
		if len(wav) > 0 {
			wavs = append(wavs, wav)
		}
	}
	return wavs, parts, nil
}

// SynthesizeVoiceSegment truncates a single short segment (legacy helper).
// Prefer SplitSpeechChunks + SynthesizeSpeechSegments for long-form reading.
func SynthesizeVoiceSegment(mgr TextSynthesizer, text string, maxRunes int) (wav []byte, err error) {
	if mgr == nil {
		return nil, fmt.Errorf("TTS manager not available")
	}
	if maxRunes <= 0 {
		maxRunes = DefaultSpeechChunkRunes
	}
	cleaned := CleanForSpeech(text)
	if cleaned == "" {
		return nil, fmt.Errorf("text is empty after cleaning")
	}
	if utf8.RuneCountInString(cleaned) > maxRunes {
		cleaned = TruncateRunesSmart(cleaned, maxRunes)
	}
	return mgr.SynthesizeText(cleaned)
}
