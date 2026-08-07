package onnxrt

import (
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
)

// TestCheckedShape covers the load-time shape validation: negative dims,
// dims above the sanity cap, and product overflow must all be rejected with
// an error — never a wrapped-around allocation size.
func TestCheckedShape(t *testing.T) {
	cases := []struct {
		name    string
		dims    []int64
		wantN   int
		wantErr bool
	}{
		{"empty (scalar)", nil, 1, false},
		{"normal", []int64{1, 3, 48, 320}, 1 * 3 * 48 * 320, false},
		{"zero dim", []int64{0, 1 << 40}, 0, true}, // second dim itself exceeds cap
		{"zero dim ok", []int64{4, 0}, 0, false},
		{"negative dim", []int64{2, -5}, 0, true},
		{"dim above cap", []int64{maxTensorElements + 1}, 0, true},
		{"product overflow", []int64{1 << 32, 1 << 32}, 0, true},
		{"product above cap", []int64{1 << 20, 1 << 20}, 0, true},
		{"int64 max dim", []int64{math.MaxInt64}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shape, n, err := checkedShape(c.dims)
			if c.wantErr {
				if err == nil {
					t.Fatalf("checkedShape(%v) = (%v, %d), want error", c.dims, shape, n)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkedShape(%v) unexpected error: %v", c.dims, err)
			}
			if n != c.wantN {
				t.Fatalf("checkedShape(%v) n = %d, want %d", c.dims, n, c.wantN)
			}
			if len(shape) != len(c.dims) {
				t.Fatalf("checkedShape(%v) shape = %v", c.dims, shape)
			}
		})
	}
}

// buildModelWithInitializer wraps a single initializer (built by buildInit)
// into a minimal loadable model: graph with one dynamic input, one output
// equal to the initializer name, and no nodes.
func buildModelWithInitializer(buildInit func(tn *enc)) []byte {
	var e enc
	e.varintField(1, 8)          // ir_version
	e.msgField(8, func(o *enc) { // opset_import
		o.varintField(2, 17)
	})
	e.msgField(7, func(g *enc) { // graph
		g.stringField(2, "g")
		g.msgField(11, func(v *enc) { // input "x": FLOAT [1]
			v.stringField(1, "x")
			v.msgField(2, func(tp *enc) {
				tp.msgField(1, func(tt *enc) {
					tt.varintField(1, 1)
					tt.msgField(2, func(sh *enc) {
						sh.msgField(1, func(d *enc) { d.varintField(1, 1) })
					})
				})
			})
		})
		g.msgField(12, func(v *enc) { // output "w": FLOAT, unknown shape
			v.stringField(1, "w")
			v.msgField(2, func(tp *enc) {
				tp.msgField(1, func(tt *enc) {
					tt.varintField(1, 1)
				})
			})
		})
		g.msgField(5, buildInit) // initializer "w"
	})
	return e.Bytes()
}

// TestNewGraphHostileInitializerDims is a regression test: initializers whose
// dims are negative or whose product overflows int must be rejected at load
// time with an error. Before the checked-shape guard these wrapped around to
// a small/zero element count, producing a tensor whose shape disagreed with
// its data (later ops could panic or loop on the bogus dims).
func TestNewGraphHostileInitializerDims(t *testing.T) {
	raw4 := make([]byte, 4) // one float32 of payload
	cases := []struct {
		name string
		init func(tn *enc)
	}{
		{"overflowing dims wrap to zero", func(tn *enc) {
			tn.packedVarintsField(1, 1<<32, 1<<32) // 2^64 -> int64 wraps to 0
			tn.varintField(2, 1)                   // FLOAT
			tn.stringField(8, "w")
			tn.bytesField(9, raw4)
		}},
		{"negative dim", func(tn *enc) {
			tn.packedVarintsField(1, 4, math.MaxUint64) // -1 as int64
			tn.varintField(2, 1)
			tn.stringField(8, "w")
			tn.bytesField(9, raw4)
		}},
		{"huge dims tiny data", func(tn *enc) {
			tn.packedVarintsField(1, 1<<30, 1<<30) // 2^60 elements, 1 float of data
			tn.varintField(2, 1)
			tn.stringField(8, "w")
			tn.bytesField(9, raw4)
		}},
		{"int64 overflowing dims", func(tn *enc) {
			tn.packedVarintsField(1, 1<<32, 1<<32)
			tn.varintField(2, 7) // INT64
			tn.stringField(8, "w")
			var raw [8]byte
			binary.LittleEndian.PutUint64(raw[:], 42)
			tn.bytesField(9, raw[:])
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := ParseModel(buildModelWithInitializer(c.init))
			if err != nil {
				t.Fatalf("ParseModel: %v", err)
			}
			if _, err := NewGraph(m); err == nil {
				t.Fatal("NewGraph must reject hostile initializer dims")
			} else if !strings.Contains(err.Error(), "onnxrt:") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestNewGraphInitializerDataMismatchStillOK: zero-element initializers (a dim
// of 0) remain legal, and matching shapes still load — the checked arithmetic
// must not change behavior for well-formed models.
func TestNewGraphInitializerDataMismatchStillOK(t *testing.T) {
	m, err := ParseModel(buildModelWithInitializer(func(tn *enc) {
		tn.packedVarintsField(1, 2, 2) // FLOAT [2,2]
		tn.varintField(2, 1)
		tn.stringField(8, "w")
		var raw [16]byte
		for i := 0; i < 4; i++ {
			binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(float32(i)))
		}
		tn.bytesField(9, raw[:])
	}))
	if err != nil {
		t.Fatalf("ParseModel: %v", err)
	}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatalf("NewGraph: %v", err)
	}
	out, err := g.Run(map[string]*Tensor{"x": NewFloat(1)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out["w"].NumElements(); got != 4 {
		t.Fatalf("output elements = %d, want 4", got)
	}
}

// TestLoadTruncatedRealModel runs the full load path (ParseModel + NewGraph)
// over truncations of a real PP-OCRv6 det model. Every truncation must either
// parse/load cleanly or return an error — never panic. Skipped when the model
// file is not present.
func TestLoadTruncatedRealModel(t *testing.T) {
	data, err := os.ReadFile("../../.tmp/ocr-models/ppocrv6_small_det.onnx")
	if err != nil {
		t.Skip("real det model not available:", err)
	}
	points := []int{0, 1, 2, 3, 8, 64}
	// Fine-grained across the first 64 KiB (headers, value infos, first
	// initializers), then coarse strides through the rest.
	for i := 128; i < 1<<16 && i < len(data); i += 1021 {
		points = append(points, i)
	}
	for i := 1 << 16; i < len(data); i += 1 << 18 {
		points = append(points, i)
	}
	points = append(points, len(data)-1)
	for _, n := range points {
		if n < 0 || n > len(data) {
			continue
		}
		m, err := ParseModel(data[:n])
		if err != nil {
			continue
		}
		if _, err := NewGraph(m); err != nil {
			continue
		}
	}
}
