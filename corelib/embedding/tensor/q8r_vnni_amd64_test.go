//go:build amd64

package tensor

import (
	"math"
	"testing"
)

// TestQ8RK512VNNI_* cover the per-row-scale int-accumulate path. The reference
// is the exact F32-A dot against the original Q8_0 weights; tolerance accounts
// for both A per-row quantization and the B per-row requantization (~2× the
// per-block VNNI path's error).

func TestQ8RK512VNNI_PlainMatchesRowDots(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 1536, 512
	for _, M := range []int{8, 16, 17, 33, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {1, N - 1}} {
			ns, ne := rg[0], rg[1]
			got := make([]float32, M*N)
			if !tryFusedK512RowVNNI(got, a, b, bias, M, N, K, ns, ne, false, false) {
				t.Fatalf("tryFusedK512RowVNNI false M=%d", M)
			}
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, false, false, make([]float32, M*N))
			if c := gemmCosine32(got, want); c < 0.999 {
				t.Fatalf("M=%d cosine=%g", M, c)
			}
			for m := 0; m < M; m++ {
				for n := ns; n < ne; n++ {
					d := float32(math.Abs(float64(got[m*N+n] - want[m*N+n])))
					if d > 0.15 {
						t.Fatalf("M=%d m=%d n=%d Δ=%g got=%g want=%g", M, m, n, d, got[m*N+n], want[m*N+n])
					}
				}
			}
		}
	}
}

func TestQ8RK512VNNI_OutPlainN512(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 512, 512
	for _, M := range []int{8, 17, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		got := make([]float32, M*N)
		if !tryFusedK512RowVNNI(got, a, b, bias, M, N, K, 0, N, false, false) {
			t.Fatalf("tryFusedK512RowVNNI false M=%d", M)
		}
		want := q8K512Ref(t, a, b, bias, M, N, K, 0, N, false, false, make([]float32, M*N))
		if c := gemmCosine32(got, want); c < 0.999 {
			t.Fatalf("M=%d cosine=%g", M, c)
		}
		for i := range got {
			d := float32(math.Abs(float64(got[i] - want[i])))
			if d > 0.15 {
				t.Fatalf("M=%d i=%d Δ=%g got=%g want=%g", M, i, d, got[i], want[i])
			}
		}
	}
}

func TestQ8RK512VNNI_ReLUMatchesRowDots(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 2048, 512
	for _, M := range []int{8, 16, 17, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {5, N - 3}} {
			ns, ne := rg[0], rg[1]
			got := make([]float32, M*N)
			if !tryFusedK512RowVNNI(got, a, b, bias, M, N, K, ns, ne, true, false) {
				t.Fatalf("tryFusedK512RowVNNI false M=%d", M)
			}
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, true, false, make([]float32, M*N))
			if c := gemmCosine32(got, want); c < 0.999 {
				t.Fatalf("M=%d cosine=%g", M, c)
			}
			for m := 0; m < M; m++ {
				for n := ns; n < ne; n++ {
					d := float32(math.Abs(float64(got[m*N+n] - want[m*N+n])))
					if d > 0.15 {
						t.Fatalf("M=%d m=%d n=%d Δ=%g got=%g want=%g", M, m, n, d, got[m*N+n], want[m*N+n])
					}
				}
			}
		}
	}
}

// TestQ8RK512VNNI_RepackSanity checks the repacked rows directly: dequantized
// Q8R values must match the Q8_0 dequantization within the requant step.
func TestQ8RK512VNNI_RepackSanity(t *testing.T) {
	const N, K = 64, 512
	_, bData, _ := q8K512TestData(1, N, K)
	b := QuantizeToQ8(bData, N, K)
	rs := q8RowScaleFor(b, 16)
	if len(rs.q) != N*K || len(rs.scale) != N || len(rs.sum128) != N {
		t.Fatalf("bad sidecar dims")
	}
	for row := 0; row < N; row++ {
		var sum int32
		for i := 0; i < K; i++ {
			sum += int32(rs.q[row*K+i])
		}
		if float32(sum*128) != rs.sum128[row] {
			t.Fatalf("row %d sum128 mismatch: %d vs %g", row, sum*128, rs.sum128[row])
		}
		// spot-check dequantized closeness for a few elements
		for _, i := range []int{0, 100, 511} {
			q0 := int8(b.Data[row*16*q8BlockBytes+2+(i/32)*q8BlockBytes+int(i%32)])
			v0 := float32(q0) * b.Scales[row*16+int(i)/32]
			v1 := float32(rs.q[row*K+i]) * rs.scale[row]
			if d := float32(math.Abs(float64(v0 - v1))); d > rs.scale[row] {
				t.Fatalf("row %d i %d: |Δ|=%g > step %g (v0=%g v1=%g)", row, i, d, rs.scale[row], v0, v1)
			}
		}
	}
}
