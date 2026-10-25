package formula

import (
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestTripleKernelMatchesScalar(t *testing.T) {
	for _, K := range []int{4, 5, 15, 16, 17, 32, 48, 120, 192, 336, 384, 432, 704, 864, 1024, 1152, 1664, 2176, 3328} {
		a := make([]float32, 4*K)
		b0 := make([]float32, K)
		b1 := make([]float32, K)
		b2 := make([]float32, K)
		for k := 0; k < K; k++ {
			b0[k] = float32(k%7) * 0.01
			b1[k] = float32(k%5) * -0.02
			b2[k] = float32(k%3) * 0.03
			for r := 0; r < 4; r++ {
				a[r*K+k] = float32((r+1)*(k%11)) * 0.015
			}
		}
		var got [12]float32
		tensor.MultiDot4TripleB(&got, a, b0, b1, b2, K)
		bs := [][]float32{b0, b1, b2}
		for j, b := range bs {
			for r := 0; r < 4; r++ {
				want := float32(0)
				row := a[r*K : (r+1)*K]
				for k := 0; k < K; k++ {
					want += row[k] * b[k]
				}
				if diff := math.Abs(float64(got[j*4+r] - want)); diff > 1e-3 {
					t.Fatalf("K=%d B%d row%d got %g want %g diff %g", K, j, r, got[j*4+r], want, diff)
				}
			}
		}
	}
}
