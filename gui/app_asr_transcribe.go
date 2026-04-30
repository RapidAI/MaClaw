package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/vad"
)

var (
	asrInstance *asr.MoonshineModel
	asrMu      sync.Mutex
)

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
// Silero VAD is applied first to filter out silence/noise windows.
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

	pcm, err := asr.WAVToFloat32(wavData)
	if err != nil {
		return "", fmt.Errorf("WAV to PCM: %w", err)
	}
	if len(pcm) == 0 {
		return "", nil
	}

	// VAD filtering â€?embedded model, no external file needed
	if vadModel, err := vad.Load(); err == nil {
		originalPCM := pcm
		filteredPCM := vadModel.FilterSpeech(pcm)
		if len(filteredPCM) == 0 {
			log.Printf("[asr] VAD filtered all %d samples as silence; falling back to raw PCM", len(originalPCM))
		} else {
			pcm = filteredPCM
		}
		if len(filteredPCM) > 0 {
			log.Printf("[asr] VAD: %d -> %d samples (%.0f%% speech)",
				len(originalPCM), len(pcm), float64(len(pcm))/float64(len(originalPCM))*100)
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

	log.Printf("[asr] transcribed %d samples â†?%q", len(pcm), text)
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
