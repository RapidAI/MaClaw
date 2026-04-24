// Package yolo provides a pure Go YOLOv8 inference engine.
// No CGo, no ONNX, no Python dependency.
package yolo

import (
	"fmt"
	"math"
)

// Tensor is a dense multi-dimensional float32 array in row-major (C) order.
// For CNN inference, the typical layout is [batch, channels, height, width].
// This is a minimal implementation — only operations needed by YOLOv8 are included.
type Tensor struct {
	Data   []float32
	Shape  []int // e.g. [1, 3, 640, 640]
	Stride []int // precomputed strides for indexing
}

// NewTensor creates a zero-initialized tensor with the given shape.
func NewTensor(shape ...int) *Tensor {
	size := 1
	for _, s := range shape {
		size *= s
	}
	stride := make([]int, len(shape))
	stride[len(shape)-1] = 1
	for i := len(shape) - 2; i >= 0; i-- {
		stride[i] = stride[i+1] * shape[i+1]
	}
	return &Tensor{
		Data:   make([]float32, size),
		Shape:  append([]int{}, shape...),
		Stride: stride,
	}
}

// NewTensorFrom creates a tensor from existing data (no copy).
func NewTensorFrom(data []float32, shape ...int) *Tensor {
	size := 1
	for _, s := range shape {
		size *= s
	}
	if len(shape) == 0 {
		// Scalar tensor
		size = 1
	}
	if len(data) != size {
		panic(fmt.Sprintf("tensor: data length %d != shape product %d", len(data), size))
	}
	stride := make([]int, len(shape))
	if len(shape) > 0 {
		stride[len(shape)-1] = 1
		for i := len(shape) - 2; i >= 0; i-- {
			stride[i] = stride[i+1] * shape[i+1]
		}
	}
	return &Tensor{
		Data:   data,
		Shape:  append([]int{}, shape...),
		Stride: stride,
	}
}

// Size returns the total number of elements.
func (t *Tensor) Size() int { return len(t.Data) }

// Dim returns the number of dimensions.
func (t *Tensor) Dim() int { return len(t.Shape) }

// At returns the value at the given indices.
func (t *Tensor) At(indices ...int) float32 {
	offset := 0
	for i, idx := range indices {
		offset += idx * t.Stride[i]
	}
	return t.Data[offset]
}

// Set sets the value at the given indices.
func (t *Tensor) Set(val float32, indices ...int) {
	offset := 0
	for i, idx := range indices {
		offset += idx * t.Stride[i]
	}
	t.Data[offset] = val
}

// Clone returns a deep copy.
func (t *Tensor) Clone() *Tensor {
	data := make([]float32, len(t.Data))
	copy(data, t.Data)
	return NewTensorFrom(data, t.Shape...)
}

// Reshape returns a view with a new shape (same underlying data).
// Total size must match.
func (t *Tensor) Reshape(shape ...int) *Tensor {
	size := 1
	for _, s := range shape {
		size *= s
	}
	if size != len(t.Data) {
		panic(fmt.Sprintf("reshape: new size %d != old size %d", size, len(t.Data)))
	}
	return NewTensorFrom(t.Data, shape...)
}

// ── Arithmetic operations (element-wise, in-place where possible) ──

// Add adds other element-wise: t = t + other. Shapes must match.
func (t *Tensor) Add(other *Tensor) {
	addInplace(t.Data, other.Data)
}

// AddScalar adds a scalar to all elements.
func (t *Tensor) AddScalar(s float32) {
	for i := range t.Data {
		t.Data[i] += s
	}
}

// Mul multiplies element-wise: t = t * other.
func (t *Tensor) Mul(other *Tensor) {
	for i := range t.Data {
		t.Data[i] *= other.Data[i]
	}
}

// MulScalar multiplies all elements by a scalar.
func (t *Tensor) MulScalar(s float32) {
	for i := range t.Data {
		t.Data[i] *= s
	}
}

// ── Activation functions ──

// SiLU applies SiLU (Swish) activation in-place: x * sigmoid(x).
// Uses standard math.Exp for precision — SiLU is <2% of total inference time,
// not worth trading precision for speed here.
func (t *Tensor) SiLU() {
	for i, v := range t.Data {
		t.Data[i] = v * sigmoid(v)
	}
}

// Sigmoid applies sigmoid activation in-place.
func (t *Tensor) Sigmoid() {
	for i, v := range t.Data {
		t.Data[i] = sigmoid(v)
	}
}

func sigmoid(x float32) float32 {
	return 1.0 / (1.0 + float32(math.Exp(float64(-x))))
}

// ── Channel-wise operations (for BatchNorm) ──

// AddChannelBias adds a per-channel bias to a [N, C, H, W] tensor.
// bias has shape [C].
func (t *Tensor) AddChannelBias(bias []float32) {
	if t.Dim() != 4 {
		panic("AddChannelBias: expected 4D tensor")
	}
	C := t.Shape[1]
	HW := t.Shape[2] * t.Shape[3]
	for n := 0; n < t.Shape[0]; n++ {
		for c := 0; c < C; c++ {
			b := bias[c]
			offset := n*t.Stride[0] + c*t.Stride[1]
			for i := 0; i < HW; i++ {
				t.Data[offset+i] += b
			}
		}
	}
}

// MulChannelScale multiplies a per-channel scale to a [N, C, H, W] tensor.
// scale has shape [C].
func (t *Tensor) MulChannelScale(scale []float32) {
	if t.Dim() != 4 {
		panic("MulChannelScale: expected 4D tensor")
	}
	C := t.Shape[1]
	HW := t.Shape[2] * t.Shape[3]
	for n := 0; n < t.Shape[0]; n++ {
		for c := 0; c < C; c++ {
			s := scale[c]
			offset := n*t.Stride[0] + c*t.Stride[1]
			for i := 0; i < HW; i++ {
				t.Data[offset+i] *= s
			}
		}
	}
}

// ── Slicing and concatenation ──

// SliceChannel returns a new tensor containing channels [start, end) from
// a [N, C, H, W] tensor. The returned tensor owns its own data.
func (t *Tensor) SliceChannel(start, end int) *Tensor {
	if t.Dim() != 4 {
		panic("SliceChannel: expected 4D tensor")
	}
	N, _, H, W := t.Shape[0], t.Shape[1], t.Shape[2], t.Shape[3]
	outC := end - start
	out := NewTensor(N, outC, H, W)
	HW := H * W
	for n := 0; n < N; n++ {
		for c := 0; c < outC; c++ {
			srcOff := n*t.Stride[0] + (start+c)*t.Stride[1]
			dstOff := n*out.Stride[0] + c*out.Stride[1]
			copy(out.Data[dstOff:dstOff+HW], t.Data[srcOff:srcOff+HW])
		}
	}
	return out
}

// ConcatChannel concatenates tensors along the channel dimension.
// All tensors must have the same N, H, W.
func ConcatChannel(tensors ...*Tensor) *Tensor {
	if len(tensors) == 0 {
		panic("ConcatChannel: no tensors")
	}
	N, H, W := tensors[0].Shape[0], tensors[0].Shape[2], tensors[0].Shape[3]
	totalC := 0
	for _, t := range tensors {
		totalC += t.Shape[1]
	}
	out := NewTensor(N, totalC, H, W)
	HW := H * W
	dstC := 0
	for _, t := range tensors {
		C := t.Shape[1]
		for n := 0; n < N; n++ {
			for c := 0; c < C; c++ {
				srcOff := n*t.Stride[0] + c*t.Stride[1]
				dstOff := n*out.Stride[0] + (dstC+c)*out.Stride[1]
				copy(out.Data[dstOff:dstOff+HW], t.Data[srcOff:srcOff+HW])
			}
		}
		dstC += C
	}
	return out
}

// MaxPool2d applies max pooling with the given kernel size, stride, and padding.
// Input shape: [N, C, H, W]. Returns a new tensor.
func (t *Tensor) MaxPool2d(kernel, stride, padding int) *Tensor {
	N, C, H, W := t.Shape[0], t.Shape[1], t.Shape[2], t.Shape[3]
	outH := (H+2*padding-kernel)/stride + 1
	outW := (W+2*padding-kernel)/stride + 1
	out := NewTensor(N, C, outH, outW)

	for n := 0; n < N; n++ {
		for c := 0; c < C; c++ {
			for oh := 0; oh < outH; oh++ {
				for ow := 0; ow < outW; ow++ {
					maxVal := float32(-math.MaxFloat32)
					for kh := 0; kh < kernel; kh++ {
						for kw := 0; kw < kernel; kw++ {
							ih := oh*stride - padding + kh
							iw := ow*stride - padding + kw
							if ih >= 0 && ih < H && iw >= 0 && iw < W {
								v := t.At(n, c, ih, iw)
								if v > maxVal {
									maxVal = v
								}
							}
						}
					}
					out.Set(maxVal, n, c, oh, ow)
				}
			}
		}
	}
	return out
}

// Upsample2x performs nearest-neighbor 2x upsampling on a [N, C, H, W] tensor.
func (t *Tensor) Upsample2x() *Tensor {
	N, C, H, W := t.Shape[0], t.Shape[1], t.Shape[2], t.Shape[3]
	out := NewTensor(N, C, H*2, W*2)
	for n := 0; n < N; n++ {
		for c := 0; c < C; c++ {
			for h := 0; h < H; h++ {
				for w := 0; w < W; w++ {
					v := t.At(n, c, h, w)
					out.Set(v, n, c, h*2, w*2)
					out.Set(v, n, c, h*2, w*2+1)
					out.Set(v, n, c, h*2+1, w*2)
					out.Set(v, n, c, h*2+1, w*2+1)
				}
			}
		}
	}
	return out
}

// Softmax applies softmax along the given axis.
func (t *Tensor) Softmax(axis int) *Tensor {
	out := t.Clone()
	shape := t.Shape
	outerSize := 1
	for i := 0; i < axis; i++ {
		outerSize *= shape[i]
	}
	axisSize := shape[axis]
	innerSize := 1
	for i := axis + 1; i < len(shape); i++ {
		innerSize *= shape[i]
	}

	for outer := 0; outer < outerSize; outer++ {
		for inner := 0; inner < innerSize; inner++ {
			// Find max for numerical stability
			maxVal := float32(-math.MaxFloat32)
			for a := 0; a < axisSize; a++ {
				idx := outer*axisSize*innerSize + a*innerSize + inner
				if out.Data[idx] > maxVal {
					maxVal = out.Data[idx]
				}
			}
			// Exp and sum
			sum := float32(0)
			for a := 0; a < axisSize; a++ {
				idx := outer*axisSize*innerSize + a*innerSize + inner
				out.Data[idx] = float32(math.Exp(float64(out.Data[idx] - maxVal)))
				sum += out.Data[idx]
			}
			// Normalize
			for a := 0; a < axisSize; a++ {
				idx := outer*axisSize*innerSize + a*innerSize + inner
				out.Data[idx] /= sum
			}
		}
	}
	return out
}

// Transpose2D transposes the last two dimensions of a [..., M, N] tensor to [..., N, M].
func (t *Tensor) Transpose2D() *Tensor {
	ndim := len(t.Shape)
	if ndim < 2 {
		panic("Transpose2D: need at least 2 dimensions")
	}
	M := t.Shape[ndim-2]
	N := t.Shape[ndim-1]
	newShape := make([]int, ndim)
	copy(newShape, t.Shape)
	newShape[ndim-2] = N
	newShape[ndim-1] = M

	out := NewTensor(newShape...)
	batchSize := 1
	for i := 0; i < ndim-2; i++ {
		batchSize *= t.Shape[i]
	}
	for b := 0; b < batchSize; b++ {
		srcOff := b * M * N
		dstOff := b * N * M
		for m := 0; m < M; m++ {
			for n := 0; n < N; n++ {
				out.Data[dstOff+n*M+m] = t.Data[srcOff+m*N+n]
			}
		}
	}
	return out
}
