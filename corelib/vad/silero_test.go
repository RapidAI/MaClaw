package vad

import (
	"math"
	"testing"
)

func TestLoad(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.hp.SampleRate != 16000 {
		t.Errorf("expected sample rate 16000, got %d", m.hp.SampleRate)
	}
	if m.hp.WindowSize != 512 {
		t.Errorf("expected window size 512, got %d", m.hp.WindowSize)
	}
	if len(m.w.stft.W) != 258*1*256 {
		t.Errorf("STFT weight size: got %d, want %d", len(m.w.stft.W), 258*256)
	}
	if len(m.w.lstmWIH) != 512*128 {
		t.Errorf("LSTM W_ih size: got %d, want %d", len(m.w.lstmWIH), 512*128)
	}
}

func TestDetectSilence(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	silence := make([]float32, m.hp.WindowSize)
	state := m.NewState()
	prob, err := m.Detect(silence, state)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	t.Logf("silence prob: %.4f", prob)
	if prob > 0.3 {
		t.Errorf("silence should have low speech probability, got %.4f", prob)
	}
}

func TestDetectTone(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ws := m.hp.WindowSize
	tone := make([]float32, ws)
	for i := range tone {
		tone[i] = 0.5 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	state := m.NewState()
	prob, err := m.Detect(tone, state)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	t.Logf("440Hz tone prob: %.4f", prob)
}

func TestFilterSpeechSilence(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	silence := make([]float32, m.hp.WindowSize*10)
	result := m.FilterSpeech(silence)
	if len(result) > 0 {
		t.Errorf("expected empty result for silence, got %d samples", len(result))
	}
}

func TestStreamingState(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	state := m.NewState()
	ws := m.hp.WindowSize
	window := make([]float32, ws)
	for i := 0; i < 5; i++ {
		_, err := m.Detect(window, state)
		if err != nil {
			t.Fatalf("Detect window %d: %v", i, err)
		}
	}
	// Scratch buffers should be allocated once and reused
	if state.scratch == nil {
		t.Error("scratch buffers should be allocated after Detect")
	}
	t.Logf("H all-zero: %v, SpeechProb: %.4f", state.H[0] == 0, state.SpeechProb)
}

func TestScratchReuse(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	state := m.NewState()
	ws := m.hp.WindowSize
	window := make([]float32, ws)

	m.Detect(window, state)
	sc1 := state.scratch
	m.Detect(window, state)
	sc2 := state.scratch
	if sc1 != sc2 {
		t.Error("scratch buffers should be reused across Detect calls")
	}
}

func TestConcurrentDetect(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Two goroutines with independent states should not race
	done := make(chan struct{}, 2)
	for g := 0; g < 2; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			state := m.NewState()
			window := make([]float32, m.hp.WindowSize)
			for i := 0; i < 20; i++ {
				m.Detect(window, state)
			}
		}()
	}
	<-done
	<-done
}

func TestLSTMCellInPlace(t *testing.T) {
	hidden := 2
	inputSize := 2
	x := []float32{1.0, 0.5}
	h := []float32{0, 0}
	c := []float32{0, 0}
	gates := make([]float32, 8)
	wIH := make([]float32, 8*2)
	for i := 0; i < 8; i++ {
		wIH[i*2+i%2] = 0.5
	}
	wHH := make([]float32, 8*2)
	lstmCellInPlace(x, h, c, gates, wIH, wHH, nil, nil, inputSize, hidden)
	if h[0] == 0 && h[1] == 0 {
		t.Error("LSTM output should be non-zero with non-zero input")
	}
}

func TestConv1dStrideInto(t *testing.T) {
	input := []float32{1, 2, 3, 4, 5, 6}
	weight := []float32{1, 0, -1}
	dst := make([]float32, 2) // outLen = (6-3)/2 + 1 = 2
	conv1dStrideInto(dst, input, weight, 1, 3, 2)
	for i, expected := range []float32{-2, -2} {
		if math.Abs(float64(dst[i]-expected)) > 1e-5 {
			t.Errorf("dst[%d] = %.4f, expected %.4f", i, dst[i], expected)
		}
	}
}

func TestConv1dPad1Into(t *testing.T) {
	input := []float32{1, 2, 3, 4, 5}
	weight := []float32{1, 1, 1}
	bias := []float32{0}
	dst := make([]float32, 5)
	conv1dPad1Into(dst, input, 1, 5, weight, bias, 1, 1, 3)
	expected := []float32{3, 6, 9, 12, 9}
	for i, e := range expected {
		if math.Abs(float64(dst[i]-e)) > 1e-5 {
			t.Errorf("dst[%d] = %.4f, expected %.4f", i, dst[i], e)
		}
	}
}
