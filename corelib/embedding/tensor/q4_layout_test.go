package tensor

import (
	"math"
	"testing"
)

func TestQuantizeToQ4UsesSplitNibbleLayout(t *testing.T) {
	w := make([]float32, q4BlockSize)
	for i := 0; i < q4BlockBytes; i++ {
		w[i] = float32(i - 8)
		w[q4BlockBytes+i] = float32(7 - i)
	}
	q := QuantizeToQ4(w, 1, q4BlockSize)
	if len(q.Data) != q4BlockBytes || q.Scales[0] == 0 {
		t.Fatalf("invalid Q4 block")
	}
	for i, packed := range q.Data {
		lo, hi := int(packed&0x0f)-8, int(packed>>4)-8
		wantLo := int(math.Round(float64(w[i] / q.Scales[0])))
		wantHi := int(math.Round(float64(w[q4BlockBytes+i] / q.Scales[0])))
		if lo != wantLo || hi != wantHi {
			t.Fatalf("byte %d: got low=%d high=%d", i, lo, hi)
		}
	}
}

func TestDotQ4Q8RowReference(t *testing.T) {
	const K = 64
	w := make([]float32, K)
	a := make([]float32, K)
	for i := range w {
		w[i] = float32((i%15)-7) * 0.125
		a[i] = float32((i%13)-6) * 0.09375
	}
	q4 := QuantizeToQ4(w, 1, K)
	q8 := QuantizeToQ8(a, 1, K)
	got := DotQ4Q8RowReference(q4, 0, q8, 0)
	var want float32
	// Compare to independently dequantized Q4/Q8 values rather than F32
	// source weights, so this verifies the packed nibble correspondence.
	for b := 0; b < K/q4BlockSize; b++ {
		for i := 0; i < q4BlockBytes; i++ {
			p := q4.Data[b*q4BlockBytes+i]
			q8b := q8.Data[b*q8BlockBytes+2:]
			want += float32(int(p&0x0f)-8) * q4.Scales[b] * float32(int8(q8b[i])) * q8.Scales[b]
			want += float32(int(p>>4)-8) * q4.Scales[b] * float32(int8(q8b[q4BlockBytes+i])) * q8.Scales[b]
		}
	}
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("got %.8f want %.8f", got, want)
	}
}

func TestQuantizeQ8ToQ4(t *testing.T) {
	const K = 64
	w := make([]float32, K)
	for i := range w {
		w[i] = float32((i%17)-8) * 0.0625
	}
	q8 := QuantizeToQ8(w, 1, K)
	q4 := QuantizeQ8ToQ4(q8)
	if q4 == nil || q4.Rows != 1 || q4.Cols != K {
		t.Fatal("Q8 to Q4 conversion failed")
	}
	for b := 0; b < K/q4BlockSize; b++ {
		if q4.Scales[b] < 0 {
			t.Fatal("negative Q4 scale")
		}
		for i := 0; i < q4BlockBytes; i++ {
			p := q4.Data[b*q4BlockBytes+i]
			if int(p&0x0f)-8 < -8 || int(p>>4)-8 > 7 {
				t.Fatal("invalid nibble")
			}
		}
	}
}

func BenchmarkDotQ4Q8RowReference_K2048(b *testing.B) {
	const K = 2048
	w := make([]float32, K)
	a := make([]float32, K)
	for i := range w {
		w[i] = float32((i%15)-7) * 0.0625
		a[i] = float32((i%13)-6) * 0.046875
	}
	q4 := QuantizeToQ4(w, 1, K)
	q8 := QuantizeToQ8(a, 1, K)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		sink = DotQ4Q8RowReference(q4, 0, q8, 0)
	}
	_ = sink
}

func BenchmarkDotQ8RowScaled_K2048(b *testing.B) {
	const K = 2048
	w := make([]float32, K)
	a := make([]float32, K)
	for i := range w {
		w[i] = float32((i%15)-7) * 0.0625
		a[i] = float32((i%13)-6) * 0.046875
	}
	q8 := QuantizeToQ8(w, 1, K)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		sink = DotQ8RowScaled(a, q8, 0)
	}
	_ = sink
}
