package onnxrt

import (
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGatherAxis0(t *testing.T) {
	data := FloatFrom([]float32{10, 20, 30}, 3)
	idx := IntFrom([]int64{2, 0, -1}, 3)
	outs, err := opGather(nil, &Node{Attrs: map[string]Attr{}}, []*Tensor{data, idx})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].F32, []float32{30, 10, 30}) {
		t.Fatalf("got %v", outs[0].F32)
	}
	if idx.I64[2] != -1 {
		t.Fatal("Gather wrapped a negative index in the input tensor")
	}
	ints := IntFrom([]int64{10, 20, 30}, 3)
	outs, err = opGather(nil, &Node{Attrs: map[string]Attr{}}, []*Tensor{ints, IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if outs[0].DType != DInt64 || outs[0].I64[0] != 20 {
		t.Fatalf("int gather got %v", outs[0].I64)
	}
}

func TestLayerNormLastAxis(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3}, 3)
	scale := FloatFrom([]float32{1, 1, 1}, 3)
	bias := FloatFrom([]float32{0, 0, 0}, 3)
	n := &Node{Attrs: map[string]Attr{"axis": {I: -1}, "epsilon": {F: 1e-5}}}
	outs, err := opLayerNormalization(nil, n, []*Tensor{x, scale, bias})
	if err != nil {
		t.Fatal(err)
	}
	vari := 2.0 / 3.0
	inv := 1.0 / math.Sqrt(vari+1e-5)
	want := []float32{float32(-inv), 0, float32(inv)}
	for i := range want {
		if math.Abs(float64(outs[0].F32[i]-want[i])) > 1e-5 {
			t.Fatalf("got %v want %v", outs[0].F32, want)
		}
	}
}

func TestEqualLessWhere(t *testing.T) {
	a := FloatFrom([]float32{1, 2, 3}, 3)
	b := FloatFrom([]float32{2}, 1)
	eq, err := opEqual(nil, &Node{}, []*Tensor{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(eq[0].I64, []int64{0, 1, 0}) {
		t.Fatalf("equal %v", eq[0].I64)
	}
	less, err := opLess(nil, &Node{}, []*Tensor{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(less[0].I64, []int64{1, 0, 0}) {
		t.Fatalf("less %v", less[0].I64)
	}
	cond := IntFrom([]int64{1, 0}, 2)
	x := IntFrom([]int64{5, 6}, 2)
	y := IntFrom([]int64{7, 8}, 2)
	wh, err := opWhere(nil, &Node{}, []*Tensor{cond, x, y})
	if err != nil {
		t.Fatal(err)
	}
	if wh[0].DType != DInt64 || !reflect.DeepEqual(wh[0].I64, []int64{5, 8}) {
		t.Fatalf("where %v", wh[0].I64)
	}
}

func TestWhereGreaterBroadcastRows(t *testing.T) {
	// masked-fill shape: condition broadcasts on the last dim, x is the
	// scalar fill, y is dense. The row walker must match elemOffset.
	cond := IntFrom([]int64{1, 0}, 2, 1)
	x := FloatFrom([]float32{-9992}, 1)
	y := FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8}, 2, 4)
	got, err := opWhere(nil, &Node{}, []*Tensor{cond, x, y})
	if err != nil {
		t.Fatal(err)
	}
	want := whereRef(cond, x, y)
	if !reflect.DeepEqual(got[0].Shape, []int{2, 4}) || !reflect.DeepEqual(got[0].F32, want) {
		t.Fatalf("masked fill = %v, want %v", got[0].F32, want)
	}

	// y broadcasts (last stride 0) and the condition varies along the row.
	cond2 := IntFrom([]int64{1, 0, 1, 0, 0, 1}, 2, 3)
	y2 := FloatFrom([]float32{10, 20}, 2, 1)
	x2 := FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	got, err = opWhere(nil, &Node{}, []*Tensor{cond2, x2, y2})
	if err != nil {
		t.Fatal(err)
	}
	want = whereRef(cond2, x2, y2)
	if !reflect.DeepEqual(got[0].F32, want) {
		t.Fatalf("broadcast y = %v, want %v", got[0].F32, want)
	}

	// Int path, same broadcast.
	iy := IntFrom([]int64{9}, 1)
	ix := IntFrom([]int64{1, 2, 3, 4, 5, 6}, 2, 3)
	igot, err := opWhere(nil, &Node{}, []*Tensor{cond2, ix, iy})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(igot[0].I64, []int64{1, 9, 3, 9, 9, 6}) {
		t.Fatalf("int where = %v", igot[0].I64)
	}

	// Parallel rows: scalar threshold Greater, and a wide Where.
	wideN := 4 * 4096
	wide := make([]float32, wideN)
	for i := range wide {
		wide[i] = float32(i%5) - 2
	}
	wf := FloatFrom(wide, 4, 4096)
	thr := FloatFrom([]float32{0}, 1)
	gt, err := opGreater(nil, &Node{}, []*Tensor{wf, thr})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range wide {
		wantBit := int64(0)
		if v > 0 {
			wantBit = 1
		}
		if gt[0].I64[i] != wantBit {
			t.Fatalf("greater[%d] = %d, want %d", i, gt[0].I64[i], wantBit)
		}
	}
	wc := make([]int64, wideN)
	for i := range wc {
		if wide[i] > 0 {
			wc[i] = 1
		}
	}
	wgot, err := opWhere(nil, &Node{}, []*Tensor{IntFrom(wc, 4, 4096), FloatFrom([]float32{-9992}, 1), wf})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range wide {
		wantV := v
		if wc[i] != 0 {
			wantV = -9992
		}
		if wgot[0].F32[i] != wantV {
			t.Fatalf("where[%d] = %v, want %v", i, wgot[0].F32[i], wantV)
		}
	}
}

func whereRef(cond, x, y *Tensor) []float32 {
	outShape, err := broadcast3(cond.Shape, x.Shape, y.Shape)
	if err != nil {
		panic(err)
	}
	cs := broadcastStrides(cond.Shape, outShape)
	xs := broadcastStrides(x.Shape, outShape)
	ys := broadcastStrides(y.Shape, outShape)
	n := numElements(outShape)
	out := make([]float32, n)
	for oi := 0; oi < n; oi++ {
		on := cond.I64[elemOffset(oi, outShape, cs)] != 0
		if on {
			out[oi] = x.F32[elemOffset(oi, outShape, xs)]
		} else {
			out[oi] = y.F32[elemOffset(oi, outShape, ys)]
		}
	}
	return out
}

func TestRangeExpandCastPad(t *testing.T) {
	outs, err := opRange(nil, &Node{}, []*Tensor{ScalarInt(0), ScalarInt(4), ScalarInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].I64, []int64{0, 1, 2, 3}) {
		t.Fatalf("range %v", outs[0].I64)
	}
	data := FloatFrom([]float32{1, 2, 3}, 1, 3)
	exp, err := opExpand(nil, &Node{}, []*Tensor{data, IntFrom([]int64{2, 3}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exp[0].Shape, []int{2, 3}) || !reflect.DeepEqual(exp[0].F32, []float32{1, 2, 3, 1, 2, 3}) {
		t.Fatalf("expand shape %v data %v", exp[0].Shape, exp[0].F32)
	}
	cast, err := opCast(nil, &Node{Attrs: map[string]Attr{"to": {I: int64(TypeInt64)}}}, []*Tensor{FloatFrom([]float32{1.9, -1.2}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cast[0].I64, []int64{1, -1}) {
		t.Fatalf("cast %v", cast[0].I64)
	}
	asBool, err := opCast(nil, &Node{Attrs: map[string]Attr{"to": {I: int64(TypeBool)}}}, []*Tensor{FloatFrom([]float32{0, 1.5, float32(math.NaN())}, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asBool[0].I64, []int64{0, 1, 1}) {
		t.Fatalf("bool cast %v", asBool[0].I64)
	}
	padIn := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
	padded, err := opPad(nil, &Node{Attrs: map[string]Attr{"mode": {S: []byte("constant")}}}, []*Tensor{padIn, IntFrom([]int64{0, 1, 0, 1}, 4)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(padded[0].Shape, []int{2, 4}) || !reflect.DeepEqual(padded[0].F32, []float32{0, 1, 2, 0, 0, 3, 4, 0}) {
		t.Fatalf("pad shape %v data %v", padded[0].Shape, padded[0].F32)
	}
}

func TestConstantOfShape(t *testing.T) {
	val := &TensorData{TensorProto: TensorProto{DataType: TypeInt64, Int64Data: []int64{3}}}
	n := &Node{Attrs: map[string]Attr{"value": {T: val}}}
	outs, err := opConstantOfShape(nil, n, []*Tensor{IntFrom([]int64{2, 2}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outs[0].Shape, []int{2, 2}) || !reflect.DeepEqual(outs[0].I64, []int64{3, 3, 3, 3}) {
		t.Fatalf("got shape %v data %v", outs[0].Shape, outs[0].I64)
	}
	td := &TensorData{TensorProto: TensorProto{DataType: TypeFloat, FloatData: []float32{1.5}}}
	c, err := opConstant(nil, &Node{Attrs: map[string]Attr{"value": {T: td}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c[0].Rank() != 0 || c[0].NumElements() != 1 || c[0].F32[0] != 1.5 {
		t.Fatalf("scalar constant rank %d data %v", c[0].Rank(), c[0].F32)
	}
}

func TestValidateInputInt64(t *testing.T) {
	g := &Graph{
		inputShapes: map[string][]Dim{
			"ids":   {{Value: -1}, {Param: "seq"}},
			"pixel": {{Value: 1}, {Value: 3}, {Value: 8}, {Value: 8}},
		},
		inputDTypes: map[string]TensorDataType{
			"ids":   TypeInt64,
			"pixel": TypeFloat,
		},
	}
	if err := g.validateInput("ids", IntFrom([]int64{0}, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := g.validateInput("ids", NewFloat(1, 1)); err == nil {
		t.Fatal("float tensor accepted as int64 input")
	}
	if err := g.validateInput("pixel", NewFloat(1, 3, 8, 8)); err != nil {
		t.Fatal(err)
	}
	if err := g.validateInput("pixel", IntFrom([]int64{0}, 1, 3, 8, 8)); err == nil {
		t.Fatal("int tensor accepted as float input")
	}
	legacy := &Graph{inputShapes: map[string][]Dim{"x": {{Value: 2}}}}
	if err := legacy.validateInput("x", IntFrom([]int64{1, 2}, 2)); err == nil {
		t.Fatal("undeclared element type accepted int64 when a shape is present")
	}
}

func TestAccel(t *testing.T) {
	got := Accel()
	if got == "" {
		t.Fatal("empty accel")
	}
	if runtime.GOARCH != "arm64" && strings.Contains(got, "neon") {
		t.Fatalf("neon reported on %s: %s", runtime.GOARCH, got)
	}
	if runtime.GOOS != "darwin" && strings.Contains(got, "metal") {
		t.Fatalf("metal reported on %s: %s", runtime.GOOS, got)
	}
	switch {
	case hasAVX512ZMM:
		if !strings.Contains(got, "avx512") {
			t.Fatalf("avx512 available, Accel=%s", got)
		}
	case hasAVX2FMA:
		if !strings.Contains(got, "avx2") {
			t.Fatalf("avx2 available, Accel=%s", got)
		}
	case runtime.GOARCH == "arm64":
		if !strings.Contains(got, "neon") {
			t.Fatalf("arm64 Accel=%s", got)
		}
	default:
		if !strings.Contains(got, "scalar") && !strings.Contains(got, "metal") {
			t.Fatalf("Accel=%s", got)
		}
	}
	t.Log("accel", got)
}
