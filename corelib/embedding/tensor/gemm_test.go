package tensor

import (
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
