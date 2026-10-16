package yolo

import (
	"os"
	"testing"
)

// BenchmarkForward1280 measures the full forward pass at the production
// input size (the OmniParser V2 weights declare InputSize=1280).
func BenchmarkForward1280(b *testing.B) {
	weightsPath := "weights/omniparser-v2.yolow"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		b.Skip("weights not found")
	}

	model, err := LoadModel(weightsPath)
	if err != nil {
		b.Fatalf("LoadModel: %v", err)
	}

	input := NewTensor(1, 3, 1280, 1280)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.Forward(input)
	}
}

// BenchmarkConv2d_1280Large benchmarks the largest conv at 1280 input:
// first layer 3→64, 3x3, stride 2, input 1280x1280.
func BenchmarkConv2d_1280Large(b *testing.B) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(64, 3, 3, 3),
		Bias:    make([]float32, 64),
		OutC:    64, InC: 3, KH: 3, KW: 3,
		Stride: 2, Padding: 1, Groups: 1, UseSiLU: true,
	}
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = float32(i%7-3) * 0.1
	}

	input := NewTensor(1, 3, 1280, 1280)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv.Forward(input)
	}
}

// BenchmarkConv2d_160sq benchmarks the C3k2 bottleneck conv shape at
// 320x320 spatial (1280-input backbone): 128→128, 3x3, stride 1.
func BenchmarkConv2d_320sq(b *testing.B) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(128, 128, 3, 3),
		Bias:    make([]float32, 128),
		OutC:    128, InC: 128, KH: 3, KW: 3,
		Stride: 1, Padding: 1, Groups: 1, UseSiLU: true,
	}
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = float32(i%7-3) * 0.01
	}

	input := NewTensor(1, 128, 320, 320)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv.Forward(input)
	}
}
