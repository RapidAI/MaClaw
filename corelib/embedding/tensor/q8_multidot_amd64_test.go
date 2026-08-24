//go:build amd64

package tensor

import (
	"math"
	"testing"
)

func TestGemmaGemmM3PackedQuadLeftover(t *testing.T) {
	// 4-col packed Dual3 must still write leftover 1/2/3 columns (N-split tails).
	ranges := [][2]int{{0, 5}, {0, 6}, {0, 7}, {1, 8}, {2, 11}, {0, 32}}
	shapes := []struct{ N, K int }{{32, 768}, {32, 1152}}
	for _, sh := range shapes {
		a := make([]float32, 3*sh.K)
		bData := make([]float32, sh.N*sh.K)
		for i := range a {
			a[i] = float32((i%19)-9) * 0.02
		}
		for i := range bData {
			bData[i] = float32((i%11)-5) * 0.03
		}
		bq := QuantizeToQ8(bData, sh.N, sh.K)
		want := make([]float32, 3*sh.N)
		for r := 0; r < 3; r++ {
			for n := 0; n < sh.N; n++ {
				want[r*sh.N+n] = DotQ8RowScaled(a[r*sh.K:(r+1)*sh.K], bq, n)
			}
		}
		bq.PackQS()
		for _, rg := range ranges {
			ns, ne := rg[0], rg[1]
			got := make([]float32, 3*sh.N)
			if sh.K == 768 {
				gemmaGemmM3N24(got, a, bq, sh.N, ns, ne)
			} else {
				gemmaGemmM3N36(got, a, bq, sh.N, ns, ne)
			}
			for r := 0; r < 3; r++ {
				for n := ns; n < ne; n++ {
					d := float32(math.Abs(float64(got[r*sh.N+n] - want[r*sh.N+n])))
					if d > 1e-4 {
						t.Fatalf("K=%d leftover [%d,%d) r=%d n=%d Δ=%g got=%g want=%g",
							sh.K, ns, ne, r, n, d, got[r*sh.N+n], want[r*sh.N+n])
					}
				}
			}
		}
	}
}

// BenchmarkQuantizePanel8Q8U_98x2048 measures the activation quantization
// repeated by N-split FFN-down workers. It is kept separate from GEMM so a
// shared-prequantization design can be evaluated against its actual cost.
func BenchmarkQuantizePanel8Q8U_98x2048(b *testing.B) {
	const rows, k = 98, 2048
	a := make([]float32, rows*k)
	for i := range a {
		a[i] = float32(i%17) * 0.05
	}
	panels := make([]*q8APanel8, (rows+7)/8)
	for i := range panels {
		panels[i] = new(q8APanel8)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for p := range panels {
			start := p * 8 * k
			if start+8*k > len(a) {
				break
			}
			quantizePanel8Q8U(panels[p], a[start:start+8*k])
		}
	}
}
