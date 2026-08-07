package onnxrt

import (
	"math"
	"testing"
)

// Regression tests for discrepancies found by the randomized differential
// harness (cmd/genfuzz + testdata/run_golden.py) against onnxruntime.

// TestConvDepthwiseMultiplier: depthwise conv with channel multiplier > 1
// (group == C but M == mult*C) must map output channel m to input channel
// m/mult with its own filter. The depthwise fast paths assume multiplier 1,
// so these configs go through the general path.
func TestConvDepthwiseMultiplier(t *testing.T) {
	// x [1,2,3,3], w [4,1,2,2]: group=2, multiplier 2.
	x := FloatFrom([]float32{
		1, 2, 3, 4, 5, 6, 7, 8, 9, // channel 0
		9, 8, 7, 6, 5, 4, 3, 2, 1, // channel 1
	}, 1, 2, 3, 3)
	w := FloatFrom([]float32{
		1, 0, 0, 0, // out ch 0 <- in ch 0
		0, 1, 0, 0, // out ch 1 <- in ch 0
		0, 0, 1, 0, // out ch 2 <- in ch 1
		0, 0, 0, 1, // out ch 3 <- in ch 1
	}, 4, 1, 2, 2)
	n := &Node{OpType: "Conv", Attrs: map[string]Attr{
		"kernel_shape": {Type: attrTypeInts, IntVals: []int64{2, 2}},
		"group":        {Type: attrTypeInt, I: 2},
	}}
	outs, err := opConv(nil, n, []*Tensor{x, w})
	if err != nil {
		t.Fatal(err)
	}
	wantF32(t, outs[0], []float32{
		1, 2, 4, 5, // ch 0: top-left tap of in ch 0
		2, 3, 5, 6, // ch 1: top-right tap of in ch 0
		6, 5, 3, 2, // ch 2: bottom-left tap of in ch 1
		5, 4, 2, 1, // ch 3: bottom-right tap of in ch 1
	})
}

// TestResizeScalesCoordinateTransform: when scales are given, the coordinate
// transform must use them as-is (output size is floor(in*scale)); deriving
// out/input loses the fractional remainder and shifts every sample.
func TestResizeScalesCoordinateTransform(t *testing.T) {
	x := FloatFrom([]float32{0, 10}, 1, 1, 1, 2)
	scales := FloatFrom([]float32{1, 1, 1, 2.6}, 4) // oW = floor(2*2.6) = 5
	n := &Node{OpType: "Resize", Attrs: map[string]Attr{
		"coordinate_transformation_mode": {Type: attrTypeString, S: []byte("half_pixel")},
		"mode":                           {Type: attrTypeString, S: []byte("linear")},
	}}
	outs, err := opResize(nil, n, []*Tensor{x, nil, scales})
	if err != nil {
		t.Fatal(err)
	}
	// v = (ow+0.5)/2.6 - 0.5, clamped into [0,1]:
	// ow=0 -> -0.3077 -> 0 ; ow=1 -> 0.0769 ; ow=2 -> 0.4615 ;
	// ow=3 -> 0.8462 ; ow=4 -> 1.2308 -> 1
	want := make([]float32, 5)
	for ow := 0; ow < 5; ow++ {
		v := (float64(ow)+0.5)/2.6 - 0.5
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		want[ow] = float32(v * 10)
	}
	wantF32(t, outs[0], want)
}

// TestResizePytorchHalfPixelSingleton: pytorch_half_pixel pins the source
// coordinate to 0 when the output dim is 1 (unlike plain half_pixel).
func TestResizePytorchHalfPixelSingleton(t *testing.T) {
	x := FloatFrom([]float32{10, 20, 30}, 1, 1, 3, 1)
	sizes := IntFrom([]int64{1, 1, 1, 1}, 4)
	n := &Node{OpType: "Resize", Attrs: map[string]Attr{
		"coordinate_transformation_mode": {Type: attrTypeString, S: []byte("pytorch_half_pixel")},
		"mode":                           {Type: attrTypeString, S: []byte("nearest")},
		"nearest_mode":                   {Type: attrTypeString, S: []byte("floor")},
	}}
	outs, err := opResize(nil, n, []*Tensor{x, nil, nil, sizes})
	if err != nil {
		t.Fatal(err)
	}
	wantF32(t, outs[0], []float32{10}) // coord 0, not (0.5/scale - 0.5) = 1.0
}

// TestResizeTfHalfPixelForNN: tf_half_pixel_for_nn uses (dst+0.5)/scale with
// no -0.5 offset.
func TestResizeTfHalfPixelForNN(t *testing.T) {
	x := FloatFrom([]float32{10, 20, 30}, 1, 1, 1, 3)
	scales := FloatFrom([]float32{1, 1, 1, 1.5}, 4) // oW = floor(3*1.5) = 4
	n := &Node{OpType: "Resize", Attrs: map[string]Attr{
		"coordinate_transformation_mode": {Type: attrTypeString, S: []byte("tf_half_pixel_for_nn")},
		"mode":                           {Type: attrTypeString, S: []byte("nearest")},
		"nearest_mode":                   {Type: attrTypeString, S: []byte("floor")},
	}}
	outs, err := opResize(nil, n, []*Tensor{x, nil, scales})
	if err != nil {
		t.Fatal(err)
	}
	// v = (ow+0.5)/1.5 = 0.33, 1.0, 1.67, 2.33 -> floor: 0, 1, 1, 2
	// (half_pixel would give -0.17, 0.5, 1.17, 1.83 -> 0, 0, 1, 1)
	wantF32(t, outs[0], []float32{10, 20, 20, 30})
}

// TestSoftmaxOpset13Axis: opset 13+ softmax normalizes along the single
// axis independently per slab (no 2D flatten coercion).
func TestSoftmaxOpset13Axis(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	n := &Node{OpType: "Softmax", Attrs: map[string]Attr{
		"axis": {Type: attrTypeInt, I: 0},
	}}
	g13 := &Graph{opset: 13}
	outs, err := opSoftmax(&runCtx{Graph: g13}, n, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	// per-column softmax over the 2 rows: p = 1/(1+e^3) for row 0.
	p := float32(1) / (1 + exp32(3))
	wantF32(t, outs[0], []float32{p, p, p, 1 - p, 1 - p, 1 - p})
}

// TestErf32IntoNonFinite: erf(±Inf) = ±1 exactly, erf(NaN) = NaN. vek32's
// Inv approximation turns 1/+Inf into NaN on the AVX2 path, so the final
// store clamps non-finite inputs explicitly.
func TestErf32IntoNonFinite(t *testing.T) {
	src := []float32{
		float32(math.Inf(1)), float32(math.Inf(-1)),
		float32(math.NaN()), 1e30, -1e30,
	}
	dst := make([]float32, len(src))
	erf32Into(dst, src)
	if dst[0] != 1 {
		t.Errorf("erf(+Inf) = %v, want 1", dst[0])
	}
	if dst[1] != -1 {
		t.Errorf("erf(-Inf) = %v, want -1", dst[1])
	}
	if !math.IsNaN(float64(dst[2])) {
		t.Errorf("erf(NaN) = %v, want NaN", dst[2])
	}
	if dst[3] != 1 || dst[4] != -1 {
		t.Errorf("erf(±1e30) = %v, %v, want ±1", dst[3], dst[4])
	}
}
