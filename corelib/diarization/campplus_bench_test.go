package diarization

import (
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

// BenchmarkCAMPlusEmbed measures the end-to-end CPU embedding path with the
// official converted model. Set CAMPLUS_TEST_MODEL and CAMPLUS_TEST_WAV.
func BenchmarkCAMPlusEmbed(b *testing.B) {
	modelPath, wavPath := os.Getenv("CAMPLUS_TEST_MODEL"), os.Getenv("CAMPLUS_TEST_WAV")
	if modelPath == "" || wavPath == "" {
		b.Skip("set CAMPLUS_TEST_MODEL and CAMPLUS_TEST_WAV")
	}
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		b.Fatal(err)
	}
	pcm, err := asr.WAVToFloat32(wav)
	if err != nil {
		b.Fatal(err)
	}
	m, err := LoadCAMPlus(modelPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Embed(pcm); err != nil {
			b.Fatal(err)
		}
	}
}
