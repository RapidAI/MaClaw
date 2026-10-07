//go:build amd64

package tensor

import (
	"math"
	"testing"
)

// TestQ8ZK2048VNNI_AccumMatchesRowDots covers the (precision-gated) ZMM
// per-block K=2048 FFN-down path against the exact row-dot reference.
// A is non-negative (ReLU) as required by the u8 quantizer.
func TestQ8ZK2048VNNI_AccumMatchesRowDots(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 512, 2048
	for _, M := range []int{8, 16, 17, 100} {
		a := make([]float32, M*K)
		for i := range a {
			a[i] = float32((i*7)%19) * 0.04
		}
		bData := make([]float32, N*K)
		for i := range bData {
			bData[i] = float32((i*5)%23-11) * 0.06
		}
		bias := make([]float32, N)
		for n := range bias {
			bias[n] = float32(n%13-6) * 0.05
		}
		b := QuantizeToQ8(bData, N, K)
		base := make([]float32, M*N)
		for i := range base {
			base[i] = float32((i%11)-5) * 0.03
		}
		for _, rg := range [][2]int{{0, N}, {37, 163}, {1, N - 1}} {
			ns, ne := rg[0], rg[1]
			want := append([]float32(nil), base...)
			for m := 0; m < M; m++ {
				for n := ns; n < ne; n++ {
					want[m*N+n] += DotQ8RowScaled(a[m*K:(m+1)*K], b, n) + bias[n]
				}
			}
			got := append([]float32(nil), base...)
			if !tryFusedK2048ZmmVNNI(got, a, b, bias, M, ns, ne) {
				t.Fatalf("tryFusedK2048ZmmVNNI false M=%d", M)
			}
			if c := gemmCosine32(got, want); c < 0.9995 {
				t.Fatalf("M=%d range=%v cosine=%g", M, rg, c)
			}
			for m := 0; m < M; m++ {
				for n := ns; n < ne; n++ {
					d := float32(math.Abs(float64(got[m*N+n] - want[m*N+n])))
					if d > 0.1 {
						t.Fatalf("M=%d m=%d n=%d Δ=%g got=%g want=%g", M, m, n, d, got[m*N+n], want[m*N+n])
					}
				}
			}
		}
	}
}
