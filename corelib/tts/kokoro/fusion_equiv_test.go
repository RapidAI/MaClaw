//go:build amd64

package kokoro

import (
	"math/rand"
	"testing"
)

// TestAdaINSnakeEquivalence verifies adaINSnake1D produces bit-identical
// results to the unfused adaIN1D + snakeInplace composition.
func TestAdaINSnakeEquivalence(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for _, shape := range [][2]int{{16, 300}, {128, 5841}, {256, 33}, {11, 500}} {
		ch, frames := shape[0], shape[1]
		style := make([]float32, 256)
		fcW := make([]float32, ch*2*256)
		fcB := make([]float32, ch*2)
		alpha := make([]float32, ch)
		x := make([]float32, ch*frames)
		for i := range style {
			style[i] = float32(rng.NormFloat64())
		}
		for i := range fcW {
			fcW[i] = float32(rng.NormFloat64()) * 0.05
		}
		for i := range fcB {
			fcB[i] = float32(rng.NormFloat64()) * 0.1
		}
		for i := range alpha {
			alpha[i] = float32(rng.NormFloat64())
		}
		for i := range x {
			x[i] = float32(rng.NormFloat64())
		}
		got := make([]float32, ch*frames)
		if err := adaINSnake1D(got, x, ch, frames, style, fcW, fcB, alpha); err != nil {
			t.Fatal(err)
		}
		want := make([]float32, ch*frames)
		if err := adaIN1D(want, x, ch, frames, style, fcW, fcB); err != nil {
			t.Fatal(err)
		}
		snakeInplace(want, alpha, ch, frames)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("shape %v idx %d (c=%d t=%d): got=%v want=%v", shape, i, i%ch, i/ch, got[i], want[i])
			}
		}
	}
}

// TestConv1DResidualFuzz verifies the fused residual epilogue against a
// reference add for a spread of shapes, including edge positions.
func TestConv1DResidualFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for _, k := range []int{3, 5, 7, 11} {
		for _, d := range []int{1, 3, 5} {
			for _, inC := range []int{16, 128, 514} {
				outC := 9 // odd count exercises the per-channel remainder too
				T := 200
				pad := (k - 1) * d / 2
				x := make([]float32, inC*T)
				w := make([]float32, outC*inC*k)
				bias := make([]float32, outC)
				res := make([]float32, outC*T)
				for i := range x {
					x[i] = float32(rng.NormFloat64())
				}
				for i := range w {
					w[i] = float32(rng.NormFloat64()) * 0.1
				}
				for i := range res {
					res[i] = float32(rng.NormFloat64())
				}
				outT := T + 2*pad - d*(k-1)
				if outT < 1 {
					continue
				}
				inputT := make([]float32, inC*T)
				transposeChannelTime(inputT, x, inC, T)
				weightT := make([]float32, outC*k*inC)
				transposeConv1DWeight(weightT, w, inC, outC, k)
				got := make([]float32, outC*outT)
				if err := conv1DSIMDTransposedWeight(got, x, weightT, bias, res, inC, T, outC, k, 1, pad, d, outT); err != nil {
					t.Fatal(err)
				}
				for oc := 0; oc < outC; oc++ {
					for ot := 0; ot < outT; ot++ {
						want := conv1DSIMDGenericDot(inputT, weightT, 0, inC, T, k, 1, pad, d, oc*k*inC, ot) + res[oc*outT+ot]
						g := got[oc*outT+ot]
						diff := g - want
						if diff < 0 {
							diff = -diff
						}
						if diff > 1e-2 {
							t.Fatalf("k=%d d=%d inC=%d oc=%d ot=%d got=%v want=%v", k, d, inC, oc, ot, g, want)
						}
					}
				}
			}
		}
	}
}
