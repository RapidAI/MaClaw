package onnxrt

import (
	"reflect"
	"testing"
)

// TestBroadcastShapes checks NumPy broadcast rules.
func TestBroadcastShapes(t *testing.T) {
	cases := []struct {
		a, b, want []int
	}{
		{[]int{2, 3}, []int{3}, []int{2, 3}},
		{[]int{2, 1}, []int{1, 3}, []int{2, 3}},
		{[]int{5, 1, 4, 1}, []int{1, 3}, []int{5, 1, 4, 3}},
		{[]int{1}, []int{2, 2}, []int{2, 2}},
	}
	for _, c := range cases {
		got, err := broadcastShapes(c.a, c.b)
		if err != nil {
			t.Fatalf("broadcastShapes(%v, %v): %v", c.a, c.b, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("broadcastShapes(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if _, err := broadcastShapes([]int{2, 3}, []int{4}); err == nil {
		t.Fatal("expected error for incompatible shapes")
	}
}

// TestBinaryBroadcastHand computes [2,2] + [2] by hand.
func TestBinaryBroadcastHand(t *testing.T) {
	a := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
	b := FloatFrom([]float32{10, 20}, 2)
	out, err := binaryOpTensor(nil, nil, a, b, bAdd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.F32, []float32{11, 22, 13, 24}) {
		t.Fatalf("got %v", out.F32)
	}
}

// TestConvHand computes a tiny 1x1x3x3 conv with 2x2 kernel by hand.
func TestConvHand(t *testing.T) {
	// x = [1..9], w = [[1,0],[0,1]] -> y[i][j] = x[i][j] + x[i+1][j+1]
	x := FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8, 9}, 1, 1, 3, 3)
	w := FloatFrom([]float32{1, 0, 0, 1}, 1, 1, 2, 2)
	n := &Node{OpType: "Conv", Attrs: map[string]Attr{}}
	outs, err := opConv(nil, n, []*Tensor{x, w})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].Shape, []int{1, 1, 2, 2}) {
		t.Fatalf("shape %v", outs[0].Shape)
	}
	if !reflect.DeepEqual(outs[0].F32, []float32{6, 8, 12, 14}) {
		t.Fatalf("got %v", outs[0].F32)
	}
}

// TestReshapeInfer checks -1 inference and 0 copy.
func TestReshapeInfer(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	// -1 inference
	outs, err := opReshape(nil, &Node{Attrs: map[string]Attr{}}, []*Tensor{x, IntFrom([]int64{-1, 2}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].Shape, []int{3, 2}) {
		t.Fatalf("shape %v", outs[0].Shape)
	}
	// 0 copies input dim
	outs, err = opReshape(nil, &Node{Attrs: map[string]Attr{}}, []*Tensor{x, IntFrom([]int64{0, -1}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].Shape, []int{2, 3}) {
		t.Fatalf("shape %v", outs[0].Shape)
	}
}

// TestSoftmaxHand checks a two-element softmax.
func TestSoftmaxHand(t *testing.T) {
	x := FloatFrom([]float32{0, 0, 1, 0}, 2, 2)
	outs, err := opSoftmax(nil, &Node{Attrs: map[string]Attr{"axis": {Type: attrTypeInt, I: -1}}}, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.5, 0.5, 1.0 / (1 + 0.36787944117144233), 0.36787944117144233 / (1 + 0.36787944117144233)}
	for i := range want {
		if d := outs[0].F32[i] - want[i]; d > 1e-6 || d < -1e-6 {
			t.Fatalf("softmax %v, want %v", outs[0].F32, want)
		}
	}
}
