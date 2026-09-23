//go:build amd64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// gemmaMaddwdRowM8ScalarRef computes the reference for the row-scale maddwd
// M8 kernels, reading weights straight from the Q8_0 Data blocks:
// out[r][n] = aS[r] · Σ_blk bS[n][blk]·Σ_i a16[r][i]·int8(w8[n][i]).
func gemmaMaddwdRowM8ScalarRef(q []int16, s []float32, b *Q8Tensor, rows, N, nBlocks int) []float32 {
	K := nBlocks * q8BlockSize
	want := make([]float32, rows*N)
	for r := 0; r < rows; r++ {
		for n := 0; n < N; n++ {
			var acc float64
			for blk := 0; blk < nBlocks; blk++ {
				off := (n*nBlocks+blk)*q8BlockBytes + 2
				var dot int32
				for i := 0; i < 32; i++ {
					dot += int32(q[r*K+blk*32+i]) * int32(int8(b.Data[off+i]))
				}
				acc += float64(b.Scales[n*nBlocks+blk]) * float64(dot)
			}
			want[r*N+n] = float32(acc * float64(s[r]))
		}
	}
	return want
}

func testGemmaMaddwdRowM8(t *testing.T, K, N, nBlocks int, ns, ne int, seed int64) {
	t.Helper()
	if !hasAVX2andFMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(seed))
	gg := make([]byte, N*nBlocks*q8BlockBytes)
	b := &Q8Tensor{Rows: N, Cols: K, Data: gg}
	encodeQ8Blocks(rng, b)
	b.PrepareScales()
	a := make([]float32, 8*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	q := make([]int16, 8*K)
	s := make([]float32, 8)
	gemmaQuantizeS16Scalar(q, s, a, 8, K)

	got := make([]float32, 8*N)
	if K == 768 {
		gemmaMaddwdRowM8N24AVX2(&got[0], &q[0], &s[0], &b.Data[0], &b.Scales[0], N, ns, ne)
	} else {
		gemmaMaddwdRowM8N36AVX2(&got[0], &q[0], &s[0], &b.Data[0], &b.Scales[0], N, ns, ne)
	}

	want := gemmaMaddwdRowM8ScalarRef(q, s, b, 8, N, nBlocks)
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

// testGemmaMaddwdRowM4 pins the 4-row × 2-column M4 kernels against the same
// scalar reference as the M8 kernels.
func testGemmaMaddwdRowM4(t *testing.T, K, N, nBlocks int, ns, ne int, seed int64) {
	t.Helper()
	if !hasAVX2andFMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(seed))
	gg := make([]byte, N*nBlocks*q8BlockBytes)
	b := &Q8Tensor{Rows: N, Cols: K, Data: gg}
	encodeQ8Blocks(rng, b)
	b.PrepareScales()
	a := make([]float32, 4*K)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
	}
	q := make([]int16, 4*K)
	s := make([]float32, 4)
	gemmaQuantizeS16Scalar(q, s, a, 4, K)

	got := make([]float32, 4*N)
	if K == 768 {
		gemmaMaddwdRowM4N24AVX2(&got[0], &q[0], &s[0], &b.Data[0], &b.Scales[0], N, ns, ne)
	} else {
		gemmaMaddwdRowM4N36AVX2(&got[0], &q[0], &s[0], &b.Data[0], &b.Scales[0], N, ns, ne)
	}

	want := gemmaMaddwdRowM8ScalarRef(q, s, b, 4, N, nBlocks)
	for r := 0; r < 4; r++ {
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

func TestGemmaMaddwdRowM4N24MatchesScalar(t *testing.T) {
	testGemmaMaddwdRowM4(t, 768, 10, 24, 0, 10, 61)
}

func TestGemmaMaddwdRowM4N36MatchesScalar(t *testing.T) {
	testGemmaMaddwdRowM4(t, 1152, 8, 36, 0, 8, 67)
}

// TestGemmaMaddwdRowM4SubRange exercises a non-zero even ns (the N-split
// worker case); odd edges are covered by gemmaMaddwdM4Cols/M4Range tests.
func TestGemmaMaddwdRowM4SubRange(t *testing.T) {
	testGemmaMaddwdRowM4(t, 768, 16, 24, 4, 12, 71)
	testGemmaMaddwdRowM4(t, 1152, 16, 36, 2, 14, 73)
}

func TestGemmaMaddwdRowM8N24MatchesScalar(t *testing.T) {
	testGemmaMaddwdRowM8(t, 768, 10, 24, 0, 10, 31)
}

func TestGemmaMaddwdRowM8N36MatchesScalar(t *testing.T) {
	testGemmaMaddwdRowM8(t, 1152, 8, 36, 0, 8, 37)
}

// TestGemmaMaddwdRowM8N24SubRange exercises a non-zero ns (the N-split worker
// case) including an odd start column handled by the Go-side tail logic.
func TestGemmaMaddwdRowM8SubRange(t *testing.T) {
	testGemmaMaddwdRowM8(t, 768, 16, 24, 4, 12, 41)
	testGemmaMaddwdRowM8(t, 1152, 16, 36, 5, 13, 43)
}

// TestGemmaQuantizeS16SIMDvsScalar pins the AVX2 quantizer against the scalar
// reference: scales must match closely, payloads may differ by 1 LSB
// (nearest-even vs half-away rounding), reconstruction error stays within
// half a scale quantum.
func TestGemmaQuantizeS16SIMDvsScalar(t *testing.T) {
	if !hasAVX2andFMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(53))
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
		for i := range a[5*K : 6*K] {
			a[5*K+i] = 0 // zero row edge case
		}

		q0 := make([]int16, rows*K)
		s0 := make([]float32, rows)
		gemmaQuantizeS16Scalar(q0, s0, a, rows, K)
		q1 := make([]int16, rows*K)
		s1 := make([]float32, rows)
		gemmaQuantizeS16AVX2(&q1[0], &s1[0], &a[0], rows, nBlocks)
		for i := range s0 {
			if math.Abs(float64(s0[i]-s1[i])) > 1e-7*math.Max(1, float64(s0[i])) {
				t.Fatalf("K=%d row scale %d: scalar %v simd %v", K, i, s0[i], s1[i])
			}
		}
		for i := range q0 {
			d := int(q0[i]) - int(q1[i])
			if d < -1 || d > 1 {
				t.Fatalf("K=%d q %d: scalar %d simd %d", K, i, q0[i], q1[i])
			}
		}
		// reconstruction error bound for the row-scale path
		for r := 0; r < rows; r++ {
			sc := float64(s1[r])
			for i := 0; i < K; i++ {
				idx := r*K + i
				rec := float64(q1[idx]) * sc
				if d := math.Abs(rec - float64(a[idx])); d > 0.6*sc+1e-6 {
					t.Fatalf("K=%d r=%d i=%d: rec %v want %v (s=%v)", K, r, i, rec, a[idx], sc)
				}
			}
		}
	}
}

// TestGemmaMaddwdM8RangeTails covers M tails (3/4/5 pure-tail via the M4
// kernel or padded M8, 9 = 8+1, 11 = 8+3, 12 = 8+4 via the M8→M4 tail
// dispatch, 15 = 8+4+3, 17 = 8+8+1) and odd column edges through
// gemmaMaddwdM8Range against MatMulQ8N as reference. Quantized-A vs f32
// reference is compared per row by cosine — production cares about direction,
// and cancellation-heavy dots break fixed tolerances. Panels are built via
// gemmaMaddwdPanel (padded to 8 rows) exactly as the production entry points
// do.
func TestGemmaMaddwdM8RangeTails(t *testing.T) {
	if !hasAVX2andFMA {
		t.Skip("no AVX2+FMA")
	}
	rng := rand.New(rand.NewSource(59))
	for _, K := range []int{768, 1152} {
		nBlocks := K / q8BlockSize
		const N = 64
		gg := make([]byte, N*nBlocks*q8BlockBytes)
		b := &Q8Tensor{Rows: N, Cols: K, Data: gg}
		encodeQ8Blocks(rng, b)
		b.PrepareScales()
		for _, M := range []int{3, 4, 5, 8, 9, 11, 12, 15, 16, 17} {
			a := make([]float32, M*K)
			for i := range a {
				a[i] = rng.Float32()*2 - 1
			}
			want := make([]float32, M*N)
			MatMulQ8N(want, a, b, M, N, K, 1)
			panel := gemmaMaddwdPanel(a, M, K)
			for _, rg := range [][2]int{{0, N}, {1, N - 1}, {3, 41}} {
				got := make([]float32, M*N)
				gemmaMaddwdM8Range(got, panel.q, panel.s, a, b, M, N, K, rg[0], rg[1])
				for r := 0; r < M; r++ {
					var dot, na, nb float64
					for n := rg[0]; n < rg[1]; n++ {
						i := r*N + n
						dot += float64(got[i]) * float64(want[i])
						na += float64(got[i]) * float64(got[i])
						nb += float64(want[i]) * float64(want[i])
					}
					cos := dot / (math.Sqrt(na) * math.Sqrt(nb))
					if cos < 0.99999 {
						t.Fatalf("K=%d M=%d range=%v row=%d: cosine=%g want >=0.99999", K, M, rg, r, cos)
					}
				}
			}
			putGemmaAS16(panel)
		}
	}
}
