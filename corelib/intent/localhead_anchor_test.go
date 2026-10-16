package intent

import (
	"math"
	"testing"
)

// r4: optional anchor_features — input = [l2norm(v), raw L2 max cos per listed label].
func TestLocalHeadAnchorFeatures(t *testing.T) {
	js := `{"labels":["a","b"],"l2norm":true,"temperature":2,"tau":0.5,"tau_s":0.9,
	"w1":[[1,0,0,0.5,0],[0,1,0,0,-1]],"b1":[0.1,0.2],"w2":[[1,-1],[-1,1]],"b2":[0,0.3],
	"anchor_features":{"labels":["a","b"]}}`
	h, err := ParseLocalHead([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if h.Dim() != 3 || !h.NeedsAnchorScores() {
		t.Fatalf("Dim=%d needs=%v", h.Dim(), h.NeedsAnchorScores())
	}
	v := []float32{3, 4, 0}
	if _, err := h.Predict(v); err == nil {
		t.Fatal("Predict without anchor scores must fail")
	}
	if _, err := h.PredictWithAnchors(v, map[string]float64{"a": 0.8}); err == nil {
		t.Fatal("missing anchor score must fail")
	}
	p, err := h.PredictWithAnchors(v, map[string]float64{"a": 0.8, "b": 0.6})
	if err != nil {
		t.Fatal(err)
	}
	x := []float64{0.6, 0.8, 0, 0.8, 0.6}
	h1 := math.Max(0, x[0]+0.5*x[3]+0.1)
	h2 := math.Max(0, x[1]-x[4]+0.2)
	z0, z1 := (h1-h2)/2, (-h1+h2+0.3)/2
	m := math.Max(z0, z1)
	want0 := math.Exp(z0-m) / (math.Exp(z0-m) + math.Exp(z1-m))
	if math.Abs(p[0]-want0) > 1e-12 || math.Abs(p[0]+p[1]-1) > 1e-12 {
		t.Fatalf("p=%v want p0=%v", p, want0)
	}
	// heads without anchor_features: PredictWithAnchors == Predict, Dim unchanged
	h0, err := ParseLocalHead([]byte(`{"labels":["a","b"],"w2":[[1,0],[0,1]],"b2":[0,0]}`))
	if err != nil {
		t.Fatal(err)
	}
	q1, _ := h0.Predict([]float32{1, 2})
	q2, _ := h0.PredictWithAnchors([]float32{1, 2}, nil)
	if h0.Dim() != 2 || h0.NeedsAnchorScores() || q1[0] != q2[0] {
		t.Fatal("plain head behaviour changed")
	}
}
