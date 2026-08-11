package onnxrt

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ctcTestModel builds MatMul -> Add(bias) -> Softmax(axis=-1) -> y over a
// [1, T, K] activation, the exact PP-OCR rec head shape.
func ctcTestModel(T, K, N int, w, b []float32) *Model {
	return &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "mm", OpType: "MatMul", Inputs: []string{"x", "w"}, Outputs: []string{"mmout"}, Attrs: map[string]Attr{}},
			{Name: "id", OpType: "Identity", Inputs: []string{"mmout"}, Outputs: []string{"idout"}, Attrs: map[string]Attr{}},
			{Name: "add", OpType: "Add", Inputs: []string{"idout", "b"}, Outputs: []string{"addout"}, Attrs: map[string]Attr{}},
			{Name: "sm", OpType: "Softmax", Inputs: []string{"addout"}, Outputs: []string{"y"},
				Attrs: map[string]Attr{"axis": {Name: "axis", Type: 2, I: -1}}},
		},
		Initializers: map[string]*TensorProto{
			"w": f32Init("w", []int64{int64(K), int64(N)}, w),
			"b": f32Init("b", []int64{int64(N)}, b),
		},
		Inputs:  []ValueInfo{{Name: "x", ElemType: TypeFloat, Shape: []Dim{{Value: 1}, {Value: int64(T)}, {Value: int64(K)}}}},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
}

// refArgmaxProb computes per-row argmax id and max probability from a
// materialized [T*N] probability tensor, mirroring the consumer's decode.
func refArgmaxProb(probs []float32, T, N int) (ids []int, maxP []float32) {
	ids = make([]int, T)
	maxP = make([]float32, T)
	for t := 0; t < T; t++ {
		row := probs[t*N : (t+1)*N]
		best, bestP := 0, float32(-1)
		for i, p := range row {
			if p > bestP {
				best, bestP = i, p
			}
		}
		ids[t], maxP[t] = best, bestP
	}
	return ids, maxP
}

func checkCTCEquivalence(t *testing.T, g *Graph, x *Tensor, T, N int, tol float64) {
	t.Helper()
	outs, err := g.Run(map[string]*Tensor{"x": x})
	if err != nil {
		t.Fatal(err)
	}
	probsT := outs[g.OutputNames()[0]]
	wantIDs, wantP := refArgmaxProb(probsT.F32, T, N)

	ids, probs, vocab, err := g.RunCTC(map[string]*Tensor{"x": x})
	if err != nil {
		t.Fatal(err)
	}
	if vocab != N {
		t.Fatalf("vocab %d, want %d", vocab, N)
	}
	if len(ids) != T || len(probs) != T {
		t.Fatalf("ids/probs len %d/%d, want %d", len(ids), len(probs), T)
	}
	maxDiff := 0.0
	for tt := 0; tt < T; tt++ {
		if ids[tt] != wantIDs[tt] {
			t.Fatalf("frame %d: id %d, want %d", tt, ids[tt], wantIDs[tt])
		}
		d := math.Abs(float64(probs[tt] - wantP[tt]))
		if d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff > tol {
		t.Fatalf("max prob diff %g exceeds tol %g", maxDiff, tol)
	}
	t.Logf("max prob diff %g (tol %g)", maxDiff, tol)
}

func TestCTCHeadSynthetic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const T, K, N = 7, 5, 13
	w := make([]float32, K*N)
	b := make([]float32, N)
	xf := make([]float32, T*K)
	for i := range w {
		w[i] = rng.Float32()*2 - 1
	}
	for i := range b {
		b[i] = rng.Float32()*2 - 1
	}
	for i := range xf {
		xf[i] = rng.Float32()*4 - 2
	}
	g, err := NewGraph(ctcTestModel(T, K, N, w, b))
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasCTCHead() {
		t.Fatal("CTC head not detected on synthetic MatMul->Add->Softmax graph")
	}
	checkCTCEquivalence(t, g, FloatFrom(xf, 1, T, K), T, N, 1e-6)
}

func TestCTCHeadSyntheticBatch(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	const B, T, K, N = 3, 5, 7, 11
	w := make([]float32, K*N)
	bias := make([]float32, N)
	xf := make([]float32, B*T*K)
	for i := range w {
		w[i] = rng.Float32()*2 - 1
	}
	for i := range bias {
		bias[i] = rng.Float32()*2 - 1
	}
	for i := range xf {
		xf[i] = rng.Float32()*4 - 2
	}
	m := ctcTestModel(T, K, N, w, bias)
	m.Graph.Inputs[0].Shape[0].Value = B
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasCTCHead() {
		t.Fatal("CTC head not detected for batched synthetic graph")
	}
	checkCTCEquivalence(t, g, FloatFrom(xf, B, T, K), B*T, N, 1e-6)
}
func TestCTCHeadSyntheticNoBias(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const T, K, N = 4, 3, 9
	w := make([]float32, K*N)
	xf := make([]float32, T*K)
	for i := range w {
		w[i] = rng.Float32()*2 - 1
	}
	for i := range xf {
		xf[i] = rng.Float32() * 4
	}
	m := &Model{Opset: 11, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "mm", OpType: "MatMul", Inputs: []string{"x", "w"}, Outputs: []string{"mmout"}, Attrs: map[string]Attr{}},
			{Name: "sm", OpType: "Softmax", Inputs: []string{"mmout"}, Outputs: []string{"y"},
				Attrs: map[string]Attr{"axis": {Name: "axis", Type: 2, I: 2}}},
		},
		Initializers: map[string]*TensorProto{"w": f32Init("w", []int64{K, N}, w)},
		Inputs:       []ValueInfo{{Name: "x", ElemType: TypeFloat, Shape: []Dim{{Value: 1}, {Value: T}, {Value: K}}}},
		Outputs:      []ValueInfo{{Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasCTCHead() {
		t.Fatal("CTC head not detected on bias-free MatMul->Softmax graph")
	}
	checkCTCEquivalence(t, g, FloatFrom(xf, 1, T, K), T, N, 1e-6)
}

// TestCTCHeadNegative: patterns that must NOT be detected as a fusable head.
func TestCTCHeadNegative(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const K, N = 3, 5
	w := make([]float32, K*N)
	for i := range w {
		w[i] = rng.Float32()
	}
	mk := func(nodes []*Node, outputs []ValueInfo) *Model {
		return &Model{Opset: 13, Graph: &GraphProto{
			Nodes:        nodes,
			Initializers: map[string]*TensorProto{"w": f32Init("w", []int64{K, N}, w)},
			Inputs:       []ValueInfo{{Name: "x", ElemType: TypeFloat, Shape: []Dim{{Value: 1}, {Value: 2}, {Value: K}}}},
			Outputs:      outputs,
		}}
	}
	// MatMul output consumed by both the Softmax and a Relu: fusing would
	// break the other consumer, so it must not be detected.
	multi := mk([]*Node{
		{Name: "mm", OpType: "MatMul", Inputs: []string{"x", "w"}, Outputs: []string{"mmout"}, Attrs: map[string]Attr{}},
		{Name: "relu", OpType: "Relu", Inputs: []string{"mmout"}, Outputs: []string{"r"}, Attrs: map[string]Attr{}},
		{Name: "sm", OpType: "Softmax", Inputs: []string{"mmout"}, Outputs: []string{"y"},
			Attrs: map[string]Attr{"axis": {Name: "axis", Type: 2, I: -1}}},
	}, []ValueInfo{{Name: "y"}, {Name: "r"}})
	// Plain MatMul output, no softmax.
	plain := mk([]*Node{
		{Name: "mm", OpType: "MatMul", Inputs: []string{"x", "w"}, Outputs: []string{"y"}, Attrs: map[string]Attr{}},
	}, []ValueInfo{{Name: "y"}})
	for name, m := range map[string]*Model{"multi-consumer": multi, "no-softmax": plain} {
		g, err := NewGraph(m)
		if err != nil {
			t.Fatal(err)
		}
		if g.HasCTCHead() {
			t.Fatalf("%s: head must not be detected", name)
		}
		if _, _, _, err := g.RunCTC(map[string]*Tensor{"x": NewFloat(1, 2, K)}); err == nil {
			t.Fatalf("%s: RunCTC must fail without a head", name)
		}
	}
}

// TestCTCHeadRealRec checks the fused decode against the full-run decode on
// the real rec model with a deterministic pseudo-random input.
func TestCTCHeadRealRec(t *testing.T) {
	model := filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx")
	g, err := LoadGraph(model)
	if err != nil {
		t.Skipf("model unavailable: %v", err)
	}
	if !g.HasCTCHead() {
		t.Fatal("CTC head not detected on ppocrv6_small_rec")
	}
	rng := rand.New(rand.NewSource(3))
	shape := []int{1, 3, 48, 320}
	n := 1
	for _, d := range shape {
		n *= d
	}
	xf := make([]float32, n)
	for i := range xf {
		xf[i] = rng.Float32()*2 - 1
	}
	T := 40 // 320 / 8 downsampling
	checkCTCEquivalence(t, g, FloatFrom(xf, shape...), T, 18710, 1e-5)
}

// TestCTCHeadRealRecGolden compares the fused decode against per-row
// argmax/max-prob derived from the onnxruntime golden output.
func TestCTCHeadRealRecGolden(t *testing.T) {
	dir := filepath.Join("testdata", "real_rec")
	if _, err := os.Stat(filepath.Join(dir, "outputs.json")); err != nil {
		t.Skip("golden not generated")
	}
	model := filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx")
	g, err := LoadGraph(model)
	if err != nil {
		t.Skipf("model unavailable: %v", err)
	}
	if !g.HasCTCHead() {
		t.Fatal("CTC head not detected on ppocrv6_small_rec")
	}
	in := readGolden[goldenInputs](t, filepath.Join(dir, "inputs.json"))
	want := readGolden[goldenOutputs](t, filepath.Join(dir, "outputs.json"))
	inputs := map[string]*Tensor{}
	for name, gt := range in.Inputs {
		inputs[name] = gt.tensor(t)
	}
	ids, probs, vocab, err := g.RunCTC(inputs)
	if err != nil {
		t.Fatal(err)
	}
	golden := want.Outputs[g.OutputNames()[0]].tensor(t)
	T := golden.NumElements() / vocab
	wantIDs, wantP := refArgmaxProb(golden.F32, T, vocab)
	if len(ids) != T {
		t.Fatalf("ids len %d, golden T %d", len(ids), T)
	}
	maxDiff := 0.0
	for tt := 0; tt < T; tt++ {
		if ids[tt] != wantIDs[tt] {
			t.Fatalf("frame %d: id %d, golden argmax %d", tt, ids[tt], wantIDs[tt])
		}
		if d := math.Abs(float64(probs[tt] - wantP[tt])); d > maxDiff {
			maxDiff = d
		}
	}
	t.Logf("fused vs golden: max prob diff %g over %d frames", maxDiff, T)
	if maxDiff > 1e-4 {
		t.Fatalf("max prob diff %g exceeds 1e-4", maxDiff)
	}
}

func benchmarkRecRun(b *testing.B, width int, fused bool) {
	model := filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx")
	g, err := LoadGraph(model)
	if err != nil {
		b.Skipf("model unavailable: %v", err)
	}
	x := NewFloat(1, 3, 48, width)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if fused {
			if _, _, _, err := g.RunCTC(map[string]*Tensor{"x": x}); err != nil {
				b.Fatal(err)
			}
		} else {
			if _, err := g.Run(map[string]*Tensor{"x": x}); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkRecClassic320(b *testing.B)  { benchmarkRecRun(b, 320, false) }
func BenchmarkRecFused320(b *testing.B)    { benchmarkRecRun(b, 320, true) }
func BenchmarkRecClassic1280(b *testing.B) { benchmarkRecRun(b, 1280, false) }
func BenchmarkRecFused1280(b *testing.B)   { benchmarkRecRun(b, 1280, true) }
func BenchmarkRecClassic3200(b *testing.B) { benchmarkRecRun(b, 3200, false) }
func BenchmarkRecFused3200(b *testing.B)   { benchmarkRecRun(b, 3200, true) }

// BenchmarkRecHeadAB interleaves classic Run and fused RunCTC in one process
// and reports the MINIMUM time of each path, which stays meaningful when the
// host is under unrelated load (means would just track the noise).
func BenchmarkRecHeadAB(b *testing.B) {
	model := filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx")
	g, err := LoadGraph(model)
	if err != nil {
		b.Skipf("model unavailable: %v", err)
	}
	for _, width := range []int{320, 1280, 3200} {
		b.Run(fmt.Sprintf("w%d", width), func(b *testing.B) {
			x := NewFloat(1, 3, 48, width)
			in := map[string]*Tensor{"x": x}
			// Warmup (also verifies both paths work).
			if _, err := g.Run(in); err != nil {
				b.Fatal(err)
			}
			if _, _, _, err := g.RunCTC(in); err != nil {
				b.Fatal(err)
			}
			var minC, minF time.Duration = math.MaxInt64, math.MaxInt64
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				outs, err := g.Run(in)
				if err != nil {
					b.Fatal(err)
				}
				// Include the consumer-side greedy argmax scan the classic
				// path needs (ocr.ctcGreedyDecode over [T,N] probabilities).
				p := outs[g.OutputNames()[0]].F32
				best, bestP := 0, float32(-1)
				for j, v := range p {
					if v > bestP {
						best, bestP = j, v
					}
				}
				_ = best
				_ = bestP
				if d := time.Since(t0); d < minC {
					minC = d
				}
				t0 = time.Now()
				if _, _, _, err := g.RunCTC(in); err != nil {
					b.Fatal(err)
				}
				if d := time.Since(t0); d < minF {
					minF = d
				}
			}
			b.ReportMetric(float64(minC), "classic-ns")
			b.ReportMetric(float64(minF), "fused-ns")
			b.ReportMetric(100*(1-float64(minF)/float64(minC)), "win-%")
		})
	}
}

// BenchmarkCTCHeadPPOCR records the exact CTC projection geometry used by
// PP-OCRv6 small (120 features, 18,710 classes).  The sub-benchmarks vary the
// temporary logit tile width; ctcHeadKernel otherwise follows the production
// path, including fused bias, argmax and softmax-denominator accumulation.
func BenchmarkCTCHeadPPOCR(b *testing.B) {
	const (
		frames = 48
		vocab  = 18710
		width  = 120
	)
	a := make([]float32, frames*width)
	w := make([]float32, vocab*width)
	bias := make([]float32, vocab)
	for i := range a {
		a[i] = float32((i%31)-15) * 0.03125
	}
	for i := range w {
		w[i] = float32((i%29)-14) * 0.015625
	}
	for i := range bias {
		bias[i] = float32((i%17)-8) * 0.0078125
	}
	old := ctcChunkCols
	b.Cleanup(func() { ctcChunkCols = old })
	for _, chunk := range []int{192, 256, 320, 384, 448, 512, 640, 768, 1024, 2048} {
		b.Run(fmt.Sprintf("chunk-%d", chunk), func(b *testing.B) {
			ctcChunkCols = chunk
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ctcHeadKernel(a, w, bias, frames, vocab, width)
			}
		})
	}
}
