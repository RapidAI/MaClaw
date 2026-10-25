package onnxrt

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func closeEnough(got, want []float32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			return false
		}
	}
	return true
}

func TestShapeStartEnd(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	full, err := opShape(nil, &Node{Attrs: map[string]Attr{"start": {I: 0}}}, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full[0].I64, []int64{2, 3}) {
		t.Fatalf("full = %v", full[0].I64)
	}
	head, err := opShape(nil, &Node{Attrs: map[string]Attr{"start": {I: 0}, "end": {I: 1}}}, []*Tensor{x})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(head[0].I64, []int64{2}) {
		t.Fatalf("head = %v", head[0].I64)
	}
}

func TestLayoutCompareClipUnary(t *testing.T) {
	a := IntFrom([]int64{1, 0, 2}, 3)
	b := IntFrom([]int64{1, 1, 1}, 3)
	and, err := opAnd(nil, &Node{}, []*Tensor{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(and[0].I64, []int64{1, 0, 1}) {
		t.Fatalf("and = %v", and[0].I64)
	}
	gt, err := opGreater(nil, &Node{}, []*Tensor{FloatFrom([]float32{1, 2}, 2), FloatFrom([]float32{1.5}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gt[0].I64, []int64{0, 1}) {
		t.Fatalf("greater = %v", gt[0].I64)
	}
	clipped, err := opClip(nil, &Node{}, []*Tensor{
		FloatFrom([]float32{-1, 0.5, 3}, 3),
		FloatFrom([]float32{0}, 1),
		FloatFrom([]float32{1}, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(clipped[0].F32, []float32{0, 0.5, 1}) {
		t.Fatalf("clip = %v", clipped[0].F32)
	}
	s, err := opSin(nil, &Node{}, []*Tensor{FloatFrom([]float32{0}, 1)})
	if err != nil || !closeEnough(s[0].F32, []float32{0}) {
		t.Fatalf("sin = %v %v", s, err)
	}
	c, err := opCos(nil, &Node{}, []*Tensor{FloatFrom([]float32{0}, 1)})
	if err != nil || !closeEnough(c[0].F32, []float32{1}) {
		t.Fatalf("cos = %v %v", c, err)
	}
	l, err := opLog(nil, &Node{}, []*Tensor{FloatFrom([]float32{1}, 1)})
	if err != nil || !closeEnough(l[0].F32, []float32{0}) {
		t.Fatalf("log = %v %v", l, err)
	}
	mx, err := opMax(nil, &Node{}, []*Tensor{FloatFrom([]float32{1, 4}, 2), FloatFrom([]float32{3, 2}, 2)})
	if err != nil || !reflect.DeepEqual(mx[0].F32, []float32{3, 4}) {
		t.Fatalf("max = %v %v", mx, err)
	}
}

func TestLayoutReduceGather(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
	sum, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{x, IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sum[0].Shape, []int{2}) || !reflect.DeepEqual(sum[0].F32, []float32{3, 7}) {
		t.Fatalf("sum = %v %v", sum[0].Shape, sum[0].F32)
	}
	mx, err := opReduceMax(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 1}}}, []*Tensor{x, IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mx[0].Shape, []int{2, 1}) || !reflect.DeepEqual(mx[0].F32, []float32{2, 4}) {
		t.Fatalf("max = %v %v", mx[0].Shape, mx[0].F32)
	}
	data := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
	got, err := opGatherElements(nil, &Node{Attrs: map[string]Attr{"axis": {I: 1}}}, []*Tensor{
		data, IntFrom([]int64{0, 0, 1, 0}, 2, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0].F32, []float32{1, 1, 4, 3}) {
		t.Fatalf("gather elements = %v", got[0].F32)
	}
	cube := FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8}, 2, 2, 2)
	nd, err := opGatherND(nil, &Node{Attrs: map[string]Attr{"batch_dims": {I: 0}}}, []*Tensor{
		cube, IntFrom([]int64{0, 1, 1, 0}, 2, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nd[0].Shape, []int{2, 2}) || !reflect.DeepEqual(nd[0].F32, []float32{3, 4, 5, 6}) {
		t.Fatalf("gather nd = %v %v", nd[0].Shape, nd[0].F32)
	}
}

func TestReduceSingleAxisExact(t *testing.T) {
	// axes [-1], contiguous rows. 1+2+3 and 4+5+6.
	row := FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	sum, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{row, IntFrom([]int64{-1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sum[0].Shape, []int{2}) || !reflect.DeepEqual(sum[0].F32, []float32{6, 15}) {
		t.Fatalf("last-axis sum = %v %v", sum[0].Shape, sum[0].F32)
	}

	// axes [-2] on rank 3 is axis 1: strided, not the contiguous row.
	cube := make([]float32, 2*3*4)
	for i := range cube {
		cube[i] = float32(i)
	}
	mx, err := opReduceMax(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 1}}}, []*Tensor{
		FloatFrom(cube, 2, 3, 4), IntFrom([]int64{-2}, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantMax := []float32{8, 9, 10, 11, 20, 21, 22, 23}
	if !reflect.DeepEqual(mx[0].Shape, []int{2, 1, 4}) || !reflect.DeepEqual(mx[0].F32, wantMax) {
		t.Fatalf("axis -2 max = %v %v", mx[0].Shape, mx[0].F32)
	}
	nanCube := append([]float32(nil), cube...)
	nanCube[4] = float32(math.NaN())
	mx, err = opReduceMax(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{
		FloatFrom(nanCube, 2, 3, 4), IntFrom([]int64{-2}, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(float64(mx[0].F32[0])) {
		t.Fatalf("NaN max = %v", mx[0].F32)
	}
	for i, v := range wantMax[1:] {
		if mx[0].F32[i+1] != v {
			t.Fatalf("NaN max neighbor [%d] = %v, want %v", i+1, mx[0].F32[i+1], v)
		}
	}
	nanRow := FloatFrom([]float32{1, float32(math.NaN()), 2}, 1, 3)
	ns, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{nanRow, IntFrom([]int64{-1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	wantSum := float32(0)
	wantSum += 1
	wantSum += float32(math.NaN())
	wantSum += 2
	if math.Float32bits(ns[0].F32[0]) != math.Float32bits(wantSum) {
		t.Fatalf("NaN sum bits %x, want %x", math.Float32bits(ns[0].F32[0]), math.Float32bits(wantSum))
	}
	nmin, err := opReduceMin(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{nanRow, IntFrom([]int64{-1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(float64(nmin[0].F32[0])) {
		t.Fatalf("NaN min = %v", nmin[0].F32)
	}

	// Empty reduced axis: max/min identities must be written, not left as zero.
	empty := FloatFrom(nil, 2, 0)
	emx, err := opReduceMax(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{empty, IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(emx[0].Shape, []int{2}) || emx[0].F32[0] != float32(math.Inf(-1)) || emx[0].F32[1] != float32(math.Inf(-1)) {
		t.Fatalf("empty max = %v %v", emx[0].Shape, emx[0].F32)
	}
	emn, err := opReduceMin(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{empty, IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if emn[0].F32[0] != float32(math.Inf(1)) || emn[0].F32[1] != float32(math.Inf(1)) {
		t.Fatalf("empty min = %v", emn[0].F32)
	}

	// Both axes: the general loop, cleared before +=.
	both, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{
		FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8}, 2, 2, 2), IntFrom([]int64{0, 2}, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(both[0].Shape, []int{2}) || !reflect.DeepEqual(both[0].F32, []float32{14, 22}) {
		t.Fatalf("multi-axis sum = %v %v", both[0].Shape, both[0].F32)
	}

	// Large enough to take ParallelRanges: last axis over outer, and axis 1 over inner.
	wide := make([]float32, 4*4096)
	wantWide := make([]float32, 4)
	for i := range wide {
		wide[i] = float32(i % 17)
	}
	for o := 0; o < 4; o++ {
		var s float32
		for j := 0; j < 4096; j++ {
			s += wide[o*4096+j]
		}
		wantWide[o] = s
	}
	ws, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{FloatFrom(wide, 4, 4096), IntFrom([]int64{-1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ws[0].F32, wantWide) {
		t.Fatal("parallel last-axis sum mismatch")
	}
	innerN := 64 * 256
	col := make([]float32, innerN)
	for i := range col {
		col[i] = float32(i % 17)
	}
	wantCol := make([]float32, 256)
	for j := 0; j < 256; j++ {
		var s float32
		for r := 0; r < 64; r++ {
			s += col[r*256+j]
		}
		wantCol[j] = s
	}
	cs, err := opReduceSum(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{FloatFrom(col, 1, 64, 256), IntFrom([]int64{1}, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cs[0].Shape, []int{1, 256}) || !reflect.DeepEqual(cs[0].F32, wantCol) {
		t.Fatalf("parallel inner sum shape %v", cs[0].Shape)
	}
}

// scalarExtreme is the left-to-right max/min loop, independent of reduceSpan.
func scalarExtreme(row []float32, code reduceCode) float32 {
	if code == reduceMax {
		best := float32(math.Inf(-1))
		for _, v := range row {
			if v > best || math.IsNaN(float64(v)) {
				best = v
			}
		}
		return best
	}
	best := float32(math.Inf(1))
	for _, v := range row {
		if v < best || math.IsNaN(float64(v)) {
			best = v
		}
	}
	return best
}

func reduceLastAxis(data []float32, shape []int, code reduceCode) ([]float32, error) {
	in := []*Tensor{FloatFrom(data, shape...), IntFrom([]int64{-1}, 1)}
	node := &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}
	var out []*Tensor
	var err error
	switch code {
	case reduceMax:
		out, err = opReduceMax(nil, node, in)
	case reduceMin:
		out, err = opReduceMin(nil, node, in)
	default:
		out, err = opReduceSum(nil, node, in)
	}
	if err != nil {
		return nil, err
	}
	return out[0].F32, nil
}

func extremePatterns(n int) []struct {
	name string
	row  []float32
} {
	fill := func(v float32) []float32 {
		row := make([]float32, n)
		for i := range row {
			row[i] = v
		}
		return row
	}
	asc := fill(0)
	for i := range asc {
		asc[i] = float32(i + 1)
	}
	desc := fill(0)
	for i := range desc {
		desc[i] = float32(n - i)
	}
	neg := fill(-3)
	neg[0] = -8
	neg[n/2] = -0.5
	posInf := fill(1)
	posInf[n-1] = float32(math.Inf(1))
	negInf := fill(1)
	negInf[0] = float32(math.Inf(-1))
	sub := fill(-2)
	sub[n-1] = math.Float32frombits(1)
	posZero := math.Float32frombits(0)
	negZero := math.Float32frombits(0x80000000)
	zPos := fill(-1)
	zNeg := fill(-1)
	if n >= 4 {
		zPos[1], zPos[3] = posZero, negZero
		zNeg[1], zNeg[3] = negZero, posZero
	} else {
		zPos[0] = posZero
		zNeg[0] = negZero
	}
	nan1 := math.Float32frombits(0x7fc00001)
	nan2 := math.Float32frombits(0xffc00002)
	nanLast := fill(1)
	nanLast[0] = nan1
	if n > 2 {
		nanLast[n/2] = 100
		nanLast[n-1] = nan2
	}
	nanEarly := fill(2)
	nanEarly[0] = nan1
	if n > 1 {
		nanEarly[n-1] = 50
	}
	return []struct {
		name string
		row  []float32
	}{
		{"asc", asc},
		{"desc", desc},
		{"neg", neg},
		{"eq", fill(4)},
		{"all-pinf", fill(float32(math.Inf(1)))},
		{"all-ninf", fill(float32(math.Inf(-1)))},
		{"pinf-end", posInf},
		{"ninf-start", negInf},
		{"sub", sub},
		{"zero-pos-first", zPos},
		{"zero-neg-first", zNeg},
		{"nan-last", nanLast},
		{"nan-early", nanEarly},
	}
}

func rowHasNaN(row []float32) bool {
	for _, v := range row {
		if math.IsNaN(float64(v)) {
			return true
		}
	}
	return false
}

func TestReduceContigExtremeBits(t *testing.T) {
	for _, n := range []int{1, 7, 8, 9, 15, 16, 17, 24, 31, 32, 63, 64, 40000} {
		for _, pat := range extremePatterns(n) {
			for _, code := range []reduceCode{reduceMax, reduceMin} {
				want := scalarExtreme(pat.row, code)
				got, err := reduceLastAxis(pat.row, []int{n}, code)
				if err != nil {
					t.Fatalf("n=%d %s %d: %v", n, pat.name, code, err)
				}
				if len(got) != 1 || math.Float32bits(got[0]) != math.Float32bits(want) {
					t.Fatalf("n=%d %s code=%d bits %x want %x", n, pat.name, code, math.Float32bits(got[0]), math.Float32bits(want))
				}
				simd, ok := reduceContigExtreme(pat.row, code)
				if hasAVX2FMA && n >= 8 && (rowHasNaN(pat.row) || want == 0) && ok {
					t.Fatalf("n=%d %s code=%d: special row took the simd path", n, pat.name, code)
				}
				if hasAVX2FMA && n >= 8 && !rowHasNaN(pat.row) && want != 0 {
					if !ok || math.Float32bits(simd) != math.Float32bits(want) {
						t.Fatalf("n=%d %s code=%d simd ok=%v bits %x want %x", n, pat.name, code, ok, math.Float32bits(simd), math.Float32bits(want))
					}
				}
			}
		}
	}

	// Odd span: row 1 starts 4 bytes off a 16-float boundary. Parallel when wide.
	for _, span := range []int{9, 17, 8193} {
		outer := 2
		if span >= 8192 {
			outer = 4
		}
		data := make([]float32, outer*span)
		wantMax := make([]float32, outer)
		wantMin := make([]float32, outer)
		for o := 0; o < outer; o++ {
			row := data[o*span : (o+1)*span]
			for i := range row {
				row[i] = float32(i%13) - 6
			}
			row[1+o] = 80 + float32(o)
			row[span-1-o] = -90 - float32(o)
			wantMax[o] = scalarExtreme(row, reduceMax)
			wantMin[o] = scalarExtreme(row, reduceMin)
		}
		gotMax, err := reduceLastAxis(data, []int{outer, span}, reduceMax)
		if err != nil {
			t.Fatal(err)
		}
		gotMin, err := reduceLastAxis(data, []int{outer, span}, reduceMin)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotMax, wantMax) || !reflect.DeepEqual(gotMin, wantMin) {
			t.Fatalf("odd span %d max %v want %v min %v want %v", span, gotMax, wantMax, gotMin, wantMin)
		}
	}

	// Axis 0 of [8, 16] steps by 16. A contiguous reduction would disagree.
	strided := make([]float32, 8*16)
	for r := 0; r < 8; r++ {
		for c := 0; c < 16; c++ {
			strided[r*16+c] = float32(r*3 - c)
		}
	}
	got, err := opReduceMax(nil, &Node{Attrs: map[string]Attr{"keepdims": {I: 0}}}, []*Tensor{
		FloatFrom(strided, 8, 16), IntFrom([]int64{0}, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0].Shape, []int{16}) {
		t.Fatalf("strided shape %v", got[0].Shape)
	}
	for c := 0; c < 16; c++ {
		col := make([]float32, 8)
		for r := 0; r < 8; r++ {
			col[r] = strided[r*16+c]
		}
		want := scalarExtreme(col, reduceMax)
		if got[0].F32[c] != want {
			t.Fatalf("strided col %d = %v, want %v", c, got[0].F32[c], want)
		}
	}

	// ReduceSum stays on the scalar association.
	ones := make([]float32, 64)
	for i := range ones {
		ones[i] = 1
	}
	sum, err := reduceLastAxis(ones, []int{64}, reduceSum)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 1 || sum[0] != 64 {
		t.Fatalf("sum = %v", sum)
	}
}

func BenchmarkReduceMaxWide(b *testing.B) {
	const rows, span = 300, 40000
	data := make([]float32, rows*span)
	for i := range data {
		data[i] = float32(int(i%997) - 400)
	}
	acc := make([]float32, rows)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for o := 0; o < rows; o++ {
			acc[o] = reduceSpan(data, o*span, 1, span, reduceMax)
		}
	}
	if acc[0] == float32(math.Inf(-1)) && acc[rows-1] == 0 {
		b.Fatal("untouched")
	}
}

func BenchmarkReduceMaxWideScalar(b *testing.B) {
	const rows, span = 300, 40000
	data := make([]float32, rows*span)
	for i := range data {
		data[i] = float32(int(i%997) - 400)
	}
	acc := make([]float32, rows)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for o := 0; o < rows; o++ {
			acc[o] = scalarExtreme(data[o*span:(o+1)*span], reduceMax)
		}
	}
	if acc[0] == float32(math.Inf(-1)) && acc[rows-1] == 0 {
		b.Fatal("untouched")
	}
}

func TestGridSampleBilinearZeros(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4}, 1, 1, 2, 2)
	n := &Node{Attrs: map[string]Attr{
		"mode":          {S: []byte("bilinear")},
		"padding_mode":  {S: []byte("zeros")},
		"align_corners": {I: 0},
	}}
	center, err := opGridSample(nil, n, []*Tensor{x, FloatFrom([]float32{0, 0}, 1, 1, 1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(center[0].F32, []float32{2.5}) {
		t.Fatalf("center = %v", center[0].F32)
	}
	corner, err := opGridSample(nil, n, []*Tensor{x, FloatFrom([]float32{-1, -1}, 1, 1, 1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(corner[0].F32, []float32{0.25}) {
		t.Fatalf("corner = %v", corner[0].F32)
	}
}

func TestLayoutScatterSplitTileTopKTrilu(t *testing.T) {
	scattered, err := opScatterElements(nil, &Node{Attrs: map[string]Attr{"axis": {I: 1}, "reduction": {S: []byte("none")}}}, []*Tensor{
		FloatFrom([]float32{1, 2, 3}, 1, 3),
		IntFrom([]int64{1, 0}, 1, 2),
		FloatFrom([]float32{9, 8}, 1, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scattered[0].F32, []float32{8, 9, 3}) {
		t.Fatalf("scatter = %v", scattered[0].F32)
	}
	parts, err := opSplit(nil, &Node{OpType: "Split", Attrs: map[string]Attr{"axis": {I: 1}}, Outputs: []string{"a", "b"}}, []*Tensor{
		FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 1, 6),
		IntFrom([]int64{2, 4}, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parts[0].F32, []float32{1, 2}) || !reflect.DeepEqual(parts[1].F32, []float32{3, 4, 5, 6}) {
		t.Fatalf("split = %v %v", parts[0].F32, parts[1].F32)
	}
	tiled, err := opTile(nil, &Node{}, []*Tensor{FloatFrom([]float32{1, 2}, 1, 2), IntFrom([]int64{2, 1}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tiled[0].Shape, []int{2, 2}) || !reflect.DeepEqual(tiled[0].F32, []float32{1, 2, 1, 2}) {
		t.Fatalf("tile = %v %v", tiled[0].Shape, tiled[0].F32)
	}
	top, err := opTopK(nil, &Node{Attrs: map[string]Attr{"axis": {I: 0}, "largest": {I: 1}, "sorted": {I: 1}}, Outputs: []string{"v", "i"}}, []*Tensor{
		FloatFrom([]float32{1, 3, 2, 5}, 4),
		IntFrom([]int64{2}, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(top[0].F32, []float32{5, 3}) || !reflect.DeepEqual(top[1].I64, []int64{3, 1}) {
		t.Fatalf("topk = %v %v", top[0].F32, top[1].I64)
	}
	tri, err := opTrilu(nil, &Node{Attrs: map[string]Attr{"upper": {I: 0}}}, []*Tensor{
		FloatFrom([]float32{1, 2, 3, 4, 5, 6, 7, 8, 9}, 3, 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tri[0].F32, []float32{1, 0, 0, 4, 5, 0, 7, 8, 9}) {
		t.Fatalf("trilu = %v", tri[0].F32)
	}
}

func TestExpAndArgMax(t *testing.T) {
	exp, err := opExp(nil, &Node{}, []*Tensor{FloatFrom([]float32{0, 1}, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(exp[0].F32, []float32{1, float32(math.E)}) {
		t.Fatalf("exp = %v", exp[0].F32)
	}
	idx, err := opArgMax(nil, &Node{Attrs: map[string]Attr{"axis": {I: 1}, "keepdims": {I: 0}}}, []*Tensor{
		FloatFrom([]float32{1, 4, 2, 3, 0, 5}, 2, 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(idx[0].Shape, []int{2}) || !reflect.DeepEqual(idx[0].I64, []int64{1, 2}) {
		t.Fatalf("argmax = %v %v", idx[0].Shape, idx[0].I64)
	}
	last, err := opArgMax(nil, &Node{Attrs: map[string]Attr{
		"axis": {I: 0}, "keepdims": {I: 1}, "select_last_index": {I: 1},
	}}, []*Tensor{FloatFrom([]float32{2, 2, 1}, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(last[0].Shape, []int{1}) || last[0].I64[0] != 1 {
		t.Fatalf("argmax last = %v %v", last[0].Shape, last[0].I64)
	}
}

func TestPPDocLayoutV3Prepares(t *testing.T) {
	path := os.Getenv("PP_DOCLAYOUT")
	if path == "" {
		t.Skip("PP_DOCLAYOUT not set")
	}
	g, err := LoadGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.InputNames()) != 1 || len(g.OutputNames()) != 4 {
		t.Fatalf("inputs %v outputs %v", g.InputNames(), g.OutputNames())
	}
}

func TestPPDocLayoutV3Forward(t *testing.T) {
	path := os.Getenv("PP_DOCLAYOUT")
	if path == "" {
		t.Skip("PP_DOCLAYOUT not set")
	}
	g, err := LoadGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	inName := g.InputNames()[0]
	in := NewFloat(1, 3, 800, 800)
	for i := range in.F32 {
		in.F32[i] = float32((i%251)+1) / 255
	}
	outs, err := g.Run(map[string]*Tensor{inName: in})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]int{}
	for name, tns := range outs {
		got[name] = append([]int(nil), tns.Shape...)
		for i, v := range tns.F32 {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("%s non-finite at %d", name, i)
			}
		}
	}
	need := [][]int{{1, 300, 25}, {1, 300, 4}, {1, 300, 200, 200}, {1, 300, 300}}
	for _, shape := range need {
		found := false
		for name, have := range got {
			if reflect.DeepEqual(have, shape) {
				delete(got, name)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing shape %v in %v", shape, outs)
		}
	}
}

func TestWiredTableUNETForward(t *testing.T) {
	path := os.Getenv("PP_TABLE_UNET")
	if path == "" {
		t.Skip("PP_TABLE_UNET not set")
	}
	g, err := LoadGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.InputNames()) != 1 || len(g.OutputNames()) != 1 {
		t.Fatalf("inputs %v outputs %v", g.InputNames(), g.OutputNames())
	}
	in := NewFloat(1, 3, 64, 64)
	for i := range in.F32 {
		in.F32[i] = float32(i%17) / 16
	}
	outs, err := g.Run(map[string]*Tensor{g.InputNames()[0]: in})
	if err != nil {
		t.Fatal(err)
	}
	got := outs[g.OutputNames()[0]]
	if got == nil || got.DType != DInt64 {
		t.Fatalf("output %#v", got)
	}
	if !reflect.DeepEqual(got.Shape, []int{1, 1, 64, 64}) && !reflect.DeepEqual(got.Shape, []int{1, 64, 64}) {
		t.Fatalf("mask shape %v", got.Shape)
	}
}

func TestExternalInitializerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	payload := []byte{0, 0, 128, 63, 0, 0, 0, 64}
	if err := os.WriteFile(filepath.Join(dir, "w.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	tp := &TensorProto{
		Name: "w", Dims: []int64{2}, DataType: TypeFloat, DataLocation: 1,
		ExternalData: []ExternalDataEntry{
			{Key: "location", Value: "w.bin"},
			{Key: "offset", Value: "0"},
			{Key: "length", Value: "8"},
		},
	}
	raw, err := externalInitializer(dir, tp, map[string][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	tp.RawData = raw
	tp.DataLocation = 0
	got, err := tensorFromProto(tp)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.F32, []float32{1, 2}) {
		t.Fatalf("weights = %v", got.F32)
	}
	tp.DataLocation = 1
	tp.ExternalData[0].Value = "../w.bin"
	if _, err := externalInitializer(dir, tp, map[string][]byte{}); err == nil {
		t.Fatal("expected escape to be rejected")
	}
}
