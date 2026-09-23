package asr

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// TestSteadyProfile warms up 2 iterations, then CPU-profiles 4 measured
// Transcribe iterations so the profile reflects steady state only.
func TestSteadyProfile(t *testing.T) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	m, err := NewSenseVoice(modelPath)
	if err != nil {
		t.Skip(err)
	}
	defer m.Close()
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		t.Skip(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := m.Transcribe(pcm); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		start := time.Now()
		if _, err := m.Transcribe(pcm); err != nil {
			t.Fatal(err)
		}
		t.Logf("measured iter %d: %v", i, time.Since(start))
	}
}

// TestRound2AB interleaves parallel pack on/off within one process (prebuilt
// panels default off — see q8PrebuiltAPanels).
func TestRound2AB(t *testing.T) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	m, err := NewSenseVoice(modelPath)
	if err != nil {
		t.Skip(err)
	}
	defer m.Close()
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		t.Skip(err)
	}
	tensor.SetQ8PrebuiltAPanelsForTest(false)
	svPackParallel = true
	if _, err := m.Transcribe(pcm); err != nil {
		t.Fatal(err)
	}
	rounds := []bool{true, false, true, false, true, false, true, false, true, false}
	for i, on := range rounds {
		svPackParallel = on
		best := time.Hour
		for j := 0; j < 2; j++ {
			start := time.Now()
			if _, err := m.Transcribe(pcm); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		fmt.Printf("ab round %d packPar=%v best=%v\n", i, on, best)
	}
}


