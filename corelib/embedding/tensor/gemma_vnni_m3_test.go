package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// gemmaVNNIM3ScalarRef computes the reference result for
// gemmaVNNIM3N24PackedAVX512 over the same packed-B / quantized-A contract:
// out[r][n] = Σ_blk aS[r][blk]·bS[n][blk]·Σ_i (aQ[r][i]−128)·int8(bPacked[n][i]).
func gemmaVNNIM3ScalarRef(got *gemmaM3AQ, b *Q8Tensor, N int) []float32 {
	want := make([]float32, 3*N)
	for r := 0; r < 3; r++ {
		for n := 0; n < N; n++ {
			var acc float64
			for blk := 0; blk < 24; blk++ {
				var dot int32
				for i := 0; i < 32; i++ {
					av := int32(got.q[r*768+blk*32+i]) - 128
					bv := int32(int8(b.Packed[n*768+blk*32+i]))
					dot += av * bv
				}
				acc += float64(got.s[r*24+blk]) * float64(b.Scales[n*24+blk]) * float64(dot)
			}
			want[r*N+n] = float32(acc)
		}
	}
	return want
}

// TestGemmaVNNIM3N24PackedMatchesScalar pins the VNNI M3 short-sequence
// kernel against a scalar reference. Production incident 2026-08-24: this
// path silently dropped every odd 32-byte block (an odd-block scale scratch
// register collided with the extracted high-half dot), corrupting 3-token
// embeddings ("你好" fused-vs-reference cosine 0.26) and with them tool
// routing and intent classification on short follow-up messages.
func TestGemmaVNNIM3N24PackedMatchesScalar(t *testing.T) {
	if !hasAVX512 || !hasAVX512VNNI {
		t.Skip("no AVX-512 VNNI")
	}
	const K, N = 768, 8
	rng := rand.New(rand.NewSource(42))

	b := &Q8Tensor{Rows: N, Cols: K}
	b.Packed = make([]byte, N*K)
	b.Scales = make([]float32, N*24)
	for i := range b.Packed {
		b.Packed[i] = byte(rng.Intn(256))
	}
	for i := range b.Scales {
		b.Scales[i] = rng.Float32()*0.02 + 0.001
	}

	a := make([]float32, 3*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	var aq gemmaM3AQ
	quantizeGemmaM3Q8U(aq.q[:3*K], aq.s[:72], a, K)

	got := make([]float32, 3*N)
	gemmaVNNIM3N24PackedAVX512(&got[0], &aq.q[0], &aq.s[0], &b.Packed[0], &b.Scales[0], N, 0, N)

	want := gemmaVNNIM3ScalarRef(&aq, b, N)
	for i := range want {
		d := math.Abs(float64(got[i] - want[i]))
		m := math.Abs(float64(want[i]))
		if m < 1 {
			m = 1
		}
		if d/m > 1e-3 {
			t.Fatalf("lane %d: got %v want %v (rel err %g)", i, got[i], want[i], d/m)
		}
	}
}

// TestGemmaVNNIM3N24PackedPerBlock isolates each of the 24 K-blocks so a
// dropped or double-counted block is identified directly instead of inferred
// from an aggregate cosine.
func TestGemmaVNNIM3N24PackedPerBlock(t *testing.T) {
	if !hasAVX512 || !hasAVX512VNNI {
		t.Skip("no AVX-512 VNNI")
	}
	const K, N = 768, 2
	rng := rand.New(rand.NewSource(7))
	b := &Q8Tensor{Rows: N, Cols: K}
	b.Packed = make([]byte, N*K)
	b.Scales = make([]float32, N*24)
	for i := range b.Packed {
		b.Packed[i] = byte(rng.Intn(256))
	}
	for i := range b.Scales {
		b.Scales[i] = rng.Float32()*0.02 + 0.001
	}
	for blk := 0; blk < 24; blk++ {
		a := make([]float32, 3*K)
		for r := 0; r < 3; r++ {
			for i := 0; i < 32; i++ {
				a[r*K+blk*32+i] = rng.Float32()*2 - 1
			}
		}
		var aq gemmaM3AQ
		quantizeGemmaM3Q8U(aq.q[:3*K], aq.s[:72], a, K)
		got := make([]float32, 3*N)
		gemmaVNNIM3N24PackedAVX512(&got[0], &aq.q[0], &aq.s[0], &b.Packed[0], &b.Scales[0], N, 0, N)
		var want float64
		for i := 0; i < 32; i++ {
			av := int32(aq.q[blk*32+i]) - 128
			bv := int32(int8(b.Packed[blk*32+i]))
			want += float64(av * bv)
		}
		want *= float64(aq.s[blk]) * float64(b.Scales[blk])
		if d := math.Abs(float64(got[0]) - want); d > 1e-3*math.Max(1, math.Abs(want)) {
			t.Fatalf("block %d: got %v want %v", blk, got[0], want)
		}
	}
}
