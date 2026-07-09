package asr

import (
	"path/filepath"
	"testing"
)

func BenchmarkSenseVoiceTranscribe(b *testing.B) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	m, err := NewSenseVoice(modelPath)
	if err != nil {
		b.Skip(err)
	}
	defer m.Close()
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		b.Skip(err)
	}
	// warm
	if _, err := m.Transcribe(pcm); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Transcribe(pcm); err != nil {
			b.Fatal(err)
		}
	}
}
