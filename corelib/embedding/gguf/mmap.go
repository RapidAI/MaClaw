// Package gguf — mmap-based GGUF file reader.
// Maps the entire file into virtual memory so tensor data can be accessed
// as []byte slices without copying into the Go heap.
package gguf

import (
	"fmt"
	"os"
	"unsafe"
)

// MmapFile holds a memory-mapped GGUF file.
type MmapFile struct {
	File                  // embedded parsed header
	data   []byte         // mmap region covering the entire file
	handle uintptr        // OS handle for cleanup (Windows: HANDLE from CreateFileMapping)
	osFile *os.File       // kept open for the mapping lifetime
}

// OpenMmap opens a GGUF file using memory mapping.
// The returned MmapFile must be closed with CloseMmap.
func OpenMmap(path string) (*MmapFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		f.Close()
		return nil, fmt.Errorf("gguf: empty file")
	}

	data, handle, err := mmapFile(f, int(size))
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("gguf: mmap: %w", err)
	}

	mf := &MmapFile{
		data:   data,
		handle: handle,
		osFile: f,
	}
	mf.File.Meta = make(map[string]MetaValue)
	mf.File.Tensors = make(map[string]*TensorInfo)
	mf.File.f = f // needed by parseHeader which uses SectionReader

	if err := mf.File.parseHeader(); err != nil {
		mf.CloseMmap()
		return nil, err
	}
	return mf, nil
}

// CloseMmap releases the memory mapping and closes the file.
func (mf *MmapFile) CloseMmap() {
	if mf.data != nil {
		munmapFile(mf.data, mf.handle)
		mf.data = nil
	}
	if mf.osFile != nil {
		mf.osFile.Close()
		mf.osFile = nil
	}
	mf.File.f = nil
}

// TensorRawBytes returns a byte slice pointing directly into the mmap region
// for the named tensor. No copy, no allocation.
// Note: uses int arithmetic for offsets, safe for files up to ~2GB on 32-bit.
// The target model is ~300MB so this is not a concern.
func (mf *MmapFile) TensorRawBytes(name string) ([]byte, *TensorInfo, error) {
	ti, ok := mf.File.Tensors[name]
	if !ok {
		return nil, nil, fmt.Errorf("gguf: tensor %q not found", name)
	}
	start := int(mf.File.dataOffset) + int(ti.Offset)
	end := start + int(ti.ByteSize())
	if end > len(mf.data) {
		return nil, nil, fmt.Errorf("gguf: tensor %q exceeds file bounds", name)
	}
	return mf.data[start:end], ti, nil
}

// TensorF32 reads a small tensor (norm weights, etc.) and dequantizes to float32.
// Uses the mmap data directly — no file I/O.
func (mf *MmapFile) TensorF32(name string) ([]float32, error) {
	raw, ti, err := mf.TensorRawBytes(name)
	if err != nil {
		return nil, err
	}
	ne := ti.NumElements()
	out := make([]float32, ne)
	switch ti.Type {
	case TypeF32:
		// GGUF data section is 32-byte aligned and F32 tensors are naturally
		// 4-byte aligned, so unsafe cast is safe here.
		src := unsafe.Slice((*float32)(unsafe.Pointer(&raw[0])), ne)
		copy(out, src)
	case TypeF16:
		for i := uint64(0); i < ne; i++ {
			out[i] = float16to32(uint16(raw[i*2]) | uint16(raw[i*2+1])<<8)
		}
	case TypeQ8_0:
		dequantQ8_0(raw, out)
	case TypeQ4_0:
		dequantQ4_0(raw, out)
	default:
		return nil, fmt.Errorf("gguf: unsupported type %d for %q", ti.Type, name)
	}
	return out, nil
}
