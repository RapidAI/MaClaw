package onnxrt

import (
	"math"
	"testing"
)

// reduceMeanRef is a straightforward reference: mean over the given axes with
// keepdims applied, independent of the vectorized fast path.
func reduceMeanRef(x *Tensor, axes []int, keepdims bool) []float32 {
	rank := x.Rank()
	reduce := make([]bool, rank)
	for _, a := range axes {
		reduce[a] = true
	}
	kept := make([]int, rank)
	n := 1
	for d := 0; d < rank; d++ {
		if reduce[d] {
			kept[d] = 1
		} else {
			kept[d] = x.Shape[d]
		}
		n *= kept[d]
	}
	acc := make([]float64, n)
	cnt := make([]int, n)
	idx := make([]int, rank)
	for si, v := range x.F32 {
		// map flat source index to kept-flat index (row-major: last dim fastest)
		rem := si
		off := 0
		for d := rank - 1; d >= 0; d-- {
			idx[d] = rem % x.Shape[d]
			rem /= x.Shape[d]
		}
		stride := 1
		for d := rank - 1; d >= 0; d-- {
			if !reduce[d] {
				off += idx[d] * stride
				stride *= kept[d]
			}
		}
		acc[off] += float64(v)
		cnt[off]++
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(acc[i] / float64(cnt[i]))
	}
	_ = keepdims
	return out
}

func TestReduceMeanSuffixFastPath(t *testing.T) {
	cases := []struct {
		name     string
		shape    []int64
		axes     []int64
		keepdims int64
	}{
		{"spatial-keep", []int64{1, 4, 6, 10}, []int64{2, 3}, 1},
		{"spatial-drop", []int64{2, 3, 5, 7}, []int64{2, 3}, 0},
		{"last-keep", []int64{3, 8}, []int64{-1}, 1},
		{"last-drop", []int64{3, 8}, []int64{1}, 0},
		{"all", []int64{2, 3, 4}, []int64{0, 1, 2}, 1},
		{"mid-not-suffix", []int64{2, 3, 4}, []int64{1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nElem := 1
			for _, d := range c.shape {
				nElem *= int(d)
			}
			xf := make([]float32, nElem)
			for i := range xf {
				xf[i] = float32(math.Sin(float64(i)*0.7) * 3)
			}
			x := FloatFrom(xf, int64sToInts(c.shape)...)
			m := &Model{Opset: 13, Graph: &GraphProto{
				Nodes: []*Node{{
					Name: "rm", OpType: "ReduceMean", Inputs: []string{"x"}, Outputs: []string{"y"},
					Attrs: map[string]Attr{
						"axes":     {Name: "axes", Type: 7, IntVals: c.axes},
						"keepdims": {Name: "keepdims", Type: 2, I: c.keepdims},
					},
				}},
				Inputs:  []ValueInfo{{Name: "x", ElemType: TypeFloat}},
				Outputs: []ValueInfo{{Name: "y"}},
			}}
			g, err := NewGraph(m)
			if err != nil {
				t.Fatal(err)
			}
			outs, err := g.Run(map[string]*Tensor{"x": x})
			if err != nil {
				t.Fatal(err)
			}
			got := outs["y"]
			var axesInt []int
			for _, a := range c.axes {
				ai := int(a)
				if ai < 0 {
					ai += len(c.shape)
				}
				axesInt = append(axesInt, ai)
			}
			want := reduceMeanRef(x, axesInt, c.keepdims != 0)
			if got.NumElements() != len(want) {
				t.Fatalf("output %d elements, want %d (shape %v)", got.NumElements(), len(want), got.Shape)
			}
			for i, w := range want {
				d := math.Abs(float64(got.F32[i] - w))
				if d > 1e-5 {
					t.Fatalf("out[%d] = %v, want %v (shape %v)", i, got.F32[i], w, got.Shape)
				}
			}
			// keepdims=0 must actually drop the reduced dims.
			if c.keepdims == 0 && got.Rank() != len(c.shape)-len(c.axes) {
				t.Fatalf("rank %d, want %d (shape %v)", got.Rank(), len(c.shape)-len(c.axes), got.Shape)
			}
		})
	}
}

func int64sToInts(v []int64) []int {
	out := make([]int, len(v))
	for i, d := range v {
		out[i] = int(d)
	}
	return out
}
