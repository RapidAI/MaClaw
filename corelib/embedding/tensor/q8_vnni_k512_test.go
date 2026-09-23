//go:build amd64

package tensor

import (
	"math"
	"testing"
)

// q8K512TestData builds deterministic signed activations / weights / bias.
func q8K512TestData(M, N, K int) (a, bData, bias []float32) {
	a = make([]float32, M*K)
	for i := range a {
		a[i] = float32((i*7)%19-9) * 0.04
	}
	bData = make([]float32, N*K)
	for i := range bData {
		bData[i] = float32((i*5)%23-11) * 0.06
	}
	bias = make([]float32, N)
	for n := range bias {
		bias[n] = float32(n%13-6) * 0.05
	}
	return a, bData, bias
}

// q8K512Ref computes want[m,n] = DotQ8RowScaled(A[m], B[n]) + bias[n] with the
// F32-A (exact) reference, then applies the relu/accum epilogue in-place.
func q8K512Ref(t *testing.T, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, base []float32) []float32 {
	t.Helper()
	want := make([]float32, M*N)
	copy(want, base)
	for m := 0; m < M; m++ {
		for n := ns; n < ne; n++ {
			v := DotQ8RowScaled(a[m*K:(m+1)*K], b, n) + bias[n]
			if relu && v < 0 {
				v = 0
			}
			if accum {
				want[m*N+n] += v
			} else {
				want[m*N+n] = v
			}
		}
	}
	return want
}

func q8K512Compare(t *testing.T, got, want []float32, M, N, ns, ne int, tol float32) {
	t.Helper()
	if c := gemmCosine32(got, want); c < 0.9995 {
		t.Fatalf("cosine=%g", c)
	}
	for m := 0; m < M; m++ {
		for n := ns; n < ne; n++ {
			d := float32(math.Abs(float64(got[m*N+n] - want[m*N+n])))
			if d > tol {
				t.Fatalf("m=%d n=%d Δ=%g got=%g want=%g", m, n, d, got[m*N+n], want[m*N+n])
			}
		}
	}
}

func TestQ8K512VNNI_PlainMatchesRowDots(t *testing.T) {
	const N, K = 1536, 512
	for _, M := range []int{8, 16, 17, 33, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {1, N - 1}, {2, 999}} {
			ns, ne := rg[0], rg[1]
			base := make([]float32, M*N)
			got := make([]float32, M*N)
			matMulQ8Range(got, a, b, bias, M, N, K, ns, ne, false, false)
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, false, false, base)
			q8K512Compare(t, got, want, M, N, ns, ne, 0.1)
		}
	}
}

func TestQ8K512VNNI_AccumMatchesRowDots(t *testing.T) {
	const N, K = 512, 512
	for _, M := range []int{8, 16, 17, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {3, N - 1}} {
			ns, ne := rg[0], rg[1]
			base := make([]float32, M*N)
			for i := range base {
				base[i] = float32((i%11)-5) * 0.03 // residual
			}
			got := append([]float32(nil), base...)
			matMulQ8Range(got, a, b, bias, M, N, K, ns, ne, false, true)
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, false, true, base)
			q8K512Compare(t, got, want, M, N, ns, ne, 0.1)
		}
	}
}

func TestQ8K512VNNI_ReLUMatchesRowDots(t *testing.T) {
	const N, K = 2048, 512
	for _, M := range []int{8, 16, 17, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {5, N - 3}} {
			ns, ne := rg[0], rg[1]
			base := make([]float32, M*N)
			got := make([]float32, M*N)
			matMulQ8Range(got, a, b, bias, M, N, K, ns, ne, true, false)
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, true, false, base)
			q8K512Compare(t, got, want, M, N, ns, ne, 0.1)
		}
	}
}

// TestQ8K512VNNI_ArgmaxMatchesReference checks the fused VNNI argmax (gated
// off by default for precision; tested directly) against a per-row reference
// over exact dots. Near-tie flips (logit gap below the quantization error
// scale) are tolerated but must be rare; separated winners must match exactly.
func TestQ8K512VNNI_ArgmaxMatchesReference(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	for _, tc := range []struct{ M, N int }{{8, 2048}, {17, 2048}, {100, 25055}, {100, 2049}} {
		M, N := tc.M, tc.N
		a, bData, bias := q8K512TestData(M, N, 512)
		b := QuantizeToQ8(bData, N, 512)
		for _, rg := range [][2]int{{0, N}, {1, N - 1}} {
			ns, ne := rg[0], rg[1]
			bestV := make([]float32, M)
			bestI := make([]int, M)
			for m := range bestV {
				bestV[m] = math.Float32frombits(0xff800000) // -inf
				bestI[m] = -1
			}
			if !argmaxK512VNNI(bestV, bestI, a, b, bias, M, ns, ne) {
				t.Fatalf("argmaxK512VNNI returned false for M=%d N=%d", M, N)
			}
			// Reference argmax + near-tie audit.
			ties := 0
			for m := 0; m < M; m++ {
				rv, ri := float32(math.Float32frombits(0xff800000)), -1
				for n := ns; n < ne; n++ {
					v := DotQ8RowScaled(a[m*512:(m+1)*512], b, n) + bias[n]
					if v > rv {
						rv, ri = v, n
					}
				}
				if bestI[m] != ri {
					gap := rv - (DotQ8RowScaled(a[m*512:(m+1)*512], b, bestI[m]) + bias[bestI[m]])
					if gap > 0.1 {
						t.Fatalf("M=%d N=%d row=%d got id=%d v=%g want id=%d v=%g gap=%g",
							M, N, m, bestI[m], bestV[m], ri, rv, gap)
					}
					ties++
				}
			}
			if ties > M/10+1 {
				t.Fatalf("M=%d N=%d too many near-tie flips: %d/%d", M, N, ties, M)
			}
		}
	}
}

// TestQ8K512VNNI_PublicAPIs exercises the exported entry points end to end.
func TestQ8K512VNNI_PublicAPIs(t *testing.T) {
	const M, N, K = 100, 1536, 512
	a, bData, bias := q8K512TestData(M, N, K)
	b := QuantizeToQ8(bData, N, K)

	got := make([]float32, M*N)
	MatMulQ8Bias(got, a, b, bias, M, N, K)
	want := q8K512Ref(t, a, b, bias, M, N, K, 0, N, false, false, make([]float32, M*N))
	q8K512Compare(t, got, want, M, N, 0, N, 0.1)

	// ReLU entry (N=2048 shape)
	a2, bData2, bias2 := q8K512TestData(M, 2048, K)
	b2 := QuantizeToQ8(bData2, 2048, K)
	got2 := make([]float32, M*2048)
	MatMulQ8BiasReLU(got2, a2, b2, bias2, M, 2048, K)
	want2 := q8K512Ref(t, a2, b2, bias2, M, 2048, K, 0, 2048, true, false, make([]float32, M*2048))
	q8K512Compare(t, got2, want2, M, 2048, 0, 2048, 0.1)

	// Accum entry (N=512 out-proj shape)
	a3, bData3, bias3 := q8K512TestData(M, 512, K)
	b3 := QuantizeToQ8(bData3, 512, K)
	base := make([]float32, M*512)
	for i := range base {
		base[i] = float32((i%9)-4) * 0.02
	}
	got3 := append([]float32(nil), base...)
	MatMulQ8BiasAdd(got3, a3, b3, bias3, M, 512, K)
	want3 := q8K512Ref(t, a3, b3, bias3, M, 512, K, 0, 512, false, true, base)
	q8K512Compare(t, got3, want3, M, 512, 0, 512, 0.1)

	// Argmax public entry
	ids := make([]int, M)
	MatMulQ8Argmax(ids, a, b, bias, M, N, K)
	for m := 0; m < M; m++ {
		rv, ri := float32(-math.MaxFloat32), -1
		for n := 0; n < N; n++ {
			v := DotQ8RowScaled(a[m*K:(m+1)*K], b, n) + bias[n]
			if v > rv {
				rv, ri = v, n
			}
		}
		if ids[m] != ri {
			gap := rv - (DotQ8RowScaled(a[m*K:(m+1)*K], b, ids[m]) + bias[ids[m]])
			if gap > 0.1 {
				t.Fatalf("argmax row=%d got id=%d want id=%d gap=%g", m, ids[m], ri, gap)
			}
		}
	}
}

// TestQ8K512VNNI_SmallMFallsBack ensures the M<8 gate does not disturb the
// existing paths.
func TestQ8K512VNNI_SmallMFallsBack(t *testing.T) {
	const M, N, K = 7, 1536, 512
	a, bData, bias := q8K512TestData(M, N, K)
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	matMulQ8Range(got, a, b, bias, M, N, K, 0, N, false, false)
	want := q8K512Ref(t, a, b, bias, M, N, K, 0, N, false, false, make([]float32, M*N))
	// M<8 never enters the VNNI path, so results are near-exact.
	q8K512Compare(t, got, want, M, N, 0, N, 1e-4)
}
