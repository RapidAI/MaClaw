package tts

import (
	"fmt"
	"log"
	"unicode/utf8"
)

// SynthesizeVoiceOGG is the high-level API for TTS → OGG Opus.
// It cleans the text, truncates to maxRunes, synthesizes WAV, and encodes to OGG Opus.
// Returns the OGG Opus bytes, or error.
func SynthesizeVoiceOGG(mgr *Manager, text string, maxRunes int) ([]byte, error) {
	if mgr == nil {
		return nil, fmt.Errorf("TTS manager not available")
	}
	if maxRunes <= 0 {
		maxRunes = 300
	}

	cleaned := CleanForSpeech(text)
	if cleaned == "" {
		return nil, fmt.Errorf("text is empty after cleaning")
	}
	if utf8.RuneCountInString(cleaned) > maxRunes {
		cleaned = TruncateRunesSmart(cleaned, maxRunes)
	}

	wav, err := mgr.SynthesizeText(cleaned)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}

	opus, err := EncodeWAVToOpus(wav)
	if err != nil {
		log.Printf("[tts] opus encode failed, returning WAV: %v", err)
		// Return WAV as fallback — caller can check the content.
		return wav, fmt.Errorf("opus encode failed: %w (WAV fallback available)", err)
	}

	return opus, nil
}
