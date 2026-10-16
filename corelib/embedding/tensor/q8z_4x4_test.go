//go:build amd64

package tensor

import (
	"math"
	"testing"
)

// TestQ8Z4x4MatchesDual8 verifies the 4×4-tile kernels are bit-identical (or
// 1e-7) to the dual8 M-outer path on the same inputs, across shapes, odd
// ranges, and M tails — for both per-call quantize and prebuilt panels.
func TestQ8Z4x4MatchesDual8(t *testing.T) {
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	defer SetQ8ZK5124x4ForTest(false)
	for _, tc := range []struct {
		M, N int
		relu bool
	}{
		{16, 1536, false}, {17, 1536, false}, {100, 1536, false},
		{16, 512, false}, {17, 512, false},
		{16, 2048, true}, {17, 2048, true}, {100, 2048, true},
	} {
		M, N, relu := tc.M, tc.N, tc.relu
		a, bData, bias := q8K512TestData(M, N, 512)
		b := QuantizeToQ8(bData, N, 512)
		for _, rg := range [][2]int{{0, N}, {1, N - 1}, {3, min(999, N)}} {
			ns, ne := rg[0], rg[1]
			want := make([]float32, M*N)
			if !tryFusedK512ZmmVNNI(want, a, b, bias, M, N, 512, ns, ne, relu, false, nil) {
				t.Fatalf("dual8 false M=%d N=%d", M, N)
			}
			for _, prebuilt := range []bool{false, true} {
				got := make([]float32, M*N)
				var panels []*q8APanel4K512Z
				if prebuilt {
					panels = q8K512ZPanels4For(a, M)
				}
				if !tryFusedK512Zmm4x4(got, a, b, bias, M, N, 512, ns, ne, relu, false, panels) {
					t.Fatalf("4x4 false M=%d N=%d prebuilt=%v", M, N, prebuilt)
				}
				if prebuilt {
					for _, ap := range panels {
						q8APanel4K512ZPool.Put(ap)
					}
				}
				for m := 0; m < M; m++ {
					for n := ns; n < ne; n++ {
						d := float32(math.Abs(float64(got[m*N+n] - want[m*N+n])))
						// Kernel-covered columns must match tightly. The ≤3
						// column tail takes exact f32 dots here vs the dual8
						// pair kernel there (which quantizes A), so the tail
						// carries A-quantization-level deviation (~1e-3).
						tol := float32(1e-6)
						if n >= ns+((ne-ns)/4)*4 {
							tol = 0.01
						}
						if d > tol {
							t.Fatalf("M=%d N=%d range=%v prebuilt=%v m=%d n=%d Δ=%g got=%g want=%g",
								M, N, rg, prebuilt, m, n, d, got[m*N+n], want[m*N+n])
						}
					}
				}
			}
		}
	}
}
