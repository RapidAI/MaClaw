package kokoro

import (
	"math"
	"testing"
)

func closeEnough(a, b float32) bool { return float32(math.Abs(float64(a-b))) < 1e-4 }

func requireSliceClose(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d len(want)=%d got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range got {
		if !closeEnough(got[i], want[i]) {
			t.Fatalf("at %d got=%v want=%v all got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func TestLinearSequenceGEMMMatchesRowDots(t *testing.T) {
	prev := kokoroSeqGEMM
	kokoroSeqGEMM = true
	t.Cleanup(func() { kokoroSeqGEMM = prev })

	const steps, in, outDim = 4, 8, 6
	x := make([]float32, steps*in)
	w := make([]float32, outDim*in)
	bias := []float32{0.1, -0.2, 0.3, -0.4, 0.5, -0.6}
	for i := range x {
		x[i] = float32((i%5)-2) * 0.15
	}
	for i := range w {
		w[i] = float32((i%7)-3) * 0.07
	}
	got := make([]float32, steps*outDim)
	weight := &Tensor{Dims: []int{outDim, in}, Data: w}
	if err := LinearSequenceTensor(got, x, weight, bias, steps, in, outDim); err != nil {
		t.Fatal(err)
	}
	want := make([]float32, steps*outDim)
	for s := 0; s < steps; s++ {
		if err := Linear(want[s*outDim:(s+1)*outDim], x[s*in:(s+1)*in], w, bias, in, outDim); err != nil {
			t.Fatal(err)
		}
	}
	var maxd float32
	for i := range got {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
	}
	if maxd > 1e-3 {
		t.Fatalf("seq gemm max abs diff %g\ngot %v\nwant %v", maxd, got, want)
	}
}

func TestPointwiseLayoutMatchesConv1D(t *testing.T) {
	const inC, inT, outC = 4, 5, 3
	x := make([]float32, inC*inT)
	w := make([]float32, outC*inC)
	bias := []float32{0.25, -0.5, 0.125}
	for i := range x {
		x[i] = float32(i+1) * 0.1
	}
	for i := range w {
		w[i] = float32((i%4)-1) * 0.3
	}
	want := make([]float32, outC*inT)
	if err := Conv1D(want, x, w, bias, inC, inT, outC, 1, 1, 0, 1, 1); err != nil {
		t.Fatal(err)
	}
	got := make([]float32, outC*inT)
	conv1DPointwiseFromMatMul(got, x, w, bias, inC, inT, outC, func(dst, a, b []float32, M, N, K int) {
		for i := 0; i < M; i++ {
			for j := 0; j < N; j++ {
				var s float32
				for k := 0; k < K; k++ {
					s += a[i*K+k] * b[j*K+k]
				}
				dst[i*N+j] = s
			}
		}
	})
	requireSliceClose(t, got, want)
}

func TestLinear(t *testing.T) {
	out := make([]float32, 2)
	err := Linear(out, []float32{1, 2, 3}, []float32{1, 0, 1, 0, 1, 1}, []float32{0.5, -0.5}, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{4.5, 4.5})
}

func TestEmbedding(t *testing.T) {
	out := make([]float32, 4)
	err := Embedding(out, []int{2, 0}, []float32{1, 2, 3, 4, 5, 6}, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{5, 6, 1, 2})
}

func TestLayerNorm1D(t *testing.T) {
	out := make([]float32, 2)
	err := LayerNorm1D(out, []float32{1, 3}, []float32{1, 1}, []float32{0, 0}, 1e-5)
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(out[0], -0.999995) || !closeEnough(out[1], 0.999995) {
		t.Fatalf("unexpected layernorm: %v", out)
	}
}

func TestConv1D(t *testing.T) {
	// One input channel [1,2,3], one output channel kernel [1,1].
	out := make([]float32, 2)
	err := Conv1D(out, []float32{1, 2, 3}, []float32{1, 1}, nil, 1, 3, 1, 2, 1, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{3, 5})
}

func TestDot3Fused(t *testing.T) {
	const n = 17
	a0 := make([]float32, n)
	a1 := make([]float32, n)
	a2 := make([]float32, n)
	w0 := make([]float32, n)
	w1 := make([]float32, n)
	w2 := make([]float32, n)
	for i := 0; i < n; i++ {
		a0[i] = float32(i-5) * 0.07
		a1[i] = float32((i%7)-3) * 0.11
		a2[i] = float32((i%5)-2) * 0.13
		w0[i] = float32((i%3)-1) * 0.17
		w1[i] = float32(i-8) * -0.03
		w2[i] = float32((i%11)-5) * 0.019
	}
	want := dot32(a0, w0) + dot32(a1, w1) + dot32(a2, w2)
	got := dot3Fused(a0, a1, a2, w0, w1, w2)
	if !closeEnough(got, want) {
		t.Fatalf("dot3Fused got=%v want=%v", got, want)
	}
}

func TestConv1DSIMDFusedInterior(t *testing.T) {
	const inC = 16
	const inT = 8
	const outC = 2
	const kernel = 3
	x := make([]float32, inC*inT)
	for c := 0; c < inC; c++ {
		for tt := 0; tt < inT; tt++ {
			x[c*inT+tt] = float32(c+1)*0.1 + float32(tt)*0.01
		}
	}
	w := make([]float32, outC*inC*kernel)
	for i := range w {
		w[i] = float32((i%7)-3) * 0.02
	}
	b := []float32{0.25, -0.125}
	wT := make([]float32, outC*kernel*inC)
	transposeConv1DWeight(wT, w, inC, outC, kernel)
	got := make([]float32, outC*inT)
	if err := conv1DSIMDTransposedWeight(got, x, wT, b, nil, inC, inT, outC, kernel, 1, 1, 1, inT); err != nil {
		t.Fatal(err)
	}
	want := make([]float32, outC*inT)
	if err := conv1DParallel(want, x, w, b, inC, inT, outC, kernel, 1, 1, 1, 1, inT); err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, got, want)
}

func TestConv1DSIMDFusedDilatedInterior(t *testing.T) {
	const inC = 16
	const inT = 16
	const outC = 2
	const kernel = 3
	const dilation = 3
	padding := paddingForKernel(kernel, dilation)
	x := make([]float32, inC*inT)
	for c := 0; c < inC; c++ {
		for tt := 0; tt < inT; tt++ {
			x[c*inT+tt] = float32((c+tt)%11) * 0.03
		}
	}
	w := make([]float32, outC*inC*kernel)
	for i := range w {
		w[i] = float32((i%9)-4) * 0.015
	}
	b := []float32{0.1, -0.2}
	wT := make([]float32, outC*kernel*inC)
	transposeConv1DWeight(wT, w, inC, outC, kernel)
	got := make([]float32, outC*inT)
	if err := conv1DSIMDTransposedWeight(got, x, wT, b, nil, inC, inT, outC, kernel, 1, padding, dilation, inT); err != nil {
		t.Fatal(err)
	}
	want := make([]float32, outC*inT)
	if err := conv1DParallel(want, x, w, b, inC, inT, outC, kernel, 1, padding, dilation, 1, inT); err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, got, want)
}

func TestConvTranspose1D(t *testing.T) {
	out := make([]float32, 4)
	err := ConvTranspose1D(out, []float32{1, 2}, []float32{1, 2}, nil, 1, 2, 1, 2, 2, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{1, 2, 2, 4})
}

func TestWeightNormConv1DWeight(t *testing.T) {
	out := make([]float32, 4)
	err := WeightNormConv1DWeight(out, []float32{3, 4, 0, 5}, []float32{10, 2}, 2, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{6, 8, 0, 2})
}

func TestLSTMLayerShapeAndZeroWeights(t *testing.T) {
	w := LSTMWeights{
		WeightIH: make([]float32, 4),
		WeightHH: make([]float32, 4),
		BiasIH:   make([]float32, 4),
		BiasHH:   make([]float32, 4),
		InputDim: 1,
		Hidden:   1,
	}
	out := make([]float32, 3)
	if err := LSTMLayer(out, []float32{1, 2, 3}, 3, w, false); err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{0, 0, 0})
}

func TestBiLSTMLayerShape(t *testing.T) {
	base := LSTMWeights{WeightIH: make([]float32, 4), WeightHH: make([]float32, 4), BiasIH: make([]float32, 4), BiasHH: make([]float32, 4), InputDim: 1, Hidden: 1}
	out := make([]float32, 4)
	if err := BiLSTMLayer(out, []float32{1, 2}, 2, BiLSTMWeights{Forward: base, Reverse: base}); err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, out, []float32{0, 0, 0, 0})
}

func TestLSTMInputGEMMMatchesStepDots(t *testing.T) {
	saved := kokoroLSTMGEMM
	t.Cleanup(func() { kokoroLSTMGEMM = saved })
	// Large enough that the gate rows run in parallel, both directions.
	const steps, inDim, hidden = 4, 400, 128
	rows := 4 * hidden
	w := LSTMWeights{
		WeightIH: make([]float32, rows*inDim),
		WeightHH: make([]float32, rows*hidden),
		BiasIH:   make([]float32, rows),
		BiasHH:   make([]float32, rows),
		InputDim: inDim,
		Hidden:   hidden,
	}
	x := make([]float32, steps*inDim)
	fillSmall := func(dst []float32, salt int) {
		for i := range dst {
			dst[i] = float32(((i+salt)%11)-5) * 0.02
		}
	}
	fillSmall(w.WeightIH, 1)
	fillSmall(w.WeightHH, 3)
	fillSmall(w.BiasIH, 5)
	fillSmall(w.BiasHH, 7)
	fillSmall(x, 9)
	run := func(reverse bool) []float32 {
		out := make([]float32, steps*hidden)
		if err := LSTMLayer(out, x, steps, w, reverse); err != nil {
			t.Fatal(err)
		}
		return out
	}
	kokoroLSTMGEMM = false
	fwdDots := run(false)
	revDots := run(true)
	kokoroLSTMGEMM = true
	fwdGEMM := run(false)
	revGEMM := run(true)
	lstmOutputsClose(t, fwdGEMM, fwdDots)
	lstmOutputsClose(t, revGEMM, revDots)

	w.BiasIH, w.BiasHH = nil, nil
	kokoroLSTMGEMM = false
	fwdDots = run(false)
	kokoroLSTMGEMM = true
	fwdGEMM = run(false)
	lstmOutputsClose(t, fwdGEMM, fwdDots)
}

func TestLSTMHH4RowMatchesDot32(t *testing.T) {
	saved := kokoroLSTMGEMM
	t.Cleanup(func() { kokoroLSTMGEMM = saved })
	kokoroLSTMGEMM = true
	if !useKokoroLSTMHH4() {
		t.Skip("4-row recurrent kernel is not selected")
	}
	check := func(hidden, g0, g1 int) {
		t.Helper()
		rows := 4 * hidden
		hh := make([]float32, rows*hidden)
		h := make([]float32, hidden)
		for i := range hh {
			hh[i] = float32((i%13)-6) * 0.03
		}
		for i := range h {
			h[i] = float32((i%9)-4) * 0.05
		}
		got := make([]float32, rows)
		want := make([]float32, rows)
		lstmHHDotRange(got, h, hh, hidden, g0, g1)
		for g := g0; g < g1; g++ {
			want[g] += dot32(h, hh[g*hidden:(g+1)*hidden])
		}
		lstmOutputsClose(t, got, want)
	}
	check(256, 0, 4*256)
	check(128, 3, 4*128-1)
	check(17, 1, 4*17-2)
	check(8, 0, 32)
}

func lstmOutputsClose(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len %d vs %d", len(got), len(want))
	}
	var maxd, acc float32
	for i := range want {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
		acc += want[i] * want[i]
	}
	rms := float32(math.Sqrt(float64(acc / float32(len(want)))))
	if maxd > 5e-3 && maxd > 0.05*rms {
		t.Fatalf("lstm gemm maxAbs=%g rms=%g", maxd, rms)
	}
}

func TestISTFTRoundTripSmoke(t *testing.T) {
	x := make([]float32, 80)
	for i := range x {
		x[i] = float32(math.Sin(2 * math.Pi * float64(i) / 20))
	}
	mag, phase, frames := stftMagnitudePhase(x, 20, 5)
	y := istft(mag, phase, frames, 20, 5)
	if len(y) == 0 || frames == 0 {
		t.Fatalf("empty istft output")
	}
	for i, v := range y {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("non-finite istft sample %d: %v", i, v)
		}
	}
}
