package tensor_test

import (
	"strconv"
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

func BenchmarkSV_Q8_100x512x512(b *testing.B)   { benchMat(b, 100, 512, 512, true) }
func BenchmarkSV_Q8_100x1536x512(b *testing.B)  { benchMat(b, 100, 1536, 512, true) }
func BenchmarkSV_Q8_100x2048x512(b *testing.B)  { benchMat(b, 100, 2048, 512, true) }
func BenchmarkSV_Q8_100x512x2048(b *testing.B)  { benchMat(b, 100, 512, 2048, true) }
func BenchmarkSV_F32_100x1536x560(b *testing.B) { benchMat(b, 100, 1536, 560, false) }
func BenchmarkSV_Q8_100x25055x512(b *testing.B) { benchMat(b, 100, 25055, 512, true) }

// This is the fused QKV projection at the encoder entry. Its input width is
// 560, so SenseVoice keeps the weights in F32 rather than Q8.
func BenchmarkSV_F32_EntryQKV_8x1536x560_Bias(b *testing.B) {
	const M, N, K = 8, 1536, 560
	a := make([]float32, M*K)
	w := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range w {
		w[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulBias(out, a, w, bias, M, N, K)
	}
}

// 200 ms audio produces two LFR frames plus four prompt frames: M=6. This
// exercises the two-row tail after the primary four-row microkernel.
func BenchmarkSV_F32_EntryQKV_6x1536x560_Bias(b *testing.B) {
	const M, N, K = 6, 1536, 560
	a := make([]float32, M*K)
	w := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range w {
		w[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulBias(out, a, w, bias, M, N, K)
	}
}

func BenchmarkSV_F32_EntryQKV_8x1536x560_BiasWorkers(b *testing.B) {
	const M, N, K = 8, 1536, 560
	a := make([]float32, M*K)
	w := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range w {
		w[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	for _, workers := range []int{1, 2, 4, 6, 8} {
		b.Run(strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tensor.MatMulBias(out, a, w, bias, M, N, K)
			}
		})
	}
}

// CTC greedy decoding is a very wide Q8 argmax. Keep its worker policy
// separate from small encoder projections: it can profit from the full pool.
func BenchmarkSV_Q8_CTCArgmax_6x25055x512_Workers(b *testing.B) {
	const M, N, K = 6, 25055, 512
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	ids := make([]int, M)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	for _, workers := range []int{1, 4, 8, 12} {
		b.Run(strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tensor.MatMulQ8Argmax(ids, a, w, bias, M, N, K)
			}
		})
	}
}

// BenchmarkSV_Q8_CTCArgmax_98x25055x512_Workers matches the long-audio
// encoder frame count, so CTC worker scaling is measured under production
// arithmetic rather than command-length overhead.
func BenchmarkSV_Q8_CTCArgmax_98x25055x512_Workers(b *testing.B) {
	const M, N, K = 98, 25055, 512
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	ids := make([]int, M)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	for _, workers := range []int{1, 4, 8, 12} {
		b.Run(strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tensor.MatMulQ8Argmax(ids, a, w, bias, M, N, K)
			}
		})
	}
}

// These are the exact 8-row AVX-512 microkernel shapes used by SenseVoice's
// FFN projections. Keep them separate from the end-to-end ASR benchmark so
// assembly changes can be compared without frontend or scheduling noise.
func BenchmarkSV_Q8_FFNUp_8x2048x512_ReLU(b *testing.B) {
	const M, N, K = 8, 2048, 512
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulQ8BiasReLU(out, a, w, bias, M, N, K)
	}
}

// BenchmarkSV_Q8_FFNUp_98x2048x512_ReLU matches the long-audio encoder frame
// count and makes the Q8-dequant panel size measurable without ASR frontend
// and scheduling noise.
func BenchmarkSV_Q8_FFNUp_98x2048x512_ReLU(b *testing.B) {
	const M, N, K = 98, 2048, 512
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.05
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulQ8BiasReLU(out, a, w, bias, M, N, K)
	}
}

func BenchmarkSV_Q8_FFNDown_8x512x2048_Add(b *testing.B) {
	const M, N, K = 8, 512, 2048
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32(i%17) * 0.05 // ReLU output domain
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulQ8BiasAdd(out, a, w, bias, M, N, K)
	}
}

// BenchmarkSV_Q8_FFNDown_98x512x2048_Add matches the 98 encoder frames in
// the long-audio SenseVoice fixture. It exposes VNNI panel reuse and N-split
// scheduling costs that the 8-row microkernel benchmark cannot show.
func BenchmarkSV_Q8_FFNDown_98x512x2048_Add(b *testing.B) {
	const M, N, K = 98, 512, 2048
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32(i%17) * 0.05 // ReLU output domain
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := tensor.QuantizeToQ8(raw, N, K)
	w.PrepareScales()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tensor.MatMulQ8BiasAdd(out, a, w, bias, M, N, K)
	}
}
