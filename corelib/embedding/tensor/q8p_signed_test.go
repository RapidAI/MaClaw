package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// TestQ8PSignedLayoutGoTile: portable s8 reference matches u8-offset reference
// on the same weights (bit-identical float output within FMA noise).
func TestQ8PSignedLayoutGoTile(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, K := range []int{32, 256, 1024} {
		wt := q8pRandWeights(rng, 64, K)
		old := q8pPackSigned
		q8pPackSigned = false
		wu := PackQ8P(wt)
		q8pPackSigned = true
		ws := PackQ8P(wt)
		q8pPackSigned = old
		if wu.Signed || !ws.Signed {
			t.Fatalf("Signed flags u=%v s=%v", wu.Signed, ws.Signed)
		}
		M := 5
		a := make([]float32, M*K)
		for i := range a {
			a[i] = float32(rng.NormFloat64())
		}
		act := QuantizeQ8Act(nil, a, M, K)
		for p := 0; p < 2; p++ {
			for m0 := 0; m0 < M; m0 += 4 {
				var u, s [128]float32
				q8pTileGo(&u, act, wu, m0, p)
				q8pTileGo(&s, act, ws, m0, p)
				for i := range u {
					if d := math.Abs(float64(u[i] - s[i])); d > 1e-5*(1+math.Abs(float64(u[i]))) {
						t.Fatalf("K=%d p=%d m0=%d i=%d u8=%g s8=%g", K, p, m0, i, u[i], s[i])
					}
				}
			}
		}
	}
}
