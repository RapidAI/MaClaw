//go:build amd64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

func TestQ8PISADispatch(t *testing.T) {
	t.Logf("auto ISA=%s available=%v avx512vnni=%v avxvnni=%v avx2=%v",
		Q8PISAName(), Q8PAvailable(), hasAVX512VNNI, hasAVXVNNI, hasAVX2andFMA)
	// CI boxes vary in ISA (the noembedding job runs on AVX2-only runners),
	// so this only asserts consistency between the detector and the
	// dispatcher, not a specific ISA.
	if !Q8PAvailable() && !hasAVX512VNNI && !hasAVXVNNI && !hasAVX2andFMA {
		return // no asm on this box: auto must stay off
	}
	if !Q8PAvailable() {
		t.Fatalf("auto ISA %s reported unavailable", Q8PISAName())
	}
	switch Q8PISAName() {
	case "avx512":
		if !hasAVX512VNNI {
			t.Fatal("avx512 selected without hasAVX512VNNI")
		}
	case "avxvnni":
		if !hasAVXVNNI {
			t.Fatal("avxvnni selected without hasAVXVNNI")
		}
	case "avx2":
		if !hasAVX2andFMA {
			t.Fatal("avx2 selected without hasAVX2andFMA")
		}
	default:
		t.Fatalf("unknown auto ISA %q", Q8PISAName())
	}
}

func TestQ8PForcedISAQuantizeAndTile(t *testing.T) {
	defer forceQ8PISAForTest("") // restore auto
	isas := []string{}
	if hasAVX512VNNI {
		isas = append(isas, "avx512")
	}
	if hasAVXVNNI {
		isas = append(isas, "avxvnni")
	}
	if hasAVX2andFMA {
		isas = append(isas, "avx2")
	}
	rng := rand.New(rand.NewSource(42))
	for _, isa := range isas {
		t.Run(isa, func(t *testing.T) {
			forceQ8PISAForTest(isa)
			if Q8PISAName() != isa {
				t.Fatalf("got ISA %s want %s", Q8PISAName(), isa)
			}
			if !Q8PAvailable() {
				t.Fatal("unavailable")
			}
			// Quantizer vs Go reference (bit-exact)
			M, K := 9, 1024
			a := make([]float32, M*K)
			for i := range a {
				a[i] = float32(rng.NormFloat64() * math.Exp(rng.NormFloat64()*2))
			}
			for i := 0; i < 64; i++ {
				a[i] = 0
				a[64+i/2] = 1e-39
			}
			nb := K / 32
			qg := make([]int8, M*K)
			sg := make([]float32, M*nb)
			cg := make([]int32, M*nb)
			quantizeQ8ActGo(qg, sg, cg, a, M, K)
			act := QuantizeQ8Act(nil, a, M, K)
			for i := range qg {
				if qg[i] != act.Q[i] {
					t.Fatalf("q[%d] go=%d asm=%d", i, qg[i], act.Q[i])
				}
			}
			for i := range sg {
				if sg[i] != act.S[i] || cg[i] != act.C[i] {
					t.Fatalf("block %d s %g/%g c %d/%d", i, sg[i], act.S[i], cg[i], act.C[i])
				}
			}
			// Tile vs Go reference
			for _, K := range []int{32, 1024, 3072} {
				w := PackQ8P(q8pRandWeights(rng, 64, K))
				Mm := 7
				aa := make([]float32, Mm*K)
				for i := range aa {
					aa[i] = float32(rng.NormFloat64())
				}
				act2 := QuantizeQ8Act(nil, aa, Mm, K)
				for p := 0; p < 2; p++ {
					for m0 := 0; m0 < Mm; m0 += 4 {
						var g, s [128]float32
						q8pTileGo(&g, act2, w, m0, p)
						if !q8pTileAsm(&s, act2, w, m0, p) {
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
		})
	}
}

// TestQ8PAVX2NoSaturation: |w|=|a|=127 proves the AVX2 sign-trick path does
// not saturate. Naive VPMADDUBSW(u8=w+128, s8=a) would compute 2·255·127=64770
// > 32767; abs(w)×sign(w)·a stays ≤ 2·127·127=32258.
func TestQ8PAVX2NoSaturation(t *testing.T) {
	if !hasAVX2andFMA {
		t.Skip("no AVX2")
	}
	defer forceQ8PISAForTest("")
	forceQ8PISAForTest("avx2")
	N, K := 32, 32
	wf := make([]float32, N*K)
	for i := range wf {
		wf[i] = 127
	}
	wt := QuantizeToQ8(wf, N, K)
	wt.PrepareScales()
	w := PackQ8P(wt)
	// Confirm packed bytes hit the u8 extremes (w=±127 → u8 255 or 1).
	var sawHi bool
	for _, b := range w.QB {
		if b == 255 {
			sawHi = true
			break
		}
	}
	if !sawHi {
		t.Fatal("expected u8=255 from w=+127")
	}
	a := make([]float32, 4*K)
	for i := range a {
		a[i] = 127
	}
	act := QuantizeQ8Act(nil, a, 4, K)
	var g, s [128]float32
	q8pTileGo(&g, act, w, 0, 0)
	if !q8pTileAsm(&s, act, w, 0, 0) {
		t.Fatal("asm")
	}
	for i := range g {
		if d := math.Abs(float64(g[i] - s[i])); d > 1e-4*(1+math.Abs(float64(g[i]))) {
			t.Fatalf("i=%d go=%g asm=%g (saturation?)", i, g[i], s[i])
		}
	}
	if g[0] == 0 {
		t.Fatal("zero output")
	}
	t.Logf("extreme tile out[0]=%g (go==avx2), |w|=|a|=127", g[0])
}
