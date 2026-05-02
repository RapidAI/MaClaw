package kokoro

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

var ErrInferenceNotComplete = errors.New("kokoro: pure Go neural forward is not complete")

type Model struct {
	Config  *Config
	Weights *TensorFile

	cacheMu sync.Mutex
	wCache  map[string][]float32
}

type Assets struct {
	ConfigPath  string
	WeightsPath string
	VoiceDir    string
}

func LoadModel(assets Assets) (*Model, error) {
	cfg, err := LoadConfig(assets.ConfigPath)
	if err != nil {
		return nil, err
	}
	weights, err := LoadTensorFile(assets.WeightsPath)
	if err != nil {
		return nil, err
	}
	return &Model{Config: cfg, Weights: weights, wCache: make(map[string][]float32)}, nil
}

func (m *Model) cachedFloat32(key string, build func() ([]float32, error)) ([]float32, error) {
	m.cacheMu.Lock()
	if m.wCache != nil {
		if v, ok := m.wCache[key]; ok {
			m.cacheMu.Unlock()
			return v, nil
		}
	}
	m.cacheMu.Unlock()

	v, err := build()
	if err != nil {
		return nil, err
	}
	m.cacheMu.Lock()
	if m.wCache == nil {
		m.wCache = make(map[string][]float32)
	}
	if existing, ok := m.wCache[key]; ok {
		m.cacheMu.Unlock()
		return existing, nil
	}
	m.wCache[key] = v
	m.cacheMu.Unlock()
	return v, nil
}

func (m *Model) LoadVoice(voiceDir, voice string) (*TensorFile, error) {
	path := filepath.Join(voiceDir, voice+".koro")
	pack, err := LoadTensorFile(path)
	if err != nil {
		return nil, fmt.Errorf("kokoro: load voice %q: %w", voice, err)
	}
	return pack, nil
}

// SynthesizePhonemes is the intended pure-Go runtime entry point. The asset
// loader, token mapping, and tensor format are implemented; the remaining work
// is porting Kokoro's ALBERT, duration/F0 predictors, and ISTFTNet decoder.
func (m *Model) SynthesizePhonemes(phonemes string, voice *TensorFile, speed float32) ([]float32, error) {
	if m == nil || m.Config == nil || m.Weights == nil {
		return nil, fmt.Errorf("kokoro: model is not loaded")
	}
	if voice == nil {
		return nil, fmt.Errorf("kokoro: voice is not loaded")
	}
	cond, err := m.BuildConditioning(phonemes, voice, speed)
	if err != nil {
		return nil, err
	}
	f0n, err := m.PredictF0N(cond, voice)
	if err != nil {
		return nil, err
	}
	feat, err := m.DecoderPreGenerator(cond, f0n, voice)
	if err != nil {
		return nil, err
	}
	return m.GeneratorForward(feat, f0n, voice)
}
