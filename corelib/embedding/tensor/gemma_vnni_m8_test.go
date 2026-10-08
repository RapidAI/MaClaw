//go:build amd64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// gemmaVNNIRowM8ScalarRef computes the reference for the row-scale M8 kernels:
// out[r][n] = aS[r] · Σ_blk bS[n][blk]·Σ_i (aQ[r][i]−128)·int8(bPacked[n][i]).
func gemmaVNNIRowM8ScalarRef(q []byte, s []float32, b *Q8Tensor, rows, N, nBlocks int) []float32 {
	K := nBlocks * q8BlockSize
	want := make([]float32, rows*N)
	for r := 0; r < rows; r++ {
		for n := 0; n < N; n++ {
			var acc float64
			for blk := 0; blk < nBlocks; blk++ {
				var dot int32
				for i := 0; i < 32; i++ {
					av := int32(q[r*K+blk*32+i]) - 128
					bv := int32(int8(b.Packed[n*K+blk*32+i]))
					dot += av * bv
				}
				acc += float64(b.Scales[n*nBlocks+blk]) * float64(dot)
			}
			want[r*N+n] = float32(acc * float64(s[r]))
		}
	}
	return want
}

func randGemmaPackedB(rng *rand.Rand, N, nBlocks int) *Q8Tensor {
	K := nBlocks * q8BlockSize
	b := &Q8Tensor{Rows: N, Cols: K}
	b.Packed = make([]byte, N*K)
	b.Scales = make([]float32, N*nBlocks)
	for i := range b.Packed {
		b.Packed[i] = byte(rng.Intn(256))
	}
	for i := range b.Scales {
		b.Scales[i] = rng.Float32()*0.02 + 0.001
	}
	return b
}

func testGemmaVNNIRowM8(t *testing.T, K, N, nBlocks int, ns, ne int, seed int64) {
	t.Helper()
	if !hasAVX512VNNI {
		t.Skip("no AVX-512 VNNI")
	}
	rng := rand.New(rand.NewSource(seed))
	b := randGemmaPackedB(rng, N, nBlocks)
	a := make([]float32, 8*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	q := make([]byte, 8*K)
	s := make([]float32, 8)
	gemmaQuantizeQ8URowScalar(q, s, a, 8, K)

	got := make([]float32, 8*N)
	b.ensureColBias()
	if K == 768 {
		gemmaVNNIRowM8N24PackedAVX512(&got[0], &q[0], &s[0], &b.Packed[0], &b.Scales[0], N, ns, ne, &b.ColBias[0])
	} else {
		gemmaVNNIRowM8N36PackedAVX512(&got[0], &q[0], &s[0], &b.Packed[0], &b.Scales[0], N, ns, ne, &b.ColBias[0])
	}

	want := gemmaVNNIRowM8ScalarRef(q, s, b, 8, N, nBlocks)
	for r := 0; r < 8; r++ {
		for n := ns; n < ne; n++ {
			i := r*N + n
			d := math.Abs(float64(got[i] - want[i]))
			m := math.Abs(float64(want[i]))
			if m < 1 {
				m = 1
			}
			if d/m > 1e-3 {
				t.Fatalf("K=%d row %d col %d: got %v want %v (rel err %g)", K, r, n, got[i], want[i], d/m)
			}
		}
	}
}

func TestGemmaVNNIRowM8N24PackedMatchesScalar(t *testing.T) {
	testGemmaVNNIRowM8(t, 768, 10, 24, 0, 10, 11)
}

func TestGemmaVNNIRowM8N36PackedMatchesScalar(t *testing.T) {
	testGemmaVNNIRowM8(t, 1152, 8, 36, 0, 8, 13)
}

// TestGemmaVNNIRowM8N24PackedSubRange exercises a non-zero even ns (the
// N-split worker case).
func TestGemmaVNNIRowM8N24PackedSubRange(t *testing.T) {
	testGemmaVNNIRowM8(t, 768, 16, 24, 4, 12, 17)
}

// TestGemmaQuantizeQ8USIMDvsScalar pins both AVX-512 quantizers against the
// scalar references: scales must match closely, payloads may differ by 1 LSB
// (nearest-even + rcp14 vs half-away + exact division), and reconstruction
// error stays within half a scale quantum plus rcp slack.
func TestGemmaQuantizeQ8USIMDvsScalar(t *testing.T) {
	if !hasAVX512 {
		t.Skip("no AVX-512")
	}
	rng := rand.New(rand.NewSource(19))
	for _, K := range []int{768, 1152} {
		nBlocks := K / q8BlockSize
		const rows = 8
		a := make([]float32, rows*K)
		for i := range a {
			a[i] = (rng.Float32()*2 - 1) * 3
		}
		for i := 32; i < 64; i++ {
			a[3*K+i] = 0 // zero block edge case
		}
		for r := 4; r < 5; r++ {
			for i := range a[r*K : (r+1)*K] {
				a[r*K+i] = 0 // zero row edge case
			}
		}

		// per-block quantizer
		q0 := make([]byte, rows*K)
		s0 := make([]float32, rows*nBlocks)
		gemmaQuantizeQ8UScalar(q0, s0, a, rows, K)
		q1 := make([]byte, rows*K)
		s1 := make([]float32, rows*nBlocks)
		gemmaQuantizeQ8UAVX512(&q1[0], &s1[0], &a[0], rows, nBlocks)
		for i := range s0 {
			if math.Abs(float64(s0[i]-s1[i])) > 1e-7*math.Max(1, float64(s0[i])) {
				t.Fatalf("K=%d scale %d: scalar %v simd %v", K, i, s0[i], s1[i])
			}
		}
		for i := range q0 {
			d := int(q0[i]) - int(q1[i])
			if d < -1 || d > 1 {
				t.Fatalf("K=%d q %d: scalar %d simd %d", K, i, q0[i], q1[i])
			}
		}

		// per-row quantizer
		qr0 := make([]byte, rows*K)
		sr0 := make([]float32, rows)
		gemmaQuantizeQ8URowScalar(qr0, sr0, a, rows, K)
		qr1 := make([]byte, rows*K)
		sr1 := make([]float32, rows)
		gemmaQuantizeQ8URowAVX512(&qr1[0], &sr1[0], &a[0], rows, nBlocks)
		for i := range sr0 {
			if math.Abs(float64(sr0[i]-sr1[i])) > 1e-7*math.Max(1, float64(sr0[i])) {
				t.Fatalf("K=%d row scale %d: scalar %v simd %v", K, i, sr0[i], sr1[i])
			}
		}
		for i := range qr0 {
			d := int(qr0[i]) - int(qr1[i])
			if d < -1 || d > 1 {
				t.Fatalf("K=%d row q %d: scalar %d simd %d", K, i, qr0[i], qr1[i])
			}
		}
		// reconstruction error bound for the row-scale path
		for r := 0; r < rows; r++ {
			sc := float64(sr1[r])
			for i := 0; i < K; i++ {
				idx := r*K + i
				rec := float64(int(qr1[idx])-128) * sc
				if d := math.Abs(rec - float64(a[idx])); d > 0.6*sc+1e-6 {
					t.Fatalf("K=%d r=%d i=%d: rec %v want %v (s=%v)", K, r, i, rec, a[idx], sc)
				}
			}
		}
	}
}

// TestGemmaVNNIM8RangeTails covers M tails (11 = 8+3, 13 = 8+4+1) and odd
// column edges through gemmaVNNIM8Range against MatMulQ8N as reference.
// Quantized-A vs f32 reference is compared per row by cosine — production
// cares about direction, and cancellation-heavy dots break fixed tolerances.
func TestGemmaVNNIM8RangeTails(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no AVX-512 VNNI")
	}
	rng := rand.New(rand.NewSource(23))
	for _, K := range []int{768, 1152} {
		nBlocks := K / q8BlockSize
		const N = 64
		gg := make([]byte, N*nBlocks*q8BlockBytes)
		b := &Q8Tensor{Rows: N, Cols: K, Data: gg}
		encodeQ8Blocks(rng, b)
		b.PrepareScales()
		b.PackQS()
		for _, M := range []int{8, 11, 13, 16} {
			a := make([]float32, M*K)
			for i := range a {
				a[i] = rng.Float32()*2 - 1
			}
			want := make([]float32, M*N)
			MatMulQ8N(want, a, b, M, N, K, 1)
			panel := getGemmaAQ8(M, K)
			gemmaQuantizeQ8URow(panel.q, panel.s, a, M, K)
			for _, rg := range [][2]int{{0, N}, {1, N - 1}, {3, 41}} {
				got := make([]float32, M*N)
				gemmaVNNIM8Range(got, panel.q, panel.s, a, b, M, N, K, rg[0], rg[1])
				for r := 0; r < M; r++ {
					var dot, na, nb float64
					for n := rg[0]; n < rg[1]; n++ {
						i := r*N + n
						dot += float64(got[i]) * float64(want[i])
						na += float64(got[i]) * float64(got[i])
						nb += float64(want[i]) * float64(want[i])
					}
					cos := dot / (math.Sqrt(na) * math.Sqrt(nb))
					if cos < 0.999 {
						t.Fatalf("K=%d M=%d range=%v row=%d: cosine=%g want >=0.999", K, M, rg, r, cos)
					}
				}
			}
			putGemmaAQ8(panel)
		}
	}
}

// encodeQ8Blocks fills b.Data with Q8_0 blocks (f16 scale + 32 int8) and keeps
// Packed/Scales consistent via PrepareScales+PackQS by the caller.
func encodeQ8Blocks(rng *rand.Rand, b *Q8Tensor) {
	nBlocks := b.Cols / q8BlockSize
	for r := 0; r < b.Rows; r++ {
		for blk := 0; blk < nBlocks; blk++ {
			off := (r*nBlocks + blk) * q8BlockBytes
			sc := float16FromFloat32Test(rng.Float32()*0.02 + 0.001)
			b.Data[off] = byte(sc)
			b.Data[off+1] = byte(sc >> 8)
			for i := 0; i < 32; i++ {
				b.Data[off+2+i] = byte(rng.Intn(256))
			}
		}
	}
}

// float16FromFloat32Test encodes IEEE 754 binary16 (round-to-nearest-even).
func float16FromFloat32Test(f float32) uint16 {
	const (
		maxHalf = 65504.0
		minHalf = 6.103515625e-05 // 2^-14, smallest normal half
		halfSub = 5.9604644775390625e-08
	)
	bits := math.Float32bits(f)
	sign := uint16(bits >> 16 & 0x8000)
	af := f
	if af < 0 {
		af = -af
	}
	if af != af {
		return sign | 0x7e00
	}
	if af > maxHalf {
		return sign | 0x7c00
	}
	if af < halfSub {
		return sign
	}
	if af < minHalf {
		return sign | uint16(math.Float32bits(af*16777216)>>13) // 2^24 scale → subnormal
	}
	e := int(bits>>23&0xff) - 127 + 15
	m := bits & 0x7fffff
	hm := m + 0xfff
	if hm&0x800000 != 0 {
		e++
		hm = 0
	}
	return sign | uint16(e<<10) | uint16(hm>>13)
}
