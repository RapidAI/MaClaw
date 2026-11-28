package tensor

import (
	"math"
	"math/rand"
	"testing"
)

func TestQ8PBiasEpilogueMatchesFloat(t *testing.T) {
	if !Q8PAvailable() {
		t.Skip("no Q8P kernel on this CPU")
	}
	rng := rand.New(rand.NewSource(7))
	M, N, K := 7, 64, 128
	wt := q8pRandWeights(rng, N, K)
	packed := PackQ8P(wt)
	if packed == nil {
		t.Fatal("pack failed")
	}
	a := make([]float32, M*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32(rng.NormFloat64())
	}
	for i := range bias {
		bias[i] = float32(rng.NormFloat64()) * 0.1
	}
	ref := make([]float32, M*N)
	MatMulQ8BiasN(ref, a, wt, bias, M, N, K, 1)

	got := make([]float32, M*N)
	if !MatMulQ8PBias(got, a, packed, bias, M, N, K, false, false) {
		t.Fatal("q8p bias declined")
	}
	assertQ8PClose(t, "bias", got, ref)

	reluRef := append([]float32(nil), ref...)
	for i, v := range reluRef {
		if v < 0 {
			reluRef[i] = 0
		}
	}
	reluGot := make([]float32, M*N)
	if !MatMulQ8PBias(reluGot, a, packed, bias, M, N, K, true, false) {
		t.Fatal("q8p relu declined")
	}
	assertQ8PClose(t, "relu", reluGot, reluRef)

	base := make([]float32, M*N)
	for i := range base {
		base[i] = 0.25
	}
	addRef := make([]float32, M*N)
	for i := range addRef {
		addRef[i] = base[i] + ref[i]
	}
	addGot := append([]float32(nil), base...)
	if !MatMulQ8PBias(addGot, a, packed, bias, M, N, K, false, true) {
		t.Fatal("q8p accum declined")
	}
	assertQ8PClose(t, "accum", addGot, addRef)

	if MatMulQ8PBias(got, a, nil, bias, M, N, K, false, false) {
		t.Fatal("nil weight must decline")
	}
}

func assertQ8PClose(t *testing.T, name string, got, ref []float32) {
	t.Helper()
	var se, sr, maxd float64
	for i := range ref {
		d := float64(got[i]) - float64(ref[i])
		se += d * d
		sr += float64(ref[i]) * float64(ref[i])
		maxd = math.Max(maxd, math.Abs(d))
	}
	rms := math.Sqrt(sr / float64(len(ref)))
	rel := math.Sqrt(se / math.Max(sr, 1e-12))
	t.Logf("%s relFro=%.2e maxAbs/rms=%.2e", name, rel, maxd/math.Max(rms, 1e-12))
	if rel > 0.02 || maxd > 0.08*math.Max(rms, 1e-6) {
		t.Fatalf("%s: relFro %.3e maxAbs/rms %.3e", name, rel, maxd/math.Max(rms, 1e-6))
	}
}

// TestQ8PWrite32MatchesSplitEpilogue locks the tile store to the old
// store-then-sweep association, including NaN (relu must not treat it as
// negative) and a negative zero.
func TestQ8PWrite32MatchesSplitEpilogue(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, accum := range []bool{false, true} {
		for _, relu := range []bool{false, true} {
			for _, withBias := range []bool{false, true} {
				dst0 := q8pRand32(rng)
				src := q8pRand32(rng)
				dst0[0] = float32(math.Copysign(0, -1))
				src[1] = float32(math.NaN())
				src[2] = float32(math.Inf(1))
				src[3] = float32(math.Inf(-1))
				var bias []float32
				if withBias {
					bias = q8pRand32(rng)
					bias[4] = float32(math.NaN())
				}
				got := append([]float32(nil), dst0...)
				ref := append([]float32(nil), dst0...)
				q8pWrite32(got, src, bias, accum, relu)
				q8pSplitEpilogue(ref, src, bias, accum, relu)
				for i := range ref {
					if !q8pSameF32(got[i], ref[i]) {
						t.Fatalf("accum=%v relu=%v bias=%v [%d] got %v ref %v", accum, relu, withBias, i, got[i], ref[i])
					}
				}
			}
		}
	}
}

// TestQ8PFusedEpilogueMatchesSplitPass is the encoder shape: the fused store
// must match quantize + GEMM + the old full-row sweep, bit for bit.
func TestQ8PFusedEpilogueMatchesSplitPass(t *testing.T) {
	if !Q8PAvailable() {
		t.Skip("no Q8P kernel on this CPU")
	}
	rng := rand.New(rand.NewSource(9))
	shapes := [][3]int{{1, 32, 32}, {5, 64, 128}, {8, 96, 64}}
	for _, shape := range shapes {
		M, N, K := shape[0], shape[1], shape[2]
		packed := PackQ8P(q8pRandWeights(rng, N, K))
		if packed == nil {
			t.Fatal("pack failed")
		}
		a := make([]float32, M*K)
		for i := range a {
			a[i] = float32(rng.NormFloat64())
		}
		for _, accum := range []bool{false, true} {
			for _, relu := range []bool{false, true} {
				for _, withBias := range []bool{false, true} {
					var bias []float32
					if withBias {
						bias = make([]float32, N)
						for i := range bias {
							bias[i] = float32(rng.NormFloat64()) * 0.1
						}
					}
					base := make([]float32, M*N)
					for i := range base {
						base[i] = 0.25
					}
					plain := append([]float32(nil), base...)
					got := append([]float32(nil), base...)
					act := QuantizeQ8Act(nil, a, M, K)
					MatMulQ8P(plain, act, packed, accum, 0)
					q8pSplitSweep(plain, bias, M, N, relu)
					if !MatMulQ8PBias(got, a, packed, bias, M, N, K, relu, accum) {
						t.Fatal("q8p fused path declined")
					}
					for i := range plain {
						if !q8pSameF32(got[i], plain[i]) {
							t.Fatalf("M=%d N=%d K=%d accum=%v relu=%v bias=%v [%d] got %v ref %v", M, N, K, accum, relu, withBias, i, got[i], plain[i])
						}
					}
				}
			}
		}
	}
}

func q8pRand32(rng *rand.Rand) []float32 {
	out := make([]float32, 32)
	for i := range out {
		out[i] = float32(rng.NormFloat64())
	}
	return out
}

func q8pSameF32(a, b float32) bool {
	if a == b {
		return true
	}
	return math.IsNaN(float64(a)) && math.IsNaN(float64(b))
}

// q8pSplitEpilogue is the pre-fusion tile math: write the GEMM value, then
// add bias and ReLU. q8pWrite32 must stay bit-identical to it.
func q8pSplitEpilogue(dst, src, bias []float32, accum, relu bool) {
	if accum {
		for i := range dst {
			dst[i] += src[i]
		}
	} else {
		copy(dst, src)
	}
	if bias == nil && !relu {
		return
	}
	for i, v := range dst {
		if bias != nil {
			v += bias[i]
		}
		if relu && v < 0 {
			v = 0
		}
		dst[i] = v
	}
}

// q8pSplitSweep is the old MatMulQ8PBias epilogue over a full matrix.
func q8pSplitSweep(out, bias []float32, M, N int, relu bool) {
	if bias == nil && !relu {
		return
	}
	for m := 0; m < M; m++ {
		row := out[m*N : (m+1)*N]
		for n, v := range row {
			if bias != nil {
				v += bias[n]
			}
			if relu && v < 0 {
				v = 0
			}
			row[n] = v
		}
	}
}
