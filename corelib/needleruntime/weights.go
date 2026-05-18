package needleruntime

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"unsafe"
)

const (
	WeightMagic     = "MLNDLQ8\x00"
	WeightVersion   = 1
	weightHeaderLen = 32
	// WeightFlagIdentityEmbedding marks artifacts whose embedding matrix is an
	// identity projection over hash buckets. Runtime can fuse token hashing
	// directly into sparse bucket counts instead of adding dense embedding rows.
	WeightFlagIdentityEmbedding = 1
	// WeightFlagSparseHashHead marks artifacts that intentionally omit the dense
	// embedding matrix. They are only valid for hashing tokenizers, where bucket
	// counts can be dotted directly with the label head.
	WeightFlagSparseHashHead = 2
)

type WeightHeader struct {
	Magic      string `json:"magic"`
	Version    uint32 `json:"version"`
	VocabSize  uint32 `json:"vocab_size"`
	HiddenSize uint32 `json:"hidden_size"`
	NumLabels  uint32 `json:"num_labels"`
	Flags      uint32 `json:"flags"`
	DataOffset uint32 `json:"data_offset"`
}

type Q8Weights struct {
	Header     *WeightHeader
	Embeddings []int8
	Head       []int8
	Bias       []float32
}

func ReadWeightHeader(path string) (*WeightHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readWeightHeader(f)
}

func ReadQ8Weights(path string) (*Q8Weights, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header, err := readWeightHeader(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(int64(header.DataOffset), io.SeekStart); err != nil {
		return nil, err
	}
	embLen, headLen, biasRawLen, err := WeightDataLengths(header)
	if err != nil {
		return nil, err
	}
	biasLen := int(header.NumLabels)
	embRaw := make([]byte, embLen)
	if _, err := io.ReadFull(f, embRaw); err != nil {
		return nil, fmt.Errorf("read Needle embeddings: %w", err)
	}
	headRaw := make([]byte, headLen)
	if _, err := io.ReadFull(f, headRaw); err != nil {
		return nil, fmt.Errorf("read Needle label head: %w", err)
	}
	biasRaw := make([]byte, biasRawLen)
	if _, err := io.ReadFull(f, biasRaw); err != nil {
		return nil, fmt.Errorf("read Needle label bias: %w", err)
	}
	weights := &Q8Weights{
		Header:     header,
		Embeddings: bytesToInt8(embRaw),
		Head:       bytesToInt8(headRaw),
		Bias:       make([]float32, biasLen),
	}
	for i := 0; i < biasLen; i++ {
		weights.Bias[i] = math.Float32frombits(binary.LittleEndian.Uint32(biasRaw[i*4 : i*4+4]))
	}
	return weights, nil
}

func WeightDataLengths(header *WeightHeader) (embeddings, head, bias int, err error) {
	if header == nil {
		return 0, 0, 0, fmt.Errorf("missing Needle weight header")
	}
	emb64 := uint64(header.VocabSize) * uint64(header.HiddenSize)
	if header.Flags&WeightFlagSparseHashHead != 0 {
		emb64 = 0
	}
	head64 := uint64(header.NumLabels) * uint64(header.HiddenSize)
	bias64 := uint64(header.NumLabels) * 4
	maxInt := uint64(int(^uint(0) >> 1))
	if emb64 > maxInt || head64 > maxInt || bias64 > maxInt {
		return 0, 0, 0, fmt.Errorf("Needle weight shape is too large")
	}
	return int(emb64), int(head64), int(bias64), nil
}

func readWeightHeader(r io.Reader) (*WeightHeader, error) {
	buf := make([]byte, weightHeaderLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read Needle weight header: %w", err)
	}
	h := &WeightHeader{
		Magic:      strings.TrimRight(string(buf[0:8]), "\x00"),
		Version:    binary.LittleEndian.Uint32(buf[8:12]),
		VocabSize:  binary.LittleEndian.Uint32(buf[12:16]),
		HiddenSize: binary.LittleEndian.Uint32(buf[16:20]),
		NumLabels:  binary.LittleEndian.Uint32(buf[20:24]),
		Flags:      binary.LittleEndian.Uint32(buf[24:28]),
		DataOffset: binary.LittleEndian.Uint32(buf[28:32]),
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}

func bytesToInt8(in []byte) []int8 {
	if len(in) == 0 {
		return nil
	}
	return unsafe.Slice((*int8)(unsafe.Pointer(&in[0])), len(in))
}

func (h *WeightHeader) Validate() error {
	if h == nil {
		return fmt.Errorf("missing Needle weight header")
	}
	if h.Magic != strings.TrimRight(WeightMagic, "\x00") {
		return fmt.Errorf("invalid Needle weight magic %q", h.Magic)
	}
	if h.Version != WeightVersion {
		return fmt.Errorf("unsupported Needle weight version %d", h.Version)
	}
	if h.VocabSize == 0 || h.HiddenSize == 0 || h.NumLabels == 0 {
		return fmt.Errorf("invalid Needle weight shape vocab=%d hidden=%d labels=%d", h.VocabSize, h.HiddenSize, h.NumLabels)
	}
	if h.DataOffset < weightHeaderLen {
		return fmt.Errorf("invalid Needle weight data offset %d", h.DataOffset)
	}
	return nil
}
