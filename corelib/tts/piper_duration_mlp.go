package tts

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// LoadDurationMLP loads MLP weights from a raw binary file.
// Format: W1[194*32] + b1[32] + W2[32] + b2[1], all float32 little-endian.
func LoadDurationMLP(path string) (W1, b1, W2 []float32, b2 float32, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return LoadDurationMLPFromBytes(data)
}

// LoadDurationMLPFromBytes loads MLP weights from raw bytes.
func LoadDurationMLPFromBytes(data []byte) (W1, b1, W2 []float32, b2 float32, err error) {
	r := bytes.NewReader(data)

	readF32 := func(n int) ([]float32, error) {
		buf := make([]byte, n*4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		out := make([]float32, n)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4 : (i+1)*4]))
		}
		return out, nil
	}

	W1, err = readF32(194 * 32)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("read W1: %w", err)
	}
	b1, err = readF32(32)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("read b1: %w", err)
	}
	W2, err = readF32(32)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("read W2: %w", err)
	}
	b2Arr, err := readF32(1)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("read b2: %w", err)
	}
	return W1, b1, W2, b2Arr[0], nil
}
