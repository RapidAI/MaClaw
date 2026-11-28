package nnapi

import "testing"

func TestWireOrderMatchesNNAPI(t *testing.T) {
	cases := []struct {
		name string
		op   operation
		want []wireKind
	}{
		{"ADD", operation{code: opAdd, in: []int{0, 1}}, []wireKind{wireT0, wireT1, wireFused}},
		{"MUL", operation{code: opMul, in: []int{0, 1}}, []wireKind{wireT0, wireT1, wireFused}},
		{"SUB", operation{code: opSub, in: []int{0, 1}}, []wireKind{wireT0, wireT1, wireFused}},
		{"FULLY_CONNECTED", operation{code: opFC, in: []int{0, 1, 2}, relu: true}, []wireKind{wireT0, wireT1, wireT2, wireFused}},
		{"RESHAPE", operation{code: opReshape, in: []int{0, 1}}, []wireKind{wireT0, wireT1}},
		{"TRANSPOSE", operation{code: opTranspose, in: []int{0, 1}}, []wireKind{wireT0, wireT1}},
		{"SPLIT", operation{code: opSplit, in: []int{4}, axis: 1, splits: 3}, []wireKind{wireT0, wireAxis, wireSplits}},
		{"BATCH_MATMUL", operation{code: opBMM, in: []int{0, 1}, adjY: true}, []wireKind{wireT0, wireT1, wireAdjX, wireAdjY}},
		{"SOFTMAX", operation{code: opSoftmax, in: []int{0}, axis: 2}, []wireKind{wireT0, wireBeta, wireAxis}},
		{"DEPTHWISE_CONV_2D", operation{code: opDWConv, in: []int{0, 1, 2}}, []wireKind{wireT0, wireT1, wireT2, wirePadSame, wireStride1, wireStride1, wireDepthMul, wireFused}},
		{"MEAN", operation{code: opMean, in: []int{0, 1}, axis: 1}, []wireKind{wireT0, wireT1, wireKeepDims}},
		{"RSQRT", operation{code: opRsqrt, in: []int{0}}, []wireKind{wireT0}},
		{"ARGMAX", operation{code: opArgMax, in: []int{0}, axis: 1}, []wireKind{wireT0, wireAxis}},
		{"TANH", operation{code: opTanh, in: []int{0}}, []wireKind{wireT0}},
		{"GATHER", operation{code: opGather, in: []int{2, 3}, axis: 0}, []wireKind{wireT0, wireAxis, wireT1}},
	}
	for _, tc := range cases {
		name, kinds, err := wireInputs(tc.op)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if name != tc.name {
			t.Fatalf("name %s want %s", name, tc.name)
		}
		if len(kinds) != len(tc.want) {
			t.Fatalf("%s kinds %v want %v", tc.name, kinds, tc.want)
		}
		for i := range kinds {
			if kinds[i] != tc.want[i] {
				t.Fatalf("%s kinds %v want %v", tc.name, kinds, tc.want)
			}
		}
	}
}
