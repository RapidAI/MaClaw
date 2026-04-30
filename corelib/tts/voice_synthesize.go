package tts

import (
	"fmt"
	"unicode/utf8"
)

// SynthesizeVoiceOGG is the high-level API for TTS → OGG Opus.
// Cleans text, truncates to maxRunes, synthesizes WAV, encodes to OGG Opus.
// Also returns the intermediate WAV for callers that need it (e.g. desktop playback).
func SynthesizeVoiceOGG(mgr *Manager, text string, maxRunes int) (ogg []byte, wav []byte, err error) {
	if mgr == nil {
		return nil, nil, fmt.Errorf("TTS manager not available")
	}
	if maxRunes <= 0 {
		maxRunes = 300
	}

	cleaned := CleanForSpeech(text)
	if cleaned == "" {
		return nil, nil, fmt.Errorf("text is empty after cleaning")
	}
	if utf8.RuneCountInString(cleaned) > maxRunes {
		cleaned = TruncateRunesSmart(cleaned, maxRunes)
	}

	wav, err = mgr.SynthesizeText(cleaned)
	if err != nil {
		return nil, nil, fmt.Errorf("synthesize: %w", err)
	}

	ogg, err = EncodeWAVToOpus(wav)
	if err != nil {
		// Return WAV as fallback — caller can use it for platforms that accept WAV.
		return nil, wav, fmt.Errorf("opus encode failed: %w", err)
	}

	return ogg, wav, nil
}
