package kokoro

import "testing"

func TestGeneratorSourceRejectsBadSpectrum(t *testing.T) {
	var m Model
	if _, err := m.generatorSourceForStage([]float32{1, 2, 3}, 4, 8, nil, 0); err == nil {
		t.Fatal("expected a length error")
	}
	if _, err := m.generatorSourceForStage(make([]float32, 22), 4, 0, nil, 0); err == nil {
		t.Fatal("expected a channel error")
	}
}

func TestFloat32Overlaps(t *testing.T) {
	if float32Overlaps(nil, []float32{1}) || float32Overlaps([]float32{1}, nil) {
		t.Fatal("empty slices overlap")
	}
	a := []float32{1, 2, 3, 4}
	b := []float32{1, 2, 3, 4}
	if float32Overlaps(a, b) {
		t.Fatal("distinct buffers overlap")
	}
	if !float32Overlaps(a, a[1:3]) || !float32Overlaps(a[1:], a[:3]) {
		t.Fatal("shared storage was missed")
	}
	if float32Overlaps(a[:2], a[2:]) {
		t.Fatal("adjacent slices overlap")
	}
}

func TestGeneratorResBlockIntoRejectsLength(t *testing.T) {
	var m Model
	if err := m.generatorAdaINResBlock1Into(make([]float32, 3), make([]float32, 4), 1, 4, nil, "missing"); err == nil {
		t.Fatal("expected a length error")
	}
	dst := make([]float32, 4)
	x := make([]float32, 4)
	if err := m.generatorAdaINResBlock1Temps(dst, x, make([]float32, 3), make([]float32, 4), 1, 4, nil, "missing"); err == nil {
		t.Fatal("expected a scratch length error")
	}
}
