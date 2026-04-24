package yolo

import (
	"math"
	"testing"
)

func TestNewTensor(t *testing.T) {
	x := NewTensor(2, 3, 4)
	if x.Size() != 24 {
		t.Errorf("expected size 24, got %d", x.Size())
	}
	if x.Dim() != 3 {
		t.Errorf("expected 3 dims, got %d", x.Dim())
	}
}

func TestAtSet(t *testing.T) {
	x := NewTensor(2, 3)
	x.Set(42.0, 1, 2)
	if v := x.At(1, 2); v != 42.0 {
		t.Errorf("expected 42.0, got %f", v)
	}
}

func TestSiLU(t *testing.T) {
	x := NewTensorFrom([]float32{0, 1, -1}, 3)
	x.SiLU()
	// SiLU(0) = 0, SiLU(1) = 1*sigmoid(1) ≈ 0.7311, SiLU(-1) ≈ -0.2689
	if math.Abs(float64(x.Data[0])) > 1e-5 {
		t.Errorf("SiLU(0) = %f, expected 0", x.Data[0])
	}
	if math.Abs(float64(x.Data[1])-0.7311) > 0.001 {
		t.Errorf("SiLU(1) = %f, expected ~0.7311", x.Data[1])
	}
	if math.Abs(float64(x.Data[2])+0.2689) > 0.001 {
		t.Errorf("SiLU(-1) = %f, expected ~-0.2689", x.Data[2])
	}
}

func TestSliceChannel(t *testing.T) {
	// [1, 4, 2, 2] tensor
	x := NewTensor(1, 4, 2, 2)
	for c := 0; c < 4; c++ {
		for h := 0; h < 2; h++ {
			for w := 0; w < 2; w++ {
				x.Set(float32(c*10+h*2+w), 0, c, h, w)
			}
		}
	}

	s := x.SliceChannel(1, 3) // channels 1 and 2
	if s.Shape[1] != 2 {
		t.Fatalf("expected 2 channels, got %d", s.Shape[1])
	}
	// Channel 1, position (0,0) should be 10
	if v := s.At(0, 0, 0, 0); v != 10 {
		t.Errorf("expected 10, got %f", v)
	}
	// Channel 2 (index 1 in slice), position (1,1) should be 23
	if v := s.At(0, 1, 1, 1); v != 23 {
		t.Errorf("expected 23, got %f", v)
	}
}

func TestConcatChannel(t *testing.T) {
	a := NewTensor(1, 2, 2, 2)
	b := NewTensor(1, 3, 2, 2)
	for i := range a.Data {
		a.Data[i] = 1
	}
	for i := range b.Data {
		b.Data[i] = 2
	}

	c := ConcatChannel(a, b)
	if c.Shape[1] != 5 {
		t.Fatalf("expected 5 channels, got %d", c.Shape[1])
	}
	if c.At(0, 0, 0, 0) != 1 {
		t.Error("first channel should be 1")
	}
	if c.At(0, 3, 0, 0) != 2 {
		t.Error("fourth channel should be 2")
	}
}

func TestMaxPool2d(t *testing.T) {
	// [1, 1, 4, 4] tensor with values 0-15
	x := NewTensor(1, 1, 4, 4)
	for i := range x.Data {
		x.Data[i] = float32(i)
	}

	// 2x2 pool, stride 2, no padding → [1, 1, 2, 2]
	out := x.MaxPool2d(2, 2, 0)
	if out.Shape[2] != 2 || out.Shape[3] != 2 {
		t.Fatalf("expected [1,1,2,2], got %v", out.Shape)
	}
	// Top-left 2x2 block: max(0,1,4,5) = 5
	if out.At(0, 0, 0, 0) != 5 {
		t.Errorf("expected 5, got %f", out.At(0, 0, 0, 0))
	}
	// Bottom-right 2x2 block: max(10,11,14,15) = 15
	if out.At(0, 0, 1, 1) != 15 {
		t.Errorf("expected 15, got %f", out.At(0, 0, 1, 1))
	}
}

func TestUpsample2x(t *testing.T) {
	x := NewTensor(1, 1, 2, 2)
	x.Data = []float32{1, 2, 3, 4}

	out := x.Upsample2x()
	if out.Shape[2] != 4 || out.Shape[3] != 4 {
		t.Fatalf("expected [1,1,4,4], got %v", out.Shape)
	}
	// (0,0) → 1, (0,1) → 1, (1,0) → 1, (1,1) → 1
	if out.At(0, 0, 0, 0) != 1 || out.At(0, 0, 0, 1) != 1 || out.At(0, 0, 1, 0) != 1 || out.At(0, 0, 1, 1) != 1 {
		t.Error("top-left 2x2 should all be 1")
	}
	// (2,2) → 4, (2,3) → 4, (3,2) → 4, (3,3) → 4
	if out.At(0, 0, 2, 2) != 4 {
		t.Errorf("expected 4 at (2,2), got %f", out.At(0, 0, 2, 2))
	}
}

func TestSoftmax(t *testing.T) {
	x := NewTensorFrom([]float32{1, 2, 3}, 1, 3)
	out := x.Softmax(1)
	sum := out.Data[0] + out.Data[1] + out.Data[2]
	if math.Abs(float64(sum)-1.0) > 1e-5 {
		t.Errorf("softmax sum = %f, expected 1.0", sum)
	}
	// Values should be monotonically increasing
	if out.Data[0] >= out.Data[1] || out.Data[1] >= out.Data[2] {
		t.Errorf("softmax should be monotonically increasing: %v", out.Data)
	}
}

func TestTranspose2D(t *testing.T) {
	// [2, 3] → [3, 2]
	x := NewTensorFrom([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	out := x.Transpose2D()
	if out.Shape[0] != 3 || out.Shape[1] != 2 {
		t.Fatalf("expected [3,2], got %v", out.Shape)
	}
	// (0,0)=1, (0,1)=4, (1,0)=2, (1,1)=5, (2,0)=3, (2,1)=6
	if out.At(0, 0) != 1 || out.At(0, 1) != 4 || out.At(1, 0) != 2 {
		t.Errorf("transpose incorrect: %v", out.Data)
	}
}

func TestAddChannelBias(t *testing.T) {
	x := NewTensor(1, 2, 2, 2) // all zeros
	x.AddChannelBias([]float32{10, 20})
	if x.At(0, 0, 0, 0) != 10 {
		t.Errorf("expected 10, got %f", x.At(0, 0, 0, 0))
	}
	if x.At(0, 1, 0, 0) != 20 {
		t.Errorf("expected 20, got %f", x.At(0, 1, 0, 0))
	}
}
