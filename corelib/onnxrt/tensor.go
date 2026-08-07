package onnxrt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// DType is the element type of a Tensor.
type DType int

const (
	// DFloat32 is a float32 tensor.
	DFloat32 DType = iota
	// DInt64 is an int64 tensor (shape tensors, indices, constants).
	DInt64
)

func (d DType) String() string {
	switch d {
	case DFloat32:
		return "float32"
	case DInt64:
		return "int64"
	default:
		return fmt.Sprintf("dtype(%d)", int(d))
	}
}

// Tensor is a dense, contiguous, row-major N-D tensor.
// Exactly one of F32/I64 is non-nil, selected by DType.
type Tensor struct {
	Shape []int
	DType DType
	F32   []float32
	I64   []int64
	// abuf marks F32 as checked out of a Run's output arena (abuf == F32);
	// the Run frees the buffer when the holding value's liveness group dies.
	// Only the tensor that newFloat handed the buffer to carries the mark at
	// any time — freeing clears it, so a buffer is never returned twice.
	abuf []float32
}

// NewFloat returns a zeroed float32 tensor with the given shape.
func NewFloat(shape ...int) *Tensor {
	n := numElements(shape)
	return &Tensor{Shape: cloneInts(shape), DType: DFloat32, F32: make([]float32, n)}
}

// NewInt returns a zeroed int64 tensor with the given shape.
func NewInt(shape ...int) *Tensor {
	n := numElements(shape)
	return &Tensor{Shape: cloneInts(shape), DType: DInt64, I64: make([]int64, n)}
}

// FloatFrom wraps existing float32 data (copied) as a tensor.
func FloatFrom(data []float32, shape ...int) *Tensor {
	t := NewFloat(shape...)
	copy(t.F32, data)
	return t
}

// IntFrom wraps existing int64 data (copied) as a tensor.
func IntFrom(data []int64, shape ...int) *Tensor {
	t := NewInt(shape...)
	copy(t.I64, data)
	return t
}

// ScalarInt returns a 1-element int64 tensor.
func ScalarInt(v int64) *Tensor { return IntFrom([]int64{v}, 1) }

func numElements(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

// maxTensorElements caps the element count of any single tensor declared by a
// model file (≈8 GiB of float32). Real models are far below this; the cap only
// stops hostile models from declaring absurd shapes.
const maxTensorElements = 1 << 31

// checkedShape converts proto dims to a Tensor shape, rejecting negative or
// absurd dims and detecting product overflow. Call this at model-load time on
// any dims coming out of a model file; numElements on the result is then safe.
func checkedShape(dims []int64) (shape []int, n int, err error) {
	shape = make([]int, len(dims))
	n = 1
	for i, d := range dims {
		if d < 0 || d > maxTensorElements {
			return nil, 0, fmt.Errorf("onnxrt: invalid tensor dim %d at index %d", d, i)
		}
		shape[i] = int(d)
		if d > 0 {
			if n > maxTensorElements/int(d) {
				return nil, 0, fmt.Errorf("onnxrt: tensor dims %v overflow the element cap", dims)
			}
			n *= int(d)
		} else {
			n = 0
		}
	}
	return shape, n, nil
}

func cloneInts(s []int) []int {
	out := make([]int, len(s))
	copy(out, s)
	return out
}

// NumElements returns the total element count.
func (t *Tensor) NumElements() int { return numElements(t.Shape) }

// Rank returns the number of dimensions.
func (t *Tensor) Rank() int { return len(t.Shape) }

// Clone returns a deep copy.
func (t *Tensor) Clone() *Tensor {
	out := &Tensor{Shape: cloneInts(t.Shape), DType: t.DType}
	if t.DType == DFloat32 {
		out.F32 = make([]float32, len(t.F32))
		copy(out.F32, t.F32)
	} else {
		out.I64 = make([]int64, len(t.I64))
		copy(out.I64, t.I64)
	}
	return out
}

// Reshape returns a tensor with the same underlying data and a new shape.
// The data is shared (view semantics); the caller must ensure the element
// count matches. Safe because tensors are always contiguous row-major.
// The arena mark (if any) is shared with the view: kernels reshape a freshly
// allocated output and drop the original handle (MatMul/ReduceMean 1-D
// promotion), and the Run frees each buffer exactly once via its liveness
// group's root value, so a copied mark on a view is never used for freeing.
func (t *Tensor) Reshape(shape ...int) *Tensor {
	if numElements(shape) != t.NumElements() {
		panic(fmt.Sprintf("onnxrt: reshape %v -> %v: element count mismatch", t.Shape, shape))
	}
	out := &Tensor{Shape: cloneInts(shape), DType: t.DType, F32: t.F32, I64: t.I64, abuf: t.abuf}
	return out
}

// Floats returns the float32 payload, converting from int64 if necessary.
func (t *Tensor) Floats() ([]float32, error) {
	switch t.DType {
	case DFloat32:
		return t.F32, nil
	case DInt64:
		out := make([]float32, len(t.I64))
		for i, v := range t.I64 {
			out[i] = float32(v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("onnxrt: unsupported dtype %v", t.DType)
}

// Ints returns the int64 payload, converting from float32 (truncating) if
// necessary. Used for shape/index inputs which may arrive as either type.
func (t *Tensor) Ints() ([]int64, error) {
	switch t.DType {
	case DInt64:
		return t.I64, nil
	case DFloat32:
		out := make([]int64, len(t.F32))
		for i, v := range t.F32 {
			out[i] = int64(v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("onnxrt: unsupported dtype %v", t.DType)
}

// ---------------------------------------------------------------------------
// broadcasting
// ---------------------------------------------------------------------------

// broadcastShapes computes the NumPy-style broadcast of two shapes.
func broadcastShapes(a, b []int) ([]int, error) {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		da, db := 1, 1
		if i < len(a) {
			da = a[len(a)-1-i]
		}
		if i < len(b) {
			db = b[len(b)-1-i]
		}
		switch {
		case da == db:
			out[n-1-i] = da
		case da == 1:
			out[n-1-i] = db
		case db == 1:
			out[n-1-i] = da
		default:
			return nil, fmt.Errorf("onnxrt: shapes %v and %v are not broadcastable", a, b)
		}
	}
	return out, nil
}

// broadcastStrides returns element strides for walking srcShape (right-aligned)
// against the iteration over outShape. Broadcast dims get stride 0.
func broadcastStrides(srcShape, outShape []int) []int {
	strides := make([]int, len(outShape))
	// contiguous strides of src
	srcStrides := make([]int, len(srcShape))
	acc := 1
	for i := len(srcShape) - 1; i >= 0; i-- {
		srcStrides[i] = acc
		acc *= srcShape[i]
	}
	off := len(outShape) - len(srcShape)
	for i := range outShape {
		si := i - off
		if si < 0 || srcShape[si] == 1 {
			strides[i] = 0
		} else {
			strides[i] = srcStrides[si]
		}
	}
	return strides
}

// shapeEqual reports whether two shapes are identical.
func shapeEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var errShapeMismatch = errors.New("onnxrt: shape mismatch")

// binaryInt computes an elementwise int64 op with NumPy broadcasting.
func binaryInt(a, b *Tensor, op func(x, y int64) int64) (*Tensor, error) {
	outShape, err := broadcastShapes(a.Shape, b.Shape)
	if err != nil {
		return nil, err
	}
	ai, err := a.Ints()
	if err != nil {
		return nil, err
	}
	bi, err := b.Ints()
	if err != nil {
		return nil, err
	}
	out := NewInt(outShape...)
	n := out.NumElements()
	if shapeEqual(a.Shape, outShape) && shapeEqual(b.Shape, outShape) {
		for i := 0; i < n; i++ {
			out.I64[i] = op(ai[i], bi[i])
		}
		return out, nil
	}
	sa := broadcastStrides(a.Shape, outShape)
	sb := broadcastStrides(b.Shape, outShape)
	broadcastLoop(outShape, sa, sb, func(x, y, oi int) {
		out.I64[oi] = op(ai[x], bi[y])
	})
	return out, nil
}

// broadcastLoop iterates all output elements, calling fn with the source
// offsets into a and b and the output offset.
func broadcastLoop(outShape []int, sa, sb []int, fn func(ai, bi, oi int)) {
	n := numElements(outShape)
	nd := len(outShape)
	if nd == 0 {
		fn(0, 0, 0)
		return
	}
	idx := make([]int, nd)
	ai, bi := 0, 0
	for oi := 0; oi < n; oi++ {
		fn(ai, bi, oi)
		// increment multi-index from the last dim
		for d := nd - 1; d >= 0; d-- {
			idx[d]++
			ai += sa[d]
			bi += sb[d]
			if idx[d] < outShape[d] {
				break
			}
			idx[d] = 0
			ai -= sa[d] * outShape[d]
			bi -= sb[d] * outShape[d]
		}
	}
}

// decodeFloat16 converts an IEEE 754 half-precision value to float32.
func decodeFloat16(h uint16) float32 {
	s := uint32(h>>15) & 1
	e := uint32(h>>10) & 0x1f
	m := uint32(h) & 0x3ff
	var f float64
	switch {
	case e == 0:
		f = math.Ldexp(float64(m), -24)
	case e == 0x1f:
		if m == 0 {
			f = math.Inf(1)
		} else {
			f = math.NaN()
		}
	default:
		f = math.Ldexp(float64(m)+1024, int(e)-25)
	}
	if s == 1 {
		f = -f
	}
	return float32(f)
}

// decodeRawFloats decodes little-endian raw bytes as float32 elements of the
// given ONNX element type.
func decodeRawFloats(raw []byte, dt TensorDataType) ([]float32, error) {
	switch dt {
	case TypeFloat:
		if len(raw)%4 != 0 {
			return nil, fmt.Errorf("onnxrt: float raw_data length %d not a multiple of 4", len(raw))
		}
		out := make([]float32, len(raw)/4)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return out, nil
	case TypeFloat16:
		if len(raw)%2 != 0 {
			return nil, fmt.Errorf("onnxrt: float16 raw_data length %d not a multiple of 2", len(raw))
		}
		out := make([]float32, len(raw)/2)
		for i := range out {
			out[i] = decodeFloat16(binary.LittleEndian.Uint16(raw[i*2:]))
		}
		return out, nil
	case TypeDouble:
		if len(raw)%8 != 0 {
			return nil, fmt.Errorf("onnxrt: double raw_data length %d not a multiple of 8", len(raw))
		}
		out := make([]float32, len(raw)/8)
		for i := range out {
			out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:])))
		}
		return out, nil
	}
	return nil, fmt.Errorf("onnxrt: cannot decode raw_data of %s as floats", dt)
}

// decodeRawInts decodes little-endian raw bytes as int64 elements.
func decodeRawInts(raw []byte, dt TensorDataType) ([]int64, error) {
	switch dt {
	case TypeInt64:
		if len(raw)%8 != 0 {
			return nil, fmt.Errorf("onnxrt: int64 raw_data length %d not a multiple of 8", len(raw))
		}
		out := make([]int64, len(raw)/8)
		for i := range out {
			out[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
		}
		return out, nil
	case TypeInt32:
		if len(raw)%4 != 0 {
			return nil, fmt.Errorf("onnxrt: int32 raw_data length %d not a multiple of 4", len(raw))
		}
		out := make([]int64, len(raw)/4)
		for i := range out {
			out[i] = int64(int32(binary.LittleEndian.Uint32(raw[i*4:])))
		}
		return out, nil
	}
	return nil, fmt.Errorf("onnxrt: cannot decode raw_data of %s as ints", dt)
}
