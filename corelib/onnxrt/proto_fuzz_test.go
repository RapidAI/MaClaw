package onnxrt

import (
	"os"
	"testing"
)

// FuzzParseModel feeds random/mutated/truncated wire bytes to the ONNX
// protobuf reader. The parser must always return an error for garbage and
// must never panic, loop forever, or allocate wildly out of proportion to the
// input (all repeated-field payloads are length-delimited by the input).
//
// Run with: go test -fuzz=FuzzParseModel -fuzztime=30s ./corelib/onnxrt/
func FuzzParseModel(f *testing.F) {
	// Seed 1: a complete, valid model.
	valid := buildTestModel()
	f.Add(valid)
	// Seed 2..N: truncations at every prefix length of a small model (the
	// most common real-world corruption: a partial download).
	for i := 0; i < len(valid); i += 7 {
		f.Add(valid[:i])
	}
	// Hand-crafted hostile buffers.
	f.Add([]byte{})                                                           // empty
	f.Add([]byte{0x08})                                                       // lone tag
	f.Add([]byte{0x08, 0x80})                                                 // truncated varint payload
	f.Add([]byte{0x3a, 0xff, 0xff, 0xff, 0xff, 0x0f})                         // graph field, huge length
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}) // max varint tag
	f.Add([]byte{0x0b})                                                       // group start wire type (unsupported)
	f.Add([]byte{0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08})

	// Seeds from a real PP-OCRv6 model when present (prefixes exercise the
	// real initializer/node attribute encodings; skipped elsewhere).
	if real, err := os.ReadFile("../../.tmp/ocr-models/ppocrv6_small_det.onnx"); err == nil {
		for _, n := range []int{1 << 10, 1 << 12, 1 << 14, 1 << 16} {
			if n <= len(real) {
				f.Add(real[:n])
			}
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := ParseModel(data)
		if err != nil {
			return
		}
		// A successful parse must be sane to inspect — walk every field the
		// loader touches so a hostile-but-parseable model cannot panic here.
		if m.Graph == nil {
			t.Fatal("parsed model with nil graph")
		}
		for _, tp := range m.Graph.Initializers {
			_ = tp.NumElements()
		}
		for _, nd := range m.Graph.Nodes {
			for _, a := range nd.Attrs {
				_ = a.Int()
				_ = a.Ints()
				_ = a.Float()
				_ = a.Floats()
				_ = a.Str()
			}
		}
	})
}
