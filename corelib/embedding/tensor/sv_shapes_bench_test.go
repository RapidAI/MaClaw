package tensor_test

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func benchMat(b *testing.B, M, N, K int, q8 bool) {
	a := make([]float32, M*K)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32(i%17) * 0.01
	}
	if q8 {
		raw := make([]float32, N*K)
		for i := range raw {
			raw[i] = float32((i%13)-6) * 0.1
		}
		w := tensor.QuantizeToQ8(raw, N, K)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tensor.MatMulQ8(out, a, w, M, N, K)
		}
		return
	}
	bb := make([]float32, N*K)
	for i := range bb {
		bb[i] = float32((i%11)-5) * 0.05
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMul(out, a, bb, M, N, K)
	}
}

func BenchmarkSV_Q8_100x512x512(b *testing.B)  { benchMat(b, 100, 512, 512, true) }
func BenchmarkSV_Q8_100x1536x512(b *testing.B) { benchMat(b, 100, 1536, 512, true) }
func BenchmarkSV_Q8_100x2048x512(b *testing.B) { benchMat(b, 100, 2048, 512, true) }
func BenchmarkSV_Q8_100x512x2048(b *testing.B) { benchMat(b, 100, 512, 2048, true) }
func BenchmarkSV_F32_100x1536x560(b *testing.B){ benchMat(b, 100, 1536, 560, false) }
func BenchmarkSV_Q8_100x25055x512(b *testing.B){ benchMat(b, 100, 25055, 512, true) }
