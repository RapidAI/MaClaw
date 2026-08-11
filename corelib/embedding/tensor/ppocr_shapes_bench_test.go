package tensor

import "testing"

// BenchmarkPPOCRF32Pointwise records the pointwise-convolution GEMM shapes
// used by PP-OCRv6.  They intentionally use the normal MatMul route so a
// kernel change is measured in the same layout as inference: weights [N,K]
// and transposed feature-map activations [M,K].
func BenchmarkPPOCRF32Pointwise(b *testing.B) {
	for _, tc := range []struct {
		name    string
		M, N, K int
	}{
		{"96x192x96", 96, 192, 96},
		{"96x384x192", 96, 384, 192},
		{"96x768x384", 96, 768, 384},
	} {
		b.Run(tc.name, func(b *testing.B) {
			a := make([]float32, tc.M*tc.K)
			w := make([]float32, tc.N*tc.K)
			out := make([]float32, tc.M*tc.N)
			for i := range a {
				a[i] = float32((i%19)-9) * 0.03125
			}
			for i := range w {
				w[i] = float32((i%23)-11) * 0.015625
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				MatMul(out, a, w, tc.M, tc.N, tc.K)
			}
		})
	}
}
