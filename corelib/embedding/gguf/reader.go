package gguf

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

// Open parses a GGUF file header, metadata, and tensor index.
// The returned File keeps the underlying os.File open for lazy tensor reads.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gf := &File{
		Meta:    make(map[string]MetaValue),
		Tensors: make(map[string]*TensorInfo),
		f:       f,
	}
	if err := gf.parseHeader(); err != nil {
		f.Close()
		return nil, err
	}
	return gf, nil
}

// Close releases the underlying file.
func (gf *File) Close() {
	if gf.f != nil {
		gf.f.Close()
		gf.f = nil
	}
}

// ReadTensorF32 reads a tensor and returns its data as float32 slice.
// Handles F32, F16, Q8_0 dequantization.
func (gf *File) ReadTensorF32(name string) ([]float32, error) {
	ti, ok := gf.Tensors[name]
	if !ok {
		return nil, fmt.Errorf("gguf: tensor %q not found", name)
	}
	ne := ti.NumElements()
	raw := make([]byte, ti.ByteSize())
	if _, err := gf.f.ReadAt(raw, gf.dataOffset+int64(ti.Offset)); err != nil {
		return nil, fmt.Errorf("gguf: read tensor %q: %w", name, err)
	}
	out := make([]float32, ne)
	switch ti.Type {
	case TypeF32:
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	case TypeF16:
		for i := range out {
			out[i] = float16to32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	case TypeQ8_0:
		dequantQ8_0(raw, out)
	case TypeQ4_0:
		dequantQ4_0(raw, out)
	default:
		return nil, fmt.Errorf("gguf: unsupported tensor type %d for %q", ti.Type, name)
	}
	return out, nil
}

// countReader counts bytes pulled from the underlying reader.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (gf *File) parseHeader() error {
	// Tokenizer vocab is a few hundred thousand short strings. A SectionReader
	// turns each binary.Read into its own ReadAt, which dominated model load.
	// Buffer the header and recover the logical offset from bytes still sitting
	// unread in the buffer, so the tensor data alignment does not move.
	sr := io.NewSectionReader(gf.f, 0, 1<<62)
	cr := &countReader{r: sr}
	br := bufio.NewReaderSize(cr, 1<<20)
	r := io.Reader(br)
	var magic, version uint32
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return fmt.Errorf("gguf: read magic: %w", err)
	}
	if magic != Magic && magic != 0x46475547 /* legacy RapidSpeech magic */ {
		return fmt.Errorf("gguf: bad magic %08x", magic)
	}
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version < 2 || version > 3 {
		return fmt.Errorf("gguf: unsupported version %d", version)
	}
	var nTensors, nMeta uint64
	if err := binary.Read(r, binary.LittleEndian, &nTensors); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &nMeta); err != nil {
		return err
	}

	// Parse metadata KV pairs
	for i := uint64(0); i < nMeta; i++ {
		key, err := readString(r)
		if err != nil {
			return fmt.Errorf("gguf: meta key %d: %w", i, err)
		}
		val, err := readMetaValue(r)
		if err != nil {
			return fmt.Errorf("gguf: meta val %q: %w", key, err)
		}
		gf.Meta[key] = val
	}

	// Parse tensor infos
	for i := uint64(0); i < nTensors; i++ {
		ti, err := readTensorInfo(r)
		if err != nil {
			return fmt.Errorf("gguf: tensor info %d: %w", i, err)
		}
		gf.Tensors[ti.Name] = ti
	}

	// Data section starts at the next 32-byte aligned offset. br has read
	// ahead, so the file position is cr.n and the parser position is that
	// minus the unread buffer.
	gf.dataOffset = align32(cr.n - int64(br.Buffered()))
	return nil
}

func align32(offset int64) int64 {
	return (offset + 31) & ^int64(31)
}

func readString(r io.Reader) (string, error) {
	var length uint64
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length > 1<<20 {
		return "", fmt.Errorf("string too long: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readMetaValue(r io.Reader) (MetaValue, error) {
	var vtype uint32
	if err := binary.Read(r, binary.LittleEndian, &vtype); err != nil {
		return MetaValue{}, err
	}
	mv := MetaValue{Type: vtype}
	switch vtype {
	case 0: // UINT8
		var v uint8
		binary.Read(r, binary.LittleEndian, &v)
		mv.U32 = uint32(v)
	case 1: // INT8
		var v int8
		binary.Read(r, binary.LittleEndian, &v)
		mv.I32 = int32(v)
	case 2: // UINT16
		var v uint16
		binary.Read(r, binary.LittleEndian, &v)
		mv.U32 = uint32(v)
	case 3: // INT16
		var v int16
		binary.Read(r, binary.LittleEndian, &v)
		mv.I32 = int32(v)
	case 4: // UINT32
		binary.Read(r, binary.LittleEndian, &mv.U32)
	case 5: // INT32
		binary.Read(r, binary.LittleEndian, &mv.I32)
	case 6: // FLOAT32
		binary.Read(r, binary.LittleEndian, &mv.F32)
	case 7: // BOOL
		var v uint8
		binary.Read(r, binary.LittleEndian, &v)
		mv.U32 = uint32(v)
	case 8: // STRING
		s, err := readString(r)
		if err != nil {
			return mv, err
		}
		mv.Str = s
	case 9: // ARRAY
		arr, err := readArray(r)
		if err != nil {
			return mv, err
		}
		mv.Arr = arr
	case 10: // UINT64
		binary.Read(r, binary.LittleEndian, &mv.U64)
	case 11: // INT64
		var v int64
		binary.Read(r, binary.LittleEndian, &v)
		mv.I32 = int32(v) // lossy but fine for our use
	case 12: // FLOAT64
		var v float64
		binary.Read(r, binary.LittleEndian, &v)
		mv.F32 = float32(v)
	default:
		return mv, fmt.Errorf("unknown meta type %d", vtype)
	}
	return mv, nil
}

func readArray(r io.Reader) ([]string, error) {
	var elemType uint32
	var count uint64
	binary.Read(r, binary.LittleEndian, &elemType)
	binary.Read(r, binary.LittleEndian, &count)
	if elemType == 8 { // string array
		if count > maxMetaStringArrayCount {
			return nil, fmt.Errorf("gguf: meta string array count %d too large (corrupt header?)", count)
		}
		out := make([]string, count)
		for i := uint64(0); i < count; i++ {
			s, err := readString(r)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	// Float32 arrays — store for later retrieval
	if elemType == 6 {
		if count > maxF32ArrayCount {
			return nil, fmt.Errorf("gguf: float32 array count %d too large (corrupt header?)", count)
		}
		buf := make([]byte, count*4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		// Parse into a local slice and publish once: writing the package-global
		// element-by-element let a concurrent LastF32Array (or a second parser
		// with count=0) shrink it mid-loop and panic the whole process.
		arr := make([]float32, count)
		for i := uint64(0); i < count; i++ {
			arr[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
		}
		lastF32Mu.Lock()
		lastF32Array = arr
		lastF32Mu.Unlock()
		return nil, nil
	}
	// Skip non-string arrays
	elemSizes := map[uint32]int{0: 1, 1: 1, 2: 2, 3: 2, 4: 4, 5: 4, 7: 1, 10: 8, 11: 8, 12: 8}
	sz, ok := elemSizes[elemType]
	if !ok {
		return nil, fmt.Errorf("unsupported array elem type %d", elemType)
	}
	if count > maxSkipArrayCount {
		return nil, fmt.Errorf("gguf: array count %d too large (corrupt header?)", count)
	}
	skip := make([]byte, int(count)*sz)
	io.ReadFull(r, skip)
	return nil, nil
}

// Sanity caps for metadata array counts: real GGUF files stay well below
// these, so a larger count means a corrupt or truncated header and must fail
// as an error instead of a huge allocation.
const (
	maxMetaStringArrayCount = 1 << 20
	maxF32ArrayCount        = 1 << 22
	maxSkipArrayCount       = 1 << 24
)

// lastF32Array holds the most recently parsed float32 array. The GGUF header
// parser can run concurrently with consumers (memory-store warmup starts two
// embedders at once), so every access is serialized.
var (
	lastF32Mu    sync.Mutex
	lastF32Array []float32
)

// LastF32Array returns and clears the last parsed float32 array.
func LastF32Array() []float32 {
	lastF32Mu.Lock()
	defer lastF32Mu.Unlock()
	arr := lastF32Array
	lastF32Array = nil
	return arr
}

func readTensorInfo(r io.Reader) (*TensorInfo, error) {
	name, err := readString(r)
	if err != nil {
		return nil, err
	}
	ti := &TensorInfo{Name: name}
	if err := binary.Read(r, binary.LittleEndian, &ti.NDims); err != nil {
		return nil, err
	}
	for i := uint32(0); i < ti.NDims; i++ {
		if err := binary.Read(r, binary.LittleEndian, &ti.Dims[i]); err != nil {
			return nil, err
		}
	}
	if err := binary.Read(r, binary.LittleEndian, &ti.Type); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &ti.Offset); err != nil {
		return nil, err
	}
	return ti, nil
}

// float16to32 converts IEEE 754 half-precision to float32.
func float16to32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		// subnormal
		for mant&0x400 == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x3ff
		fallthrough
	case exp < 31:
		return math.Float32frombits(sign | (exp+112)<<23 | mant<<13)
	default: // inf/nan
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
}
