package yolo

import (
	"math"
	"testing"
)

func TestConv2dBNSiLU_1x1(t *testing.T) {
	// 1x1 conv, 2 input channels → 3 output channels, no padding, stride 1
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(3, 2, 1, 1), // [OutC=3, InC=2, KH=1, KW=1]
		Bias:    []float32{0, 0, 0},
		OutC:    3,
		InC:     2,
		KH:      1,
		KW:      1,
		Stride:  1,
		Padding: 0,
		UseSiLU: false,
	}
	// Set weights: identity-like
	conv.Weight.Data[0] = 1 // out[0] = in[0]*1 + in[1]*0
	conv.Weight.Data[1] = 0
	conv.Weight.Data[2] = 0 // out[1] = in[0]*0 + in[1]*1
	conv.Weight.Data[3] = 1
	conv.Weight.Data[4] = 1 // out[2] = in[0]*1 + in[1]*1
	conv.Weight.Data[5] = 1

	input := NewTensor(1, 2, 2, 2)
	// Channel 0: all 1s, Channel 1: all 2s
	for i := 0; i < 4; i++ {
		input.Data[i] = 1   // channel 0
		input.Data[4+i] = 2 // channel 1
	}

	out := conv.Forward(input)
	if out.Shape[0] != 1 || out.Shape[1] != 3 || out.Shape[2] != 2 || out.Shape[3] != 2 {
		t.Fatalf("expected [1,3,2,2], got %v", out.Shape)
	}

	// out[0] = 1*1 + 0*2 = 1
	if math.Abs(float64(out.At(0, 0, 0, 0))-1.0) > 1e-5 {
		t.Errorf("out[0,0,0,0] = %f, expected 1.0", out.At(0, 0, 0, 0))
	}
	// out[1] = 0*1 + 1*2 = 2
	if math.Abs(float64(out.At(0, 1, 0, 0))-2.0) > 1e-5 {
		t.Errorf("out[0,1,0,0] = %f, expected 2.0", out.At(0, 1, 0, 0))
	}
	// out[2] = 1*1 + 1*2 = 3
	if math.Abs(float64(out.At(0, 2, 0, 0))-3.0) > 1e-5 {
		t.Errorf("out[0,2,0,0] = %f, expected 3.0", out.At(0, 2, 0, 0))
	}
}

func TestConv2dBNSiLU_3x3_WithPadding(t *testing.T) {
	// 3x3 conv, 1→1 channel, stride 1, padding 1 (same spatial size)
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(1, 1, 3, 3),
		Bias:    []float32{0},
		OutC:    1,
		InC:     1,
		KH:      3,
		KW:      3,
		Stride:  1,
		Padding: 1,
		UseSiLU: false,
	}
	// Set all weights to 1 → output = sum of 3x3 neighborhood
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = 1
	}

	// 4x4 input, all 1s
	input := NewTensor(1, 1, 4, 4)
	for i := range input.Data {
		input.Data[i] = 1
	}

	out := conv.Forward(input)
	// With padding=1, output should be 4x4
	if out.Shape[2] != 4 || out.Shape[3] != 4 {
		t.Fatalf("expected [1,1,4,4], got %v", out.Shape)
	}

	// Center pixel (1,1): full 3x3 neighborhood, all 1s → sum = 9
	if math.Abs(float64(out.At(0, 0, 1, 1))-9.0) > 1e-4 {
		t.Errorf("center = %f, expected 9.0", out.At(0, 0, 1, 1))
	}

	// Corner (0,0): 2x2 neighborhood (padding zeros) → sum = 4
	if math.Abs(float64(out.At(0, 0, 0, 0))-4.0) > 1e-4 {
		t.Errorf("corner = %f, expected 4.0", out.At(0, 0, 0, 0))
	}
}

func TestConv2dBNSiLU_Stride2(t *testing.T) {
	// 3x3 conv, stride 2, padding 1 → spatial size halved
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(1, 1, 3, 3),
		Bias:    []float32{0},
		OutC:    1,
		InC:     1,
		KH:      3,
		KW:      3,
		Stride:  2,
		Padding: 1,
		UseSiLU: false,
	}
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = 1
	}

	input := NewTensor(1, 1, 8, 8)
	for i := range input.Data {
		input.Data[i] = 1
	}

	out := conv.Forward(input)
	// (8 + 2*1 - 3) / 2 + 1 = 4
	if out.Shape[2] != 4 || out.Shape[3] != 4 {
		t.Fatalf("expected [1,1,4,4], got %v", out.Shape)
	}
}

func TestConv2dBNSiLU_WithSiLU(t *testing.T) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(1, 1, 1, 1),
		Bias:    []float32{0},
		OutC:    1,
		InC:     1,
		KH:      1,
		KW:      1,
		Stride:  1,
		Padding: 0,
		UseSiLU: true,
	}
	conv.Weight.Data[0] = 1 // identity

	input := NewTensor(1, 1, 1, 1)
	input.Data[0] = 2.0

	out := conv.Forward(input)
	// SiLU(2) = 2 * sigmoid(2) ≈ 2 * 0.8808 ≈ 1.7616
	expected := float32(2.0 * (1.0 / (1.0 + math.Exp(-2.0))))
	if math.Abs(float64(out.Data[0]-expected)) > 0.01 {
		t.Errorf("SiLU output = %f, expected %f", out.Data[0], expected)
	}
}
