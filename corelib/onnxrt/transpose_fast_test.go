package onnxrt

import (
	"testing"
)

func transposeRef(src []float32, shape, perm []int) []float32 {
	rank := len(shape)
	outShape := make([]int, rank)
	for i := range perm {
		outShape[i] = shape[perm[i]]
	}
	dst := make([]float32, numElements(shape))
	srcStrides := make([]int, rank)
	acc := 1
	for d := rank - 1; d >= 0; d-- {
		srcStrides[d] = acc
		acc *= shape[d]
	}
	n := len(dst)
	idx := make([]int, rank)
	for oi := 0; oi < n; oi++ {
		off := 0
		for d := 0; d < rank; d++ {
			off += idx[d] * srcStrides[perm[d]]
		}
		dst[oi] = src[off]
		for d := rank - 1; d >= 0; d-- {
			idx[d]++
			if idx[d] < outShape[d] {
				break
			}
			idx[d] = 0
		}
	}
	return dst
}

func TestTransposeAdjacent(t *testing.T) {
	cases := []struct {
		shape []int
		perm  []int64
	}{
		{[]int{8, 8}, []int64{1, 0}},
		{[]int{7, 5}, []int64{1, 0}},
		{[]int{4, 4}, []int64{1, 0}},
		{[]int{3, 6, 5}, []int64{0, 2, 1}},
		{[]int{2, 9, 8, 3}, []int64{0, 2, 1, 3}},
		{[]int{1, 6, 4, 2, 3}, []int64{0, 2, 1, 3, 4}},
		{[]int{1, 64, 40}, []int64{0, 2, 1}},
		{[]int{1, 256, 128}, []int64{0, 2, 1}},
		// outer >= pool width and the 8×8 bands are also large. The outer
		// split must not wait on a second ParallelRanges.
		{[]int{16, 128, 96}, []int64{0, 2, 1}},
		{[]int{2, 3, 4}, []int64{2, 0, 1}},
		{[]int{2, 0, 1}, []int64{2, 0, 1}},
	}
	for _, tc := range cases {
		n := numElements(tc.shape)
		src := make([]float32, n)
		for i := range src {
			src[i] = float32(i%19) - 9
		}
		perm := make([]int, len(tc.perm))
		for i, v := range tc.perm {
			perm[i] = int(v)
		}
		got, err := opTranspose(nil, &Node{Attrs: map[string]Attr{"perm": {IntVals: tc.perm}}}, []*Tensor{FloatFrom(src, tc.shape...)})
		if err != nil {
			t.Fatal(err)
		}
		want := transposeRef(src, tc.shape, perm)
		if len(got[0].F32) != len(want) {
			t.Fatalf("shape %v perm %v len %d want %d", tc.shape, tc.perm, len(got[0].F32), len(want))
		}
		for i := range want {
			if got[0].F32[i] != want[i] {
				t.Fatalf("shape %v perm %v [%d]=%v want %v", tc.shape, tc.perm, i, got[0].F32[i], want[i])
			}
		}
	}
	src := []int64{1, 2, 3, 4}
	got, err := opTranspose(nil, &Node{Attrs: map[string]Attr{"perm": {IntVals: []int64{1, 0}}}}, []*Tensor{IntFrom(src, 2, 2)})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{1, 3, 2, 4}
	for i := range want {
		if got[0].I64[i] != want[i] {
			t.Fatalf("int64 [%d]=%d", i, got[0].I64[i])
		}
	}
}

func BenchmarkTransposeFeature(b *testing.B) {
	// [1, 256, 10000] perm [0,2,1] is the layout backbone token transpose.
	shape := []int{1, 256, 10000}
	src := make([]float32, 256*10000)
	for i := range src {
		src[i] = float32(i)
	}
	dst := make([]float32, len(src))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transposeAdjacent(dst, src, shape, 1)
	}
}

func BenchmarkTransposeFeatureRef(b *testing.B) {
	shape := []int{1, 256, 10000}
	src := make([]float32, 256*10000)
	perm := []int{0, 2, 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transposeRef(src, shape, perm)
	}
}
