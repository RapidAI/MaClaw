package tensor

import (
	"math"
	"testing"
)

func TestRMSNorm_Dim256And768MatchesFormula(t *testing.T) {
	eps := float32(1e-6)
	for _, n := range []int{256, 768} {
		x := make([]float32, n)
		w := make([]float32, n)
		for i := 0; i < n; i++ {
			x[i] = float32((i%17)-8) * 0.05
			w[i] = 1 + float32(i%5)*0.01
		}
		got := make([]float32, n)
		copy(got, x)
		RMSNorm(got, got, w, eps)
		var ss float64
		for i := 0; i < n; i++ {
			ss += float64(x[i]) * float64(x[i])
		}
		scale := 1.0 / math.Sqrt(ss/float64(n)+float64(eps))
		var maxd float32
		for i := 0; i < n; i++ {
			want := float32(float64(x[i]) * float64(w[i]) * scale)
			d := float32(math.Abs(float64(got[i] - want)))
			if d > maxd {
				maxd = d
			}
		}
		if maxd > 2e-4 {
			t.Fatalf("n=%d RMSNorm vs formula max|Δ|=%g", n, maxd)
		}
	}
}

func TestMatMulWorkersForSmallWideMatrix(t *testing.T) {
	SetMatMulMaxParallel(12)
	defer SetMatMulMaxParallel(0)

	base := poolWorkers()
	got := matMulWorkersFor(7, 512, 512)
	want := base
	if want > 8 {
		want = 8
	}
	if got != want {
		t.Fatalf("small wide matrix workers = %d, want %d", got, want)
	}
	if got := matMulWorkersFor(9, 512, 512); got != base {
		t.Fatalf("larger batch workers = %d, want %d", got, base)
	}
}

func TestMatMulBiasParallelStable(t *testing.T) {
	// This shape forces the N-partitioned worker-pool path. Repeating it
	// catches task-reuse races that only appear when a new call checks a task
	// out immediately after the preceding one completes.
	const M, N, K = 16, 384, 96
	a := make([]float32, M*K)
	b := make([]float32, N*K)
	bias := make([]float32, N)
	for i := range a {
		a[i] = float32((i%17)-8) * 0.125
	}
	for i := range b {
		b[i] = float32((i%23)-11) * 0.0625
	}
	for i := range bias {
		bias[i] = float32((i%7)-3) * 0.25
	}
	want := make([]float32, M*N)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			var s float32
			for k := 0; k < K; k++ {
				s += a[m*K+k] * b[n*K+k]
			}
			want[m*N+n] = s + bias[n]
		}
	}
	for iter := 0; iter < 32; iter++ {
		got := make([]float32, M*N)
		MatMulBias(got, a, b, bias, M, N, K)
		for i := range got {
			if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
				t.Fatalf("iter %d index %d: got %g want %g", iter, i, got[i], want[i])
			}
		}
	}
}

func TestElemMul_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	ElemMul(a, a, b)
	want := []float32{20, 60, 120}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestElemMul_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	ElemMul(b, a, b)
	want := []float32{20, 60, 120}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	Add(a, a, b)
	want := []float32{12, 23, 34}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	Add(b, a, b)
	want := []float32{12, 23, 34}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAddBiasGELU_MatchesAddBiasThenGELU(t *testing.T) {
	data := []float32{-2, -1, 0, 1, 2, 3}
	bias := []float32{0.5, -0.25, 1}
	got := append([]float32(nil), data...)
	want := append([]float32(nil), data...)

	AddBiasGELU(got, 2, 3, bias)
	AddBias(want, 2, 3, bias)
	GELU(want)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-6 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestWeightedSumStrided(t *testing.T) {
	weights := []float32{0.25, 0.5, -0.75}
	values := []float32{
		1, 2, 3, 99,
		4, 5, 6, 99,
		7, 8, 9, 99,
	}
	got := make([]float32, 3)
	WeightedSumStrided(got, weights, values, 3, 4, 3)

	want := []float32{-3, -3, -3}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-6 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSoftmaxWeightedSumStrided_MatchesSoftmaxThenWeightedSum(t *testing.T) {
	scores := []float32{-2, 0.5, 3, -1.25}
	values := []float32{
		1, 2, 3, 99,
		4, 5, 6, 99,
		7, 8, 9, 99,
		-1, -2, -3, 99,
	}
	got := make([]float32, 3)
	want := make([]float32, 3)
	gotScores := append([]float32(nil), scores...)
	wantScores := append([]float32(nil), scores...)

	SoftmaxWeightedSumStrided(got, gotScores, values, 4, 4, 3)
	Softmax(wantScores)
	WeightedSumStrided(want, wantScores, values, 4, 4, 3)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSoftmaxWeightedSumBatched3_Dim256MatchesSequential(t *testing.T) {
	// Gemma short Embed: seq=3, headDim=256, one batched nQ=3 tile (not leftover nQ=1).
	const rows, dim, nQ = 3, 256, 3
	values := make([]float32, rows*dim)
	for i := range values {
		values[i] = float32((i%17)-8) * 0.1
	}
	scoresBatched := make([]float32, nQ*rows)
	scoresSeq := make([]float32, nQ*rows)
	for t := 0; t < nQ; t++ {
		for r := 0; r < rows; r++ {
			v := float32(t-3)*0.2 + float32(r-8)*0.05
			scoresBatched[t*rows+r] = v
			scoresSeq[t*rows+r] = v
		}
	}
	got := make([]float32, nQ*dim)
	want := make([]float32, nQ*dim)
	SoftmaxWeightedSumBatched(got, scoresBatched, values, nQ, rows, dim, dim, dim, 0, 0)
	for t := 0; t < nQ; t++ {
		SoftmaxWeightedSumStrided(want[t*dim:(t+1)*dim], scoresSeq[t*rows:(t+1)*rows], values, rows, dim, dim)
	}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
			t.Fatalf("idx %d: got %v want %v diff %v", i, got[i], want[i], diff)
		}
	}
}

func TestSoftmaxWeightedSumBatched4_Dim256MatchesSequential(t *testing.T) {
	const rows, dim, nQ = 16, 256, 4
	values := make([]float32, rows*dim)
	for i := range values {
		values[i] = float32((i%17)-8) * 0.1
	}
	scoresBatched := make([]float32, nQ*rows)
	scoresSeq := make([]float32, nQ*rows)
	for t := 0; t < nQ; t++ {
		for r := 0; r < rows; r++ {
			v := float32(t-3)*0.2 + float32(r-8)*0.05
			scoresBatched[t*rows+r] = v
			scoresSeq[t*rows+r] = v
		}
	}
	got := make([]float32, nQ*dim)
	want := make([]float32, nQ*dim)
	SoftmaxWeightedSumBatched(got, scoresBatched, values, nQ, rows, dim, dim, dim, 0, 0)
	for t := 0; t < nQ; t++ {
		SoftmaxWeightedSumStrided(want[t*dim:(t+1)*dim], scoresSeq[t*rows:(t+1)*rows], values, rows, dim, dim)
	}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
			t.Fatalf("idx %d: got %v want %v diff %v", i, got[i], want[i], diff)
		}
	}
}

func TestSoftmaxWeightedSumBatched8_MatchesSequential(t *testing.T) {
	const rows, dim, nQ = 16, 128, 8
	// Contiguous V [rows][dim]
	values := make([]float32, rows*dim)
	for i := range values {
		values[i] = float32((i%17)-8) * 0.1
	}
	// scores [nQ][rows]
	scoresBatched := make([]float32, nQ*rows)
	scoresSeq := make([]float32, nQ*rows)
	for t := 0; t < nQ; t++ {
		for r := 0; r < rows; r++ {
			v := float32(t-3)*0.2 + float32(r-8)*0.05
			scoresBatched[t*rows+r] = v
			scoresSeq[t*rows+r] = v
		}
	}
	// out: [nQ][dim] with outStride=dim, hOff=0, qf=0
	got := make([]float32, nQ*dim)
	want := make([]float32, nQ*dim)
	SoftmaxWeightedSumBatched(got, scoresBatched, values, nQ, rows, dim, dim, dim, 0, 0)
	for t := 0; t < nQ; t++ {
		SoftmaxWeightedSumStrided(want[t*dim:(t+1)*dim], scoresSeq[t*rows:(t+1)*rows], values, rows, dim, dim)
	}
	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
			t.Fatalf("idx %d: got %v want %v diff %v", i, got[i], want[i], diff)
		}
	}
}

func TestLayerNorm_MatchesReference(t *testing.T) {
	x := []float32{-1.5, 0.25, 2, -0.75, 1.25, 3.5, -2.25, 0.5}
	weight := []float32{1, 0.5, -1, 2, 1.5, -0.25, 0.75, 1.25}
	got := make([]float32, len(x))
	want := make([]float32, len(x))

	LayerNorm(got, x, weight, 1e-5)
	layerNormReference(want, x, weight, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLayerNorm_InPlaceMatchesReference(t *testing.T) {
	x := []float32{-1.5, 0.25, 2, -0.75, 1.25, 3.5, -2.25, 0.5}
	weight := []float32{1, 0.5, -1, 2, 1.5, -0.25, 0.75, 1.25}
	got := append([]float32(nil), x...)
	want := make([]float32, len(x))

	LayerNorm(got, got, weight, 1e-5)
	layerNormReference(want, x, weight, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestGroupNorm1_MatchesReference(t *testing.T) {
	data := []float32{-1, 2, 0.5, 3, -2, 1.5, 0.25, -0.75, 2.5, -1.5, 1, 0}
	weight := []float32{1, 0.5, -1, 2}
	bias := []float32{0.1, -0.2, 0.3, -0.4}
	got := append([]float32(nil), data...)
	want := append([]float32(nil), data...)

	GroupNorm1(got, 3, 4, weight, bias, 1e-5)
	groupNorm1Reference(want, 3, 4, weight, bias, 1e-5)

	for i := range got {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-5 {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func layerNormReference(out, x, weight []float32, eps float32) {
	n := len(x)
	var mean float32
	for _, v := range x {
		mean += v
	}
	mean /= float32(n)
	var variance float32
	for _, v := range x {
		d := v - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for i := range x {
		out[i] = (x[i] - mean) * scale * weight[i]
	}
}

func TestWsumBatchedDual_MatchesSequential(t *testing.T) {
	const dim = 128
	o0, o1, o2, o3 := make([]float32, dim), make([]float32, dim), make([]float32, dim), make([]float32, dim)
	r0, r1, r2, r3 := make([]float32, dim), make([]float32, dim), make([]float32, dim), make([]float32, dim)
	va, vb := make([]float32, dim), make([]float32, dim)
	for i := 0; i < dim; i++ {
		va[i] = float32(i%7) * 0.1
		vb[i] = float32(i%5) * 0.2
		o0[i], o1[i], o2[i], o3[i] = 1, 2, 3, 4
		r0[i], r1[i], r2[i], r3[i] = 1, 2, 3, 4
	}
	wa0, wa1, wa2, wa3 := float32(0.1), float32(0.2), float32(0.3), float32(0.4)
	wb0, wb1, wb2, wb3 := float32(0.5), float32(0.6), float32(0.7), float32(0.8)
	wsumBatched4Add128Dual(o0, o1, o2, o3, va, vb, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3)
	wsumBatched4Add128(r0, r1, r2, r3, va, wa0, wa1, wa2, wa3)
	wsumBatched4Add128(r0, r1, r2, r3, vb, wb0, wb1, wb2, wb3)
	for i := 0; i < dim; i++ {
		if math.Abs(float64(o0[i]-r0[i])) > 1e-5 || math.Abs(float64(o3[i]-r3[i])) > 1e-5 {
			t.Fatalf("dual4 mismatch at %d: got %v want %v", i, o0[i], r0[i])
		}
	}
	// set-add dual seed
	wsumBatched4SetAdd128Dual(o0, o1, o2, o3, va, vb, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3)
	for i := 0; i < dim; i++ {
		want0 := wa0*va[i] + wb0*vb[i]
		if math.Abs(float64(o0[i]-want0)) > 1e-5 {
			t.Fatalf("setAdd dual4 mismatch at %d: got %v want %v", i, o0[i], want0)
		}
	}
	// 8-way dual
	outs := make([][]float32, 8)
	refs := make([][]float32, 8)
	for q := 0; q < 8; q++ {
		outs[q] = make([]float32, dim)
		refs[q] = make([]float32, dim)
		for i := 0; i < dim; i++ {
			outs[q][i] = float32(q)
			refs[q][i] = float32(q)
		}
	}
	var wa, wb [8]float32
	for q := 0; q < 8; q++ {
		wa[q] = float32(q+1) * 0.05
		wb[q] = float32(q+1) * 0.07
	}
	wsumBatched8Add128Dual(outs[0], outs[1], outs[2], outs[3], outs[4], outs[5], outs[6], outs[7], va, vb, &wa, &wb)
	wsumBatched8Add128(refs[0], refs[1], refs[2], refs[3], refs[4], refs[5], refs[6], refs[7], va,
		wa[0], wa[1], wa[2], wa[3], wa[4], wa[5], wa[6], wa[7])
	wsumBatched8Add128(refs[0], refs[1], refs[2], refs[3], refs[4], refs[5], refs[6], refs[7], vb,
		wb[0], wb[1], wb[2], wb[3], wb[4], wb[5], wb[6], wb[7])
	for q := 0; q < 8; q++ {
		for i := 0; i < dim; i++ {
			if math.Abs(float64(outs[q][i]-refs[q][i])) > 1e-4 {
				t.Fatalf("dual8 q=%d i=%d got %v want %v", q, i, outs[q][i], refs[q][i])
			}
		}
	}
}

func TestPackQKV128_MatchesSequential(t *testing.T) {
	srcQ, srcK, srcV := make([]float32, 128), make([]float32, 128), make([]float32, 128)
	dQ, dK, dV := make([]float32, 128), make([]float32, 128), make([]float32, 128)
	rQ, rK, rV := make([]float32, 128), make([]float32, 128), make([]float32, 128)
	scale := float32(0.08838835)
	for i := 0; i < 128; i++ {
		srcQ[i] = float32(i) * 0.01
		srcK[i] = float32(i) * 0.02
		srcV[i] = float32(i) * 0.03
	}
	PackQKV128(dQ, dK, dV, srcQ, srcK, srcV, scale)
	Copy128Mul(rQ, srcQ, scale)
	Copy128(rK, srcK)
	Copy128(rV, srcV)
	for i := 0; i < 128; i++ {
		if dQ[i] != rQ[i] || dK[i] != rK[i] || dV[i] != rV[i] {
			t.Fatalf("PackQKV128 mismatch at %d", i)
		}
	}
}

func TestMul2Fmadd2_MatchesSequential(t *testing.T) {
	const n = 512
	o0, o1 := make([]float32, n), make([]float32, n)
	r0, r1 := make([]float32, n), make([]float32, n)
	a0, a1 := make([]float32, n), make([]float32, n)
	b := make([]float32, n)
	for i := 0; i < n; i++ {
		a0[i] = float32(i%7) * 0.1
		a1[i] = float32(i%5) * 0.2
		b[i] = float32(i%13)*0.01 + 0.5
	}
	Mul2Into(o0, o1, a0, a1, b)
	for i := 0; i < n; i++ {
		r0[i], r1[i] = a0[i]*b[i], a1[i]*b[i]
	}
	for i := 0; i < n; i++ {
		if o0[i] != r0[i] || o1[i] != r1[i] {
			t.Fatalf("Mul2Into mismatch at %d", i)
		}
	}
	Fmadd2Into(o0, o1, a0, a1, b)
	for i := 0; i < n; i++ {
		r0[i] += a0[i] * b[i]
		r1[i] += a1[i] * b[i]
	}
	for i := 0; i < n; i++ {
		if o0[i] != r0[i] || o1[i] != r1[i] {
			t.Fatalf("Fmadd2Into mismatch at %d", i)
		}
	}
	FmaddPlusOne2Into(o0, o1, a0, a1, b)
	for i := 0; i < n; i++ {
		bp1 := b[i] + 1
		r0[i] += a0[i] * bp1
		r1[i] += a1[i] * bp1
	}
	for i := 0; i < n; i++ {
		if math.Abs(float64(o0[i]-r0[i])) > 1e-5 {
			t.Fatalf("FmaddPlusOne2Into mismatch at %d", i)
		}
	}
}

func TestMul4Fmadd4_MatchesSequential(t *testing.T) {
	const n = 512
	o0, o1, o2, o3 := make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	r0, r1, r2, r3 := make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	a0, a1, a2, a3 := make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	b := make([]float32, n)
	for i := 0; i < n; i++ {
		a0[i] = float32(i%7) * 0.1
		a1[i] = float32(i%5) * 0.2
		a2[i] = float32(i%3) * 0.3
		a3[i] = float32(i%11) * 0.05
		b[i] = float32(i%13)*0.01 + 0.5
	}
	Mul4Into(o0, o1, o2, o3, a0, a1, a2, a3, b)
	for i := 0; i < n; i++ {
		r0[i], r1[i], r2[i], r3[i] = a0[i]*b[i], a1[i]*b[i], a2[i]*b[i], a3[i]*b[i]
	}
	for i := 0; i < n; i++ {
		if o0[i] != r0[i] || o1[i] != r1[i] || o2[i] != r2[i] || o3[i] != r3[i] {
			t.Fatalf("Mul4Into mismatch at %d", i)
		}
	}
	// accumulate
	Fmadd4Into(o0, o1, o2, o3, a0, a1, a2, a3, b)
	for i := 0; i < n; i++ {
		r0[i] += a0[i] * b[i]
		r1[i] += a1[i] * b[i]
		r2[i] += a2[i] * b[i]
		r3[i] += a3[i] * b[i]
	}
	for i := 0; i < n; i++ {
		if o0[i] != r0[i] || o1[i] != r1[i] || o2[i] != r2[i] || o3[i] != r3[i] {
			t.Fatalf("Fmadd4Into mismatch at %d: got %v want %v", i, o0[i], r0[i])
		}
	}
	// center+1
	copy(o0, r0)
	copy(o1, r1)
	copy(o2, r2)
	copy(o3, r3)
	FmaddPlusOne4Into(o0, o1, o2, o3, a0, a1, a2, a3, b)
	for i := 0; i < n; i++ {
		bp1 := b[i] + 1
		r0[i] += a0[i] * bp1
		r1[i] += a1[i] * bp1
		r2[i] += a2[i] * bp1
		r3[i] += a3[i] * bp1
	}
	for i := 0; i < n; i++ {
		if math.Abs(float64(o0[i]-r0[i])) > 1e-5 {
			t.Fatalf("FmaddPlusOne4Into mismatch at %d: got %v want %v", i, o0[i], r0[i])
		}
	}
}

func groupNorm1Reference(data []float32, time, channels int, weight, bias []float32, eps float32) {
	n := time * channels
	var mean float32
	for _, v := range data[:n] {
		mean += v
	}
	mean /= float32(n)
	var variance float32
	for _, v := range data[:n] {
		d := v - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for t := 0; t < time; t++ {
		off := t * channels
		for c := 0; c < channels; c++ {
			v := (data[off+c] - mean) * scale
			if weight != nil {
				v *= weight[c]
			}
			if bias != nil {
				v += bias[c]
			}
			data[off+c] = v
		}
	}
}
