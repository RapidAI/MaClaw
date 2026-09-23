//go:build amd64

package kokoro

import (
	"math/rand"
	"testing"
)

func TestConv1DStridedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	kernels := []int{3, 5, 7, 11, 12, 20}
	dils := []int{1, 3, 5}
	inCs := []int{16, 22, 64, 128, 256, 514, 1024, 1090}
	for _, k := range kernels {
		for _, d := range dils {
			for _, inC := range inCs {
				outC := 8
				T := 300
				pad := (k - 1) * d / 2
				if pad > 10 {
					pad = 10
				}
				x := make([]float32, inC*T)
				w := make([]float32, outC*inC*k)
				bias := make([]float32, outC)
				for i := range x {
					x[i] = float32(rng.NormFloat64())
				}
				for i := range w {
					w[i] = float32(rng.NormFloat64()) * 0.1
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
				if err := conv1DSIMDTransposedWeight(got, x, weightT, bias, nil, inC, T, outC, k, 1, pad, d, outT); err != nil {
					t.Fatal(err)
				}
				// reference: generic per-oc dot
				inputTRef := make([]float32, inC*T)
				transposeChannelTimeScalar(inputTRef, x, inC, T)
				if len(inputTRef) != len(inputT) {
					t.Fatal("bad")
				}
				for oc := 0; oc < outC; oc++ {
					for ot := 0; ot < outT; ot++ {
						want := conv1DSIMDGenericDot(inputTRef, weightT, 0, inC, T, k, 1, pad, d, oc*k*inC, ot)
						g := got[oc*outT+ot]
						diff := g - want
						if diff < 0 {
							diff = -diff
						}
						if diff > 1e-3 {
							t.Fatalf("k=%d d=%d inC=%d oc=%d ot=%d got=%v want=%v", k, d, inC, oc, ot, g, want)
						}
					}
				}
			}
		}
	}
}

func transposeChannelTimeScalar(dst, x []float32, channels, T int) {
	for c := 0; c < channels; c++ {
		for tt := 0; tt < T; tt++ {
			dst[tt*channels+c] = x[c*T+tt]
		}
	}
}
