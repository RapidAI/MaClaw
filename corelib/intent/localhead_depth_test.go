package intent

import (
	"encoding/json"
	"math"
	"testing"
)

// TestLocalHeadExtraHiddenLayers checks a depth-2 head (w1 + one "hidden"
// layer) against a hand-computed forward pass, and shape validation.
func TestLocalHeadExtraHiddenLayers(t *testing.T) {
	head := map[string]any{
		"labels":      []string{"a", "b"},
		"w1":          [][]float64{{1, 0}, {0, 1}, {1, 1}},
		"b1":          []float64{0, 0, -0.5},
		"hidden":      []map[string]any{{"w": [][]float64{{1, -1, 0}, {0, 1, 1}}, "b": []float64{0.1, 0}}},
		"w2":          [][]float64{{1, 0}, {0, 1}},
		"b2":          []float64{0, 0},
		"temperature": 2.0,
	}
	b, _ := json.Marshal(head)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Predict([]float32{0.6, 0.2})
	if err != nil {
		t.Fatal(err)
	}
	// h1 = relu([0.6,0.2,0.3]) ; h2 = relu([0.6-0.2+0.1, 0.2+0.3]) = [0.5,0.5]
	// logits/T = [0.25,0.25] -> p = [0.5,0.5]
	if math.Abs(p[0]-0.5) > 1e-6 || math.Abs(p[1]-0.5) > 1e-6 {
		t.Fatalf("p=%v", p)
	}
	p, _ = h.Predict([]float32{1, 0})
	// h1=[1,0,0.5]; h2=[1.1,0.5]; z=[0.55,0.25]
	want := 1 / (1 + math.Exp(0.25-0.55))
	if math.Abs(p[0]-want) > 1e-6 {
		t.Fatalf("p0=%v want %v", p[0], want)
	}
	head["w2"] = [][]float64{{1, 0, 0}, {0, 1, 0}}
	b, _ = json.Marshal(head)
	if _, err := ParseLocalHead(b); err == nil {
		t.Fatal("expected shape error (w2 in != last hidden width)")
	}
}
