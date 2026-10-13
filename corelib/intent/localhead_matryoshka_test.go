package intent

import (
	"encoding/json"
	"math"
	"testing"
)

// TestLocalHeadMatryoshka checks source_dim heads: Dim() reports the full
// query dimension, the network sees the re-normalised prefix.
func TestLocalHeadMatryoshka(t *testing.T) {
	head := map[string]any{
		"labels": []string{"a", "b"}, "l2norm": true, "source_dim": 4,
		"w1": [][]float64{{1, 0}, {0, 1}}, "b1": []float64{0, 0},
		"w2": [][]float64{{1, 0}, {0, 1}}, "b2": []float64{0, 0},
	}
	b, _ := json.Marshal(head)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	if h.Dim() != 4 {
		t.Fatalf("Dim=%d want 4", h.Dim())
	}
	// prefix (3,4) -> (0.6,0.8); trailing dims must be ignored.
	p, err := h.Predict([]float32{3, 4, 100, -100})
	if err != nil {
		t.Fatal(err)
	}
	want := 1 / (1 + math.Exp(0.6-0.8))
	if math.Abs(p[1]-want) > 1e-6 {
		t.Fatalf("p=%v want p1 %v", p, want)
	}
	if _, err := h.Predict([]float32{3, 4}); err == nil {
		t.Fatal("expected dim error for truncated input")
	}
	head["l2norm"] = false
	b, _ = json.Marshal(head)
	if _, err := ParseLocalHead(b); err == nil {
		t.Fatal("expected error: source_dim without l2norm")
	}
}
