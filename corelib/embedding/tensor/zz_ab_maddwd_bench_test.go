//go:build amd64

package tensor

import (
	"math/rand"
	"testing"
)

func BenchmarkABMaddwdOldVsNew(b *testing.B) {
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

	b.Run("New", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for m := 0; m+7 < 48; m += 8 {
				gemmaMaddwdRowM8N24AVX2(&out[m*N], &panel.q[m*K], &panel.s[m],
					&bt.Data[0], &bt.Scales[0], N, 0, N)
			}
		}
	})
	b.Run("Orig", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for m := 0; m+7 < 48; m += 8 {
				gemmaMaddwdRowM8N24AVX2Orig(&out[m*N], &panel.q[m*K], &panel.s[m],
					&bt.Data[0], &bt.Scales[0], N, 0, N)
			}
		}
	})
}
