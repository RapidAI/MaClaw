package kokoro

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

var tensorMagic = [8]byte{'K', 'O', 'R', 'O', 'T', 'N', 'S', 'R'}

const (
	tensorFormatVersionV1 uint32 = 1
	tensorFormatVersionV2 uint32 = 2
)

type TensorDType uint8

const (
	TensorFloat32   TensorDType = 0
	TensorQ8Rowwise TensorDType = 1
)

type Tensor struct {
	Name   string
	Dims   []int
	DType  TensorDType
	Data   []float32
	Scales []float32
	QData  []byte

	dataOnce sync.Once
	dataErr  error
}

func (t *Tensor) NumElements() int {
	n := 1
	for _, d := range t.Dims {
		n *= d
	}
	return n
}

func (t *Tensor) Float32() ([]float32, error) {
	if t == nil {
		return nil, fmt.Errorf("kokoro: nil tensor")
	}
	if t.DType == TensorFloat32 || t.DType == 0 && t.QData == nil {
		return t.Data, nil
	}
	if t.DType != TensorQ8Rowwise {
		return nil, fmt.Errorf("kokoro: tensor %s has unsupported dtype %d", t.Name, t.DType)
	}
	t.dataOnce.Do(func() {
		ne := t.NumElements()
		if ne == 0 {
			t.dataErr = fmt.Errorf("kokoro: tensor %s has no elements", t.Name)
			return
		}
		rows := 1
		if len(t.Dims) > 0 {
			rows = t.Dims[0]
		}
		if rows <= 0 || ne%rows != 0 {
			t.dataErr = fmt.Errorf("kokoro: tensor %s has invalid q8 row shape", t.Name)
			return
		}
		cols := ne / rows
		if len(t.Scales) != rows || len(t.QData) != ne {
			t.dataErr = fmt.Errorf("kokoro: tensor %s q8 payload shape mismatch", t.Name)
			return
		}
		data := make([]float32, ne)
		for r := 0; r < rows; r++ {
			scale := t.Scales[r]
			base := r * cols
			for c := 0; c < cols; c++ {
				data[base+c] = scale * float32(int8(t.QData[base+c]))
			}
		}
		t.Data = data
	})
	if t.dataErr != nil {
		return nil, t.dataErr
	}
	return t.Data, nil
}

func (t *Tensor) Q8Shape() (rows, cols int, ok bool) {
	if t == nil || t.DType != TensorQ8Rowwise {
		return 0, 0, false
	}
	ne := t.NumElements()
	if len(t.Dims) == 0 || t.Dims[0] <= 0 || ne%t.Dims[0] != 0 {
		return 0, 0, false
	}
	return t.Dims[0], ne / t.Dims[0], true
}

func (t *Tensor) DotQ8Row(row int, x []float32) (float32, error) {
	rows, cols, ok := t.Q8Shape()
	if !ok {
		return 0, fmt.Errorf("kokoro: tensor %s is not q8 row-wise", t.Name)
	}
	if row < 0 || row >= rows || len(x) != cols || len(t.Scales) != rows || len(t.QData) != rows*cols {
		return 0, fmt.Errorf("kokoro: tensor %s q8 dot shape mismatch", t.Name)
	}
	base := row * cols
	scale := t.Scales[row]
	q := t.QData[base : base+cols]
	var sum float32
	i := 0
	for ; i+7 < cols; i += 8 {
		sum += float32(int8(q[i])) * x[i]
		sum += float32(int8(q[i+1])) * x[i+1]
		sum += float32(int8(q[i+2])) * x[i+2]
		sum += float32(int8(q[i+3])) * x[i+3]
		sum += float32(int8(q[i+4])) * x[i+4]
		sum += float32(int8(q[i+5])) * x[i+5]
		sum += float32(int8(q[i+6])) * x[i+6]
		sum += float32(int8(q[i+7])) * x[i+7]
	}
	for ; i < cols; i++ {
		sum += float32(int8(q[i])) * x[i]
	}
	return scale * sum, nil
}

func (t *Tensor) DequantQ8Row(row int, dst []float32) error {
	rows, cols, ok := t.Q8Shape()
	if !ok {
		return fmt.Errorf("kokoro: tensor %s is not q8 row-wise", t.Name)
	}
	if row < 0 || row >= rows || len(dst) < cols || len(t.Scales) != rows || len(t.QData) != rows*cols {
		return fmt.Errorf("kokoro: tensor %s q8 dequant shape mismatch", t.Name)
	}
	base := row * cols
	scale := t.Scales[row]
	q := t.QData[base : base+cols]
	for i := 0; i < cols; i++ {
		dst[i] = scale * float32(int8(q[i]))
	}
	return nil
}

type TensorFile struct {
	Tensors map[string]*Tensor
}

func LoadTensorFile(path string) (*TensorFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("kokoro: open tensor file: %w", err)
	}
	defer f.Close()
	return ReadTensorFile(f)
}

func ReadTensorFile(r io.Reader) (*TensorFile, error) {
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("kokoro: read tensor magic: %w", err)
	}
	if magic != tensorMagic {
		return nil, fmt.Errorf("kokoro: invalid tensor file magic")
	}
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != tensorFormatVersionV1 && version != tensorFormatVersionV2 {
		return nil, fmt.Errorf("kokoro: unsupported tensor format version %d", version)
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	out := &TensorFile{Tensors: make(map[string]*Tensor, count)}
	for i := uint32(0); i < count; i++ {
		var nameLen uint16
		if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
			return nil, err
		}
		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return nil, err
		}
		var ndims uint8
		if err := binary.Read(r, binary.LittleEndian, &ndims); err != nil {
			return nil, err
		}
		if ndims == 0 || ndims > 8 {
			return nil, fmt.Errorf("kokoro: invalid tensor rank %d", ndims)
		}
		dims := make([]int, ndims)
		ne := uint64(1)
		for j := range dims {
			var d uint32
			if err := binary.Read(r, binary.LittleEndian, &d); err != nil {
				return nil, err
			}
			if d == 0 {
				return nil, fmt.Errorf("kokoro: zero dim in tensor %s", string(nameBytes))
			}
			dims[j] = int(d)
			ne *= uint64(d)
		}
		if ne > uint64(math.MaxInt32) {
			return nil, fmt.Errorf("kokoro: tensor %s too large", string(nameBytes))
		}
		name := string(nameBytes)
		if version == tensorFormatVersionV1 {
			data := make([]float32, int(ne))
			if err := binary.Read(r, binary.LittleEndian, data); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s: %w", name, err)
			}
			out.Tensors[name] = &Tensor{Name: name, Dims: dims, DType: TensorFloat32, Data: data}
			continue
		}

		var dtype uint8
		if err := binary.Read(r, binary.LittleEndian, &dtype); err != nil {
			return nil, fmt.Errorf("kokoro: read tensor %s dtype: %w", name, err)
		}
		switch TensorDType(dtype) {
		case TensorFloat32:
			data := make([]float32, int(ne))
			if err := binary.Read(r, binary.LittleEndian, data); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s: %w", name, err)
			}
			out.Tensors[name] = &Tensor{Name: name, Dims: dims, DType: TensorFloat32, Data: data}
		case TensorQ8Rowwise:
			var rows, cols uint32
			if err := binary.Read(r, binary.LittleEndian, &rows); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s q8 rows: %w", name, err)
			}
			if err := binary.Read(r, binary.LittleEndian, &cols); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s q8 cols: %w", name, err)
			}
			if rows == 0 || cols == 0 || uint64(rows)*uint64(cols) != ne {
				return nil, fmt.Errorf("kokoro: tensor %s q8 shape mismatch rows=%d cols=%d ne=%d", name, rows, cols, ne)
			}
			scales := make([]float32, rows)
			if err := binary.Read(r, binary.LittleEndian, scales); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s q8 scales: %w", name, err)
			}
			qdata := make([]byte, int(ne))
			if _, err := io.ReadFull(r, qdata); err != nil {
				return nil, fmt.Errorf("kokoro: read tensor %s q8 data: %w", name, err)
			}
			out.Tensors[name] = &Tensor{Name: name, Dims: dims, DType: TensorQ8Rowwise, Scales: scales, QData: qdata}
		default:
			return nil, fmt.Errorf("kokoro: tensor %s has unsupported dtype %d", name, dtype)
		}
	}
	return out, nil
}

func (tf *TensorFile) Get(name string) (*Tensor, bool) {
	if tf == nil {
		return nil, false
	}
	t, ok := tf.Tensors[name]
	return t, ok
}
