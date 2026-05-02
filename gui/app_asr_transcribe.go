package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/vad"
)

var (
	asrInstance *asr.MoonshineModel
	asrMu       sync.Mutex
)

const asrRawFallbackMinRMS = 0.0025
const asrPreferRawMaxSec = 15.0
const asrVadMinFilteredSec = 0.5
const asrVadMinSpeechRatio = 0.25
const asrVadMaxSpeechRatio = 0.98
const asrVadMinRMSRatio = 0.35

type wavHeaderInfo struct {
	Format        int
	Channels      int
	SampleRate    int
	BitsPerSample int
	DataBytes     int
	OK            bool
}

func inspectWAVHeader(data []byte) wavHeaderInfo {
	info := wavHeaderInfo{}
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return info
	}
	for i := 12; i+8 <= len(data); {
		id := string(data[i : i+4])
		sz := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if sz < 0 || i+8+sz > len(data)+1 {
			break
		}
		if id == "fmt " && sz >= 16 && i+8+16 <= len(data) {
			p := i + 8
			info.Format = int(binary.LittleEndian.Uint16(data[p : p+2]))
			info.Channels = int(binary.LittleEndian.Uint16(data[p+2 : p+4]))
			info.SampleRate = int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
			info.BitsPerSample = int(binary.LittleEndian.Uint16(data[p+14 : p+16]))
		}
		if id == "data" {
			info.DataBytes = sz
		}
		if info.Format != 0 && info.DataBytes > 0 {
			info.OK = true
			return info
		}
		i += 8 + sz
		if sz%2 != 0 {
			i++
		}
	}
	return info
}

func pcmRMS(pcm []float32) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range pcm {
		s := float64(sample)
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func compactASRRunes(text string) []rune {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
		}
	}
	return out
}

func hasRepeatedPhrase(runes []rune) bool {
	if len(runes) < 12 {
		return false
	}
	maxSize := len(runes) / 2
	if maxSize > 32 {
		maxSize = 32
	}
	for size := 2; size <= maxSize; size++ {
		minRepeats := 4
		if size >= 5 {
			minRepeats = 3
		}
		for start := 0; start+size*minRepeats <= len(runes); start++ {
			phrase := string(runes[start : start+size])
			repeats := 1
			for pos := start + size; pos+size <= len(runes); pos += size {
				if string(runes[pos:pos+size]) != phrase {
					break
				}
				repeats++
			}
			if repeats >= minRepeats {
				covered := repeats * size
				if covered*100/len(runes) >= 50 || covered >= 18 {
					return true
				}
			}
		}
	}
	return false
}

func hasRepeatedTokenPhrase(tokens []string) bool {
	if len(tokens) < 4 {
		return false
	}
	maxSize := len(tokens) / 2
	if maxSize > 8 {
		maxSize = 8
	}
	for size := 1; size <= maxSize; size++ {
		minRepeats := 4
		if size >= 2 {
			minRepeats = 3
		}
		for start := 0; start+size*minRepeats <= len(tokens); start++ {
			repeats := 1
			for pos := start + size; pos+size <= len(tokens); pos += size {
				same := true
				for i := 0; i < size; i++ {
					if !strings.EqualFold(tokens[start+i], tokens[pos+i]) {
						same = false
						break
					}
				}
				if !same {
					break
				}
				repeats++
			}
			if repeats >= minRepeats {
				covered := repeats * size
				if covered*100/len(tokens) >= 50 || covered >= 8 {
					return true
				}
			}
		}
	}
	return false
}

func shouldDropASRText(text string, sampleCount int) (bool, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true, "empty"
	}

	runes := []rune(trimmed)
	if !utf8.ValidString(trimmed) {
		return true, "invalid utf8"
	}

	badRunes := 0
	letterRunes := 0
	counts := make(map[rune]int)
	for _, r := range runes {
		if r == utf8.RuneError || r == '\ufffd' || unicode.IsControl(r) {
			badRunes++
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letterRunes++
			counts[r]++
		}
	}
	if len(runes) >= 12 && badRunes*4 >= len(runes) {
		return true, "too many replacement/control chars"
	}

	maxCharCount := 0
	for _, n := range counts {
		if n > maxCharCount {
			maxCharCount = n
		}
	}
	if letterRunes >= 20 && maxCharCount*100/letterRunes >= 45 {
		return true, "single character repetition"
	}

	compact := compactASRRunes(trimmed)
	if hasRepeatedPhrase(compact) {
		return true, "phrase repetition"
	}

	durationSec := float64(sampleCount) / 16000.0
	prefixLen := len(compact)
	if prefixLen > 48 {
		prefixLen = 48
	}
	if durationSec > 0 && durationSec < 3.0 && len(compact) > 28 && hasRepeatedPhrase(compact[:prefixLen]) {
		return true, "short audio phrase loop"
	}

	tokens := strings.FieldsFunc(trimmed, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	if hasRepeatedTokenPhrase(tokens) {
		return true, "token phrase repetition"
	}
	if len(tokens) >= 8 {
		freq := make(map[string]int)
		maxTokenCount := 0
		for _, tok := range tokens {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			freq[tok]++
			if freq[tok] > maxTokenCount {
				maxTokenCount = freq[tok]
			}
		}
		if maxTokenCount*100/len(tokens) >= 55 {
			return true, "token repetition"
		}
	}

	return false, ""
}

// getASRModel returns the singleton ASR model, loading on first call.
// Thread-safe. Automatically retries if the model wasn't available before.
func (a *App) getASRModel() (*asr.MoonshineModel, error) {
	asrMu.Lock()
	defer asrMu.Unlock()

	if asrInstance != nil {
		return asrInstance, nil
	}

	info := a.CheckASRModel()
	if !info["exists"].(bool) {
		return nil, fmt.Errorf("ASR model not downloaded")
	}
	dir, err := embeddingModelsDir()
	if err != nil {
		return nil, fmt.Errorf("models dir: %w", err)
	}
	modelPath := filepath.Join(dir, asrModelFilename)
	log.Printf("[asr] loading model from %s", modelPath)
	m, err := asr.NewMoonshine(modelPath)
	if err != nil {
		return nil, fmt.Errorf("load moonshine model: %w", err)
	}
	asrInstance = m
	log.Printf("[asr] model loaded successfully")
	return m, nil
}

// TranscribeAudioBase64 accepts base64-encoded WAV audio data and returns
// the transcribed text using the local Moonshine ASR model.
//
// The frontend already records short, trimmed 16kHz mono speech segments. For
// those segments, fidelity matters more than aggressive cleanup: cutting only
// VAD-positive windows can remove weak Chinese syllable edges and corrupt ASR.
// Silero VAD is therefore used as a diagnostic and only as a conservative aid
// for long recordings when its output looks plausible.
func (a *App) TranscribeAudioBase64(wavBase64 string) (string, error) {
	if wavBase64 == "" {
		return "", fmt.Errorf("empty audio data")
	}
	if !a.GetASREnabled() {
		return "", fmt.Errorf("ASR is not enabled")
	}

	wavData, err := base64.StdEncoding.DecodeString(wavBase64)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(wavData) < 44 {
		return "", fmt.Errorf("audio data too short")
	}
	header := inspectWAVHeader(wavData)
	if header.OK {
		log.Printf("[asr] WAV header: format=%d channels=%d sample_rate=%d bits=%d data_bytes=%d", header.Format, header.Channels, header.SampleRate, header.BitsPerSample, header.DataBytes)
	} else {
		log.Printf("[asr] WAV header: unavailable/invalid bytes=%d", len(wavData))
	}

	pcm, err := asr.WAVToFloat32(wavData)
	if err != nil {
		return "", fmt.Errorf("WAV to PCM: %w", err)
	}
	if len(pcm) == 0 {
		return "", nil
	}
	originalDurationSec := float64(len(pcm)) / 16000.0
	originalRMS := pcmRMS(pcm)
	log.Printf("[asr] decoded WAV: bytes=%d samples=%d duration=%.2fs rms=%.5f", len(wavData), len(pcm), originalDurationSec, originalRMS)
	if originalRMS < asrRawFallbackMinRMS {
		log.Printf("[asr] dropping low-energy raw PCM before ASR (duration=%.2fs rms=%.5f)", originalDurationSec, originalRMS)
		return "", nil
	}

	// VAD filtering uses the embedded model, but for normal voice-input segments
	// raw PCM is safer. VAD is allowed to replace raw PCM only for longer audio
	// when it keeps a plausible amount of speech with comparable energy.
	if vadModel, err := vad.Load(); err == nil {
		originalPCM := pcm
		filteredPCM := vadModel.FilterSpeech(pcm)
		if len(filteredPCM) == 0 {
			log.Printf("[asr] VAD filtered all %d samples as silence; keeping raw PCM (duration=%.2fs rms=%.5f)", len(originalPCM), originalDurationSec, originalRMS)
		} else {
			filteredDurationSec := float64(len(filteredPCM)) / 16000.0
			filteredRMS := pcmRMS(filteredPCM)
			speechRatio := float64(len(filteredPCM)) / float64(len(originalPCM))
			useFiltered := originalDurationSec > asrPreferRawMaxSec &&
				filteredDurationSec >= asrVadMinFilteredSec &&
				speechRatio >= asrVadMinSpeechRatio &&
				speechRatio <= asrVadMaxSpeechRatio &&
				filteredRMS >= originalRMS*asrVadMinRMSRatio
			log.Printf("[asr] VAD: %d -> %d samples (%.0f%% speech, raw_rms=%.5f vad_rms=%.5f use_filtered=%t)",
				len(originalPCM), len(filteredPCM), speechRatio*100, originalRMS, filteredRMS, useFiltered)
			if useFiltered {
				pcm = filteredPCM
			}
		}
	}

	model, err := a.getASRModel()
	if err != nil {
		return "", fmt.Errorf("ASR model: %w", err)
	}

	text, err := model.Transcribe(pcm)
	if err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}
	if drop, reason := shouldDropASRText(text, len(pcm)); drop {
		log.Printf("[asr] dropped suspicious transcript (%s): %q", reason, text)
		return "", nil
	}

	log.Printf("[asr] transcribed %d samples -> %q", len(pcm), text)
	return text, nil
}

// IsASRReady returns true if ASR is enabled and the model file exists.
func (a *App) IsASRReady() bool {
	if !a.GetASREnabled() {
		return false
	}
	info := a.CheckASRModel()
	return info["exists"].(bool)
}
