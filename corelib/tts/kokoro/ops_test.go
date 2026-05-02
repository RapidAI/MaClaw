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
	if err := conv1DSIMDTransposedWeight(got, x, wT, b, inC, inT, outC, kernel, 1, 1, 1, inT); err != nil {
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
	if err := conv1DSIMDTransposedWeight(got, x, wT, b, inC, inT, outC, kernel, 1, padding, dilation, inT); err != nil {
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
