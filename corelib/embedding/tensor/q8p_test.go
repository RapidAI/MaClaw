package tensor

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func q8pRandWeights(rng *rand.Rand, N, K int) *Q8Tensor {
	w := make([]float32, N*K)
	for i := range w {
		w[i] = float32(rng.NormFloat64()) * 0.05
	}
	t := QuantizeToQ8(w, N, K)
	t.PrepareScales()
	return t
}

// TestQ8PTileAsmMatchesGo: the assembly tile must match the portable
// reference to float rounding (same int32 block dots, FMA vs mul+add only).
func TestQ8PTileAsmMatchesGo(t *testing.T) {
	if !Q8PAvailable() {
		t.Skip("no Q8P asm on this CPU")
	}
	rng := rand.New(rand.NewSource(1))
	for _, K := range []int{32, 1024, 2048, 3072} {
		w := PackQ8P(q8pRandWeights(rng, 64, K))
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

// TestQ8PMatchesFloatPath compares MatMulQ8P (int8 activations) against the
// existing float-activation MatMulQ8N on Qwen3 shapes. Tolerance: per-output
// abs error ≤ 4% of the output RMS and relative Frobenius error ≤ 1%
// (activation int8 rounding; measured values are logged).
func TestQ8PMatchesFloatPath(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, sh := range [][3]int{{1, 1024, 1024}, {5, 2048, 1024}, {16, 1024, 2048}, {103, 3072, 1024}, {33, 1024, 3072}} {
		M, N, K := sh[0], sh[1], sh[2]
		wt := q8pRandWeights(rng, N, K)
		w := PackQ8P(wt)
		a := make([]float32, M*K)
		for i := range a {
			a[i] = float32(rng.NormFloat64())
		}
		ref := make([]float32, M*N)
		MatMulQ8N(ref, a, wt, M, N, K, 1)
		for _, accum := range []bool{false, true} {
			got := make([]float32, M*N)
			if accum {
				for i := range got {
					got[i] = 1
				}
			}
			act := QuantizeQ8Act(nil, a, M, K)
			MatMulQ8P(got, act, w, accum, 0)
			var se, sr, maxd float64
			for i := range ref {
				g := float64(got[i])
				if accum {
					g -= 1
				}
				d := g - float64(ref[i])
				se += d * d
				sr += float64(ref[i]) * float64(ref[i])
				maxd = math.Max(maxd, math.Abs(d))
			}
			rms := math.Sqrt(sr / float64(len(ref)))
			rel := math.Sqrt(se / sr)
			t.Logf("M=%d N=%d K=%d accum=%v relFro=%.2e maxAbs/rms=%.2e", M, N, K, accum, rel, maxd/rms)
			if rel > 0.01 || maxd > 0.04*rms {
				t.Fatalf("M=%d N=%d K=%d: relFro %.3e maxAbs/rms %.3e", M, N, K, rel, maxd/rms)
			}
		}
	}
}

func BenchmarkQ8PQwen3Shapes(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	for _, M := range []int{16, 103} {
		for _, sh := range [][2]int{{2048, 1024}, {1024, 1024}, {1024, 2048}, {3072, 1024}, {1024, 3072}} {
			N, K := sh[0], sh[1]
			wt := q8pRandWeights(rng, N, K)
			w := PackQ8P(wt)
			a := make([]float32, M*K)
			for i := range a {
				a[i] = float32(rng.NormFloat64())
			}
			out := make([]float32, M*N)
			b.Run(fmt.Sprintf("old/M%d_N%d_K%d", M, N, K), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					MatMulQ8N(out, a, wt, M, N, K, 0)
				}
			})
			act := &Q8Act{}
			b.Run(fmt.Sprintf("q8p/M%d_N%d_K%d", M, N, K), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					QuantizeQ8Act(act, a, M, K)
					MatMulQ8P(out, act, w, false, 0)
				}
			})
		}
	}
}

func BenchmarkQ8PQuantize(b *testing.B) {
	M, K := 103, 1024
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32(i%97) - 40
	}
	act := &Q8Act{}
	for i := 0; i < b.N; i++ {
		QuantizeQ8Act(act, a, M, K)
	}
}

func BenchmarkQ8PKernelOnly(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	M, N, K := 103, 1024, 1024
	wt := q8pRandWeights(rng, N, K)
	w := PackQ8P(wt)
	a := make([]float32, M*K)
	act := QuantizeQ8Act(nil, a, M, K)
	out := make([]float32, M*N)
	b.Run("par", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			MatMulQ8P(out, act, w, false, 0)
		}
	})
	b.Run("serial", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			MatMulQ8P(out, act, w, false, 1)
		}
	})
	b.Run("oldserial", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			MatMulQ8N(out, a, wt, M, N, K, 1)
		}
	})
}

func TestQ8PQuantizeAsmMatchesGo(t *testing.T) {
	if !Q8PAvailable() {
		t.Skip("no asm")
	}
	rng := rand.New(rand.NewSource(5))
	M, K := 9, 1024
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32(rng.NormFloat64() * math.Exp(rng.NormFloat64()*3))
	}
	for i := 0; i < 64; i++ { // zero block + tiny block
		a[i] = 0
		a[64+i/2] = 1e-39
	}
	nb := K / 32
	q := make([]int8, M*K)
	s := make([]float32, M*nb)
	c := make([]int32, M*nb)
	quantizeQ8ActGo(q, s, c, a, M, K)
	act := QuantizeQ8Act(nil, a, M, K)
	for i := range q {
		if q[i] != act.Q[i] {
			t.Fatalf("q[%d] go=%d asm=%d", i, q[i], act.Q[i])
		}
	}
	for i := range s {
		if s[i] != act.S[i] || c[i] != act.C[i] {
			t.Fatalf("block %d: s %g/%g c %d/%d", i, s[i], act.S[i], c[i], act.C[i])
		}
	}
}
