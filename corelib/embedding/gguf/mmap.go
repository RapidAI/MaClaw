// Package gguf — mmap-based GGUF file reader.
// Maps the entire file into virtual memory so tensor data can be accessed
// as []byte slices without copying into the Go heap.
package gguf

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"unsafe"
)

// NoMmapEnv=1 skips mmap and reads the whole file into the heap (diagnostic /
// rollback switch for hosts where file mapping misbehaves).
const NoMmapEnv = "MACLAW_GGUF_NO_MMAP"

// mmapFileFn is swappable in tests to simulate a mapping failure.
var mmapFileFn = mmapFile

// MmapFile holds a memory-mapped GGUF file.
type MmapFile struct {
	File            // embedded parsed header
	data   []byte   // mmap region covering the entire file
	handle uintptr  // OS handle for cleanup (Windows: HANDLE from CreateFileMapping)
	osFile *os.File // kept open for the mapping lifetime
	heap   bool     // data is a heap copy (mmap failed or was disabled)
}

// Heap reports whether the file content was read into the heap because
// mapping failed or was disabled (NoMmapEnv). Release is a no-op then.
func (mf *MmapFile) Heap() bool { return mf != nil && mf.heap }

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

	var (
		data   []byte
		handle uintptr
		heap   bool
	)
	if v := strings.TrimSpace(os.Getenv(NoMmapEnv)); v == "1" || strings.EqualFold(v, "true") {
		err = fmt.Errorf("disabled by %s", NoMmapEnv)
	} else {
		data, handle, err = mmapFileFn(f, int(size))
	}
	if err != nil {
		// Fallback: read the file into the heap. Costs RSS (the whole file
		// becomes anonymous memory) but keeps the model usable on hosts where
		// mapping fails (address-space limits, odd filesystems, AV hooks).
		mapErr := err
		data, err = readAllAt(f, size)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("gguf: mmap: %v; heap read fallback: %w", mapErr, err)
		}
		log.Printf("[gguf] mmap unavailable for %s (%v); loaded %d bytes into heap", path, mapErr, size)
		handle, heap = 0, true
	}

	mf := &MmapFile{
		data:   data,
		handle: handle,
		osFile: f,
		heap:   heap,
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
		if !mf.heap {
			munmapFile(mf.data, mf.handle)
		}
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

// Release hints that the mapped bytes b (a sub-slice of this file's mapping,
// e.g. a tensor already copied elsewhere) are no longer needed in memory.
// Content is unchanged; pages fault back in if read again.
func (mf *MmapFile) Release(b []byte) {
	if mf == nil || mf.heap || len(b) == 0 || len(mf.data) == 0 {
		// Heap copies must never be madvise(DONTNEED)'d: anonymous pages
		// would read back as zeros.
		return
	}
	lo := uintptr(unsafe.Pointer(unsafe.SliceData(mf.data)))
	p := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	if p < lo || p+uintptr(len(b)) > lo+uintptr(len(mf.data)) {
		return // not ours (heap copy)
	}
	releasePages(b)
}

// readAllAt reads the whole file into a fresh heap buffer.
func readAllAt(f *os.File, size int64) (buf []byte, err error) {
	defer func() {
		if r := recover(); r != nil { // make() of a huge buffer can panic on 32-bit / OOM-limited hosts
			buf, err = nil, fmt.Errorf("allocate %d bytes: %v", size, r)
		}
	}()
	if int64(int(size)) != size {
		return nil, fmt.Errorf("file too large for this platform (%d bytes)", size)
	}
	buf = make([]byte, int(size))
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, size), buf); err != nil {
		return nil, err
	}
	return buf, nil
}
