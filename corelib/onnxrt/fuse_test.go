package onnxrt

import (
	"math"
	"testing"
)

// f32Init builds a float32 initializer TensorProto.
func f32Init(name string, dims []int64, data []float32) *TensorProto {
	return &TensorProto{Name: name, Dims: dims, DataType: TypeFloat, FloatData: data}
}

// i64Init builds an int64 initializer TensorProto.
func i64Init(name string, dims []int64, data []int64) *TensorProto {
	return &TensorProto{Name: name, Dims: dims, DataType: TypeInt64, Int64Data: data}
}

// runModel builds a graph and runs it with a single float input named "x".
func runModel(t *testing.T, m *Model, x *Tensor) map[string]*Tensor {
	t.Helper()
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	outs, err := g.Run(map[string]*Tensor{"x": x})
	if err != nil {
		t.Fatal(err)
	}
	return outs
}

func wantF32(t *testing.T, got *Tensor, want []float32) {
	t.Helper()
	if got == nil {
		t.Fatal("missing output tensor")
	}
	if len(got.F32) != len(want) {
		t.Fatalf("len %d, want %d (shape %v)", len(got.F32), len(want), got.Shape)
	}
	for i, v := range want {
		d := got.F32[i] - v
		if d > 1e-5 || d < -1e-5 {
			t.Fatalf("out[%d] = %v, want %v (full %v)", i, got.F32[i], v, got.F32)
		}
	}
}

func conv1x1Node(name, in, out string) *Node {
	return &Node{Name: name, OpType: "Conv", Inputs: []string{in, "w"}, Outputs: []string{out}, Attrs: map[string]Attr{}}
}

func in4D(name string, n, c, h, w int64) ValueInfo {
	return ValueInfo{Name: name, ElemType: TypeFloat, Shape: []Dim{{Value: n}, {Value: c}, {Value: h}, {Value: w}}}
}

// TestEpilogueSkipsGraphOutput: Conv -> Relu where the conv output is also a
// graph output. The epilogue fusion must not fire, or the raw conv output
// would no longer be produced.
func TestEpilogueSkipsGraphOutput(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			conv1x1Node("conv", "x", "conv_out"),
			{Name: "relu", OpType: "Relu", Inputs: []string{"conv_out"}, Outputs: []string{"y"}, Attrs: map[string]Attr{}},
		},
		Initializers: map[string]*TensorProto{"w": f32Init("w", []int64{1, 1, 1, 1}, []float32{2})},
		Inputs:       []ValueInfo{in4D("x", 1, 1, 2, 2)},
		Outputs:      []ValueInfo{{Name: "conv_out"}, {Name: "y"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.epilogues) != 0 {
		t.Fatalf("epilogue fused onto graph-output conv")
	}
	outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{-1, 2, 3, -4}, 1, 1, 2, 2)})
	if err != nil {
		t.Fatal(err)
	}
	wantF32(t, outs["conv_out"], []float32{-2, 4, 6, -8})
	wantF32(t, outs["y"], []float32{0, 4, 6, 0})
}

// TestEpilogueStillFusesWhenSafe: same pattern but the conv output is
// internal; the Relu epilogue must still fuse (guard is not over-broad).
func TestEpilogueStillFusesWhenSafe(t *testing.T) {
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
	if len(g.epilogues) != 1 {
		t.Fatalf("expected fused epilogue, got %d", len(g.epilogues))
	}
	outs, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{-1, 2, 3, -4}, 1, 1, 2, 2)})
	if err != nil {
		t.Fatal(err)
	}
	wantF32(t, outs["y"], []float32{0, 4, 6, 0})
}

// TestBatchNormFoldSkipsGraphOutput: Conv -> BN where the conv output is a
// graph output; folding BN into the conv would lose that output (and export
// post-BN values under its name).
func TestBatchNormFoldSkipsGraphOutput(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			conv1x1Node("conv", "x", "c"),
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"c", "scale", "bias", "mean", "var"},
				Outputs: []string{"y"}, Attrs: map[string]Attr{}},
		},
		Initializers: map[string]*TensorProto{
			"w":     f32Init("w", []int64{1, 1, 1, 1}, []float32{2}),
			"scale": f32Init("scale", []int64{1}, []float32{2}),
			"bias":  f32Init("bias", []int64{1}, []float32{1}),
			"mean":  f32Init("mean", []int64{1}, []float32{0.5}),
			"var":   f32Init("var", []int64{1}, []float32{4}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 2)},
		Outputs: []ValueInfo{{Name: "c"}, {Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{1, 3}, 1, 1, 1, 2))
	wantF32(t, outs["c"], []float32{2, 6})
	// eps defaults to 1e-5: y = (c-0.5)*2/sqrt(4+1e-5)+1
	for i, cv := range []float64{2, 6} {
		want := (cv-0.5)*2.0/math.Sqrt(4.0+1e-5) + 1.0
		got := float64(outs["y"].F32[i])
		if d := got - want; d > 1e-4 || d < -1e-4 {
			t.Fatalf("y[%d] = %v, want %v", i, got, want)
		}
	}
}

// TestPreConvNormFoldSkipsGraphOutput: Mul(x, k) feeds a Conv but the Mul
// output is a graph output; the chain fold must not remove it.
func TestPreConvNormFoldSkipsGraphOutput(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "mul", OpType: "Mul", Inputs: []string{"x", "k"}, Outputs: []string{"m"}, Attrs: map[string]Attr{}},
			conv1x1Node("conv", "m", "y"),
		},
		Initializers: map[string]*TensorProto{
			"w": f32Init("w", []int64{1, 1, 1, 1}, []float32{2}),
			"k": f32Init("k", []int64{1}, []float32{0.5}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 2)},
		Outputs: []ValueInfo{{Name: "m"}, {Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{2, 8}, 1, 1, 1, 2))
	wantF32(t, outs["m"], []float32{1, 4})
	wantF32(t, outs["y"], []float32{2, 8})
}

// TestConvBiasFoldSkipsGraphOutput: Conv -> Add(scalar) where the conv output
// is a graph output; the bias fold must not fire.
func TestConvBiasFoldSkipsGraphOutput(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			conv1x1Node("conv", "x", "c"),
			{Name: "add", OpType: "Add", Inputs: []string{"c", "k"}, Outputs: []string{"y"}, Attrs: map[string]Attr{}},
		},
		Initializers: map[string]*TensorProto{
			"w": f32Init("w", []int64{1, 1, 1, 1}, []float32{2}),
			"k": f32Init("k", []int64{1}, []float32{10}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 2)},
		Outputs: []ValueInfo{{Name: "c"}, {Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{1, 3}, 1, 1, 1, 2))
	wantF32(t, outs["c"], []float32{2, 6})
	wantF32(t, outs["y"], []float32{12, 16})
}

// TestConvBiasFoldRequiresChannelVector: an Add constant shaped [1,1,M]
// broadcasts over the W axis of a 4D conv output, not over channels — it
// must NOT be folded into the conv bias.
func TestConvBiasFoldRequiresChannelVector(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			conv1x1Node("conv", "x", "c"),
			{Name: "add", OpType: "Add", Inputs: []string{"c", "k"}, Outputs: []string{"y"}, Attrs: map[string]Attr{}},
		},
		Initializers: map[string]*TensorProto{
			"w": f32Init("w", []int64{2, 1, 1, 1}, []float32{1, 1}),
			"k": f32Init("k", []int64{1, 1, 2}, []float32{10, 20}), // per-W, M==oW==2
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 2, 2)},
		Outputs: []ValueInfo{{Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{1, 2, 3, 4}, 1, 1, 2, 2))
	// Both output channels equal x; adding k over W gives {11,22,13,24} per
	// channel. A per-channel fold would have produced {11,12,13,14} and
	// {21,22,23,24}.
	wantF32(t, outs["y"], []float32{11, 22, 13, 24, 11, 22, 13, 24})
}

// TestFoldConstantsDoesNotAlias: a constant-folded Reshape of a conv weight
// must not share storage with the weight initializer — BN folding mutates
// the weight in place afterwards.
func TestFoldConstantsDoesNotAlias(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			{Name: "reshape", OpType: "Reshape", Inputs: []string{"w", "sh"}, Outputs: []string{"wflat"}, Attrs: map[string]Attr{}},
			conv1x1Node("conv", "x", "c"),
			{Name: "bn", OpType: "BatchNormalization",
				Inputs:  []string{"c", "scale", "bias", "mean", "var"},
				Outputs: []string{"y"},
				Attrs:   map[string]Attr{"epsilon": {Type: attrTypeFloat, F: 0}}},
		},
		Initializers: map[string]*TensorProto{
			"w":     f32Init("w", []int64{1, 1, 1, 1}, []float32{3}),
			"sh":    i64Init("sh", []int64{1}, []int64{1}),
			"scale": f32Init("scale", []int64{1}, []float32{4}), // f = 4/sqrt(4) = 2
			"bias":  f32Init("bias", []int64{1}, []float32{0}),
			"mean":  f32Init("mean", []int64{1}, []float32{0}),
			"var":   f32Init("var", []int64{1}, []float32{4}),
		},
		Inputs:  []ValueInfo{in4D("x", 1, 1, 1, 1)},
		Outputs: []ValueInfo{{Name: "wflat"}, {Name: "y"}},
	}}
	outs := runModel(t, m, FloatFrom([]float32{2}, 1, 1, 1, 1))
	// wflat must still hold the ORIGINAL weight; if it aliased w it would
	// read 6 after BN folding scaled w by 2.
	wantF32(t, outs["wflat"], []float32{3})
	wantF32(t, outs["y"], []float32{12}) // x * (3*2) = 12
}
