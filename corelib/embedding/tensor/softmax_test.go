package tensor

import (
	"math"
	"testing"
)

func TestSoftmaxInplaceInv_MatchesScalar(t *testing.T) {
	cases := [][]float32{
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0.1, -0.2, 0.3, -0.4, 0.5, -0.6, 0.7, -0.8, 0.9},
		make([]float32, 64),
		make([]float32, 97),
	}
	for i := range cases[2] {
		cases[2][i] = float32(i%13)*0.1 - 0.5
	}
	for i := range cases[3] {
		cases[3][i] = float32(math.Sin(float64(i) * 0.3))
	}
	for ci, src := range cases {
		a := append([]float32(nil), src...)
		b := append([]float32(nil), src...)
		invA := softmaxInplaceInvASM(a)
		invB := softmaxInplaceInvScalar(b)
		if math.Abs(float64(invA-invB)) > 1e-5 {
			t.Fatalf("case %d inv: got %g want %g", ci, invA, invB)
		}
		for i := range a {
			if math.Abs(float64(a[i]-b[i])) > 2e-5 {
				t.Fatalf("case %d idx %d: got %g want %g", ci, i, a[i], b[i])
			}
		}
	}
}
