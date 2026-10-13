package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// TestQ8PX2ReferenceAccuracy checks the portable X2/I8MM layout + reference
// tile on any arch: ≈ float accuracy (two-term activations), far better than
// single-term Q8P.
func TestQ8PX2ReferenceAccuracy(t *testing.T) {
	old := q8pX2
	defer func() { q8pX2 = old }()
	rng := rand.New(rand.NewSource(5))
	N, K, M := 64, 1024, 7
	w := make([]float32, N*K)
	for i := range w {
		w[i] = float32(rng.NormFloat64()) * 0.05
	}
	wt := QuantizeToQ8(w, N, K)
	wt.PrepareScales()
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32(rng.NormFloat64())
	}
	ref := make([]float32, M*N)
	MatMulQ8N(ref, a, wt, M, N, K, 1)
	q8pX2 = true
	wp := PackQ8P(wt)
	act := QuantizeQ8Act(nil, a, M, K)
	if !wp.I8MM || !act.X2 {
		t.Fatal("expected X2 layout")
	}
	out := make([]float32, M*N)
	for p := 0; p < N/32; p++ {
		for m0 := 0; m0 < M; m0 += 4 {
			var tile [128]float32
			q8pTileGo(&tile, act, wp, m0, p)
			for m := 0; m < min(4, M-m0); m++ {
				copy(out[(m0+m)*N+p*32:], tile[m*32:m*32+32])
			}
		}
	}
	var se, sr float64
	for i := range ref {
		d := float64(out[i] - ref[i])
		se += d * d
		sr += float64(ref[i]) * float64(ref[i])
	}
	if rel := math.Sqrt(se / sr); rel > 1e-4 {
		t.Fatalf("X2 rel err %.3e", rel)
	} else {
		t.Logf("X2 rel err %.3e", rel)
	}
}
