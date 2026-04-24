package yolo

import (
	"fmt"
	"os"
	"testing"
)

// BenchmarkForward profiles the full forward pass to identify bottlenecks.
func BenchmarkForward(b *testing.B) {
	weightsPath := "weights/omniparser-v2.yolow"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		b.Skip("weights not found")
	}

	model, err := LoadModel(weightsPath)
	if err != nil {
		b.Fatalf("LoadModel: %v", err)
	}

	input := NewTensor(1, 3, 640, 640)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.Forward(input)
	}
}

// BenchmarkConv2d_Large benchmarks the largest conv in the model:
// first layer: 3→64, 3x3, stride 2, input 640x640
func BenchmarkConv2d_Large(b *testing.B) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(64, 3, 3, 3),
		Bias:    make([]float32, 64),
		OutC:    64, InC: 3, KH: 3, KW: 3,
		Stride:  2, Padding: 1, Groups: 1, UseSiLU: true,
	}
	// Random-ish weights
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = float32(i%7-3) * 0.1
	}

	input := NewTensor(1, 3, 640, 640)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv.Forward(input)
	}
}

// BenchmarkConv2d_Mid benchmarks a mid-network conv:
// 256→256, 3x3, stride 2, input 80x80
func BenchmarkConv2d_Mid(b *testing.B) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(256, 256, 3, 3),
		Bias:    make([]float32, 256),
		OutC:    256, InC: 256, KH: 3, KW: 3,
		Stride:  2, Padding: 1, Groups: 1, UseSiLU: true,
	}
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = float32(i%7-3) * 0.01
	}

	input := NewTensor(1, 256, 80, 80)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv.Forward(input)
	}
}

// BenchmarkConv2d_1x1 benchmarks a 1x1 conv (no im2col needed):
// 512→512, 1x1, stride 1, input 20x20
func BenchmarkConv2d_1x1(b *testing.B) {
	conv := &Conv2dBNSiLU{
		Weight:  NewTensor(512, 512, 1, 1),
		Bias:    make([]float32, 512),
		OutC:    512, InC: 512, KH: 1, KW: 1,
		Stride:  1, Padding: 0, Groups: 1, UseSiLU: true,
	}
	for i := range conv.Weight.Data {
		conv.Weight.Data[i] = float32(i%7-3) * 0.01
	}

	input := NewTensor(1, 512, 20, 20)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv.Forward(input)
	}
}

// BenchmarkTranspose benchmarks the col transpose for the largest conv.
func BenchmarkTranspose_Large(b *testing.B) {
	K := 3 * 3 * 3   // colSize for 3→64, 3x3
	N := 320 * 320    // spatialSize for 640→320 (stride 2)
	col := make([]float32, K*N)
	for i := range col {
		col[i] = float32(i % 100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transposeParallel(col, K, N)
	}
}

// BenchmarkTranspose_Mid benchmarks transpose for mid-network conv.
func BenchmarkTranspose_Mid(b *testing.B) {
	K := 256 * 3 * 3  // colSize for 256→256, 3x3
	N := 40 * 40       // spatialSize for 80→40 (stride 2)
	col := make([]float32, K*N)
	for i := range col {
		col[i] = float32(i % 100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transposeParallel(col, K, N)
	}
}

// BenchmarkMatmul benchmarks the matmul for mid-network conv.
func BenchmarkMatmul_Mid(b *testing.B) {
	M := 256           // OutC
	K := 256 * 3 * 3   // colSize
	N := 40 * 40        // spatialSize
	W := make([]float32, M*K)
	col := make([]float32, K*N)
	bias := make([]float32, M)
	out := make([]float32, M*N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matmulConv(W, col, bias, out, M, K, N)
	}
}

func BenchmarkIm2col_Mid(b *testing.B) {
	input := NewTensor(1, 256, 80, 80)
	for i := range input.Data {
		input.Data[i] = float32(i % 100)
	}
	outH, outW := 40, 40
	dst := make([]float32, 256*3*3*outH*outW)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		im2colParallel(input, 0, 3, 3, 2, 1, outH, outW, dst)
	}
}

func init() {
	_ = fmt.Sprintf // suppress unused import
}
