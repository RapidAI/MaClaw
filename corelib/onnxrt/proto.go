// Package onnxrt is a minimal, pure-Go ONNX runtime.
//
// This file implements a hand-written protobuf wire-format reader that
// decodes an onnx.ModelProto buffer into Go structs. Only the fields a
// runtime actually needs are decoded; all unknown fields are skipped.
// It has no third-party dependencies and is CGO-free.
package onnxrt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// TensorDataType is the ONNX TensorProto.DataType enum.
type TensorDataType int32

// Common ONNX element types (subset of the full enum).
const (
	TypeUndefined    TensorDataType = 0
	TypeFloat        TensorDataType = 1
	TypeUint8        TensorDataType = 2
	TypeInt8         TensorDataType = 3
	TypeUint16       TensorDataType = 4
	TypeInt16        TensorDataType = 5
	TypeInt32        TensorDataType = 6
	TypeInt64        TensorDataType = 7
	TypeString       TensorDataType = 8
	TypeBool         TensorDataType = 9
	TypeFloat16      TensorDataType = 10
	TypeDouble       TensorDataType = 11
	TypeUint32       TensorDataType = 12
	TypeUint64       TensorDataType = 13
	TypeBFloat16     TensorDataType = 16
	TypeFloat8E4M3FN TensorDataType = 17
)

// String returns a human-readable name for the element type.
func (t TensorDataType) String() string {
	switch t {
	case TypeFloat:
		return "FLOAT"
	case TypeUint8:
		return "UINT8"
	case TypeInt8:
		return "INT8"
	case TypeUint16:
		return "UINT16"
	case TypeInt16:
		return "INT16"
	case TypeInt32:
		return "INT32"
	case TypeInt64:
		return "INT64"
	case TypeString:
		return "STRING"
	case TypeBool:
		return "BOOL"
	case TypeFloat16:
		return "FLOAT16"
	case TypeDouble:
		return "DOUBLE"
	case TypeUint32:
		return "UINT32"
	case TypeUint64:
		return "UINT64"
	case TypeBFloat16:
		return "BFLOAT16"
	case TypeFloat8E4M3FN:
		return "FLOAT8E4M3FN"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int32(t))
	}
}

// Model is a parsed onnx.ModelProto.
type Model struct {
	IRVersion int64
	Opset     int64 // highest opset_import version (empty domain preferred)
	Graph     *GraphProto
}

// GraphProto is a parsed onnx.GraphProto.
type GraphProto struct {
	Name         string
	Nodes        []*Node
	Initializers map[string]*TensorProto
	Inputs       []ValueInfo
	Outputs      []ValueInfo
}

// Node is a parsed onnx.NodeProto.
type Node struct {
	OpType  string
	Name    string
	Domain  string
	Inputs  []string
	Outputs []string
	Attrs   map[string]Attr
}

// Attr is a parsed onnx.AttributeProto. Exactly one of its payloads is
// populated, indicated by Type (as in AttributeProto.AttributeType:
// 0=UNDEFINED, 1=FLOAT, 2=INT, 3=STRING, 4=TENSOR, 6=FLOATS, 7=INTS,
// 8=STRINGS).
type Attr struct {
	Name      string
	Type      int32
	F         float32
	I         int64
	S         []byte
	T         *TensorData
	FloatVals []float32
	IntVals   []int64
	Strings   [][]byte
}

// Int returns the scalar int payload.
func (a Attr) Int() int64 { return a.I }

// Ints returns the repeated int payload (nil if not an INTS attribute).
func (a Attr) Ints() []int64 { return a.IntVals }

// Float returns the scalar float payload.
func (a Attr) Float() float32 { return a.F }

// Floats returns the repeated float payload.
func (a Attr) Floats() []float32 { return a.FloatVals }

// Str returns the string payload as a Go string.
func (a Attr) Str() string { return string(a.S) }

// Tensor returns the tensor payload.
func (a Attr) Tensor() *TensorData { return a.T }

// ExternalDataEntry records one key/value pair of TensorProto.external_data.
type ExternalDataEntry struct {
	Key   string
	Value string
}

// TensorProto mirrors onnx.TensorProto metadata plus its payload location.
type TensorProto struct {
	Name       string
	Dims       []int64
	DataType   TensorDataType
	RawData    []byte
	FloatData  []float32
	Int32Data  []int32
	Int64Data  []int64
	DoubleData []float64
	// ExternalData is non-nil when data_location == 1 (EXTERNAL).
	ExternalData []ExternalDataEntry
	DataLocation int32
}

// TensorData is the decoded payload of a tensor-valued attribute or
// initializer. It wraps the same fields as TensorProto; the helper methods
// give a unified view of the data regardless of storage encoding.
type TensorData struct {
	TensorProto
}

// NumElements returns the product of Dims (0 for scalars/unknown).
func (t *TensorProto) NumElements() int64 {
	if len(t.Dims) == 0 {
		return 0
	}
	n := int64(1)
	for _, d := range t.Dims {
		if d < 0 {
			return 0
		}
		n *= d
	}
	return n
}

// Floats returns the tensor's data as a []float32 when the element type is
// FLOAT. It decodes raw_data if necessary. Returns an error for external
// data (loading is not implemented yet) and for non-float types.
func (t *TensorProto) Floats() ([]float32, error) {
	if t.DataLocation == 1 {
		return nil, fmt.Errorf("onnxrt: tensor %q uses external data (location=%v); loading not implemented", t.Name, t.ExternalData)
	}
	if t.DataType != TypeFloat {
		return nil, fmt.Errorf("onnxrt: tensor %q is %s, not FLOAT", t.Name, t.DataType)
	}
	if len(t.RawData) > 0 {
		if len(t.RawData)%4 != 0 {
			return nil, fmt.Errorf("onnxrt: tensor %q raw_data length %d not a multiple of 4", t.Name, len(t.RawData))
		}
		out := make([]float32, len(t.RawData)/4)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(t.RawData[i*4:]))
		}
		return out, nil
	}
	return t.FloatData, nil
}

// Int64s returns the tensor's data as []int64 when the element type is
// INT64. Decodes raw_data if necessary. External data returns an error.
func (t *TensorProto) Int64s() ([]int64, error) {
	if t.DataLocation == 1 {
		return nil, fmt.Errorf("onnxrt: tensor %q uses external data (location=%v); loading not implemented", t.Name, t.ExternalData)
	}
	if t.DataType != TypeInt64 {
		return nil, fmt.Errorf("onnxrt: tensor %q is %s, not INT64", t.Name, t.DataType)
	}
	if len(t.RawData) > 0 {
		if len(t.RawData)%8 != 0 {
			return nil, fmt.Errorf("onnxrt: tensor %q raw_data length %d not a multiple of 8", t.Name, len(t.RawData))
		}
		out := make([]int64, len(t.RawData)/8)
		for i := range out {
			out[i] = int64(binary.LittleEndian.Uint64(t.RawData[i*8:]))
		}
		return out, nil
	}
	return t.Int64Data, nil
}

// ValueInfo is a parsed onnx.ValueInfoProto (graph input/output).
type ValueInfo struct {
	Name     string
	ElemType TensorDataType
	Shape    []Dim
}

// Dim is one dimension of a tensor shape. Exactly one of Value/Param is
// set: Value >= 0 for a static dimension, Param non-empty for a symbolic
// (dynamic) dimension.
type Dim struct {
	Value int64  // static size; -1 if the dimension is symbolic/unknown
	Param string // symbolic name when dynamic
}

// ---------------------------------------------------------------------------
// protobuf wire-format reader
// ---------------------------------------------------------------------------

// Wire types.
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2 // length-delimited
	wireFixed32 = 5
)

// reader is a cursor over a protobuf wire buffer.
type reader struct {
	buf []byte
	pos int
}

var errTruncated = errors.New("onnxrt: truncated protobuf input")

func (r *reader) eof() bool { return r.pos >= len(r.buf) }

// uvarint reads a base-128 varint.
func (r *reader) uvarint() (uint64, error) {
	var x uint64
	for i := 0; i < 10; i++ {
		if r.pos >= len(r.buf) {
			return 0, errTruncated
		}
		b := r.buf[r.pos]
		r.pos++
		if b < 0x80 {
			if i == 9 && b > 1 {
				return 0, errors.New("onnxrt: varint overflow")
			}
			return x | uint64(b)<<uint(7*i), nil
		}
		x |= uint64(b&0x7f) << uint(7*i)
	}
	return 0, errors.New("onnxrt: varint overflow")
}

// int64v reads a varint and reinterprets it as int64 (protobuf int64/uint64
// are two's-complement when negative).
func (r *reader) int64v() (int64, error) {
	u, err := r.uvarint()
	return int64(u), err
}

func (r *reader) fixed32() (uint32, error) {
	if r.pos+4 > len(r.buf) {
		return 0, errTruncated
	}
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *reader) fixed64() (uint64, error) {
	if r.pos+8 > len(r.buf) {
		return 0, errTruncated
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v, nil
}

// bytes reads a length-delimited field and returns its payload.
func (r *reader) bytes() ([]byte, error) {
	n, err := r.uvarint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.buf)-r.pos) {
		return nil, errTruncated
	}
	b := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b, nil
}

// tag reads a field tag and returns (fieldNumber, wireType).
func (r *reader) tag() (int, int, error) {
	v, err := r.uvarint()
	if err != nil {
		return 0, 0, err
	}
	field := int(v >> 3)
	wire := int(v & 7)
	if field <= 0 {
		return 0, 0, fmt.Errorf("onnxrt: invalid field number %d", field)
	}
	return field, wire, nil
}

// skip discards the value of a field with the given wire type.
func (r *reader) skip(wire int) error {
	switch wire {
	case wireVarint:
		_, err := r.uvarint()
		return err
	case wireFixed64:
		_, err := r.fixed64()
		return err
	case wireBytes:
		_, err := r.bytes()
		return err
	case wireFixed32:
		_, err := r.fixed32()
		return err
	default:
		return fmt.Errorf("onnxrt: unsupported wire type %d", wire)
	}
}

// packedVarints reads a length-delimited payload as packed varints.
// (Caller has already consumed the tag; this reads the bytes field.)
func (r *reader) packedVarints() ([]int64, error) {
	b, err := r.bytes()
	if err != nil {
		return nil, err
	}
	sub := &reader{buf: b}
	var out []int64
	for !sub.eof() {
		v, err := sub.int64v()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// packedFixed32 reads a length-delimited payload as packed fixed32 floats.
func (r *reader) packedFixed32() ([]float32, error) {
	b, err := r.bytes()
	if err != nil {
		return nil, err
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("onnxrt: packed fixed32 length %d not a multiple of 4", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// packedFixed64 reads a length-delimited payload as packed fixed64 doubles.
func (r *reader) packedFixed64() ([]float64, error) {
	b, err := r.bytes()
	if err != nil {
		return nil, err
	}
	if len(b)%8 != 0 {
		return nil, fmt.Errorf("onnxrt: packed fixed64 length %d not a multiple of 8", len(b))
	}
	out := make([]float64, len(b)/8)
	for i := range out {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[i*8:]))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// message decoders
// ---------------------------------------------------------------------------

// ParseModel decodes an onnx.ModelProto wire buffer.
func ParseModel(data []byte) (*Model, error) {
	r := &reader{buf: data}
	m := &Model{}
	var opsetFallback int64
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, fmt.Errorf("onnxrt: ModelProto: %w", err)
		}
		switch field {
		case 1: // ir_version
			if wire != wireVarint {
				return nil, fmt.Errorf("onnxrt: ir_version: unexpected wire type %d", wire)
			}
			if m.IRVersion, err = r.int64v(); err != nil {
				return nil, err
			}
		case 8: // opset_import
			if wire != wireBytes {
				return nil, fmt.Errorf("onnxrt: opset_import: unexpected wire type %d", wire)
			}
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			domain, version, err := parseOperatorSetId(b)
			if err != nil {
				return nil, err
			}
			// Prefer the default (ai.onnx) domain; otherwise take the max.
			if domain == "" {
				m.Opset = version
			} else if version > opsetFallback {
				opsetFallback = version
			}
		case 7: // graph
			if wire != wireBytes {
				return nil, fmt.Errorf("onnxrt: graph: unexpected wire type %d", wire)
			}
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			if m.Graph, err = parseGraph(b); err != nil {
				return nil, err
			}
		default:
			if err := r.skip(wire); err != nil {
				return nil, fmt.Errorf("onnxrt: ModelProto field %d: %w", field, err)
			}
		}
	}
	if m.Opset == 0 {
		m.Opset = opsetFallback
	}
	if m.Graph == nil {
		return nil, errors.New("onnxrt: model has no graph")
	}
	return m, nil
}

// parseOperatorSetId decodes an OperatorSetIdProto. Only the version field
// (2, varint) matters; domain (1, string) is also read for selection.
func parseOperatorSetId(b []byte) (domain string, version int64, err error) {
	r := &reader{buf: b}
	for !r.eof() {
		field, wire, e := r.tag()
		if e != nil {
			return "", 0, fmt.Errorf("onnxrt: OperatorSetIdProto: %w", e)
		}
		switch field {
		case 1:
			s, e := r.bytes()
			if e != nil {
				return "", 0, e
			}
			domain = string(s)
		case 2:
			if version, e = r.int64v(); e != nil {
				return "", 0, e
			}
		default:
			if e := r.skip(wire); e != nil {
				return "", 0, e
			}
		}
	}
	return domain, version, nil
}

// parseGraph decodes a GraphProto.
func parseGraph(b []byte) (*GraphProto, error) {
	r := &reader{buf: b}
	g := &GraphProto{Initializers: map[string]*TensorProto{}}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, fmt.Errorf("onnxrt: GraphProto: %w", err)
		}
		switch field {
		case 1: // node
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n, err := parseNode(p)
			if err != nil {
				return nil, err
			}
			g.Nodes = append(g.Nodes, n)
		case 2: // name
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			g.Name = string(s)
		case 5: // initializer
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			t, err := parseTensor(p)
			if err != nil {
				return nil, err
			}
			g.Initializers[t.Name] = t
		case 11: // input
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			vi, err := parseValueInfo(p)
			if err != nil {
				return nil, err
			}
			g.Inputs = append(g.Inputs, vi)
		case 12: // output
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			vi, err := parseValueInfo(p)
			if err != nil {
				return nil, err
			}
			g.Outputs = append(g.Outputs, vi)
		default:
			if err := r.skip(wire); err != nil {
				return nil, fmt.Errorf("onnxrt: GraphProto field %d: %w", field, err)
			}
		}
	}
	return g, nil
}

// parseNode decodes a NodeProto.
func parseNode(b []byte) (*Node, error) {
	r := &reader{buf: b}
	n := &Node{Attrs: map[string]Attr{}}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, fmt.Errorf("onnxrt: NodeProto: %w", err)
		}
		switch field {
		case 1: // input
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n.Inputs = append(n.Inputs, string(s))
		case 2: // output
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n.Outputs = append(n.Outputs, string(s))
		case 3: // name
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n.Name = string(s)
		case 4: // op_type
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n.OpType = string(s)
		case 5: // attribute
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			a, err := parseAttribute(p)
			if err != nil {
				return nil, err
			}
			n.Attrs[a.Name] = a
		case 7: // domain
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			n.Domain = string(s)
		default:
			if err := r.skip(wire); err != nil {
				return nil, fmt.Errorf("onnxrt: NodeProto field %d: %w", field, err)
			}
		}
	}
	return n, nil
}

// Attribute type codes (AttributeProto.AttributeType).
const (
	attrTypeFloat   = 1
	attrTypeInt     = 2
	attrTypeString  = 3
	attrTypeTensor  = 4
	attrTypeFloats  = 6
	attrTypeInts    = 7
	attrTypeStrings = 8
)

// parseAttribute decodes an AttributeProto.
func parseAttribute(b []byte) (Attr, error) {
	r := &reader{buf: b}
	var a Attr
	// Track which payload fields were seen so that a missing type field
	// (common in older exports) can be inferred.
	seen := int32(0)
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return a, fmt.Errorf("onnxrt: AttributeProto: %w", err)
		}
		switch field {
		case 1: // name
			s, err := r.bytes()
			if err != nil {
				return a, err
			}
			a.Name = string(s)
		case 2: // f
			if wire != wireFixed32 {
				return a, fmt.Errorf("onnxrt: attribute %q f: unexpected wire type %d", a.Name, wire)
			}
			v, err := r.fixed32()
			if err != nil {
				return a, err
			}
			a.F = math.Float32frombits(v)
			seen = attrTypeFloat
		case 3: // i
			v, err := r.int64v()
			if err != nil {
				return a, err
			}
			a.I = v
			seen = attrTypeInt
		case 4: // s
			s, err := r.bytes()
			if err != nil {
				return a, err
			}
			a.S = append(a.S[:0], s...)
			seen = attrTypeString
		case 5: // t
			p, err := r.bytes()
			if err != nil {
				return a, err
			}
			t, err := parseTensor(p)
			if err != nil {
				return a, err
			}
			a.T = &TensorData{TensorProto: *t}
			seen = attrTypeTensor
		case 7: // floats (packed fixed32 or unpacked)
			if wire == wireBytes {
				fs, err := r.packedFixed32()
				if err != nil {
					return a, err
				}
				a.FloatVals = append(a.FloatVals, fs...)
			} else if wire == wireFixed32 {
				v, err := r.fixed32()
				if err != nil {
					return a, err
				}
				a.FloatVals = append(a.FloatVals, math.Float32frombits(v))
			} else {
				return a, fmt.Errorf("onnxrt: attribute %q floats: unexpected wire type %d", a.Name, wire)
			}
			seen = attrTypeFloats
		case 8: // ints (packed or unpacked varint)
			if wire == wireBytes {
				is, err := r.packedVarints()
				if err != nil {
					return a, err
				}
				a.IntVals = append(a.IntVals, is...)
			} else if wire == wireVarint {
				v, err := r.int64v()
				if err != nil {
					return a, err
				}
				a.IntVals = append(a.IntVals, v)
			} else {
				return a, fmt.Errorf("onnxrt: attribute %q ints: unexpected wire type %d", a.Name, wire)
			}
			seen = attrTypeInts
		case 9: // strings
			s, err := r.bytes()
			if err != nil {
				return a, err
			}
			cp := make([]byte, len(s))
			copy(cp, s)
			a.Strings = append(a.Strings, cp)
			seen = attrTypeStrings
		case 20: // type
			v, err := r.uvarint()
			if err != nil {
				return a, err
			}
			a.Type = int32(v)
		default:
			if err := r.skip(wire); err != nil {
				return a, fmt.Errorf("onnxrt: AttributeProto %q field %d: %w", a.Name, field, err)
			}
		}
	}
	if a.Type == 0 {
		a.Type = seen
	}
	return a, nil
}

// parseTensor decodes a TensorProto.
func parseTensor(b []byte) (*TensorProto, error) {
	r := &reader{buf: b}
	t := &TensorProto{}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return nil, fmt.Errorf("onnxrt: TensorProto: %w", err)
		}
		switch field {
		case 1: // dims (packed or unpacked varint)
			if wire == wireBytes {
				ds, err := r.packedVarints()
				if err != nil {
					return nil, err
				}
				t.Dims = append(t.Dims, ds...)
			} else if wire == wireVarint {
				v, err := r.int64v()
				if err != nil {
					return nil, err
				}
				t.Dims = append(t.Dims, v)
			} else {
				return nil, fmt.Errorf("onnxrt: tensor dims: unexpected wire type %d", wire)
			}
		case 2: // data_type
			v, err := r.int64v()
			if err != nil {
				return nil, err
			}
			t.DataType = TensorDataType(v)
		case 8: // name
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			t.Name = string(s)
		case 9: // raw_data
			s, err := r.bytes()
			if err != nil {
				return nil, err
			}
			// Copy: the raw buffer may outlive the caller's parse buffer.
			t.RawData = append(t.RawData[:0], s...)
		case 4: // float_data (packed fixed32 or unpacked)
			if wire == wireBytes {
				fs, err := r.packedFixed32()
				if err != nil {
					return nil, err
				}
				t.FloatData = append(t.FloatData, fs...)
			} else if wire == wireFixed32 {
				v, err := r.fixed32()
				if err != nil {
					return nil, err
				}
				t.FloatData = append(t.FloatData, math.Float32frombits(v))
			} else {
				return nil, fmt.Errorf("onnxrt: tensor float_data: unexpected wire type %d", wire)
			}
		case 5: // int32_data (packed or unpacked varint)
			if wire == wireBytes {
				vs, err := r.packedVarints()
				if err != nil {
					return nil, err
				}
				for _, v := range vs {
					t.Int32Data = append(t.Int32Data, int32(v))
				}
			} else if wire == wireVarint {
				v, err := r.int64v()
				if err != nil {
					return nil, err
				}
				t.Int32Data = append(t.Int32Data, int32(v))
			} else {
				return nil, fmt.Errorf("onnxrt: tensor int32_data: unexpected wire type %d", wire)
			}
		case 7: // int64_data (packed or unpacked varint)
			if wire == wireBytes {
				vs, err := r.packedVarints()
				if err != nil {
					return nil, err
				}
				t.Int64Data = append(t.Int64Data, vs...)
			} else if wire == wireVarint {
				v, err := r.int64v()
				if err != nil {
					return nil, err
				}
				t.Int64Data = append(t.Int64Data, v)
			} else {
				return nil, fmt.Errorf("onnxrt: tensor int64_data: unexpected wire type %d", wire)
			}
		case 10: // double_data (packed fixed64 or unpacked)
			if wire == wireBytes {
				ds, err := r.packedFixed64()
				if err != nil {
					return nil, err
				}
				t.DoubleData = append(t.DoubleData, ds...)
			} else if wire == wireFixed64 {
				v, err := r.fixed64()
				if err != nil {
					return nil, err
				}
				t.DoubleData = append(t.DoubleData, math.Float64frombits(v))
			} else {
				return nil, fmt.Errorf("onnxrt: tensor double_data: unexpected wire type %d", wire)
			}
		case 13: // external_data (StringStringEntryProto)
			p, err := r.bytes()
			if err != nil {
				return nil, err
			}
			e, err := parseStringStringEntry(p)
			if err != nil {
				return nil, err
			}
			t.ExternalData = append(t.ExternalData, e)
		case 14: // data_location
			v, err := r.int64v()
			if err != nil {
				return nil, err
			}
			t.DataLocation = int32(v)
		default:
			if err := r.skip(wire); err != nil {
				return nil, fmt.Errorf("onnxrt: TensorProto %q field %d: %w", t.Name, field, err)
			}
		}
	}
	return t, nil
}

// parseStringStringEntry decodes a StringStringEntryProto {key(1), value(2)}.
func parseStringStringEntry(b []byte) (ExternalDataEntry, error) {
	r := &reader{buf: b}
	var e ExternalDataEntry
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return e, fmt.Errorf("onnxrt: StringStringEntryProto: %w", err)
		}
		switch field {
		case 1:
			s, err := r.bytes()
			if err != nil {
				return e, err
			}
			e.Key = string(s)
		case 2:
			s, err := r.bytes()
			if err != nil {
				return e, err
			}
			e.Value = string(s)
		default:
			if err := r.skip(wire); err != nil {
				return e, err
			}
		}
	}
	return e, nil
}

// parseValueInfo decodes a ValueInfoProto.
func parseValueInfo(b []byte) (ValueInfo, error) {
	r := &reader{buf: b}
	var vi ValueInfo
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return vi, fmt.Errorf("onnxrt: ValueInfoProto: %w", err)
		}
		switch field {
		case 1: // name
			s, err := r.bytes()
			if err != nil {
				return vi, err
			}
			vi.Name = string(s)
		case 2: // type (TypeProto)
			p, err := r.bytes()
			if err != nil {
				return vi, err
			}
			if err := parseTypeProto(p, &vi); err != nil {
				return vi, err
			}
		default:
			if err := r.skip(wire); err != nil {
				return vi, fmt.Errorf("onnxrt: ValueInfoProto %q field %d: %w", vi.Name, field, err)
			}
		}
	}
	return vi, nil
}

// parseTypeProto decodes a TypeProto, keeping only tensor_type (field 1).
func parseTypeProto(b []byte, vi *ValueInfo) error {
	r := &reader{buf: b}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return fmt.Errorf("onnxrt: TypeProto: %w", err)
		}
		if field == 1 { // tensor_type
			p, err := r.bytes()
			if err != nil {
				return err
			}
			if err := parseTensorType(p, vi); err != nil {
				return err
			}
		} else {
			if err := r.skip(wire); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTensorType decodes a TypeProto.Tensor {elem_type(1), shape(2)}.
func parseTensorType(b []byte, vi *ValueInfo) error {
	r := &reader{buf: b}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return fmt.Errorf("onnxrt: TensorTypeProto: %w", err)
		}
		switch field {
		case 1: // elem_type
			v, err := r.int64v()
			if err != nil {
				return err
			}
			vi.ElemType = TensorDataType(v)
		case 2: // shape (TensorShapeProto)
			p, err := r.bytes()
			if err != nil {
				return err
			}
			if err := parseTensorShape(p, vi); err != nil {
				return err
			}
		default:
			if err := r.skip(wire); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTensorShape decodes a TensorShapeProto {dim(1) repeated}.
func parseTensorShape(b []byte, vi *ValueInfo) error {
	r := &reader{buf: b}
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return fmt.Errorf("onnxrt: TensorShapeProto: %w", err)
		}
		if field == 1 { // dim
			p, err := r.bytes()
			if err != nil {
				return err
			}
			d, err := parseDim(p)
			if err != nil {
				return err
			}
			vi.Shape = append(vi.Shape, d)
		} else {
			if err := r.skip(wire); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseDim decodes a TensorShapeProto.Dimension
// {dim_value(1) varint, dim_param(2) string}.
func parseDim(b []byte) (Dim, error) {
	r := &reader{buf: b}
	d := Dim{Value: -1} // unknown unless dim_value says otherwise
	for !r.eof() {
		field, wire, err := r.tag()
		if err != nil {
			return d, fmt.Errorf("onnxrt: Dimension: %w", err)
		}
		switch field {
		case 1: // dim_value
			v, err := r.int64v()
			if err != nil {
				return d, err
			}
			d.Value = v
		case 2: // dim_param
			s, err := r.bytes()
			if err != nil {
				return d, err
			}
			d.Param = string(s)
			d.Value = -1
		default:
			if err := r.skip(wire); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}
