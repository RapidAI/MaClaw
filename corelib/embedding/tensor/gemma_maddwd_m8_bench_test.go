//go:build amd64

package tensor

import (
	"fmt"
	"math/rand"
	"testing"
)

// BenchmarkGemmaM8Maddwd isolates the AVX2 VPMADDWD M8 tile against the f32
// Dual8 tile on the same GEMM shape (M=8/48, N=768, K=768), mirroring
// BenchmarkGemmaM8Kernels so the numbers are directly comparable.
func BenchmarkGemmaM8Maddwd(b *testing.B) {
	if !hasAVX2andFMA {
		b.Skip("no AVX2+FMA")
	}
	const K, N, nBlocks = 768, 768, 24
	rng := rand.New(rand.NewSource(29))
	gg := make([]byte, N*nBlocks*q8BlockBytes)
	bt := &Q8Tensor{Rows: N, Cols: K, Data: gg}
	encodeQ8Blocks(rng, bt)
	bt.PrepareScales()

	a := make([]float32, 48*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	panel := getGemmaAS16(48, K)
	defer putGemmaAS16(panel)
	gemmaQuantizeS16(panel.q, panel.s, a, 48, K)
	out := make([]float32, 48*N)

	b.Run("Maddwd_RowM8tile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for m := 0; m+7 < 48; m += 8 {
				gemmaMaddwdRowM8N24AVX2(&out[m*N], &panel.q[m*K], &panel.s[m],
					&bt.Data[0], &bt.Scales[0], N, 0, N)
			}
		}
	})
	b.Run("Maddwd_M8_range", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gemmaMaddwdM8Range(out, panel.q, panel.s, a, bt, 48, N, K, 0, N)
		}
	})
	b.Run("QuantizeS16Row48", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gemmaQuantizeS16(panel.q, panel.s, a, 48, K)
		}
	})
}

// BenchmarkGemmaM4VsM8Shape compares the two maddwd tile shapes on full
// production GEMMs (M=48): M8 (8 rows × 1 col/iter, amortizes weight loads
// over 8 rows) vs M4-2col (4 rows × 2 cols/iter, ~40% fewer instructions per
// MAC but half the weight-load amortization). Winner decides the seq>=5
// dispatch shape.
func BenchmarkGemmaM4VsM8Shape(b *testing.B) {
	if !hasAVX2andFMA {
		b.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(31))
	for _, shape := range [][3]int{{768, 768, 24}, {1152, 768, 24}, {768, 1152, 36}} { // N, K, nBlocks
		N, K, nBlocks := shape[0], shape[1], shape[2]
		gg := make([]byte, N*nBlocks*q8BlockBytes)
		bt := &Q8Tensor{Rows: N, Cols: K, Data: gg}
		encodeQ8Blocks(rng, bt)
		bt.PrepareScales()
		a := make([]float32, 48*K)
		for i := range a {
			a[i] = rng.Float32()*2 - 1
		}
		panel := getGemmaAS16(48, K)
		defer putGemmaAS16(panel)
		gemmaQuantizeS16(panel.q, panel.s, a, 48, K)
		out := make([]float32, 48*N)
		b.Run(fmt.Sprintf("M8_N%d_K%d", N, K), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				gemmaMaddwdM8Range(out, panel.q, panel.s, a, bt, 48, N, K, 0, N)
			}
		})
		b.Run(fmt.Sprintf("M4x12_N%d_K%d", N, K), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				for m := 0; m+3 < 48; m += 4 {
					gemmaMaddwdM4Cols(out[m*N:], panel.q[m*K:], panel.s[m:], a[m*K:], bt, N, K, 0, N)
				}
			}
		})
	}
}
