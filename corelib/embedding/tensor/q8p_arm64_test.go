//go:build arm64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

func TestQ8PArm64TileAsmMatchesGo(t *testing.T) {
	if !Q8PAvailable() {
		t.Skip("no SDOT")
	}
	rng := rand.New(rand.NewSource(3))
	for _, K := range []int{32, 1024, 3072} {
		w := PackQ8P(q8pRandWeights(rng, 64, K))
		if !w.Signed {
			t.Fatal("expected signed pack on arm64")
		}
		M := 7
		a := make([]float32, M*K)
		for i := range a {
			a[i] = float32(rng.NormFloat64())
		}
		act := QuantizeQ8Act(nil, a, M, K)
		for p := 0; p < 2; p++ {
			for m0 := 0; m0 < M; m0 += 4 {
				var g, s [128]float32
				q8pTileGo(&g, act, w, m0, p)
				if !q8pTileAsm(&s, act, w, m0, p) {
					t.Fatal("asm unavailable")
				}
				for i := range g {
					if d := math.Abs(float64(g[i] - s[i])); d > 1e-5*(1+math.Abs(float64(g[i]))) {
						t.Fatalf("K=%d p=%d m0=%d i=%d go=%g asm=%g", K, p, m0, i, g[i], s[i])
					}
				}
			}
		}
	}
}
