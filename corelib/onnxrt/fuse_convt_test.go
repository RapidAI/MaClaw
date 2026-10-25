package onnxrt

import (
	"os"
	"testing"
)

func TestConvTransposeBatchNormReluFold(t *testing.T) {
	// Weight [Cin=1, Cout=2, 1, 1]. Scaling axis 0 would treat both taps as
	// one input channel; the two output channels must pick up different factors.
	m := &Model{Opset: 11, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "ct", OpType: "ConvTranspose", Inputs: []string{"x", "w"}, Outputs: []string{"c"}, Attrs: map[string]Attr{}},
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"c", "scale", "bias", "mean", "var"},
				Outputs: []string{"bn"},
				Attrs:   map[string]Attr{"epsilon": {F: 0}}},
			{Name: "relu", OpType: "Relu", Inputs: []string{"bn"}, Outputs: []string{"y"}},
		},
		Initializers: map[string]*TensorProto{
			"w":     f32Init("w", []int64{1, 2, 1, 1}, []float32{3, 5}),
			"scale": f32Init("scale", []int64{2}, []float32{2, 4}),
			"bias":  f32Init("bias", []int64{2}, []float32{0, 0}),
			"mean":  f32Init("mean", []int64{2}, []float32{0, 0}),
			"var":   f32Init("var", []int64{2}, []float32{1, 1}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 1)},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.nodes {
		if n.OpType == "BatchNormalization" || n.OpType == "Relu" {
			t.Fatalf("node %s survived the fold", n.OpType)
		}
	}
	outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{1}, 1, 1, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	// y = relu((x*w)*scale) = relu(3*2, 5*4) = 6, 20
	wantF32(t, outs["y"], []float32{6, 20})
}

func TestConvTransposeGroupedBatchNormFold(t *testing.T) {
	// group=2, Mg=2. Weight layout [Cin, Mg, 1, 1] is
	// (cin0,m0)=3, (cin0,m1)=5, (cin1,m0)=7, (cin1,m1)=11.
	// Output channels are axis 1, so the factors 2,4,6,8 land on
	// 3*2, 5*4, 7*6, 11*8. Scaling contiguous axis 0 would see M=2
	// and refuse the fold (scale length 4).
	m := &Model{Opset: 11, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "ct", OpType: "ConvTranspose", Inputs: []string{"x", "w"}, Outputs: []string{"c"},
				Attrs: map[string]Attr{"group": {I: 2}}},
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"c", "scale", "bias", "mean", "var"},
				Outputs: []string{"y"},
				Attrs:   map[string]Attr{"epsilon": {F: 0}}},
		},
		Initializers: map[string]*TensorProto{
			"w":     f32Init("w", []int64{2, 2, 1, 1}, []float32{3, 5, 7, 11}),
			"scale": f32Init("scale", []int64{4}, []float32{2, 4, 6, 8}),
			"bias":  f32Init("bias", []int64{4}, []float32{0, 0, 0, 0}),
			"mean":  f32Init("mean", []int64{4}, []float32{0, 0, 0, 0}),
			"var":   f32Init("var", []int64{4}, []float32{1, 1, 1, 1}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 2, 1, 1)},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.nodes {
		if n.OpType == "BatchNormalization" {
			t.Fatal("grouped convtranspose batchnorm was not folded")
		}
	}
	outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{1, 1}, 1, 2, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	wantF32(t, outs["y"], []float32{6, 20, 42, 88})
}

func TestConvTransposeBatchNormRejectKeepsWeights(t *testing.T) {
	m := &Model{Opset: 11, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "ct", OpType: "ConvTranspose", Inputs: []string{"x", "w"}, Outputs: []string{"c"}, Attrs: map[string]Attr{}},
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"c", "scale", "bias", "mean", "var"},
				Outputs: []string{"y", "saved"},
				Attrs:   map[string]Attr{"epsilon": {F: 0}}},
		},
		Initializers: map[string]*TensorProto{
			"w":     f32Init("w", []int64{1, 1, 1, 1}, []float32{2}),
			"scale": f32Init("scale", []int64{1}, []float32{2}),
			"bias":  f32Init("bias", []int64{1}, []float32{0}),
			"mean":  f32Init("mean", []int64{1}, []float32{0}),
			"var":   f32Init("var", []int64{1}, []float32{1}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 1)},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{1}, 1, 1, 1, 1))
	// Extra BN output blocks the fold, so the weight stays 2 and BN still runs:
	// (1*2 - 0) * 2 / 1 = 4. A partial scale would make this 8.
	wantF32(t, outs["y"], []float32{4})
}

func TestStandaloneBatchNormRelu(t *testing.T) {
	m := &Model{Opset: 11, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"x", "scale", "bias", "mean", "var"},
				Outputs: []string{"bn"},
				Attrs:   map[string]Attr{"epsilon": {F: 0}}},
			{Name: "relu", OpType: "Relu", Inputs: []string{"bn"}, Outputs: []string{"y"}},
		},
		Initializers: map[string]*TensorProto{
			"scale": f32Init("scale", []int64{1}, []float32{2}),
			"bias":  f32Init("bias", []int64{1}, []float32{-3}),
			"mean":  f32Init("mean", []int64{1}, []float32{0}),
			"var":   f32Init("var", []int64{1}, []float32{1}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 4)},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.nodes {
		if n.OpType == "Relu" {
			t.Fatal("relu was not fused into batchnorm")
		}
	}
	outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{-1, 1, 2, 4}, 1, 1, 1, 4)})
	if err != nil {
		t.Fatal(err)
	}
	// relu(x*2 - 3) = relu(-5, -1, 1, 5) = 0, 0, 1, 5
	wantF32(t, outs["y"], []float32{0, 0, 1, 5})
}

func TestWiredUNETBNFolded(t *testing.T) {
	path := `C:\Users\ma139\.maclaw\models\table\unet.onnx`
	if _, err := os.Stat(path); err != nil {
		t.Skip(err)
	}
	g, err := LoadGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	prod := map[string]*Node{}
	bn, relu := 0, 0
	for _, n := range g.nodes {
		for _, o := range n.Outputs {
			if o != "" {
				prod[o] = n
			}
		}
	}
	for _, n := range g.nodes {
		switch n.OpType {
		case "BatchNormalization":
			bn++
			p := prod[n.Inputs[0]]
			if p != nil && (p.OpType == "Conv" || p.OpType == "ConvTranspose") {
				t.Fatalf("batchnorm still follows %s", p.OpType)
			}
			e := g.epilogues[n]
			if e == nil || e.kind != EpiRelu {
				t.Fatal("concat batchnorm relu was not fused")
			}
		case "Relu":
			relu++
		}
	}
	if bn != 3 || relu != 17 {
		t.Fatalf("after fold bn=%d relu=%d, want 3 and 17", bn, relu)
	}
}
