package diarization

import (
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

func TestCAMPlusEmbedConvertsMalformedWeightPanicToError(t *testing.T) {
	// A syntactically plausible but incomplete file can make a late CAM++ block
	// request an absent tensor. Embed must keep that corruption recoverable for
	// callers such as the desktop ASR fallback path.
	m := &CAMPlus{w: map[string]tensor{}, fused: map[string]fusedPointwise{}}
	pcm := make([]float32, SampleRate)
	if _, err := m.Embed(pcm); err == nil {
		t.Fatal("Embed() error = nil for incomplete weights")
	}
}

func TestLoadCAMPlusAndEmbedOptionalModel(t *testing.T) {
	modelPath := os.Getenv("CAMPLUS_TEST_MODEL")
	wavPath := os.Getenv("CAMPLUS_TEST_WAV")
	if modelPath == "" || wavPath == "" {
		t.Skip("set CAMPLUS_TEST_MODEL and CAMPLUS_TEST_WAV to exercise official CAM++ weights")
	}
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := asr.WAVToFloat32(wav)
	if err != nil {
		t.Fatal(err)
	}
	m, err := LoadCAMPlus(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	emb, err := m.Embed(pcm)
	if err != nil {
		t.Fatal(err)
	}
	if len(emb) != 192 {
		t.Fatalf("embedding length = %d, want 192", len(emb))
	}
	var squared float32
	for _, value := range emb {
		squared += value * value
	}
	if squared < .99 || squared > 1.01 {
		t.Fatalf("embedding norm² = %f, want 1", squared)
	}
}
