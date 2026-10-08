//go:build amd64

package tensor

import (
	"math/rand"
	"testing"
)

// BenchmarkGemmaM8Kernels isolates VNNI M8 vs f32 Dual8 on the same GEMM
// shape (M=8/48, N=768, K=768) to attribute medium-path gains/losses.
func BenchmarkGemmaM8Kernels(b *testing.B) {
	if !hasAVX512VNNI {
		b.Skip("no AVX-512 VNNI")
	}
	const K, N, nBlocks = 768, 768, 24
	rng := rand.New(rand.NewSource(29))
	gg := make([]byte, N*nBlocks*q8BlockBytes)
	bt := &Q8Tensor{Rows: N, Cols: K, Data: gg}
	encodeQ8Blocks(rng, bt)
	bt.PrepareScales()
	bt.PackQS()

	a := make([]float32, 48*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	panel := getGemmaAQ8(48, K)
	defer putGemmaAQ8(panel)
	gemmaQuantizeQ8URow(panel.q, panel.s, a, 48, K)
	out := make([]float32, 48*N)
	var d0, d1 [8]float32

	b.Run("f32Dual8_M8tile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for m := 0; m+7 < 48; m += 8 {
				ap := &a[m*K]
				for n := 0; n+1 < N; n += 2 {
					q8DualMultiDot8ScaledAVX512N24(&d0, &d1, ap, &bt.Data[0],
						&bt.Scales[n*nBlocks], &bt.Scales[(n+1)*nBlocks],
						n*nBlocks*q8BlockBytes, (n+1)*nBlocks*q8BlockBytes)
					gemmaStoreM4(out[m*N:], n, N, &d0)
					gemmaStoreM4(out[(m+4)*N:], n, N, &d1)
				}
			}
		}
	})
	b.Run("VNNI_RowM8tile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for m := 0; m+7 < 48; m += 8 {
				gemmaVNNIRowM8N24PackedAVX512(&out[m*N], &panel.q[m*K], &panel.s[m],
					&bt.Packed[0], &bt.Scales[0], N, 0, N, &bt.ColBias[0])
			}
		}
	})
	b.Run("VNNI_M8_range", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gemmaVNNIM8Range(out, panel.q, panel.s, a, bt, 48, N, K, 0, N)
		}
	})
	b.Run("QuantizeRow48", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gemmaQuantizeQ8URow(panel.q, panel.s, a, 48, K)
		}
	})
}
