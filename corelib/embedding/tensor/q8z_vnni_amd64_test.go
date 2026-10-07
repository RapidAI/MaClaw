//go:build amd64

package tensor

import (
	"math"
	"sync"
	"testing"
)

// TestQ8ZK512VNNI_* cover the ZMM per-block path. Same numerics as the per-block
// YMM path (per-block A quant + exact Q8_0 B), so the same tolerance applies.

func TestQ8ZK512VNNI_PlainMatchesRowDots(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 1536, 512
	for _, M := range []int{8, 9, 16, 17, 33, 97, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {1, N - 1}} {
			ns, ne := rg[0], rg[1]
			got := make([]float32, M*N)
			if !tryFusedK512ZmmVNNI(got, a, b, bias, M, N, K, ns, ne, false, false, nil) {
				t.Fatalf("tryFusedK512ZmmVNNI false M=%d", M)
			}
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, false, false, make([]float32, M*N))
			if c := gemmCosine32(got, want); c < 0.9995 {
				t.Fatalf("M=%d cosine=%g", M, c)
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

func TestQ8ZK512VNNI_OutPlainN512(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 512, 512
	for _, M := range []int{8, 9, 17, 97, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		got := make([]float32, M*N)
		if !tryFusedK512ZmmVNNI(got, a, b, bias, M, N, K, 0, N, false, false, nil) {
			t.Fatalf("tryFusedK512ZmmVNNI false M=%d", M)
		}
		want := q8K512Ref(t, a, b, bias, M, N, K, 0, N, false, false, make([]float32, M*N))
		if c := gemmCosine32(got, want); c < 0.9995 {
			t.Fatalf("M=%d cosine=%g", M, c)
		}
		for i := range got {
			d := float32(math.Abs(float64(got[i] - want[i])))
			if d > 0.1 {
				t.Fatalf("M=%d i=%d Δ=%g got=%g want=%g", M, i, d, got[i], want[i])
			}
		}
	}
}

func TestQ8ZK512VNNI_ReLUMatchesRowDots(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	SetFusedK512VNNIForTest(true)
	t.Cleanup(func() { SetFusedK512VNNIForTest(false) })
	SetK512VNNILayerGatesForTest(true, true, true)
	t.Cleanup(func() { SetK512VNNILayerGatesForTest(true, true, false) })
	const N, K = 2048, 512
	for _, M := range []int{8, 9, 16, 17, 97, 100} {
		a, bData, bias := q8K512TestData(M, N, K)
		b := QuantizeToQ8(bData, N, K)
		for _, rg := range [][2]int{{0, N}, {5, N - 3}} {
			ns, ne := rg[0], rg[1]
			got := make([]float32, M*N)
			if !tryFusedK512ZmmVNNI(got, a, b, bias, M, N, K, ns, ne, true, false, nil) {
				t.Fatalf("tryFusedK512ZmmVNNI false M=%d", M)
			}
			want := q8K512Ref(t, a, b, bias, M, N, K, ns, ne, true, false, make([]float32, M*N))
			if c := gemmCosine32(got, want); c < 0.9995 {
				t.Fatalf("M=%d cosine=%g", M, c)
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

// TestQ8ZK512RowDualKernelMatchesDual8 asserts the 1-row tail kernel produces
// the same values as the full dual8 kernel for the same panel row.
func TestQ8ZK512RowDualKernelMatchesDual8(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("no VNNI")
	}
	const N, K = 1536, 512
	a, bData, bias := q8K512TestData(8, N, K)
	b := QuantizeToQ8(bData, N, K)
	st := q8StrippedFor(b, 16)
	ap := q8APanel8K512ZPool.Get().(*q8APanel8K512Z)
	defer q8APanel8K512ZPool.Put(ap)
	quantizePanel8Q8SK512Zmm(ap, a[:8*512])
	for _, r := range []int{0, 3, 7} {
		full := make([]float32, 8*N)
		q8zPlainDual8N1536(full, ap, st, b, 0, 10, bias[10], bias[11])
		var pair [2]float32
		q8zRowDualVNNIK512(&pair[0], &ap.q[r*64], &ap.sAvec[r*16], &st.q[0],
			&b.Scales[10*16], &b.Scales[11*16],
			10*k512ZRowBytes, 11*k512ZRowBytes, bias[10], bias[11], false)
		for i := 0; i < 2; i++ {
			want := full[r*N+10+i]
			d := math.Abs(float64(pair[i] - want))
			if d > 1e-4 && d > 1e-4*math.Abs(float64(want)) {
				t.Fatalf("r=%d i=%d Δ=%g got=%g want=%g", r, i, d, pair[i], want)
			}
		}
	}
	// ReLU variant, with a clamping bias to exercise VMAXSS.
	const NR, KR = 2048, 512
	ar, bDataR, _ := q8K512TestData(8, NR, KR)
	br := QuantizeToQ8(bDataR, NR, KR)
	str := q8StrippedFor(br, 16)
	apR := q8APanel8K512ZPool.Get().(*q8APanel8K512Z)
	defer q8APanel8K512ZPool.Put(apR)
	quantizePanel8Q8SK512Zmm(apR, ar[:8*KR])
	full := make([]float32, 8*NR)
	q8zReLUDual8N2048(full, apR, str, br, 0, 20, -1e9, -1e9)
	var pair [2]float32
	q8zRowDualVNNIK512(&pair[0], &apR.q[5*64], &apR.sAvec[5*16], &str.q[0],
		&br.Scales[20*16], &br.Scales[21*16],
		20*k512ZRowBytes, 21*k512ZRowBytes, -1e9, -1e9, true)
	for i := 0; i < 2; i++ {
		if want := full[5*NR+20+i]; pair[i] != want {
			t.Fatalf("relu clamp r=5 i=%d got=%g want=%g", i, pair[i], want)
		}
		if pair[i] != 0 {
			t.Fatalf("relu clamp r=5 i=%d got=%g want 0", i, pair[i])
		}
	}
}

// TestQ8ZStrippedConcurrentBuild asserts concurrent cold calls build exactly
// one shared copy (run with -race to catch data races).
func TestQ8ZStrippedConcurrentBuild(t *testing.T) {
	const N, K = 2048, 512
	_, bData, _ := q8K512TestData(4, N, K)
	b := QuantizeToQ8(bData, N, K)
	q8StrippedCache.Delete(b)
	const workers = 16
	results := make([]*q8Stripped, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = q8StrippedFor(b, 16)
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if results[i] != results[0] {
			t.Fatalf("worker %d got a distinct stripped copy", i)
		}
	}
}
