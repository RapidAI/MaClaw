package onnxrt

import (
	"math"
	"path/filepath"
	"sync"
	"testing"
)

// TestGraphRealModelsShape loads the real OCR models (skipping when they are
// not downloaded) and checks output shapes and finiteness on zero input.
func TestGraphRealModelsShape(t *testing.T) {
	cases := []struct {
		name     string
		model    string
		inShape  []int
		wantRank int
		wantLast int // last-dim size, -1 = don't check
	}{
		{"det", filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_det.onnx"), []int{1, 3, 64, 64}, 4, 64},
		{"rec", filepath.Join("..", "..", ".tmp", "ocr-models", "ppocrv6_small_rec.onnx"), []int{1, 3, 48, 320}, 3, 18710},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := LoadGraph(c.model)
			if err != nil {
				t.Skipf("model unavailable: %v", err)
			}
			in := NewFloat(c.inShape...)
			outs, err := g.Run(map[string]*Tensor{g.InputNames()[0]: in})
			if err != nil {
				t.Fatal(err)
			}
			if len(outs) != 1 {
				t.Fatalf("expected 1 output, got %d", len(outs))
			}
			out := outs[g.OutputNames()[0]]
			if out.Rank() != c.wantRank {
				t.Fatalf("output rank %d, want %d (shape %v)", out.Rank(), c.wantRank, out.Shape)
			}
			if c.wantLast > 0 && out.Shape[c.wantRank-1] != c.wantLast {
				t.Fatalf("output last dim %d, want %d (shape %v)", out.Shape[c.wantRank-1], c.wantLast, out.Shape)
			}
			for i, v := range out.F32 {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					t.Fatalf("non-finite output at %d: %v (shape %v)", i, v, out.Shape)
				}
			}
			t.Logf("output shape %v", out.Shape)
		})
	}
}

// TestGraphRunConcurrent verifies Graph.Run is safe for concurrent use: each
// goroutine runs the same graph with its own input tensor (run with -race).
func TestGraphRunConcurrent(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			conv1x1Node("conv", "x", "c"),
			{Name: "relu", OpType: "Relu", Inputs: []string{"c"}, Outputs: []string{"y"}, Attrs: map[string]Attr{}},
		},
		Initializers: map[string]*TensorProto{"w": f32Init("w", []int64{1, 1, 1, 1}, []float32{2})},
		Inputs:       []ValueInfo{in4D("x", 1, 1, 2, 2)},
		Outputs:      []ValueInfo{{Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(base float32) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				v := base + float32(i)
				outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{v, -v, 1, -1}, 1, 1, 2, 2)})
				if err != nil {
					t.Error(err)
					return
				}
				y := outs["y"].F32
				want := []float32{2 * v, 0, 2, 0}
				if y[0] != want[0] || y[1] != want[1] || y[2] != want[2] || y[3] != want[3] {
					t.Errorf("got %v, want %v", y, want)
					return
				}
			}
		}(float32(worker))
	}
	wg.Wait()
}

// TestGraphInputValidation checks input name/shape validation.
func TestGraphInputValidation(t *testing.T) {
	model := filepath.Join("testdata", "conv_basic", "model.onnx")
	if _, err := LoadGraph(model); err != nil {
		t.Skip("testdata not generated")
	}
	g, err := LoadGraph(model)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(map[string]*Tensor{}); err == nil {
		t.Fatal("expected error for missing input")
	}
	if _, err := g.Run(map[string]*Tensor{"x": NewFloat(1, 2, 5)}); err == nil {
		t.Fatal("expected error for wrong rank")
	}
	if _, err := g.Run(map[string]*Tensor{"x": NewFloat(1, 9, 5, 5)}); err == nil {
		t.Fatal("expected error for wrong static dim")
	}
}
