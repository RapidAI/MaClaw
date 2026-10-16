package tensor

import "testing"

// BenchmarkMultiDot8TripleB measures the PP-OCR triple-B micro-kernel
// dispatch at the widths the det/rec models actually run, so kernel-level
// changes are visible below the noise of the full-GEMM benchmark.
func BenchmarkMultiDot8TripleB(b *testing.B) {
	for _, K := range []int{96, 192, 384, 512} {
		a := make([]float32, 8*K)
		b0, b1, b2 := make([]float32, K), make([]float32, K), make([]float32, K)
		for i := range a {
			a[i] = float32((i%17)-8) * 0.125
		}
		for i := range b0 {
			b0[i] = float32((i%19)-9) * 0.0625
			b1[i] = float32((i%13)-6) * 0.09375
			b2[i] = float32((i%11)-5) * 0.03125
		}
		var o0, o1 [12]float32
		b.Run("K"+itoa(K), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				multiDot8TripleB(&o0, &o1, a, b0, b1, b2, K)
			}
		})
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
