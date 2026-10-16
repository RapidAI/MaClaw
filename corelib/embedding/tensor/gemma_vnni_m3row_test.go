//go:build amd64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// TestGemmaVNNIRowM3PackedMatchesScalar pins the row-scale M3 short-sequence
// kernels (seq==3 fast path used by every short Embed) against the scalar
// reference over the same quantized-A contract.
func TestGemmaVNNIRowM3PackedMatchesScalar(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no AVX-512 VNNI")
	}
	rng := rand.New(rand.NewSource(31))
	for _, tc := range []struct {
		K, N, nBlocks int
	}{
		{768, 10, 24},
		{1152, 8, 36},
	} {
		K, N, nBlocks := tc.K, tc.N, tc.nBlocks
		b := randGemmaPackedB(rng, N, nBlocks)
		a := make([]float32, 3*K)
		for i := range a {
			a[i] = rng.Float32()*2 - 1
		}
		q := make([]byte, 3*K)
		s := make([]float32, 3)
		gemmaQuantizeQ8URowScalar(q, s, a, 3, K)

		got := make([]float32, 3*N)
		b.ensureColBias()
		if K == 768 {
			gemmaVNNIRowM3N24PackedAVX512(&got[0], &q[0], &s[0], &b.Packed[0], &b.Scales[0], N, 0, N, &b.ColBias[0])
		} else {
			gemmaVNNIRowM3N36PackedAVX512(&got[0], &q[0], &s[0], &b.Packed[0], &b.Scales[0], N, 0, N, &b.ColBias[0])
		}
		want := gemmaVNNIRowM8ScalarRef(q, s, b, 3, N, nBlocks)
		for i := range want {
			d := math.Abs(float64(got[i] - want[i]))
			m := math.Abs(float64(want[i]))
			if m < 1 {
				m = 1
			}
			if d/m > 1e-3 {
				t.Fatalf("K=%d lane %d: got %v want %v (rel err %g)", K, i, got[i], want[i], d/m)
			}
		}
	}
}

func BenchmarkGemmaM3RowVNNI(b *testing.B) {
	if !hasAVX512VNNI {
		b.Skip("no AVX-512 VNNI")
	}
	const K, N, nBlocks = 768, 768, 24
	rng := rand.New(rand.NewSource(31))
	gg := make([]byte, N*nBlocks*q8BlockBytes)
	bt := &Q8Tensor{Rows: N, Cols: K, Data: gg}
	encodeQ8Blocks(rng, bt)
	bt.PrepareScales()
	bt.PackQS()
	a := make([]float32, 3*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	q := make([]byte, 3*K)
	s := make([]float32, 3)
	gemmaQuantizeQ8URow(q, s, a, 3, K)
	out := make([]float32, 3*N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gemmaVNNIRowM3N24PackedAVX512(&out[0], &q[0], &s[0], &bt.Packed[0], &bt.Scales[0], N, 0, N, &bt.ColBias[0])
	}
}
