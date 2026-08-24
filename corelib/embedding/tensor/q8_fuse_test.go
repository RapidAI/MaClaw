package tensor

import (
	"math"
	"testing"
)

func gemmCosine32(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestPrepareScales_MatchesUnprepared(t *testing.T) {
	const M, N, K = 4, 64, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range bData {
		bData[i] = float32((i%13)-6) * 0.02
	}
	bq := QuantizeToQ8(bData, N, K)
	unprepared := Q8Tensor{Data: bq.Data, Rows: N, Cols: K}
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(want, a, &unprepared, M, N, K, 1)
	MatMulQ8N(got, a, bq, M, N, K, 1)
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 1e-5 {
		t.Fatalf("PrepareScales vs unprepared max|Δ|=%g want <=1e-5", maxd)
	}
}

func TestMatMulQ8N_Workers1DoesNotEnqueue(t *testing.T) {
	const M, N, K = 32, 768, 768
	a := make([]float32, M*K)
	b := QuantizeToQ8(make([]float32, N*K), N, K)
	out := make([]float32, M*N)
	ResetJobEnqueueCount()
	before := JobEnqueueCount()
	MatMulQ8N(out, a, b, M, N, K, 1)
	if JobEnqueueCount() != before {
		t.Fatalf("maxWorkers=1 enqueued %d jobs", JobEnqueueCount()-before)
	}
}

func TestMatMulQ8NKRange_MatchesFullSlice(t *testing.T) {
	const M, N, Kfull, k0, kLen = 3, 8, 1152, 384, 384
	aFull := make([]float32, M*Kfull)
	aTile := make([]float32, M*kLen)
	bData := make([]float32, N*Kfull)
	for i := range aFull {
		aFull[i] = float32(i%11) * 0.01
	}
	for m := 0; m < M; m++ {
		copy(aTile[m*kLen:(m+1)*kLen], aFull[m*Kfull+k0:m*Kfull+k0+kLen])
	}
	for i := range bData {
		bData[i] = float32((i%7)-3) * 0.03
	}
	b := QuantizeToQ8(bData, N, Kfull)
	full := make([]float32, M*N)
	window := make([]float32, M*N)
	MatMulQ8N(full, aFull, b, M, N, Kfull, 1)
	MatMulQ8NKRange(window, aTile, b, M, N, k0, kLen, 1, false)
	// Window is only part of K so it won't match full GEMM; check accum of 3 tiles.
	acc := make([]float32, M*N)
	for t := 0; t < 3; t++ {
		off := t * kLen
		tile := make([]float32, M*kLen)
		for m := 0; m < M; m++ {
			copy(tile[m*kLen:(m+1)*kLen], aFull[m*Kfull+off:m*Kfull+off+kLen])
		}
		MatMulQ8NKRange(acc, tile, b, M, N, off, kLen, 1, true)
	}
	var maxd float32
	for i := range full {
		d := float32(math.Abs(float64(acc[i] - full[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("tiled K-range vs full max|Δ|=%g", maxd)
	}
}

func TestDot256_MatchesScalar(t *testing.T) {
	a := make([]float32, 256)
	b := make([]float32, 256)
	for i := range a {
		a[i] = float32(i-128) * 0.01
		b[i] = float32((i%7)-3) * 0.02
	}
	got := Dot(a, b)
	want := dot256Scalar(a, b)
	if math.Abs(float64(got-want)) > 1e-4 {
		t.Fatalf("dot256=%g scalar=%g", got, want)
	}
}

func TestQ8DualDot2N24_MatchesRowDots(t *testing.T) {
	const K = 768
	a := make([]float32, K)
	bData := make([]float32, 2*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, 2, K)
	g0, g1 := DotQ8RowDualScaled(a, b, 0, 1)
	w0 := DotQ8RowScaled(a, b, 0)
	w1 := DotQ8RowScaled(a, b, 1)
	d0 := float32(math.Abs(float64(g0 - w0)))
	d1 := float32(math.Abs(float64(g1 - w1)))
	if d0 > 2e-3 || d1 > 2e-3 {
		t.Fatalf("DualDot2 N24 vs row-dot Δ=(%g,%g)", d0, d1)
	}
}

func TestQ8DualMultiDot2N24_MatchesRowDot(t *testing.T) {
	const M, N, K = 2, 64, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	if gemmCosine32(got, want) < 0.999 {
		t.Fatalf("DualMultiDot2 N24 vs row-dot cosine=%g", gemmCosine32(got, want))
	}
}

func TestQ8MultiDotN24_M2ParallelRangeMatches(t *testing.T) {
	const M, N, K = 2, 768, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 0)
	MatMulQ8N(want, a, b, M, N, K, 1)
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("M=2 workers=0 vs 1 max|Δ|=%g", maxd)
	}
}

func BenchmarkMatMulQ8_M3K768(b *testing.B) {
	const M, N, K = 3, 768, 768
	a := make([]float32, M*K)
	out := make([]float32, M*N)
	bq := QuantizeToQ8(make([]float32, N*K), N, K)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MatMulQ8N(out, a, bq, M, N, K, 1)
	}
}

func BenchmarkMatMulQ8_M4K768(b *testing.B) {
	const M, N, K = 4, 768, 768
	a := make([]float32, M*K)
	out := make([]float32, M*N)
	bq := QuantizeToQ8(make([]float32, N*K), N, K)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MatMulQ8N(out, a, bq, M, N, K, 1)
	}
}

func TestQ8MultiDotM3_NSplitMatchesRowDot(t *testing.T) {
	// Dual3 N-split: workers=0 with 5/7-way pool makes odd chunks on N=1152
	// (231 and 165). Serial workers=1 is the unfused-width oracle path's
	// fused Dual3; row-dot is the per-column oracle.
	defer SetMatMulMaxParallel(0)
	shapes := []struct{ M, N, K int }{
		{3, 256, 768},
		{3, 1152, 768},
		{3, 768, 1152},
		{3, 1152, 1152},
		{4, 1152, 768},
		{8, 768, 768},
		{8, 768, 1152},
	}
	for _, sh := range shapes {
		a := make([]float32, sh.M*sh.K)
		bData := make([]float32, sh.N*sh.K)
		for i := range a {
			a[i] = float32((i%19)-9) * 0.02
		}
		for i := range bData {
			bData[i] = float32((i%11)-5) * 0.03
		}
		bq := QuantizeToQ8(bData, sh.N, sh.K)
		want := make([]float32, sh.M*sh.N)
		for m := 0; m < sh.M; m++ {
			for n := 0; n < sh.N; n++ {
				want[m*sh.N+n] = DotQ8RowScaled(a[m*sh.K:(m+1)*sh.K], bq, n)
			}
		}
		serial := make([]float32, sh.M*sh.N)
		MatMulQ8N(serial, a, bq, sh.M, sh.N, sh.K, 1)
		if gemmCosine32(serial, want) < 0.999 {
			t.Fatalf("M=%d N=%d K=%d serial vs row-dot cosine=%g", sh.M, sh.N, sh.K, gemmCosine32(serial, want))
		}
		for _, nw := range []int{5, 7} {
			SetMatMulMaxParallel(nw)
			got := make([]float32, sh.M*sh.N)
			MatMulQ8N(got, a, bq, sh.M, sh.N, sh.K, 0)
			SetMatMulMaxParallel(0)
			cosW := gemmCosine32(got, want)
			cosS := gemmCosine32(got, serial)
			if cosW < 0.999 || cosS < 0.999 {
				t.Fatalf("M=%d N=%d K=%d workers=%d vs row-dot cosine=%g vs serial=%g",
					sh.M, sh.N, sh.K, nw, cosW, cosS)
			}
			var maxd float32
			for i := range got {
				d := float32(math.Abs(float64(got[i] - want[i])))
				if d > maxd {
					maxd = d
				}
			}
			if maxd > 2e-3 {
				t.Fatalf("M=%d N=%d K=%d workers=%d max|Δ|=%g vs row-dot", sh.M, sh.N, sh.K, nw, maxd)
			}
		}
	}
}

func TestQ8PackQS_M3GEMMMatchesUnpacked(t *testing.T) {
	defer SetMatMulMaxParallel(0)
	shapes := []struct{ M, N, K int }{
		{3, 768, 768},
		{3, 256, 768},
		{3, 768, 1152},
		{8, 768, 768},
		{8, 768, 1152},
	}
	for _, sh := range shapes {
		a := make([]float32, sh.M*sh.K)
		bData := make([]float32, sh.N*sh.K)
		for i := range a {
			a[i] = float32((i%19)-9) * 0.02
		}
		for i := range bData {
			bData[i] = float32((i%11)-5) * 0.03
		}
		bq := QuantizeToQ8(bData, sh.N, sh.K)
		unpacked := make([]float32, sh.M*sh.N)
		MatMulQ8N(unpacked, a, bq, sh.M, sh.N, sh.K, 1)
		bq.PackQS()
		if len(bq.Packed) != sh.N*sh.K {
			t.Fatalf("PackQS N=%d K=%d packed len=%d want %d", sh.N, sh.K, len(bq.Packed), sh.N*sh.K)
		}
		got := make([]float32, sh.M*sh.N)
		MatMulQ8N(got, a, bq, sh.M, sh.N, sh.K, 1)
		var maxd float32
		for i := range got {
			d := float32(math.Abs(float64(got[i] - unpacked[i])))
			if d > maxd {
				maxd = d
			}
		}
		if maxd > 1e-5 {
			t.Fatalf("packed vs unpacked M=%d N=%d K=%d max|Δ|=%g", sh.M, sh.N, sh.K, maxd)
		}
		for _, nw := range []int{5, 7} {
			SetMatMulMaxParallel(nw)
			split := make([]float32, sh.M*sh.N)
			MatMulQ8N(split, a, bq, sh.M, sh.N, sh.K, 0)
			SetMatMulMaxParallel(0)
			if gemmCosine32(split, unpacked) < 0.999 {
				t.Fatalf("packed N-split workers=%d N=%d K=%d cosine=%g", nw, sh.N, sh.K, gemmCosine32(split, unpacked))
			}
		}
	}
}

func TestQ8QuadMultiDot3N24_MatchesRowDot(t *testing.T) {
	const M, N, K = 3, 32, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	if gemmCosine32(got, want) < 0.999 {
		t.Fatalf("quad-3 N24 vs row-dot cosine=%g", gemmCosine32(got, want))
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("quad-3 N24 vs row-dot max|Δ|=%g", maxd)
	}
}

func TestQ8QuadMultiDot3N36_MatchesRowDot(t *testing.T) {
	const M, N, K = 3, 32, 1152
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	if gemmCosine32(got, want) < 0.999 {
		t.Fatalf("quad-3 N36 vs row-dot cosine=%g", gemmCosine32(got, want))
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("quad-3 N36 vs row-dot max|Δ|=%g", maxd)
	}
}

func TestQ8MultiDotN24_M3MatchesGeneric(t *testing.T) {
	const M, N, K = 3, 32, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("N24 M=3 padded vs row-dot max|Δ|=%g", maxd)
	}
}

func TestQ8MultiDotN24_M4MatchesRowDot(t *testing.T) {
	const M, N, K = 4, 64, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	var dot, na, nb float64
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
		dot += float64(got[i]) * float64(want[i])
		na += float64(got[i]) * float64(got[i])
		nb += float64(want[i]) * float64(want[i])
	}
	cos := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if cos < 0.999 {
		t.Fatalf("N24 M=4 vs row-dot cosine=%g max|Δ|=%g", cos, maxd)
	}
}

func TestQ8MultiDotN24_M16MatchesRowDot(t *testing.T) {
	const M, N, K = 16, 64, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	for _, w := range []int{1, 0} {
		got := make([]float32, M*N)
		want := make([]float32, M*N)
		MatMulQ8N(got, a, b, M, N, K, w)
		for m := 0; m < M; m++ {
			for n := 0; n < N; n++ {
				want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
			}
		}
		if gemmCosine32(got, want) < 0.999 {
			t.Fatalf("workers=%d N24 M=16 vs row-dot cosine=%g", w, gemmCosine32(got, want))
		}
	}
}

func TestQ8MultiDotN24_MatchesGeneric(t *testing.T) {
	const M, N, K = 8, 32, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	// Generic path via MatMulQ8N workers=1 uses fused N24 when K=768.
	MatMulQ8N(got, a, b, M, N, K, 1)
	old := useQ8DequantOnce(M, N, K, true)
	_ = old
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("N24 fused vs row-dot max|Δ|=%g", maxd)
	}
}

func TestMatMulQ8PackedQKV_MatchesThreeGemms(t *testing.T) {
	for _, seq := range []int{3, 6, 16} {
		for _, w := range []int{1, 0} {
			testMatMulQ8PackedQKVSeq(t, seq, w)
		}
	}
	defer SetMatMulMaxParallel(0)
	for _, nw := range []int{5, 7} {
		SetMatMulMaxParallel(nw)
		testMatMulQ8PackedQKVSeq(t, 3, 0)
	}
}

func testMatMulQ8PackedQKVSeq(t *testing.T, seq, maxWorkers int) {
	t.Helper()
	const K, Nq, Nkv = 768, 768, 256
	a := make([]float32, seq*K)
	qData := make([]float32, Nq*K)
	kData := make([]float32, Nkv*K)
	vData := make([]float32, Nkv*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range qData {
		qData[i] = float32((i%13)-6) * 0.02
	}
	for i := range kData {
		kData[i] = float32((i%11)-5) * 0.015
		vData[i] = float32((i%7)-3) * 0.018
	}
	wq := QuantizeToQ8(qData, Nq, K)
	wk := QuantizeToQ8(kData, Nkv, K)
	wv := QuantizeToQ8(vData, Nkv, K)
	if seq == 3 {
		wq.PackQS()
		wk.PackQS()
		wv.PackQS()
	}
	gotQ := make([]float32, seq*Nq)
	gotK := make([]float32, seq*Nkv)
	gotV := make([]float32, seq*Nkv)
	wantQ := make([]float32, seq*Nq)
	wantK := make([]float32, seq*Nkv)
	wantV := make([]float32, seq*Nkv)
	MatMulQ8N(wantQ, a, wq, seq, Nq, K, 1)
	MatMulQ8N(wantK, a, wk, seq, Nkv, K, 1)
	MatMulQ8N(wantV, a, wv, seq, Nkv, K, 1)
	MatMulQ8PackedQKV(gotQ, gotK, gotV, a, wq, wk, wv, seq, maxWorkers)
	var maxd float32
	check := func(got, want []float32) {
		for i := range got {
			d := float32(math.Abs(float64(got[i] - want[i])))
			if d > maxd {
				maxd = d
			}
		}
	}
	check(gotQ, wantQ)
	check(gotK, wantK)
	check(gotV, wantV)
	if maxd > 2e-3 {
		t.Fatalf("seq=%d workers=%d PackedQKV vs three GEMMs max|Δ|=%g", seq, maxWorkers, maxd)
	}
}

func TestMatMulQ8RMSResidual_MatchesSeparate(t *testing.T) {
	const seq, N, K, mt = 6, 768, 1152, 8
	x0 := make([]float32, seq*N)
	a := make([]float32, seq*K)
	bData := make([]float32, N*K)
	wRMS := make([]float32, N)
	for i := range x0 {
		x0[i] = float32((i%9)-4) * 0.02
	}
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	for i := range wRMS {
		wRMS[i] = 1 + float32(i%5)*0.01
	}
	b := QuantizeToQ8(bData, N, K)
	want := append([]float32(nil), x0...)
	y := make([]float32, seq*N)
	MatMulQ8N(y, a, b, seq, N, K, 1)
	for s := 0; s < seq; s++ {
		row := y[s*N : (s+1)*N]
		RMSNorm(row, row, wRMS, 1e-6)
		Add(want[s*N:(s+1)*N], want[s*N:(s+1)*N], row)
	}
	got := append([]float32(nil), x0...)
	yTile := make([]float32, mt*N)
	MatMulQ8RMSResidual(got, a, yTile, b, wRMS, seq, N, K, mt, 1, 1e-6)
	if gemmCosine32(got, want) < 0.999 {
		t.Fatalf("RMSResidual vs MatMul+RMS+Add cosine=%g", gemmCosine32(got, want))
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 1e-4 {
		t.Fatalf("RMSResidual vs separate max|Δ|=%g want <=1e-4", maxd)
	}
}

func TestMatMulQ8RMSResidual_M3NSplitMatchesSeparate(t *testing.T) {
	defer SetMatMulMaxParallel(0)
	const seq, N, mt = 3, 768, 8
	for _, K := range []int{768, 1152} {
		x0 := make([]float32, seq*N)
		a := make([]float32, seq*K)
		bData := make([]float32, N*K)
		wRMS := make([]float32, N)
		for i := range x0 {
			x0[i] = float32((i%9)-4) * 0.02
		}
		for i := range a {
			a[i] = float32((i%17)-8) * 0.01
		}
		for i := range bData {
			bData[i] = float32((i%11)-5) * 0.03
		}
		for i := range wRMS {
			wRMS[i] = 1 + float32(i%5)*0.01
		}
		b := QuantizeToQ8(bData, N, K)
		b.PackQS()
		want := append([]float32(nil), x0...)
		y := make([]float32, seq*N)
		MatMulQ8N(y, a, b, seq, N, K, 1)
		for s := 0; s < seq; s++ {
			row := y[s*N : (s+1)*N]
			RMSNorm(row, row, wRMS, 1e-6)
			Add(want[s*N:(s+1)*N], want[s*N:(s+1)*N], row)
		}
		for _, nw := range []int{0, 5, 7} {
			SetMatMulMaxParallel(nw)
			got := append([]float32(nil), x0...)
			yTile := make([]float32, mt*N)
			MatMulQ8RMSResidual(got, a, yTile, b, wRMS, seq, N, K, mt, nw, 1e-6)
			if gemmCosine32(got, want) < 0.999 {
				t.Fatalf("K=%d workers=%d RMSResidual M=3 cosine=%g", K, nw, gemmCosine32(got, want))
			}
		}
	}
}

func TestMatMulQ8DualOut_MatchesTwoGemms(t *testing.T) {
	for _, seq := range []int{3, 6, 8, 16} {
		for _, w := range []int{1, 0} {
			testMatMulQ8DualOutSeq(t, seq, w)
		}
	}
}

func TestMatMulQ8DualOut_M3NSplitLeftover(t *testing.T) {
	defer SetMatMulMaxParallel(0)
	for _, nw := range []int{5, 7} {
		SetMatMulMaxParallel(nw)
		testMatMulQ8DualOutSeq(t, 3, 0)
		SetMatMulMaxParallel(0)
	}
}

func testMatMulQ8DualOutSeq(t *testing.T, seq, maxWorkers int) {
	t.Helper()
	const K, N = 768, 1152
	a := make([]float32, seq*K)
	gData := make([]float32, N*K)
	uData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range gData {
		gData[i] = float32((i%13)-6) * 0.02
		uData[i] = float32((i%11)-5) * 0.015
	}
	wGate := QuantizeToQ8(gData, N, K)
	wUp := QuantizeToQ8(uData, N, K)
	gotG := make([]float32, seq*N)
	gotU := make([]float32, seq*N)
	wantG := make([]float32, seq*N)
	wantU := make([]float32, seq*N)
	MatMulQ8N(wantG, a, wGate, seq, N, K, 1)
	MatMulQ8N(wantU, a, wUp, seq, N, K, 1)
	SiLUMul(wantG, wantU)
	MatMulQ8DualOut(gotG, gotU, a, wGate, wUp, seq, maxWorkers)
	var maxd float32
	for i := range gotG {
		d := float32(math.Abs(float64(gotG[i] - wantG[i])))
		if d > maxd {
			maxd = d
		}
		d = float32(math.Abs(float64(gotU[i] - wantU[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("seq=%d workers=%d DualOut vs two GEMMs+SiLUMul max|Δ|=%g", seq, maxWorkers, maxd)
	}
}

func TestMatMulQ8DualOut_PackedM3MatchesTwoGEMMs(t *testing.T) {
	defer SetMatMulMaxParallel(0)
	const seq, K, N = 3, 768, 1152
	a := make([]float32, seq*K)
	gData := make([]float32, N*K)
	uData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range gData {
		gData[i] = float32((i%13)-6) * 0.02
		uData[i] = float32((i%11)-5) * 0.015
	}
	wGate := QuantizeToQ8(gData, N, K)
	wUp := QuantizeToQ8(uData, N, K)
	wGate.PackQS()
	wUp.PackQS()
	wantG := make([]float32, seq*N)
	wantU := make([]float32, seq*N)
	MatMulQ8N(wantG, a, wGate, seq, N, K, 1)
	MatMulQ8N(wantU, a, wUp, seq, N, K, 1)
	SiLUMul(wantG, wantU)
	for _, nw := range []int{0, 5, 7} {
		SetMatMulMaxParallel(nw)
		gotG := make([]float32, seq*N)
		gotU := make([]float32, seq*N)
		MatMulQ8DualOut(gotG, gotU, a, wGate, wUp, seq, nw)
		var maxd float32
		for i := range gotG {
			d := float32(math.Abs(float64(gotG[i] - wantG[i])))
			if d > maxd {
				maxd = d
			}
			d = float32(math.Abs(float64(gotU[i] - wantU[i])))
			if d > maxd {
				maxd = d
			}
		}
		if maxd > 2e-3 {
			t.Fatalf("packed DualOut M=3 workers=%d vs two GEMMs max|Δ|=%g", nw, maxd)
		}
	}
}

func TestMatMulQ8DualOutDownRMS_MatchesSeparate(t *testing.T) {
	const seq, K, N, D = 16, 768, 1152, 768
	a := make([]float32, seq*K)
	x0 := make([]float32, seq*D)
	gData := make([]float32, N*K)
	uData := make([]float32, N*K)
	dData := make([]float32, D*N)
	wRMS := make([]float32, D)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.01
	}
	for i := range x0 {
		x0[i] = float32((i%9)-4) * 0.02
	}
	for i := range gData {
		gData[i] = float32((i%13)-6) * 0.02
		uData[i] = float32((i%11)-5) * 0.015
	}
	for i := range dData {
		dData[i] = float32((i%7)-3) * 0.03
	}
	for i := range wRMS {
		wRMS[i] = 1 + float32(i%5)*0.01
	}
	wGate := QuantizeToQ8(gData, N, K)
	wUp := QuantizeToQ8(uData, N, K)
	wDown := QuantizeToQ8(dData, D, N)
	gate := make([]float32, seq*N)
	up := make([]float32, seq*N)
	yTile := make([]float32, 8*D)
	wantX := append([]float32(nil), x0...)
	MatMulQ8DualOut(gate, up, a, wGate, wUp, seq, 1)
	MatMulQ8RMSResidual(wantX, gate, yTile, wDown, wRMS, seq, D, N, 8, 1, 1e-6)
	for _, w := range []int{1, 0} {
		gotX := append([]float32(nil), x0...)
		MatMulQ8DualOutDownRMS(gotX, a, make([]float32, seq*N), make([]float32, seq*N), yTile, wGate, wUp, wDown, wRMS, seq, w, 1e-6)
		if gemmCosine32(gotX, wantX) < 0.999 {
			t.Fatalf("workers=%d DualOutDownRMS vs DualOut+RMSResidual cosine=%g", w, gemmCosine32(gotX, wantX))
		}
	}
}

func TestQ8DequantOnce_M32K768MatchesRowDot(t *testing.T) {
	const M, N, K = 32, 64, 768
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	if gemmCosine32(got, want) < 0.999 {
		t.Fatalf("dequant-once M=32 vs row-dot cosine=%g", gemmCosine32(got, want))
	}
}

func TestQ8MultiDotN36_MatchesGeneric(t *testing.T) {
	const M, N, K = 8, 32, 1152
	a := make([]float32, M*K)
	bData := make([]float32, N*K)
	for i := range a {
		a[i] = float32((i%19)-9) * 0.02
	}
	for i := range bData {
		bData[i] = float32((i%11)-5) * 0.03
	}
	b := QuantizeToQ8(bData, N, K)
	got := make([]float32, M*N)
	want := make([]float32, M*N)
	MatMulQ8N(got, a, b, M, N, K, 1)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			want[m*N+n] = DotQ8RowScaled(a[m*K:(m+1)*K], b, n)
		}
	}
	var maxd float32
	for i := range got {
		d := float32(math.Abs(float64(got[i] - want[i])))
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 2e-3 {
		t.Fatalf("N36 fused vs row-dot max|Δ|=%g", maxd)
	}
}

func TestRMSNormRows_MatchesPerRow(t *testing.T) {
	const seq, dim = 5, 768
	x := make([]float32, seq*dim)
	w := make([]float32, dim)
	for i := range x {
		x[i] = float32((i%19)-9) * 0.05
	}
	for i := range w {
		w[i] = 1 + float32(i%5)*0.01
	}
	got := make([]float32, seq*dim)
	want := make([]float32, seq*dim)
	RMSNormRows(got, x, w, seq, dim, 1e-6)
	for s := 0; s < seq; s++ {
		off := s * dim
		RMSNorm(want[off:off+dim], x[off:off+dim], w, 1e-6)
	}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			t.Fatalf("row %d delta %g", i, got[i]-want[i])
		}
	}
}
