package onnxrt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// tiny protobuf encoder helpers (test-only)
// ---------------------------------------------------------------------------

type enc struct{ bytes.Buffer }

func (e *enc) uvarint(v uint64) {
	var b [10]byte
	n := binary.PutUvarint(b[:], v)
	e.Write(b[:n])
}

func (e *enc) tag(field int, wire int) { e.uvarint(uint64(field)<<3 | uint64(wire)) }

func (e *enc) varintField(field int, v uint64) {
	e.tag(field, wireVarint)
	e.uvarint(v)
}

func (e *enc) fixed32Field(field int, v uint32) {
	e.tag(field, wireFixed32)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	e.Write(b[:])
}

func (e *enc) fixed64Field(field int, v uint64) {
	e.tag(field, wireFixed64)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	e.Write(b[:])
}

func (e *enc) bytesField(field int, b []byte) {
	e.tag(field, wireBytes)
	e.uvarint(uint64(len(b)))
	e.Write(b)
}

func (e *enc) stringField(field int, s string) { e.bytesField(field, []byte(s)) }

func (e *enc) msgField(field int, build func(e *enc)) {
	var sub enc
	build(&sub)
	e.bytesField(field, sub.Bytes())
}

// packedVarintsField writes a packed varint repeated field.
func (e *enc) packedVarintsField(field int, vs ...uint64) {
	var sub enc
	for _, v := range vs {
		sub.uvarint(v)
	}
	e.bytesField(field, sub.Bytes())
}

// packedFloatsField writes a packed fixed32 repeated field.
func (e *enc) packedFloatsField(field int, fs ...float32) {
	var sub enc
	for _, f := range fs {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(f))
		sub.Write(b[:])
	}
	e.bytesField(field, sub.Bytes())
}

// ---------------------------------------------------------------------------
// reader-level tests
// ---------------------------------------------------------------------------

func TestUvarint(t *testing.T) {
	// 300 = 0b1_0101100 -> 0xAC 0x02
	r := &reader{buf: []byte{0xAC, 0x02}}
	v, err := r.uvarint()
	if err != nil {
		t.Fatal(err)
	}
	if v != 300 {
		t.Fatalf("got %d, want 300", v)
	}
	if !r.eof() {
		t.Fatal("expected EOF")
	}
}

func TestUvarintNegativeInt64(t *testing.T) {
	// int64 -1 encodes as 10 bytes of varint (two's complement).
	var e enc
	e.varintField(3, math.MaxUint64) // -1 as uint64
	r := &reader{buf: e.Bytes()}
	_, wire, err := r.tag()
	if err != nil {
		t.Fatal(err)
	}
	if wire != wireVarint {
		t.Fatalf("wire %d", wire)
	}
	v, err := r.int64v()
	if err != nil {
		t.Fatal(err)
	}
	if v != -1 {
		t.Fatalf("got %d, want -1", v)
	}
}

func TestVarintOverflow(t *testing.T) {
	r := &reader{buf: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}}
	if _, err := r.uvarint(); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestTruncatedVarint(t *testing.T) {
	r := &reader{buf: []byte{0xAC}} // continuation bit set, no next byte
	if _, err := r.uvarint(); !errors.Is(err, errTruncated) {
		t.Fatalf("got %v, want errTruncated", err)
	}
}

func TestTruncatedLengthDelimited(t *testing.T) {
	r := &reader{buf: []byte{0x05, 'a', 'b'}} // claims 5 bytes, has 2
	if _, err := r.bytes(); !errors.Is(err, errTruncated) {
		t.Fatalf("got %v, want errTruncated", err)
	}
}

func TestTruncatedModel(t *testing.T) {
	// ir_version varint field claiming a graph of 10 bytes, only 3 present.
	var e enc
	e.varintField(1, 8)
	e.tag(7, wireBytes)
	e.uvarint(10)
	e.Write([]byte{1, 2, 3})
	if _, err := ParseModel(e.Bytes()); err == nil {
		t.Fatal("expected error on truncated input")
	}
}

func TestSkipUnknownFields(t *testing.T) {
	var e enc
	e.varintField(1, 9) // ir_version
	// unknown fields of every supported wire type
	e.varintField(99, 12345)
	e.fixed64Field(100, 0xDEADBEEF)
	e.bytesField(101, []byte("skip me"))
	e.fixed32Field(102, 42)
	e.msgField(7, func(g *enc) { // graph
		g.stringField(2, "g")
	})
	m, err := ParseModel(e.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if m.IRVersion != 9 {
		t.Fatalf("ir_version %d", m.IRVersion)
	}
	if m.Graph == nil || m.Graph.Name != "g" {
		t.Fatalf("graph %+v", m.Graph)
	}
}

func TestUnsupportedWireType(t *testing.T) {
	var e enc
	e.tag(1, 3) // wire type 3 (group start) is unsupported
	if _, err := ParseModel(e.Bytes()); err == nil {
		t.Fatal("expected error for unsupported wire type")
	}
}

// ---------------------------------------------------------------------------
// message-level tests
// ---------------------------------------------------------------------------

// buildTestModel builds a small but feature-complete model:
//   - opset 17, ir 8
//   - graph "g" with dynamic input, one output, one initializer (raw float),
//     and one Conv node exercising all attribute kinds.
func buildTestModel() []byte {
	var e enc
	e.varintField(1, 8)          // ir_version
	e.msgField(8, func(o *enc) { // opset_import
		o.varintField(2, 17)
	})
	e.msgField(7, func(g *enc) { // graph
		g.stringField(2, "g")
		// input "x": FLOAT [1, 'N', 224, 224]
		g.msgField(11, func(v *enc) {
			v.stringField(1, "x")
			v.msgField(2, func(tp *enc) {
				tp.msgField(1, func(tt *enc) {
					tt.varintField(1, 1) // FLOAT
					tt.msgField(2, func(sh *enc) {
						sh.msgField(1, func(d *enc) { d.varintField(1, 1) })
						sh.msgField(1, func(d *enc) { d.stringField(2, "N") })
						sh.msgField(1, func(d *enc) { d.varintField(1, 224) })
						sh.msgField(1, func(d *enc) { d.varintField(1, 224) })
					})
				})
			})
		})
		// output "y": FLOAT [1]
		g.msgField(12, func(v *enc) {
			v.stringField(1, "y")
			v.msgField(2, func(tp *enc) {
				tp.msgField(1, func(tt *enc) {
					tt.varintField(1, 1)
					tt.msgField(2, func(sh *enc) {
						sh.msgField(1, func(d *enc) { d.varintField(1, 1) })
					})
				})
			})
		})
		// initializer "w": FLOAT [2], raw_data = {1.5, -2.25}
		g.msgField(5, func(tn *enc) {
			tn.packedVarintsField(1, 2)
			tn.varintField(2, 1) // FLOAT
			tn.stringField(8, "w")
			var raw [8]byte
			binary.LittleEndian.PutUint32(raw[0:], math.Float32bits(1.5))
			binary.LittleEndian.PutUint32(raw[4:], math.Float32bits(-2.25))
			tn.bytesField(9, raw[:])
		})
		// node: Conv x,w -> y, with attributes of every kind
		g.msgField(1, func(n *enc) {
			n.stringField(1, "x")
			n.stringField(1, "w")
			n.stringField(2, "y")
			n.stringField(3, "conv0")
			n.stringField(4, "Conv")
			// pads: unpacked ints, explicit type=INTS(7)
			n.msgField(5, func(a *enc) {
				a.stringField(1, "pads")
				a.varintField(8, 1)
				a.varintField(8, 1)
				a.varintField(20, 7)
			})
			// strides: packed ints (no type field)
			n.msgField(5, func(a *enc) {
				a.stringField(1, "strides")
				a.packedVarintsField(8, 2, 2)
			})
			// alpha: scalar float
			n.msgField(5, func(a *enc) {
				a.stringField(1, "alpha")
				a.fixed32Field(2, math.Float32bits(0.5))
				a.varintField(20, 1)
			})
			// beta: scalar int
			n.msgField(5, func(a *enc) {
				a.stringField(1, "beta")
				a.varintField(3, 7)
				a.varintField(20, 2)
			})
			// mode: string
			n.msgField(5, func(a *enc) {
				a.stringField(1, "mode")
				a.stringField(4, "reflect")
			})
			// scales: packed floats
			n.msgField(5, func(a *enc) {
				a.stringField(1, "scales")
				a.packedFloatsField(7, 1.0, 2.5)
			})
			// labels: strings
			n.msgField(5, func(a *enc) {
				a.stringField(1, "labels")
				a.stringField(9, "cat")
				a.stringField(9, "dog")
			})
			// seed: tensor attr, INT64 via int64_data packed
			n.msgField(5, func(a *enc) {
				a.stringField(1, "seed")
				a.msgField(5, func(tn *enc) {
					tn.varintField(1, 3) // unpacked dim
					tn.varintField(2, 7) // INT64
					tn.packedVarintsField(7, 10, 20, 30)
				})
			})
		})
	})
	return e.Bytes()
}

func TestParseModelTopLevel(t *testing.T) {
	m, err := ParseModel(buildTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if m.IRVersion != 8 {
		t.Fatalf("ir_version = %d, want 8", m.IRVersion)
	}
	if m.Opset != 17 {
		t.Fatalf("opset = %d, want 17", m.Opset)
	}
	g := m.Graph
	if g == nil || g.Name != "g" {
		t.Fatalf("graph %+v", g)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(g.Nodes))
	}
	if len(g.Initializers) != 1 {
		t.Fatalf("initializers = %d, want 1", len(g.Initializers))
	}
}

func TestParseValueInfoDynamicDims(t *testing.T) {
	m, err := ParseModel(buildTestModel())
	if err != nil {
		t.Fatal(err)
	}
	in := m.Graph.Inputs[0]
	if in.Name != "x" || in.ElemType != TypeFloat {
		t.Fatalf("input %+v", in)
	}
	want := []Dim{{Value: 1}, {Value: -1, Param: "N"}, {Value: 224}, {Value: 224}}
	if !reflect.DeepEqual(in.Shape, want) {
		t.Fatalf("shape = %+v, want %+v", in.Shape, want)
	}
	out := m.Graph.Outputs[0]
	if out.Name != "y" || len(out.Shape) != 1 || out.Shape[0].Value != 1 {
		t.Fatalf("output %+v", out)
	}
}

func TestParseInitializerRawData(t *testing.T) {
	m, err := ParseModel(buildTestModel())
	if err != nil {
		t.Fatal(err)
	}
	w := m.Graph.Initializers["w"]
	if w == nil {
		t.Fatal("initializer w missing")
	}
	if w.DataType != TypeFloat || !reflect.DeepEqual(w.Dims, []int64{2}) {
		t.Fatalf("tensor %+v", w)
	}
	fs, err := w.Floats()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fs, []float32{1.5, -2.25}) {
		t.Fatalf("floats = %v", fs)
	}
}

func TestParseNodeAndAttrs(t *testing.T) {
	m, err := ParseModel(buildTestModel())
	if err != nil {
		t.Fatal(err)
	}
	n := m.Graph.Nodes[0]
	if n.OpType != "Conv" || n.Name != "conv0" {
		t.Fatalf("node %+v", n)
	}
	if !reflect.DeepEqual(n.Inputs, []string{"x", "w"}) || !reflect.DeepEqual(n.Outputs, []string{"y"}) {
		t.Fatalf("io: in=%v out=%v", n.Inputs, n.Outputs)
	}
	if got := n.Attrs["pads"].Ints(); !reflect.DeepEqual(got, []int64{1, 1}) {
		t.Fatalf("pads = %v", got)
	}
	if n.Attrs["pads"].Type != attrTypeInts {
		t.Fatalf("pads type = %d", n.Attrs["pads"].Type)
	}
	// packed ints, type inferred
	if got := n.Attrs["strides"].Ints(); !reflect.DeepEqual(got, []int64{2, 2}) {
		t.Fatalf("strides = %v", got)
	}
	if n.Attrs["strides"].Type != attrTypeInts {
		t.Fatalf("strides inferred type = %d, want %d", n.Attrs["strides"].Type, attrTypeInts)
	}
	if got := n.Attrs["alpha"].Float(); got != 0.5 {
		t.Fatalf("alpha = %v", got)
	}
	if got := n.Attrs["beta"].Int(); got != 7 {
		t.Fatalf("beta = %v", got)
	}
	if got := n.Attrs["mode"].Str(); got != "reflect" {
		t.Fatalf("mode = %q", got)
	}
	if got := n.Attrs["scales"].Floats(); !reflect.DeepEqual(got, []float32{1.0, 2.5}) {
		t.Fatalf("scales = %v", got)
	}
	if got := n.Attrs["labels"].Strings; len(got) != 2 || string(got[0]) != "cat" || string(got[1]) != "dog" {
		t.Fatalf("labels = %q", got)
	}
	seed := n.Attrs["seed"].Tensor()
	if seed == nil {
		t.Fatal("seed tensor missing")
	}
	if seed.DataType != TypeInt64 || !reflect.DeepEqual(seed.Dims, []int64{3}) {
		t.Fatalf("seed tensor %+v", seed)
	}
	iv, err := seed.Int64s()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(iv, []int64{10, 20, 30}) {
		t.Fatalf("seed values = %v", iv)
	}
}

func TestTensorTypedPayloads(t *testing.T) {
	// float_data (unpacked), int32_data (packed), double_data (packed)
	var tn enc
	tn.fixed32Field(4, math.Float32bits(3.5))       // unpacked float_data
	tn.packedVarintsField(5, uint64(0xFFFFFFFF), 4) // int32_data: -1, 4
	var dbl enc
	var b8 [8]byte
	binary.LittleEndian.PutUint64(b8[:], math.Float64bits(2.5))
	dbl.Write(b8[:])
	binary.LittleEndian.PutUint64(b8[:], math.Float64bits(-0.5))
	dbl.Write(b8[:])
	tn.bytesField(10, dbl.Bytes()) // packed double_data

	tp, err := parseTensor(tn.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tp.FloatData, []float32{3.5}) {
		t.Fatalf("float_data = %v", tp.FloatData)
	}
	if !reflect.DeepEqual(tp.Int32Data, []int32{-1, 4}) {
		t.Fatalf("int32_data = %v", tp.Int32Data)
	}
	if !reflect.DeepEqual(tp.DoubleData, []float64{2.5, -0.5}) {
		t.Fatalf("double_data = %v", tp.DoubleData)
	}
}

func TestExternalDataRecorded(t *testing.T) {
	var tn enc
	tn.varintField(2, 1) // FLOAT
	tn.stringField(8, "ext")
	tn.msgField(13, func(e *enc) { // external_data entry
		e.stringField(1, "location")
		e.stringField(2, "weights.bin")
	})
	tn.varintField(14, 1) // data_location = EXTERNAL

	tp, err := parseTensor(tn.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tp.DataLocation != 1 {
		t.Fatalf("data_location = %d", tp.DataLocation)
	}
	if len(tp.ExternalData) != 1 || tp.ExternalData[0].Key != "location" || tp.ExternalData[0].Value != "weights.bin" {
		t.Fatalf("external_data = %+v", tp.ExternalData)
	}
	if _, err := tp.Floats(); err == nil {
		t.Fatal("expected descriptive error for external data")
	}
}

func TestOpsetPrefersDefaultDomain(t *testing.T) {
	var e enc
	e.varintField(1, 8)
	e.msgField(8, func(o *enc) { // custom domain first, higher version
		o.stringField(1, "ai.onnx.ml")
		o.varintField(2, 3)
	})
	e.msgField(8, func(o *enc) { // default domain
		o.varintField(2, 17)
	})
	e.msgField(7, func(g *enc) { g.stringField(2, "g") })
	m, err := ParseModel(e.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if m.Opset != 17 {
		t.Fatalf("opset = %d, want 17 (default domain)", m.Opset)
	}
}

func TestMissingGraphError(t *testing.T) {
	var e enc
	e.varintField(1, 8)
	if _, err := ParseModel(e.Bytes()); err == nil {
		t.Fatal("expected error for model without graph")
	}
}

func TestTensorTypeString(t *testing.T) {
	cases := map[TensorDataType]string{
		TypeFloat: "FLOAT", TypeInt64: "INT64", TypeDouble: "DOUBLE",
		TypeBFloat16: "BFLOAT16", TensorDataType(99): "UNKNOWN(99)",
	}
	for dt, want := range cases {
		if got := dt.String(); got != want {
			t.Fatalf("DataType(%d).String() = %q, want %q", int32(dt), got, want)
		}
	}
}
