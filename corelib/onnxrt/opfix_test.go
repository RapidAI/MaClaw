package onnxrt

import (
	"math"
	"testing"
)

// TestSliceAxesLengthMismatch: a malformed axes tensor longer than starts
// must produce an error, not an index-out-of-range panic.
func TestSliceAxesLengthMismatch(t *testing.T) {
	data := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
	n := &Node{OpType: "Slice", Attrs: map[string]Attr{}}
	args := []*Tensor{
		data,
		IntFrom([]int64{0}, 1),    // starts
		IntFrom([]int64{1}, 1),    // ends
		IntFrom([]int64{0, 1}, 2), // axes: len 2 > len(starts) 1
	}
	if _, err := opSlice(nil, n, args); err == nil {
		t.Fatal("expected error for axes/starts length mismatch")
	}
	// steps mismatch must also error.
	args[3] = IntFrom([]int64{0}, 1)
	args = append(args, IntFrom([]int64{1, 1}, 2))
	if _, err := opSlice(nil, n, args); err == nil {
		t.Fatal("expected error for steps/starts length mismatch")
	}
}

// TestSoftmaxDefaultAxisOpset: without an explicit axis attribute, opset 13+
// defaults to -1 (last axis) while opset <= 12 defaults to 1 (2D coercion).
func TestSoftmaxDefaultAxisOpset(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8}, 2, 2, 2)
	n := &Node{OpType: "Softmax", Attrs: map[string]Attr{}}

	// opset 13: per-last-dim softmax over pairs.
	g13 := &Graph{opset: 13}
	outs, err := opSoftmax(&runCtx{Graph: g13}, n, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	e1 := float32(1) / (1 + exp32(1)) // softmax([1,2])[0] = 1/(1+e)
	if d := outs[0].F32[0] - e1; d > 1e-6 || d < -1e-6 {
		t.Fatalf("opset13 default axis: got %v, want %v (out %v)", outs[0].F32[0], e1, outs[0].F32)
	}

	// opset 11: coerce to [2,4], softmax per row of 4.
	g11 := &Graph{opset: 11}
	outs, err = opSoftmax(&runCtx{Graph: g11}, n, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	// softmax([1,2,3,4])[0] = e^1/(e+e^2+e^3+e^4) = 1/(1+e+e^2+e^3)
	den := 1 + exp32(1) + exp32(2) + exp32(3)
	want := 1 / den
	if d := outs[0].F32[0] - want; d > 1e-6 || d < -1e-6 {
		t.Fatalf("opset11 default axis: got %v, want %v (out %v)", outs[0].F32[0], want, outs[0].F32)
	}
}

func exp32(v float32) float32 { return float32(math.Exp(float64(v))) }

// TestResizeNearestRoundPreferFloor: asymmetric 2x nearest upsample hits
// exact .5 source coordinates; round_prefer_floor must round them DOWN
// (matches onnxruntime's ceil(v - 0.5)).
func TestResizeNearestRoundPreferFloor(t *testing.T) {
	x := FloatFrom([]float32{10, 20}, 1, 1, 1, 2)
	scales := FloatFrom([]float32{1, 1, 1, 2}, 4)
	n := &Node{OpType: "Resize", Attrs: map[string]Attr{
		"coordinate_transformation_mode": {Type: attrTypeString, S: []byte("asymmetric")},
		// nearest_mode omitted: default is round_prefer_floor
	}}
	outs, err := opResize(nil, n, []*Tensor{x, nil, scales})
	if err != nil {
		t.Fatal(err)
	}
	// v = ow/2 = 0, 0.5, 1.0, 1.5 -> floor-preferred: 0, 0, 1, 1
	wantF32(t, outs[0], []float32{10, 10, 20, 20})
}

// TestMaxPoolDilationsRejected: dilations other than 1 are not implemented;
// they must error instead of being silently ignored.
func TestMaxPoolDilationsRejected(t *testing.T) {
	x := NewFloat(1, 1, 4, 4)
	n := &Node{OpType: "MaxPool", Attrs: map[string]Attr{
		"kernel_shape": {Type: attrTypeInts, IntVals: []int64{2, 2}},
		"dilations":    {Type: attrTypeInts, IntVals: []int64{2, 2}},
	}}
	if _, err := opMaxPool(nil, n, []*Tensor{x}); err == nil {
		t.Fatal("expected error for dilated MaxPool")
	}
}
