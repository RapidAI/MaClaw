package tensor

import "testing"

func TestQ4Q8BlockVNNI(t *testing.T) {
	w := make([]float32, q4BlockSize)
	a := make([]byte, q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
		a[i] = byte((i * 7) % 127)
	}
	q4 := QuantizeToQ4(w, 1, q4BlockSize)
	var got [8]int32
	q4Q8BlockVNNI(&got, a, q4.Data)
	var want [8]int32
	for i, p := range q4.Data {
		want[i/4] += int32(int(p&0x0f)-8) * int32(a[i])
		want[4+i/4] += int32(int(p>>4)-8) * int32(a[q4BlockBytes+i])
	}
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func BenchmarkQ4Q8BlockVNNI(b *testing.B) {
	w := make([]float32, q4BlockSize)
	a := make([]byte, q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
		a[i] = byte((i * 7) % 127)
	}
	q4 := QuantizeToQ4(w, 1, q4BlockSize)
	var out [8]int32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out = [8]int32{}
		q4Q8BlockVNNI(&out, a, q4.Data)
	}
}

func TestQ4Q8BlocksVNNI(t *testing.T) {
	const blocks = 4
	w := make([]float32, blocks*q4BlockSize)
	a := make([]byte, blocks*q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
		a[i] = byte((i * 11) % 127)
	}
	q4 := QuantizeToQ4(w, 1, blocks*q4BlockSize)
	got := make([]int32, blocks*8)
	q4Q8BlocksVNNI(got, a, q4.Data, blocks)
	for b := 0; b < blocks; b++ {
		var want [8]int32
		q4Q8BlockVNNI(&want, a[b*q4BlockSize:], q4.Data[b*q4BlockBytes:])
		for lane := range want {
			if got[b*8+lane] != want[lane] {
				t.Fatalf("block=%d lane=%d got=%d want=%d", b, lane, got[b*8+lane], want[lane])
			}
		}
	}
}

func TestQ4Q8BlocksVNNIStride(t *testing.T) {
	const blocks, stride = 4, 64
	w := make([]float32, blocks*q4BlockSize)
	a := make([]byte, (blocks-1)*stride+q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
	}
	for b := 0; b < blocks; b++ {
		for i := 0; i < q4BlockSize; i++ {
			a[b*stride+i] = byte((b*17 + i*3) % 127)
		}
	}
	q4 := QuantizeToQ4(w, 1, blocks*q4BlockSize)
	got := make([]int32, blocks*8)
	q4Q8BlocksVNNIStride(got, a, q4.Data, blocks, stride)
	for b := 0; b < blocks; b++ {
		var want [8]int32
		q4Q8BlockVNNI(&want, a[b*stride:], q4.Data[b*q4BlockBytes:])
		for lane := range want {
			if got[b*8+lane] != want[lane] {
				t.Fatalf("block=%d lane=%d", b, lane)
			}
		}
	}
}

func TestQ4Q8BlocksDualVNNIStride(t *testing.T) {
	const blocks, stride = 4, 64
	w := make([]float32, 2*blocks*q4BlockSize)
	a := make([]byte, (blocks-1)*stride+q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
	}
	for b := 0; b < blocks; b++ {
		for i := 0; i < q4BlockSize; i++ {
			a[b*stride+i] = byte((b*17 + i*3) % 127)
		}
	}
	q4 := QuantizeToQ4(w, 2, blocks*q4BlockSize)
	got0, got1 := make([]int32, blocks*8), make([]int32, blocks*8)
	q4Q8BlocksDualVNNIStride(got0, got1, a, q4.Data[:blocks*16], q4.Data[blocks*16:], blocks, stride)
	want0, want1 := make([]int32, blocks*8), make([]int32, blocks*8)
	q4Q8BlocksVNNIStride(want0, a, q4.Data[:blocks*16], blocks, stride)
	q4Q8BlocksVNNIStride(want1, a, q4.Data[blocks*16:], blocks, stride)
	for i := range got0 {
		if got0[i] != want0[i] || got1[i] != want1[i] {
			t.Fatalf("lane %d mismatch", i)
		}
	}
}

func TestQ4Q8Panel8VNNI(t *testing.T) {
	const K = 2048
	wData := make([]float32, K)
	a := make([]float32, 8*K)
	for i := range wData {
		wData[i] = float32((i%15)-7) * 0.0625
	}
	for i := range a {
		a[i] = float32(i%19) * 0.03125
	}
	w := QuantizeToQ4(wData, 1, K)
	ap := q8APanelPool.Get().(*q8APanel8)
	defer q8APanelPool.Put(ap)
	quantizePanel8Q8U(ap, a)
	var got [8]float32
	if !dotQ4Q8Panel8VNNI(&got, w, 0, ap) {
		t.Fatal("panel path rejected")
	}
	for r := 0; r < 8; r++ {
		var want float32
		for b := 0; b < 64; b++ {
			var dot int
			for i := 0; i < 16; i++ {
				p := w.Data[b*16+i]
				base := b*256 + r*32
				dot += (int(p&0x0f) - 8) * int(int8(ap.q[base+i]))
				dot += (int(p>>4) - 8) * int(int8(ap.q[base+16+i]))
			}
			want += float32(dot) * w.Scales[b] * ap.s[b*8+r]
		}
		if d := got[r] - want; d < -1e-4 || d > 1e-4 {
			t.Fatalf("row %d got %.6f want %.6f", r, got[r], want)
		}
	}
}

func TestQ4Q8Panel8DualVNNI(t *testing.T) {
	const K = 2048
	wData := make([]float32, 2*K)
	a := make([]float32, 8*K)
	for i := range wData {
		wData[i] = float32((i%15)-7) * 0.0625
	}
	for i := range a {
		a[i] = float32(i%19) * 0.03125
	}
	w := QuantizeToQ4(wData, 2, K)
	ap := q8APanelPool.Get().(*q8APanel8)
	defer q8APanelPool.Put(ap)
	quantizePanel8Q8U(ap, a)
	var got0, got1, want0, want1 [8]float32
	if !dotQ4Q8Panel8DualVNNI(&got0, &got1, w, 0, ap) {
		t.Fatal("dual panel path rejected")
	}
	if !dotQ4Q8Panel8VNNI(&want0, w, 0, ap) || !dotQ4Q8Panel8VNNI(&want1, w, 1, ap) {
		t.Fatal("single panel path rejected")
	}
	if got0 != want0 || got1 != want1 {
		t.Fatal("dual panel output differs from single paths")
	}
}

func TestMatMulQ4BiasAddPanel(t *testing.T) {
	const M, N, K = 8, 2, 2048
	a := make([]float32, M*K)
	wData := make([]float32, N*K)
	bias := []float32{0.1, -0.2}
	for i := range a {
		a[i] = float32(i%19) * 0.03125
	}
	for i := range wData {
		wData[i] = float32((i%15)-7) * 0.0625
	}
	w := QuantizeToQ4(wData, N, K)
	got := make([]float32, M*N)
	MatMulQ4BiasAdd(got, a, w, bias, M, N, K)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want := dotQ4F32(a[m*K:(m+1)*K], w, n) + bias[n]
			if d := got[m*N+n] - want; d < -0.2 || d > 0.2 {
				t.Fatalf("m=%d n=%d got %.5f want %.5f", m, n, got[m*N+n], want)
			}
		}
	}
}

func TestQ4Q8Dual8AccumVNNI(t *testing.T) {
	const M, N, K = 8, 2, 2048
	a := make([]float32, M*K)
	wData := make([]float32, N*K)
	for i := range a {
		a[i] = float32(i%19) * 0.03125
	}
	for i := range wData {
		wData[i] = float32((i%15)-7) * 0.0625
	}
	w := QuantizeToQ4(wData, N, K)
	ap := q8APanelPool.Get().(*q8APanel8)
	defer q8APanelPool.Put(ap)
	quantizePanel8Q8U(ap, a)
	got := make([]float32, M*N)
	for i := range got {
		got[i] = float32(i) * 0.013
	}
	want := append([]float32(nil), got...)
	var d0, d1 [8]float32
	if !dotQ4Q8Panel8DualVNNI(&d0, &d1, w, 0, ap) {
		t.Fatal("reference panel path rejected")
	}
	const b0, b1 = float32(0.1), float32(-0.2)
	for r := 0; r < M; r++ {
		want[r*N] += d0[r] + b0
		want[r*N+1] += d1[r] + b1
	}
	if !q4Q8Dual8AccumVNNI(got, ap, w, 0, 0, N, b0, b1) {
		t.Fatal("fused panel path rejected")
	}
	for i := range got {
		if d := got[i] - want[i]; d < -1e-4 || d > 1e-4 {
			t.Fatalf("i=%d got %.7f want %.7f", i, got[i], want[i])
		}
	}
}

func TestDotQ4Q8RowVNNI(t *testing.T) {
	const K = 2048
	w := make([]float32, K)
	a := make([]float32, K)
	for i := range w {
		w[i] = float32((i%15)-7) * 0.0625
		a[i] = float32(i%17) * 0.03125
	}
	q4 := QuantizeToQ4(w, 1, K)
	q8 := QuantizeToQ8(a, 1, K)
	got, want := DotQ4Q8RowVNNI(q4, 0, q8, 0), DotQ4Q8RowReference(q4, 0, q8, 0)
	if diff := got - want; diff < -1e-4 || diff > 1e-4 {
		t.Fatalf("got %.7f want %.7f", got, want)
	}
}

func BenchmarkQ4Q8BlocksVNNI_K2048(b *testing.B) {
	const blocks = 64
	w := make([]float32, blocks*q4BlockSize)
	a := make([]byte, blocks*q4BlockSize)
	for i := range w {
		w[i] = float32((i % 15) - 7)
		a[i] = byte((i * 11) % 127)
	}
	q4 := QuantizeToQ4(w, 1, blocks*q4BlockSize)
	out := make([]int32, blocks*8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q4Q8BlocksVNNI(out, a, q4.Data, blocks)
	}
}

func BenchmarkDotQ4Q8RowVNNI_K2048(b *testing.B) {
	const K = 2048
	w := make([]float32, K)
	a := make([]float32, K)
	for i := range w {
		w[i] = float32((i%15)-7) * 0.0625
		a[i] = float32(i%17) * 0.03125
	}
	q4 := QuantizeToQ4(w, 1, K)
	q8 := QuantizeToQ8(a, 1, K)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		sink = DotQ4Q8RowVNNI(q4, 0, q8, 0)
	}
	_ = sink
}

func BenchmarkQ4_FFNDown_8x512x2048_Add(b *testing.B) {
	const M, N, K = 8, 512, 2048
	a := make([]float32, M*K)
	raw := make([]float32, N*K)
	bias := make([]float32, N)
	out := make([]float32, M*N)
	for i := range a {
		a[i] = float32(i%17) * 0.05
	}
	for i := range raw {
		raw[i] = float32((i%13)-6) * 0.1
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.02
	}
	w := QuantizeToQ4(raw, N, K)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MatMulQ4BiasAdd(out, a, w, bias, M, N, K)
	}
}
