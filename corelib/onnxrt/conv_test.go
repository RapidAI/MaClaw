package onnxrt

import (
	"testing"
	"time"
)

// TestConvGeneralNestedParallelNoDeadlock exercises convGeneral with enough
// im2row blocks to occupy the whole shared worker pool while each block's
// MatMul is large enough to parallelize internally. Before the fix (outer
// level on pool workers, nested MatMul submitting to the same pool and
// blocking on it) this deadlocked once every worker was waiting on jobs no
// free worker could drain.
func TestConvGeneralNestedParallelNoDeadlock(t *testing.T) {
	// K=64*3*3=576, OHW=288*288=82944 -> blockPixels=7281, nBlocks=12.
	x := NewFloat(1, 64, 288, 288)
	w := NewFloat(64, 64, 3, 3)
	for i := range x.F32 {
		x.F32[i] = 0.001 * float32(i%97)
	}
	for i := range w.F32 {
		w.F32[i] = 0.001 * float32(i%89)
	}
	n := &Node{OpType: "Conv", Attrs: map[string]Attr{}}
	done := make(chan error, 1)
	go func() {
		_, err := opConv(nil, n, []*Tensor{x, w})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("opConv deadlocked: nested submission to the shared worker pool")
	}
}

// TestConvEmptyBias ensures an empty (0-length) bias tensor is treated as "no
// bias" instead of panicking inside the kernels.
func TestConvEmptyBias(t *testing.T) {
	x := FloatFrom([]float32{1, 2, 3, 4}, 1, 1, 2, 2)
	w := FloatFrom([]float32{1, 1}, 2, 1, 1, 1)
	b := &Tensor{Shape: []int{0}, DType: DFloat32, F32: []float32{}}
	n := &Node{OpType: "Conv", Attrs: map[string]Attr{}}
	outs, err := opConv(nil, n, []*Tensor{x, w, b})
	if err != nil {
		t.Fatal(err)
	}
	// 1x1 conv with identity-per-channel weights: output == input per channel.
	for i, want := range []float32{1, 2, 3, 4, 1, 2, 3, 4} {
		if outs[0].F32[i] != want {
			t.Fatalf("out[%d] = %v, want %v (full %v)", i, outs[0].F32[i], want, outs[0].F32)
		}
	}
}
