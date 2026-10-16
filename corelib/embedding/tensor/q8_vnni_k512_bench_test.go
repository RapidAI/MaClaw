//go:build amd64

package tensor_test

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Micro-benchmarks for the K=512 VNNI paths (QKV / out-proj / FFN up).
// Shapes match the SenseVoice encoder layers at M≈100 frames.

func benchSVQ8Bias(b *testing.B, M, N, K int, relu, accum bool) {
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32((i*7)%19-9) * 0.04
	}
	raw := make([]float32, N*K)
	for i := range raw {
		raw[i] = float32((i*5)%23-11) * 0.06
	}
	bias := make([]float32, N)
	for n := range bias {
		bias[n] = float32(n%13-6) * 0.05
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	out := make([]float32, M*N)
	for i := range out {
		out[i] = float32((i%11)-5) * 0.03
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch {
		case relu:
			tensor.MatMulQ8BiasReLU(out, a, w, bias, M, N, K)
		case accum:
			tensor.MatMulQ8BiasAdd(out, a, w, bias, M, N, K)
		default:
			tensor.MatMulQ8Bias(out, a, w, bias, M, N, K)
		}
	}
}

func BenchmarkSV_VNNI_QKV_100x1536x512_Bias(b *testing.B)    { benchSVQ8Bias(b, 100, 1536, 512, false, false) }
func BenchmarkSV_VNNI_Out_100x512x512_BiasAdd(b *testing.B)  { benchSVQ8Bias(b, 100, 512, 512, false, true) }
func BenchmarkSV_VNNI_FFNUp_100x2048x512_BiasReLU(b *testing.B) {
	benchSVQ8Bias(b, 100, 2048, 512, true, false)
}
