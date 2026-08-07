// gengolden generates small synthetic ONNX models plus random inputs into
// corelib/onnxrt/testdata/<case>/. A companion python script
// (testdata/run_golden.py) runs each case through onnxruntime to produce
// outputs.json golden files, which golden_test.go compares against this
// pure-Go runtime. Models are hand-encoded (no onnx python package needed).
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// minimal protobuf encoder
// ---------------------------------------------------------------------------

type enc struct{ bytes.Buffer }

func (e *enc) uvarint(v uint64) {
	var b [10]byte
	n := binary.PutUvarint(b[:], v)
	e.Write(b[:n])
}
func (e *enc) tag(f, w int) { e.uvarint(uint64(f)<<3 | uint64(w)) }
func (e *enc) varintField(f int, v uint64) {
	e.tag(f, 0)
	e.uvarint(v)
}
func (e *enc) fixed32Field(f int, v float32) {
	e.tag(f, 5)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	e.Write(b[:])
}
func (e *enc) bytesField(f int, b []byte) {
	e.tag(f, 2)
	e.uvarint(uint64(len(b)))
	e.Write(b)
}
func (e *enc) stringField(f int, s string) { e.bytesField(f, []byte(s)) }
func (e *enc) msgField(f int, build func(e *enc)) {
	var sub enc
	build(&sub)
	e.bytesField(f, sub.Bytes())
}
func (e *enc) packedInts(f int, vs ...int64) {
	var sub enc
	for _, v := range vs {
		sub.uvarint(uint64(v))
	}
	e.bytesField(f, sub.Bytes())
}

// ---------------------------------------------------------------------------
// ONNX model builder
// ---------------------------------------------------------------------------

type attr struct {
	name   string
	typ    int // 1 float, 2 int, 3 string, 6 floats, 7 ints
	f      float32
	i      int64
	s      string
	floats []float32
	ints   []int64
}

func attrF(name string, v float32) attr     { return attr{name: name, typ: 1, f: v} }
func attrI(name string, v int64) attr       { return attr{name: name, typ: 2, i: v} }
func attrS(name, v string) attr             { return attr{name: name, typ: 3, s: v} }
func attrFs(name string, v ...float32) attr { return attr{name: name, typ: 6, floats: v} }
func attrIs(name string, v ...int64) attr   { return attr{name: name, typ: 7, ints: v} }

type node struct {
	op     string
	inputs []string
	outs   []string
	attrs  []attr
}

type tensorInit struct {
	name  string
	shape []int64
	dtype int // 1 float, 7 int64
	f32   []float32
	i64   []int64
}

type valueInfo struct {
	name  string
	dtype int
	shape []int64 // -1 = dynamic
}

type modelDef struct {
	opset   int64
	nodes   []node
	inits   []tensorInit
	inputs  []valueInfo
	outputs []valueInfo
}

func buildModel(m modelDef) []byte {
	var e enc
	e.varintField(1, 8) // ir_version
	e.msgField(8, func(o *enc) { o.varintField(2, uint64(m.opset)) })
	e.msgField(7, func(g *enc) {
		g.stringField(2, "test")
		for _, nd := range m.nodes {
			g.msgField(1, func(ne *enc) {
				for _, in := range nd.inputs {
					ne.stringField(1, in)
				}
				for _, out := range nd.outs {
					ne.stringField(2, out)
				}
				ne.stringField(3, nd.op+"_0")
				ne.stringField(4, nd.op)
				for _, a := range nd.attrs {
					ne.msgField(5, func(ae *enc) {
						ae.stringField(1, a.name)
						switch a.typ {
						case 1:
							ae.fixed32Field(2, a.f)
						case 2:
							ae.varintField(3, uint64(a.i))
						case 3:
							ae.stringField(4, a.s)
						case 6:
							var sub enc
							for _, v := range a.floats {
								var b [4]byte
								binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
								sub.Write(b[:])
							}
							ae.bytesField(7, sub.Bytes())
						case 7:
							ae.packedInts(8, a.ints...)
						}
						ae.varintField(20, uint64(a.typ))
					})
				}
			})
		}
		for _, t := range m.inits {
			g.msgField(5, func(te *enc) {
				te.packedInts(1, t.shape...)
				te.varintField(2, uint64(t.dtype))
				te.stringField(8, t.name)
				var raw bytes.Buffer
				if t.dtype == 1 {
					for _, v := range t.f32 {
						var b [4]byte
						binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
						raw.Write(b[:])
					}
				} else {
					for _, v := range t.i64 {
						var b [8]byte
						binary.LittleEndian.PutUint64(b[:], uint64(v))
						raw.Write(b[:])
					}
				}
				te.bytesField(9, raw.Bytes())
			})
		}
		writeVI := func(f int, vi valueInfo) {
			g.msgField(f, func(ve *enc) {
				ve.stringField(1, vi.name)
				ve.msgField(2, func(tp *enc) {
					tp.msgField(1, func(tt *enc) {
						tt.varintField(1, uint64(vi.dtype))
						tt.msgField(2, func(sh *enc) {
							for _, d := range vi.shape {
								sh.msgField(1, func(de *enc) {
									if d >= 0 {
										de.varintField(1, uint64(d))
									} else {
										de.stringField(2, "dyn")
									}
								})
							}
						})
					})
				})
			})
		}
		for _, vi := range m.inputs {
			writeVI(11, vi)
		}
		for _, vi := range m.outputs {
			writeVI(12, vi)
		}
	})
	return e.Bytes()
}

// ---------------------------------------------------------------------------
// case definitions
// ---------------------------------------------------------------------------

type tensorData struct {
	Shape []int  `json:"shape"`
	DType string `json:"dtype"`
	Data  string `json:"data"` // base64 little-endian
}

type caseFile struct {
	Inputs map[string]tensorData `json:"inputs"`
}

func f32td(shape []int, data []float32) tensorData {
	var buf bytes.Buffer
	for _, v := range data {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		buf.Write(b[:])
	}
	return tensorData{Shape: shape, DType: "float32", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

func i64td(shape []int, data []int64) tensorData {
	var buf bytes.Buffer
	for _, v := range data {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		buf.Write(b[:])
	}
	return tensorData{Shape: shape, DType: "int64", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

func randF32(rng *rand.Rand, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = rng.Float32()*2 - 1
	}
	return out
}

func randPosF32(rng *rand.Rand, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = rng.Float32() + 0.01
	}
	return out
}

func numEl(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

func wInit(name string, shape []int64, rng *rand.Rand) tensorInit {
	n := 1
	sh := make([]int, len(shape))
	for i, d := range shape {
		n *= int(d)
		sh[i] = int(d)
	}
	// small weights to keep outputs in range
	data := randF32(rng, n)
	for i := range data {
		data[i] *= 0.5
	}
	return tensorInit{name: name, shape: shape, dtype: 1, f32: data}
}

func iInit(name string, vals ...int64) tensorInit {
	return tensorInit{name: name, shape: []int64{int64(len(vals))}, dtype: 7, i64: vals}
}

// single-op float case helper
func unaryCase(op string, inShape []int, attrs ...attr) modelDef {
	return modelDef{
		opset:   11,
		nodes:   []node{{op: op, inputs: []string{"x"}, outs: []string{"y"}, attrs: attrs}},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
		outputs: []valueInfo{{name: "y", dtype: 1, shape: nil}},
	}
}

func toI64(s []int) []int64 {
	out := make([]int64, len(s))
	for i, d := range s {
		out[i] = int64(d)
	}
	return out
}

type testCase struct {
	name   string
	model  modelDef
	inputs map[string]tensorData
}

func main() {
	outRoot := "corelib/onnxrt/testdata"
	if len(os.Args) > 1 {
		outRoot = os.Args[1]
	}
	rng := rand.New(rand.NewSource(42))
	var cases []testCase

	add := func(name string, m modelDef, inputs map[string]tensorData) {
		cases = append(cases, testCase{name, m, inputs})
	}
	oneInput := func(shape []int, pos bool) map[string]tensorData {
		if pos {
			return map[string]tensorData{"x": f32td(shape, randPosF32(rng, numEl(shape)))}
		}
		return map[string]tensorData{"x": f32td(shape, randF32(rng, numEl(shape)))}
	}

	// --- Conv variants ---
	convShape := []int{1, 2, 5, 5}
	add("conv_basic", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w", "b"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 3, 3), attrIs("pads", 1, 1, 1, 1), attrIs("strides", 1, 1)}}},
		inits:   []tensorInit{wInit("w", []int64{3, 2, 3, 3}, rng), wInit("b", []int64{3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(convShape)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(convShape, false))

	add("conv_stride2_dil2", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 3, 3), attrIs("pads", 2, 2, 2, 2), attrIs("strides", 2, 2), attrIs("dilations", 2, 2)}}},
		inits:   []tensorInit{wInit("w", []int64{2, 3, 3, 3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 3, 8, 8}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 3, 8, 8}, false))

	add("conv_group2", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 3, 3), attrIs("pads", 1, 1, 1, 1), attrI("group", 2)}}},
		inits:   []tensorInit{wInit("w", []int64{4, 2, 3, 3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 4, 6, 6}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 4, 6, 6}, false))

	add("conv_depthwise", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w", "b"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 3, 3), attrIs("pads", 1, 1, 1, 1), attrI("group", 3)}}},
		inits:   []tensorInit{wInit("w", []int64{3, 1, 3, 3}, rng), wInit("b", []int64{3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 3, 7, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{2, 3, 7, 5}, false))

	add("conv_1x1", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w", "b"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 1, 1)}}},
		inits:   []tensorInit{wInit("w", []int64{8, 4, 1, 1}, rng), wInit("b", []int64{8}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 4, 6, 6}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{2, 4, 6, 6}, false))

	add("conv_same_upper", modelDef{
		opset: 11,
		nodes: []node{{op: "Conv", inputs: []string{"x", "w"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 2, 2), attrIs("strides", 2, 2), attrS("auto_pad", "SAME_UPPER")}}},
		inits:   []tensorInit{wInit("w", []int64{3, 2, 2, 2}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 2, 7, 7}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 2, 7, 7}, false))

	// --- ConvTranspose ---
	add("convtranspose_2x", modelDef{
		opset: 11,
		nodes: []node{{op: "ConvTranspose", inputs: []string{"x", "w", "b"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 2, 2), attrIs("strides", 2, 2), attrIs("pads", 0, 0, 0, 0)}}},
		inits:   []tensorInit{wInit("w", []int64{2, 3, 2, 2}, rng), wInit("b", []int64{3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 2, 4, 4}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 2, 4, 4}, false))

	add("convtranspose_pad", modelDef{
		opset: 11,
		nodes: []node{{op: "ConvTranspose", inputs: []string{"x", "w"}, outs: []string{"y"},
			attrs: []attr{attrIs("kernel_shape", 3, 3), attrIs("strides", 1, 1), attrIs("pads", 1, 1, 1, 1)}}},
		inits:   []tensorInit{wInit("w", []int64{2, 2, 3, 3}, rng)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 2, 4, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 2, 4, 5}, false))

	// --- Resize ---
	resizeIn := []int{1, 2, 3, 4}
	add("resize_nearest_2x", modelDef{
		opset: 11,
		nodes: []node{{op: "Resize", inputs: []string{"x", "roi", "scales"}, outs: []string{"y"},
			attrs: []attr{attrS("coordinate_transformation_mode", "asymmetric"), attrS("mode", "nearest"), attrS("nearest_mode", "floor")}}},
		inits: []tensorInit{
			{name: "roi", shape: []int64{0}, dtype: 1},
			{name: "scales", shape: []int64{4}, dtype: 1, f32: []float32{1, 1, 2, 2}},
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(resizeIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(resizeIn, false))

	add("resize_linear_half", modelDef{
		opset: 11,
		nodes: []node{{op: "Resize", inputs: []string{"x", "roi", "scales"}, outs: []string{"y"},
			attrs: []attr{attrS("coordinate_transformation_mode", "half_pixel"), attrS("mode", "linear")}}},
		inits: []tensorInit{
			{name: "roi", shape: []int64{0}, dtype: 1},
			{name: "scales", shape: []int64{4}, dtype: 1, f32: []float32{1, 1, 2, 2}},
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(resizeIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(resizeIn, false))

	add("resize_linear_asym_down", modelDef{
		opset: 11,
		nodes: []node{{op: "Resize", inputs: []string{"x", "roi", "scales"}, outs: []string{"y"},
			attrs: []attr{attrS("coordinate_transformation_mode", "asymmetric"), attrS("mode", "linear")}}},
		inits: []tensorInit{
			{name: "roi", shape: []int64{0}, dtype: 1},
			{name: "scales", shape: []int64{4}, dtype: 1, f32: []float32{1, 1, 0.75, 0.5}},
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 2, 8, 8}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 2, 8, 8}, false))

	add("resize_nearest_sizes", modelDef{
		opset: 13,
		nodes: []node{{op: "Resize", inputs: []string{"x", "roi", "scales", "sizes"}, outs: []string{"y"},
			attrs: []attr{attrS("coordinate_transformation_mode", "asymmetric"), attrS("mode", "nearest"), attrS("nearest_mode", "round_prefer_floor")}}},
		inits: []tensorInit{
			{name: "roi", shape: []int64{0}, dtype: 1},
			{name: "scales", shape: []int64{0}, dtype: 1},
			iInit("sizes", 1, 2, 3, 7),
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(resizeIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(resizeIn, false))

	// --- Slice ---
	sliceIn := []int{2, 3, 6, 5}
	add("slice_basic", modelDef{
		opset:   11,
		nodes:   []node{{op: "Slice", inputs: []string{"x", "starts", "ends", "axes"}, outs: []string{"y"}}},
		inits:   []tensorInit{iInit("starts", 1, 2), iInit("ends", 5, 4), iInit("axes", 2, 3)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(sliceIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(sliceIn, false))

	add("slice_neg", modelDef{
		opset:   11,
		nodes:   []node{{op: "Slice", inputs: []string{"x", "starts", "ends", "axes"}, outs: []string{"y"}}},
		inits:   []tensorInit{iInit("starts", -3, -1), iInit("ends", -1, 1000), iInit("axes", 2, 3)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(sliceIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(sliceIn, false))

	add("slice_step2", modelDef{
		opset:   11,
		nodes:   []node{{op: "Slice", inputs: []string{"x", "starts", "ends", "axes", "steps"}, outs: []string{"y"}}},
		inits:   []tensorInit{iInit("starts", 0), iInit("ends", 6), iInit("axes", 2), iInit("steps", 2)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(sliceIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(sliceIn, false))

	add("slice_step_neg1", modelDef{
		opset:   11,
		nodes:   []node{{op: "Slice", inputs: []string{"x", "starts", "ends", "axes", "steps"}, outs: []string{"y"}}},
		inits:   []tensorInit{iInit("starts", -1), iInit("ends", -7), iInit("axes", 2), iInit("steps", -1)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(sliceIn)}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput(sliceIn, false))

	// --- broadcasting binary ops ---
	binCase := func(name, op string, aShape, bShape []int) {
		m := modelDef{
			opset:   11,
			nodes:   []node{{op: op, inputs: []string{"a", "b"}, outs: []string{"y"}}},
			inputs:  []valueInfo{{name: "a", dtype: 1, shape: toI64(aShape)}, {name: "b", dtype: 1, shape: toI64(bShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}
		add(name, m, map[string]tensorData{
			"a": f32td(aShape, randF32(rng, numEl(aShape))),
			"b": f32td(bShape, randPosF32(rng, numEl(bShape))),
		})
	}
	binCase("add_bcast_row", "Add", []int{2, 3}, []int{3})
	binCase("mul_bcast_col", "Mul", []int{2, 1}, []int{1, 3})
	binCase("sub_bcast_scalar", "Sub", []int{2, 3, 4}, []int{1})
	binCase("div_bcast_rank", "Div", []int{2, 1, 3}, []int{4, 3})
	binCase("pow_bcast", "Pow", []int{2, 3}, []int{1})

	// --- BatchNormalization ---
	bnC := int64(3)
	add("batchnorm", modelDef{
		opset: 11,
		nodes: []node{{op: "BatchNormalization", inputs: []string{"x", "scale", "bias", "mean", "var"}, outs: []string{"y"},
			attrs: []attr{attrF("epsilon", 1e-5), attrF("momentum", 0.9)}}},
		inits: []tensorInit{
			wInit("scale", []int64{bnC}, rng),
			wInit("bias", []int64{bnC}, rng),
			wInit("mean", []int64{bnC}, rng),
			{name: "var", shape: []int64{bnC}, dtype: 1, f32: []float32{0.5, 1.2, 2.0}},
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 3, 4, 4}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{2, 3, 4, 4}, false))

	// --- ReduceMean ---
	add("reducemean_23", unaryCase("ReduceMean", []int{2, 3, 4, 5}, attrIs("axes", 2, 3), attrI("keepdims", 1)), oneInput([]int{2, 3, 4, 5}, false))
	add("reducemean_neg", unaryCase("ReduceMean", []int{2, 3, 4}, attrIs("axes", -1), attrI("keepdims", 0)), oneInput([]int{2, 3, 4}, false))

	// --- Softmax (opset 11 flatten semantics) ---
	add("softmax_last", unaryCase("Softmax", []int{2, 3, 4}, attrI("axis", -1)), oneInput([]int{2, 3, 4}, false))
	add("softmax_axis1", unaryCase("Softmax", []int{2, 3, 4}, attrI("axis", 1)), oneInput([]int{2, 3, 4}, false))

	// --- MatMul ---
	add("matmul_2d", modelDef{
		opset:   11,
		nodes:   []node{{op: "MatMul", inputs: []string{"a", "b"}, outs: []string{"y"}}},
		inputs:  []valueInfo{{name: "a", dtype: 1, shape: []int64{3, 4}}, {name: "b", dtype: 1, shape: []int64{4, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, map[string]tensorData{
		"a": f32td([]int{3, 4}, randF32(rng, 12)),
		"b": f32td([]int{4, 5}, randF32(rng, 20)),
	})
	add("matmul_batched", modelDef{
		opset:   11,
		nodes:   []node{{op: "MatMul", inputs: []string{"a", "b"}, outs: []string{"y"}}},
		inputs:  []valueInfo{{name: "a", dtype: 1, shape: []int64{2, 3, 4}}, {name: "b", dtype: 1, shape: []int64{2, 4, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, map[string]tensorData{
		"a": f32td([]int{2, 3, 4}, randF32(rng, 24)),
		"b": f32td([]int{2, 4, 5}, randF32(rng, 40)),
	})
	add("matmul_bcast_b", modelDef{
		opset:   11,
		nodes:   []node{{op: "MatMul", inputs: []string{"a", "b"}, outs: []string{"y"}}},
		inputs:  []valueInfo{{name: "a", dtype: 1, shape: []int64{2, 3, 4}}, {name: "b", dtype: 1, shape: []int64{4, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, map[string]tensorData{
		"a": f32td([]int{2, 3, 4}, randF32(rng, 24)),
		"b": f32td([]int{4, 5}, randF32(rng, 20)),
	})

	// --- shape ops ---
	add("transpose_perm", unaryCase("Transpose", []int{2, 3, 4}, attrIs("perm", 2, 0, 1)), oneInput([]int{2, 3, 4}, false))
	add("transpose_default", unaryCase("Transpose", []int{2, 3, 4}), oneInput([]int{2, 3, 4}, false))

	add("concat_axis1", modelDef{
		opset:   11,
		nodes:   []node{{op: "Concat", inputs: []string{"a", "b"}, outs: []string{"y"}, attrs: []attr{attrI("axis", 1)}}},
		inputs:  []valueInfo{{name: "a", dtype: 1, shape: []int64{2, 2, 3}}, {name: "b", dtype: 1, shape: []int64{2, 3, 3}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, map[string]tensorData{
		"a": f32td([]int{2, 2, 3}, randF32(rng, 12)),
		"b": f32td([]int{2, 3, 3}, randF32(rng, 18)),
	})

	add("reshape_m1", modelDef{
		opset:   11,
		nodes:   []node{{op: "Reshape", inputs: []string{"x", "shape"}, outs: []string{"y"}}},
		inits:   []tensorInit{iInit("shape", 0, -1, 6)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 3, 4}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{2, 3, 4}, false))

	add("squeeze_attr", unaryCase("Squeeze", []int{1, 3, 1, 4}, attrIs("axes", 0, 2)), oneInput([]int{1, 3, 1, 4}, false))
	add("unsqueeze_attr", unaryCase("Unsqueeze", []int{3, 4}, attrIs("axes", 0, 2)), oneInput([]int{3, 4}, false))
	add("shape_op", modelDef{
		opset:   11,
		nodes:   []node{{op: "Shape", inputs: []string{"x"}, outs: []string{"y"}}},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 3, 4, 5}}},
		outputs: []valueInfo{{name: "y", dtype: 7}},
	}, oneInput([]int{2, 3, 4, 5}, false))

	// --- unary activations chained (order keeps domain valid) ---
	add("unary_chain", modelDef{
		opset: 11,
		nodes: []node{
			{op: "HardSigmoid", inputs: []string{"x"}, outs: []string{"h"}, attrs: []attr{attrF("alpha", 0.2), attrF("beta", 0.5)}},
			{op: "Erf", inputs: []string{"h"}, outs: []string{"e"}},
			{op: "Sigmoid", inputs: []string{"e"}, outs: []string{"s"}},
			{op: "Sqrt", inputs: []string{"s"}, outs: []string{"q"}},
			{op: "Relu", inputs: []string{"q"}, outs: []string{"y"}},
		},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{2, 3, 4}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{2, 3, 4}, false))

	// --- pools ---
	add("globalaveragepool", unaryCase("GlobalAveragePool", []int{2, 3, 4, 5}), oneInput([]int{2, 3, 4, 5}, false))
	add("maxpool_same_upper", unaryCase("MaxPool", []int{1, 2, 7, 7},
		attrIs("kernel_shape", 2, 2), attrIs("strides", 1, 1), attrS("auto_pad", "SAME_UPPER"), attrI("ceil_mode", 0)),
		oneInput([]int{1, 2, 7, 7}, false))
	add("maxpool_ceil", unaryCase("MaxPool", []int{1, 2, 7, 8},
		attrIs("kernel_shape", 3, 3), attrIs("strides", 2, 2), attrI("ceil_mode", 1)),
		oneInput([]int{1, 2, 7, 8}, false))
	add("averagepool_rec", unaryCase("AveragePool", []int{1, 2, 9, 9},
		attrIs("kernel_shape", 3, 2), attrIs("strides", 3, 2), attrIs("pads", 0, 0, 0, 0), attrI("count_include_pad", 0)),
		oneInput([]int{1, 2, 9, 9}, false))
	add("averagepool_pad", unaryCase("AveragePool", []int{1, 2, 6, 6},
		attrIs("kernel_shape", 3, 3), attrIs("strides", 2, 2), attrIs("pads", 1, 1, 1, 1), attrI("count_include_pad", 1)),
		oneInput([]int{1, 2, 6, 6}, false))

	// --- dynamic reshape via Shape/Slice/Concat (int64 chain) ---
	add("dyn_reshape", modelDef{
		opset: 11,
		nodes: []node{
			{op: "Shape", inputs: []string{"x"}, outs: []string{"sh"}},
			{op: "Slice", inputs: []string{"sh", "starts", "ends", "axes", "steps"}, outs: []string{"hw"}},
			{op: "Concat", inputs: []string{"hw", "m1"}, outs: []string{"tgt"}, attrs: []attr{attrI("axis", 0)}},
			{op: "Reshape", inputs: []string{"x", "tgt"}, outs: []string{"y"}},
		},
		inits:   []tensorInit{iInit("starts", 2), iInit("ends", 4), iInit("axes", 0), iInit("steps", 1), iInit("m1", -1)},
		inputs:  []valueInfo{{name: "x", dtype: 1, shape: []int64{1, 3, 6, 8}}},
		outputs: []valueInfo{{name: "y", dtype: 1}},
	}, oneInput([]int{1, 3, 6, 8}, false))

	// write cases
	for _, c := range cases {
		dir := filepath.Join(outRoot, c.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "model.onnx"), buildModel(c.model), 0o644); err != nil {
			panic(err)
		}
		cf := caseFile{Inputs: c.inputs}
		jb, err := json.MarshalIndent(cf, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "inputs.json"), jb, 0o644); err != nil {
			panic(err)
		}
		fmt.Println("wrote", dir)
	}
	fmt.Printf("%d cases\n", len(cases))
}
