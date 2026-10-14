package intent

import (
	"encoding/json"
	"math"
	"testing"
)

func TestLocalHeadInputBlocks(t *testing.T) {
	d, hid := 4, 3
	w1 := make([][]float64, hid)
	for j := range w1 {
		w1[j] = make([]float64, 2*d)
		for i := range w1[j] {
			w1[j][i] = math.Sin(float64(j*7 + i))
		}
	}
	f := map[string]any{"labels": []string{"a", "b"}, "w1": w1, "b1": []float64{0.1, -0.2, 0.3},
		"w2": [][]float64{{1, -1, 0.5}, {-0.5, 0.2, 1}}, "b2": []float64{0, 0.1}, "temperature": 0.7, "tau": 0.75, "tau_s": 0.99,
		"l2norm": true, "input_blocks": []string{"last", "mean"}, "embed_model": "qwen3"}
	b, _ := json.Marshal(f)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	if h.Dim() != d || !h.NeedsPooling() {
		t.Fatalf("Dim %d NeedsPooling %v", h.Dim(), h.NeedsPooling())
	}
	v := []float32{1, 2, 3, 4, 10, 0, 0, 0}
	p, err := h.Predict(v)
	if err != nil {
		t.Fatal(err)
	}
	// reference: per-block L2 norm then dense
	x := []float64{1, 2, 3, 4, 1, 0, 0, 0}
	n := math.Sqrt(30)
	for i := 0; i < 4; i++ {
		x[i] /= n
	}
	z := make([]float64, 2)
	for k := 0; k < 2; k++ {
		s := f["b2"].([]float64)[k]
		for j := 0; j < hid; j++ {
			a := f["b1"].([]float64)[j]
			for i := range x {
				a += w1[j][i] * x[i]
			}
			s += f["w2"].([][]float64)[k][j] * math.Max(a, 0)
		}
		z[k] = s / 0.7
	}
	p0 := 1 / (1 + math.Exp(z[1]-z[0]))
	if math.Abs(p[0]-p0) > 1e-12 {
		t.Fatalf("p0 %v want %v", p[0], p0)
	}
	if _, err := h.Predict(v[:4]); err == nil {
		t.Fatal("want dim error for single block")
	}
	f["input_blocks"] = []string{"last", "last"}
	b, _ = json.Marshal(f)
	if _, err := ParseLocalHead(b); err == nil {
		t.Fatal("want error for repeated block")
	}
}
