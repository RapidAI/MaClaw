//go:build amd64

package tensor

import "testing"

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
