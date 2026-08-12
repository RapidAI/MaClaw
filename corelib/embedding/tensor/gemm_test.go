package tensor

import (
	"fmt"
	"math"
	"testing"
)

func TestMultiDot4(t *testing.T) {
	K := 64
	a := make([]float32, 4*K)
	b := make([]float32, K)
	for i := range a {
		a[i] = float32(i%7) * 0.1
	}
	for i := range b {
		b[i] = float32((i%5)-2) * 0.2
	}
	var got [4]float32
	multiDot4(&got, a, b, K)

	for r := 0; r < 4; r++ {
		want := float32(0)
		for k := 0; k < K; k++ {
			want += a[r*K+k] * b[k]
		}
		if math.Abs(float64(got[r]-want)) > 1e-4 {
			t.Fatalf("row %d: got %v want %v", r, got[r], want)
		}
	}
}

func TestMultiDot8(t *testing.T) {
	K := 560 // not multiple of 8-free... 560%8==0
	a := make([]float32, 8*K)
	b := make([]float32, K)
	for i := range a {
		a[i] = float32(i%7) * 0.1
	}
	for i := range b {
		b[i] = float32((i%5)-2) * 0.2
	}
	var got [8]float32
	multiDot8(&got, a, b, K)
	for r := 0; r < 8; r++ {
		want := float32(0)
		for k := 0; k < K; k++ {
			want += a[r*K+k] * b[k]
		}
		if math.Abs(float64(got[r]-want)) > 1e-3 {
			t.Fatalf("row %d: got %v want %v", r, got[r], want)
		}
	}
}

func TestDequantScaledMatches(t *testing.T) {
	K := 128
	raw := make([]float32, K)
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	w := QuantizeToQ8(raw, 1, K)
	if len(w.Scales) == 0 {
		t.Fatal("expected PrepareScales from QuantizeToQ8")
	}
	a := make([]float32, K)
	b := make([]float32, K)
	// Path with scales
	dequantQ8Row(w, 0, a)
	// Force unscaled path
	w2 := &Q8Tensor{Data: w.Data, Rows: 1, Cols: K}
	dequantQ8Row(w2, 0, b)
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-5 {
			t.Fatalf("idx %d: scaled %v unscaled %v", i, a[i], b[i])
		}
	}
}

func TestQ8DualMultiDot4(t *testing.T) {
	K := 64
	a := make([]float32, 4*K)
	raw0 := make([]float32, K)
	raw1 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%11) * 0.01
	}
	for i := range raw0 {
		raw0[i] = float32((i%9)-4) * 0.05
		raw1[i] = float32((i%7)-3) * 0.04
	}
	// Two-row Q8 weight matrix
	raw := make([]float32, 2*K)
	copy(raw[:K], raw0)
	copy(raw[K:], raw1)
	w := QuantizeToQ8(raw, 2, K)
	nBlocks := K / 32
	var got [8]float32
	q8DualMultiDot4(&got, a, w.Data, 0, 1, nBlocks, K)

	var ref0, ref1 [4]float32
	q8MultiDot4(&ref0, a, w.Data, 0, nBlocks, K)
	q8MultiDot4(&ref1, a, w.Data, 1, nBlocks, K)
	for r := 0; r < 4; r++ {
		if math.Abs(float64(got[r]-ref0[r])) > 1e-3 {
			t.Fatalf("B0 row %d: got %v want %v", r, got[r], ref0[r])
		}
		if math.Abs(float64(got[4+r]-ref1[r])) > 1e-3 {
			t.Fatalf("B1 row %d: got %v want %v", r, got[4+r], ref1[r])
		}
	}
}

func TestMultiDot4DualB(t *testing.T) {
	K := 64
	a := make([]float32, 4*K)
	b0 := make([]float32, K)
	b1 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%7) * 0.1
	}
	for i := range b0 {
		b0[i] = float32((i%5)-2) * 0.2
		b1[i] = float32((i%3)-1) * 0.15
	}
	var got [8]float32
	multiDot4DualB(&got, a, b0, b1, K)
	var ref0, ref1 [4]float32
	multiDot4(&ref0, a, b0, K)
	multiDot4(&ref1, a, b1, K)
	for r := 0; r < 4; r++ {
		if math.Abs(float64(got[r]-ref0[r])) > 1e-4 {
			t.Fatalf("b0 row %d: got %v want %v", r, got[r], ref0[r])
		}
		if math.Abs(float64(got[4+r]-ref1[r])) > 1e-4 {
			t.Fatalf("b1 row %d: got %v want %v", r, got[4+r], ref1[r])
		}
	}
	// Tail path: K not multiple of 8
	K2 := 67
	a2 := make([]float32, 4*K2)
	b0s := make([]float32, K2)
	b1s := make([]float32, K2)
	for i := range a2 {
		a2[i] = float32(i%9) * 0.05
	}
	for i := range b0s {
		b0s[i] = float32(i%4) * 0.1
		b1s[i] = float32((i%6)-3) * 0.07
	}
	var got2 [8]float32
	multiDot4DualB(&got2, a2, b0s, b1s, K2)
	var r0, r1 [4]float32
	multiDot4(&r0, a2, b0s, K2)
	multiDot4(&r1, a2, b1s, K2)
	for r := 0; r < 4; r++ {
		if math.Abs(float64(got2[r]-r0[r])) > 1e-3 {
			t.Fatalf("tail b0 row %d: got %v want %v", r, got2[r], r0[r])
		}
		if math.Abs(float64(got2[4+r]-r1[r])) > 1e-3 {
			t.Fatalf("tail b1 row %d: got %v want %v", r, got2[4+r], r1[r])
		}
	}
}

func TestMultiDot4TripleBPPOCRWidths(t *testing.T) {
	for _, K := range []int{96, 120, 192, 384} {
		t.Run(fmt.Sprintf("K%d", K), func(t *testing.T) {
			a := make([]float32, 4*K)
			b0, b1, b2 := make([]float32, K), make([]float32, K), make([]float32, K)
			for i := range a {
				a[i] = float32((i%17)-8) * 0.125
			}
			for i := range b0 {
				b0[i] = float32((i%19)-9) * 0.0625
				b1[i] = float32((i%13)-6) * 0.09375
				b2[i] = float32((i%11)-5) * 0.03125
			}
			var got [12]float32
			multiDot4TripleB(&got, a, b0, b1, b2, K)
			for r := 0; r < 4; r++ {
				for j, b := range [][]float32{b0, b1, b2} {
					var want float32
					for k := 0; k < K; k++ {
						want += a[r*K+k] * b[k]
					}
					if diff := math.Abs(float64(got[j*4+r] - want)); diff > 1e-4 {
						t.Fatalf("B%d row %d: got %v want %v", j, r, got[j*4+r], want)
					}
				}
			}
		})
	}
}

func TestQ8MultiDot4(t *testing.T) {
	M, K := 4, 64
	a := make([]float32, M*K)
	raw := make([]float32, K) // one B row
	for i := range a {
		a[i] = float32(i%11) * 0.01
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	w := QuantizeToQ8(raw, 1, K)
	nBlocks := K / 32
	var got [4]float32
	q8MultiDot4(&got, a, w.Data, 0, nBlocks, K)

	buf := make([]float32, K)
	dequantRowInto(w.Data, 0, nBlocks, buf)
	for r := 0; r < 4; r++ {
		want := float32(0)
		for k := 0; k < K; k++ {
			want += a[r*K+k] * buf[k]
		}
		if math.Abs(float64(got[r]-want)) > 1e-3 {
			t.Fatalf("row %d: got %v want %v", r, got[r], want)
		}
	}
}

func TestMatMulQ8MultiDot(t *testing.T) {
	M, N, K := 12, 32, 64
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	for i := range a {
		a[i] = float32(i%11) * 0.01
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	w := QuantizeToQ8(raw, N, K)
	out := make([]float32, M*N)
	ref := make([]float32, M*N)
	// reference via sequential Dot after dequant
	buf := make([]float32, K)
	nBlocks := K / 32
	for n := 0; n < N; n++ {
		dequantRowInto(w.Data, n, nBlocks, buf)
		for m := 0; m < M; m++ {
			var s float32
			for k := 0; k < K; k++ {
				s += a[m*K+k] * buf[k]
			}
			ref[m*N+n] = s
		}
	}
	MatMulQ8(out, a, w, M, N, K)
	for i := range out {
		if math.Abs(float64(out[i]-ref[i])) > 1e-3 {
			t.Fatalf("idx %d: got %v want %v", i, out[i], ref[i])
		}
	}
}

// TestMultiDot8TripleArgmax matches multiDot8TripleB + updateArgmaxTriple4.
func TestMultiDot8TripleArgmax(t *testing.T) {
	K := 512
	a := make([]float32, 8*K)
	b0 := make([]float32, K)
	b1 := make([]float32, K)
	b2 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%11)*0.01 - 0.05
	}
	for i := 0; i < K; i++ {
		b0[i] = float32((i%7)-3) * 0.02
		b1[i] = float32((i%5)-2) * 0.03
		b2[i] = float32((i%9)-4) * 0.01
	}
	bn0, bn1, bn2 := float32(0.1), float32(-0.05), float32(0.2)
	n := 12

	var d0, d1 [12]float32
	multiDot8TripleB(&d0, &d1, a, b0, b1, b2, K)
	refV := make([]float32, 8)
	refI := make([]int, 8)
	for i := range refV {
		refV[i] = float32(-1e30)
		refI[i] = -1
	}
	updateArgmaxTriple4(refV, refI, 0, n, &d0, bn0, bn1, bn2)
	updateArgmaxTriple4(refV, refI, 4, n, &d1, bn0, bn1, bn2)

	gotV := make([]float32, 8)
	gotI := make([]int, 8)
	for i := range gotV {
		gotV[i] = float32(-1e30)
		gotI[i] = -1
	}
	if !multiDot8TripleArgmax(gotV, gotI, a, b0, b1, b2, n, K, bn0, bn1, bn2) {
		t.Skip("fused argmax not available")
	}
	for r := 0; r < 8; r++ {
		if gotI[r] != refI[r] || math.Abs(float64(gotV[r]-refV[r])) > 1e-4 {
			t.Fatalf("row %d: got id=%d v=%v want id=%d v=%v", r, gotI[r], gotV[r], refI[r], refV[r])
		}
	}
}

// TestMultiDot8DualK560 matches dual-4×2 scalar for entry feats_dim=560.
// TestMultiDot8TripleArgmaxTie keeps greedy CTC's first-token tie rule when
// the AVX-512 branchless candidate reduction sees equal logits.
func TestMultiDot8TripleArgmaxTie(t *testing.T) {
	const K = 512
	a := make([]float32, 8*K)
	b := make([]float32, K)
	bestV := make([]float32, 8)
	bestI := make([]int, 8)
	for i := range bestV {
		bestV[i] = -float32(math.MaxFloat32)
		bestI[i] = -1
	}

	const n = 37
	if !multiDot8TripleArgmax(bestV, bestI, a, b, b, b, n, K, 0, 0, 0) {
		t.Skip("fused argmax not available")
	}
	for r := range bestI {
		if bestI[r] != n || bestV[r] != 0 {
			t.Fatalf("row %d: got id=%d value=%v, want id=%d value=0", r, bestI[r], bestV[r], n)
		}
	}
}

func TestMultiDot8DualK560(t *testing.T) {
	K := 560
	a := make([]float32, 8*K)
	b0 := make([]float32, K)
	b1 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%13)*0.01 - 0.06
	}
	for i := 0; i < K; i++ {
		b0[i] = float32((i%7)-3) * 0.02
		b1[i] = float32((i%5)-2) * 0.03
	}
	var got0, got1, ref0, ref1 [8]float32
	multiDot8DualB(&got0, &got1, a, b0, b1, K)
	multiDot4DualBScalar(&ref0, a[:4*K], b0, b1, K)
	multiDot4DualBScalar(&ref1, a[4*K:8*K], b0, b1, K)
	for i := 0; i < 8; i++ {
		if abs32(got0[i]-ref0[i]) > 2e-3 || abs32(got1[i]-ref1[i]) > 2e-3 {
			t.Fatalf("i=%d got %v/%v want %v/%v", i, got0[i], got1[i], ref0[i], ref1[i])
		}
	}
}

func TestMultiDot2DualK560(t *testing.T) {
	const K = 560
	a := make([]float32, 2*K)
	b0 := make([]float32, K)
	b1 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%13)*0.01 - 0.06
	}
	for i := 0; i < K; i++ {
		b0[i] = float32((i%7)-3) * 0.02
		b1[i] = float32((i%5)-2) * 0.03
	}
	var got [4]float32
	multiDot2DualB(&got, a, b0, b1, K)
	want := [4]float32{
		Dot(a[:K], b0), Dot(a[K:], b0),
		Dot(a[:K], b1), Dot(a[K:], b1),
	}
	for i := range got {
		if abs32(got[i]-want[i]) > 2e-3 {
			t.Fatalf("i=%d got %v want %v", i, got[i], want[i])
		}
	}
}

func TestQ8DualMultiDot2ScaledK512(t *testing.T) {
	const K = 512
	a := make([]float32, 2*K)
	raw := make([]float32, 2*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.03
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.05
	}
	w := QuantizeToQ8(raw, 2, K)
	w.PrepareScales()
	var got [4]float32
	q8DualMultiDot2T(&got, a, w, 0, 1, K/q8BlockSize, K)
	want := [4]float32{
		DotQ8RowScaled(a[:K], w, 0), DotQ8RowScaled(a[K:], w, 0),
		DotQ8RowScaled(a[:K], w, 1), DotQ8RowScaled(a[K:], w, 1),
	}
	for i := range got {
		if abs32(got[i]-want[i]) > 2e-3 {
			t.Fatalf("i=%d got %v want %v", i, got[i], want[i])
		}
	}
}

func BenchmarkQ8DualMultiDot2ScaledK512(b *testing.B) {
	const K = 512
	a := make([]float32, 2*K)
	raw := make([]float32, 2*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.03
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.05
	}
	w := QuantizeToQ8(raw, 2, K)
	w.PrepareScales()
	b.Run("2x2", func(b *testing.B) {
		var out [4]float32
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			q8DualMultiDot2T(&out, a, w, 0, 1, K/q8BlockSize, K)
		}
	})
	b.Run("two_1x2", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			DotQ8RowDualScaled(a[:K], w, 0, 1)
			DotQ8RowDualScaled(a[K:], w, 0, 1)
		}
	})
}

// TestDotQ8RowScaledAVX512 matches scalar scaled dot (and dual shares A).
func TestDotQ8RowScaledAVX512(t *testing.T) {
	K := 2048
	raw := make([]float32, 2*K)
	a := make([]float32, K)
	for i := 0; i < K; i++ {
		a[i] = float32((i%13)-6) * 0.02
		raw[i] = float32((i%7)-3) * 0.05
		raw[K+i] = float32((i%11)-5) * 0.04
	}
	w := QuantizeToQ8(raw, 2, K)
	got0 := DotQ8RowScaled(a, w, 0)
	// Reference via dequant + Dot
	buf := make([]float32, K)
	w.DequantRow(0, buf)
	want0 := Dot(a, buf)
	if abs32(got0-want0) > 2e-3 {
		t.Fatalf("row0: got %v want %v", got0, want0)
	}
	s0, s1 := DotQ8RowDualScaled(a, w, 0, 1)
	w.DequantRow(1, buf)
	want1 := Dot(a, buf)
	if abs32(s0-want0) > 2e-3 || abs32(s1-want1) > 2e-3 {
		t.Fatalf("dual: got %v,%v want %v,%v", s0, s1, want0, want1)
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// TestMultiDot8DualArgmax matches multiDot8DualB + updateArgmaxDual4.
func TestMultiDot8DualArgmax(t *testing.T) {
	K := 512
	a := make([]float32, 8*K)
	b0 := make([]float32, K)
	b1 := make([]float32, K)
	for i := range a {
		a[i] = float32(i%11)*0.01 - 0.05
	}
	for i := 0; i < K; i++ {
		b0[i] = float32((i%7)-3) * 0.02
		b1[i] = float32((i%5)-2) * 0.03
	}
	bn0, bn1 := float32(0.1), float32(-0.05)
	n := 7

	var d0, d1 [8]float32
	multiDot8DualB(&d0, &d1, a, b0, b1, K)
	refV := make([]float32, 8)
	refI := make([]int, 8)
	for i := range refV {
		refV[i] = float32(-1e30)
		refI[i] = -1
	}
	updateArgmaxDual4(refV, refI, 0, n, &d0, bn0, bn1)
	updateArgmaxDual4(refV, refI, 4, n, &d1, bn0, bn1)

	gotV := make([]float32, 8)
	gotI := make([]int, 8)
	for i := range gotV {
		gotV[i] = float32(-1e30)
		gotI[i] = -1
	}
	if !multiDot8DualArgmax(gotV, gotI, a, b0, b1, n, K, bn0, bn1) {
		t.Skip("fused dual argmax not available")
	}
	for r := 0; r < 8; r++ {
		if gotI[r] != refI[r] || math.Abs(float64(gotV[r]-refV[r])) > 1e-4 {
			t.Fatalf("row %d: got id=%d v=%v want id=%d v=%v", r, gotI[r], gotV[r], refI[r], refV[r])
		}
	}
}

// TestMatMulQ8Argmax_CTC exercises CTC K=512 argmax end-to-end.
func TestMatMulQ8Argmax_CTC(t *testing.T) {
	M, N, K := 16, 96, 512 // N multiple of 3 for triple coverage
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32(i%11)*0.01 - 0.05
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	for i := range bias {
		bias[i] = float32(i%5)*0.02 - 0.04
	}
	w := QuantizeToQ8(raw, N, K)

	// Reference: same MatMulQ8Argmax path with fused disabled is hard; use
	// two runs consistency via dequant multiDot by comparing scores from
	// a second independent argmax over MatMulQ8Bias (same Q8 math as deq path).
	logits := make([]float32, M*N)
	MatMulQ8Bias(logits, a, w, bias, M, N, K)
	refI := make([]int, M)
	for m := 0; m < M; m++ {
		bi, bv := 0, logits[m*N]
		for n := 1; n < N; n++ {
			if logits[m*N+n] > bv {
				bv, bi = logits[m*N+n], n
			}
		}
		refI[m] = bi
	}

	gotI := make([]int, M)
	MatMulQ8Argmax(gotI, a, w, bias, M, N, K)
	for m := 0; m < M; m++ {
		if gotI[m] != refI[m] {
			got := logits[m*N+gotI[m]]
			ref := logits[m*N+refI[m]]
			if math.Abs(float64(got-ref)) > 2e-3 {
				t.Fatalf("m=%d: got id %d (%.6f) want %d (%.6f)", m, gotI[m], got, refI[m], ref)
			}
		}
	}
}

// TestQ8DualMultiDot2T checks the 2A×2B CTC-tail kernel layout used when an
// argmax tile has two rows remaining. It compares directly against independent
// scaled Q8 dot products so the SIMD path cannot silently swap its outputs.
func TestQ8DualMultiDot2T(t *testing.T) {
	const K = 512
	a := make([]float32, 2*K)
	raw := make([]float32, 2*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.03125
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.046875
	}
	w := QuantizeToQ8(raw, 2, K)
	w.PrepareScales()

	var got [4]float32
	q8DualMultiDot2T(&got, a, w, 0, 1, K/q8BlockSize, K)
	r00, r01 := DotQ8RowDualScaled(a[:K], w, 0, 1)
	r10, r11 := DotQ8RowDualScaled(a[K:], w, 0, 1)
	want := [4]float32{r00, r10, r01, r11}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-3 {
			t.Fatalf("output %d: got %.6f want %.6f", i, got[i], want[i])
		}
	}
}

// TestMatMulQ8Bias_EncoderN512 exercises N=512/K=512 encoder plain (triple+bias fuse).
func TestMatMulQ8Bias_EncoderN512(t *testing.T) {
	testMatMulQ8BiasShape(t, 16, 512, 512)
}

// TestMatMulQ8Bias_QKVN1536 exercises N=1536/K=512 fused-QKV plain store.
func TestMatMulQ8Bias_QKVN1536(t *testing.T) {
	testMatMulQ8BiasShape(t, 16, 1536, 512)
}

func testMatMulQ8BiasShape(t *testing.T, M, N, K int) {
	t.Helper()
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32(i%11)*0.01 - 0.05
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	for i := range bias {
		bias[i] = float32(i%5)*0.02 - 0.04
	}
	w := QuantizeToQ8(raw, N, K)

	// Reference: serial dequant+dot (MatMulQ8Bias is the path under test;
	// compare against Dot after dequant for independence).
	out := make([]float32, M*N)
	MatMulQ8Bias(out, a, w, bias, M, N, K)

	ref := make([]float32, M*N)
	buf := make([]float32, K)
	nBlocks := K / q8BlockSize
	for n := 0; n < N; n++ {
		dequantRowInto(w.Data, n, nBlocks, buf)
		bn := bias[n]
		for m := 0; m < M; m++ {
			var s float32
			for k := 0; k < K; k++ {
				s += a[m*K+k] * buf[k]
			}
			ref[m*N+n] = s + bn
		}
	}
	for i := range out {
		if math.Abs(float64(out[i]-ref[i])) > 2e-3 {
			t.Fatalf("idx %d (m=%d n=%d): got %v want %v", i, i/N, i%N, out[i], ref[i])
		}
	}
}

// TestMatMulQ8BiasReLU_FFNUp exercises N=2048/K=512 FFN up (triple+ReLU fuse).
func TestMatMulQ8BiasReLU_FFNUp(t *testing.T) {
	M, N, K := 16, 2048, 512
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32(i%11)*0.01 - 0.05
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	for i := range bias {
		bias[i] = float32(i%5)*0.02 - 0.04
	}
	w := QuantizeToQ8(raw, N, K)

	// Reference: MatMulQ8Bias then ReLU.
	ref := make([]float32, M*N)
	MatMulQ8Bias(ref, a, w, bias, M, N, K)
	for i := range ref {
		if ref[i] < 0 {
			ref[i] = 0
		}
	}

	out := make([]float32, M*N)
	MatMulQ8BiasReLU(out, a, w, bias, M, N, K)
	for i := range out {
		if math.Abs(float64(out[i]-ref[i])) > 2e-3 {
			t.Fatalf("idx %d (m=%d n=%d): got %v want %v", i, i/N, i%N, out[i], ref[i])
		}
	}
}

// TestMatMulQ8BiasAdd_FFNDown exercises the N=512/K=2048 residual path
// (including AVX-512 fused dual8-accum / VNNI prequant when available).
// A is non-negative to match SenseVoice FFN-down (post-ReLU) activations.
func TestMatMulQ8BiasAdd_FFNDown(t *testing.T) {
	M, N, K := 16, 512, 2048
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32(i%11) * 0.01 // ≥0 like ReLU output
	}
	for i := range raw {
		raw[i] = float32((i%9)-4) * 0.05
	}
	for i := range bias {
		bias[i] = float32(i%5)*0.02 - 0.04
	}
	w := QuantizeToQ8(raw, N, K)

	// Seed residual so accumulate is non-trivial.
	out := make([]float32, M*N)
	ref := make([]float32, M*N)
	for i := range out {
		out[i] = float32(i%7) * 0.1
		ref[i] = out[i]
	}

	// Reference: MatMulQ8Bias into temp then Add.
	tmp := make([]float32, M*N)
	MatMulQ8Bias(tmp, a, w, bias, M, N, K)
	for i := range ref {
		ref[i] += tmp[i]
	}

	MatMulQ8BiasAdd(out, a, w, bias, M, N, K)
	for i := range out {
		if math.Abs(float64(out[i]-ref[i])) > 2e-3 {
			t.Fatalf("idx %d (m=%d n=%d): got %v want %v", i, i/N, i%N, out[i], ref[i])
		}
	}
}
