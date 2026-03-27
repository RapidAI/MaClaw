// Package gguf implements a minimal GGUF v3 file reader for loading model
// weights and metadata. Only the subset needed for Gemma embedding is supported.
package gguf

import (
	"os"
)

const (
	Magic   uint32 = 0x46554747 // "GGUF" little-endian
	Version uint32 = 3
)

// Tensor type constants matching GGML.
const (
	TypeF32  uint32 = 0
	TypeF16  uint32 = 1
	TypeQ4_0 uint32 = 2
	TypeQ4_1 uint32 = 3
	TypeQ8_0 uint32 = 8
)

// BlockSize returns the block size for a quantized type.
func BlockSize(t uint32) int {
	switch t {
	case TypeF32:
		return 1
	case TypeF16:
		return 1
	case TypeQ4_0:
		return 32
	case TypeQ4_1:
		return 32
	case TypeQ8_0:
		return 32
	default:
		return 1
	}
}

// TypeSize returns bytes per block for a given type.
func TypeSize(t uint32) int {
	switch t {
	case TypeF32:
		return 4
	case TypeF16:
		return 2
	case TypeQ4_0:
		return 2 + 16 // scale(f16) + 16 bytes for 32 nibbles
	case TypeQ4_1:
		return 2 + 2 + 16 // scale(f16) + min(f16) + 16 bytes
	case TypeQ8_0:
		return 2 + 32 // scale(f16) + 32 bytes
	default:
		return 0
	}
}

// MetaValue holds a parsed GGUF metadata value.
type MetaValue struct {
	Type uint32
	// Only one of these is set depending on Type.
	U32 uint32
	I32 int32
	F32 float32
	U64 uint64
	Str string
	Arr []string // for string arrays
}

// TensorInfo describes a tensor in the GGUF file.
type TensorInfo struct {
	Name   string
	NDims  uint32
	Dims   [4]uint64
	Type   uint32
	Offset uint64 // offset from start of tensor data section
}

// NumElements returns the total number of elements.
func (t *TensorInfo) NumElements() uint64 {
	n := uint64(1)
	for i := uint32(0); i < t.NDims; i++ {
		n *= t.Dims[i]
	}
	return n
}

// ByteSize returns the total byte size of the tensor data.
func (t *TensorInfo) ByteSize() uint64 {
	ne := t.NumElements()
	bs := uint64(BlockSize(t.Type))
	ts := uint64(TypeSize(t.Type))
	return (ne / bs) * ts
}

// File represents a parsed GGUF file.
type File struct {
	Meta    map[string]MetaValue
	Tensors map[string]*TensorInfo

	dataOffset int64 // absolute file offset where tensor data begins
	f          *os.File
}
