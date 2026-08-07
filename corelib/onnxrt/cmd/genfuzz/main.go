// genfuzz generates randomized small ONNX graphs plus seeded random inputs
// into corelib/onnxrt/testdata/fuzz/<case>/ for differential testing against
// onnxruntime. A companion python script (testdata/run_golden.py) runs each
// case through onnxruntime to produce outputs.json goldens, which
// fuzz_golden_test.go compares against this pure-Go runtime.
//
// Models are hand-encoded (no onnx python package needed), reusing the same
// minimal protobuf encoding as cmd/gengolden.
//
// Usage: go run ./corelib/onnxrt/cmd/genfuzz [-seed N] [-out DIR]
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// minimal protobuf encoder (same wire format as cmd/gengolden)
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

func attrF(name string, v float32) attr   { return attr{name: name, typ: 1, f: v} }
func attrI(name string, v int64) attr     { return attr{name: name, typ: 2, i: v} }
func attrS(name, v string) attr           { return attr{name: name, typ: 3, s: v} }
func attrIs(name string, v ...int64) attr { return attr{name: name, typ: 7, ints: v} }

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
		for idx, nd := range m.nodes {
			g.msgField(1, func(ne *enc) {
				for _, in := range nd.inputs {
					ne.stringField(1, in)
				}
				for _, out := range nd.outs {
					ne.stringField(2, out)
				}
				ne.stringField(3, fmt.Sprintf("%s_%d", nd.op, idx))
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
// helpers
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

func randF32(rng *rand.Rand, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = rng.Float32()*2 - 1
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

func numEl64(shape []int64) int {
	n := 1
	for _, d := range shape {
		n *= int(d)
	}
	return n
}

func wInit(name string, shape []int64, rng *rand.Rand) tensorInit {
	data := randF32(rng, numEl64(shape))
	for i := range data {
		data[i] *= 0.5
	}
	return tensorInit{name: name, shape: shape, dtype: 1, f32: data}
}

func iInit(name string, vals ...int64) tensorInit {
	return tensorInit{name: name, shape: []int64{int64(len(vals))}, dtype: 7, i64: vals}
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

// randShape returns a random shape of the given rank with dims in [lo, hi].
func randShape(rng *rand.Rand, rank, lo, hi int) []int {
	s := make([]int, rank)
	for i := range s {
		s[i] = lo + rng.Intn(hi-lo+1)
	}
	return s
}

// bcastShape derives a shape broadcastable against base: align right, each
// dim either kept or replaced by 1, optionally dropping leading dims.
func bcastShape(rng *rand.Rand, base []int) []int {
	out := make([]int, len(base))
	for i, d := range base {
		if rng.Intn(2) == 0 {
			out[i] = 1
		} else {
			out[i] = d
		}
	}
	// possibly drop leading dims (broadcasting aligns ranks from the right)
	return out[rng.Intn(len(base)):]
}

func divisors(n int) []int {
	var out []int
	for d := 1; d <= n; d++ {
		if n%d == 0 {
			out = append(out, d)
		}
	}
	return out
}

func main() {
	outRoot := flag.String("out", "corelib/onnxrt/testdata/fuzz", "output directory")
	seed := flag.Int64("seed", 20250807, "rng seed")
	only := flag.String("only", "", "write only cases whose name contains this substring")
	flag.Parse()
	rng := rand.New(rand.NewSource(*seed))

	var cases []testCase
	counts := map[string]int{}
	add := func(family string, m modelDef, inputs map[string]tensorData) {
		name := fmt.Sprintf("fz_%s_%02d", family, counts[family])
		counts[family]++
		cases = append(cases, testCase{name, m, inputs})
	}
	oneInput := func(shape []int) map[string]tensorData {
		return map[string]tensorData{"x": f32td(shape, randF32(rng, numEl(shape)))}
	}

	// ------------------------------------------------------------------
	// Conv: random pads/strides/dilations/groups/auto_pad
	// ------------------------------------------------------------------
	for i := 0; i < 10; i++ {
		N := 1 + rng.Intn(2)
		C := 1 + rng.Intn(6)
		H, W := 3+rng.Intn(7), 3+rng.Intn(7)
		group := divisors(C)[rng.Intn(len(divisors(C)))]
		M := group * (1 + rng.Intn(3))
		Cg := C / group
		var attrs []attr
		var pads [4]int
		autoPads := []string{"NOTSET", "NOTSET", "NOTSET", "VALID", "SAME_UPPER", "SAME_LOWER"}
		ap := autoPads[rng.Intn(len(autoPads))]
		sH, sW := 1+rng.Intn(2), 1+rng.Intn(2)
		dH, dW := 1+rng.Intn(2), 1+rng.Intn(2)
		if ap == "SAME_UPPER" || ap == "SAME_LOWER" {
			dH, dW = 1, 1 // onnxruntime rejects dilated SAME padding
		}
		kH := 1 + rng.Intn(min(4, H))
		kW := 1 + rng.Intn(min(4, W))
		attrs = append(attrs, attrIs("kernel_shape", int64(kH), int64(kW)))
		attrs = append(attrs, attrIs("strides", int64(sH), int64(sW)))
		if dH > 1 || dW > 1 {
			attrs = append(attrs, attrIs("dilations", int64(dH), int64(dW)))
		}
		if group > 1 {
			attrs = append(attrs, attrI("group", int64(group)))
		}
		effH, effW := dH*(kH-1)+1, dW*(kW-1)+1
		switch ap {
		case "NOTSET":
			for try := 0; ; try++ {
				pads = [4]int{rng.Intn(3), rng.Intn(3), rng.Intn(3), rng.Intn(3)}
				if (H+pads[0]+pads[2]-effH)/sH+1 >= 1 && (W+pads[1]+pads[3]-effW)/sW+1 >= 1 {
					break
				}
			}
			attrs = append(attrs, attrIs("pads", int64(pads[0]), int64(pads[1]), int64(pads[2]), int64(pads[3])))
		case "VALID":
			attrs = append(attrs, attrS("auto_pad", "VALID"))
			if H < effH || W < effW {
				i--
				continue
			}
		default:
			attrs = append(attrs, attrS("auto_pad", ap))
		}
		inits := []tensorInit{wInit("w", []int64{int64(M), int64(Cg), int64(kH), int64(kW)}, rng)}
		ins := []string{"x", "w"}
		if rng.Intn(2) == 0 {
			inits = append(inits, wInit("b", []int64{int64(M)}, rng))
			ins = append(ins, "b")
		}
		inShape := []int{N, C, H, W}
		add("conv", modelDef{
			opset:   11,
			nodes:   []node{{op: "Conv", inputs: ins, outs: []string{"y"}, attrs: attrs}},
			inits:   inits,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(inShape))
	}

	// ------------------------------------------------------------------
	// ConvTranspose: random strides/pads/output_padding/groups/dilations
	// ------------------------------------------------------------------
	for i := 0; i < 8; i++ {
		N := 1 + rng.Intn(2)
		C := 1 + rng.Intn(4)
		H, W := 2+rng.Intn(4), 2+rng.Intn(4)
		group := divisors(C)[rng.Intn(len(divisors(C)))]
		Mg := 1 + rng.Intn(3)
		M := Mg * group
		kH, kW := 1+rng.Intn(3), 1+rng.Intn(3)
		sH, sW := 1+rng.Intn(3), 1+rng.Intn(3)
		dH, dW := 1+rng.Intn(2), 1+rng.Intn(2)
		var attrs []attr
		attrs = append(attrs, attrIs("kernel_shape", int64(kH), int64(kW)))
		attrs = append(attrs, attrIs("strides", int64(sH), int64(sW)))
		if dH > 1 || dW > 1 {
			attrs = append(attrs, attrIs("dilations", int64(dH), int64(dW)))
		}
		if group > 1 {
			attrs = append(attrs, attrI("group", int64(group)))
		}
		opH, opW := 0, 0
		if rng.Intn(2) == 0 && (sH > 1 || sW > 1) {
			opH = rng.Intn(sH)
			opW = rng.Intn(sW)
			if opH > 0 || opW > 0 {
				attrs = append(attrs, attrIs("output_padding", int64(opH), int64(opW)))
			}
		}
		var pads [4]int
		for try := 0; ; try++ {
			pads = [4]int{rng.Intn(kH), rng.Intn(kW), rng.Intn(kH), rng.Intn(kW)}
			oH := sH*(H-1) + opH + (kH-1)*dH + 1 - pads[0] - pads[2]
			oW := sW*(W-1) + opW + (kW-1)*dW + 1 - pads[1] - pads[3]
			if oH >= 1 && oW >= 1 {
				break
			}
		}
		attrs = append(attrs, attrIs("pads", int64(pads[0]), int64(pads[1]), int64(pads[2]), int64(pads[3])))
		inits := []tensorInit{wInit("w", []int64{int64(C), int64(Mg), int64(kH), int64(kW)}, rng)}
		ins := []string{"x", "w"}
		if rng.Intn(2) == 0 {
			inits = append(inits, wInit("b", []int64{int64(M)}, rng))
			ins = append(ins, "b")
		}
		inShape := []int{N, C, H, W}
		add("convtranspose", modelDef{
			opset:   11,
			nodes:   []node{{op: "ConvTranspose", inputs: ins, outs: []string{"y"}, attrs: attrs}},
			inits:   inits,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(inShape))
	}

	// ------------------------------------------------------------------
	// MaxPool / AveragePool: ceil_mode, pads, auto_pad, count_include_pad
	// ------------------------------------------------------------------
	for i := 0; i < 10; i++ {
		op := "MaxPool"
		if rng.Intn(2) == 0 {
			op = "AveragePool"
		}
		C := 1 + rng.Intn(3)
		H, W := 3+rng.Intn(7), 3+rng.Intn(7)
		kH, kW := 1+rng.Intn(4), 1+rng.Intn(4)
		sH, sW := 1+rng.Intn(3), 1+rng.Intn(3)
		ceil := rng.Intn(2)
		var attrs []attr
		attrs = append(attrs, attrIs("kernel_shape", int64(kH), int64(kW)))
		attrs = append(attrs, attrIs("strides", int64(sH), int64(sW)))
		if ceil > 0 {
			attrs = append(attrs, attrI("ceil_mode", int64(ceil)))
		}
		autoPads := []string{"NOTSET", "NOTSET", "NOTSET", "VALID", "SAME_UPPER", "SAME_LOWER"}
		ap := autoPads[rng.Intn(len(autoPads))]
		switch ap {
		case "NOTSET":
			pads := [4]int{rng.Intn(min(kH, 3)), rng.Intn(min(kW, 3)), rng.Intn(min(kH, 3)), rng.Intn(min(kW, 3))}
			attrs = append(attrs, attrIs("pads", int64(pads[0]), int64(pads[1]), int64(pads[2]), int64(pads[3])))
			// ensure at least one output cell in floor mode
			if ceil == 0 && (H+pads[0]+pads[2] < kH || W+pads[1]+pads[3] < kW) {
				i--
				continue
			}
		case "VALID":
			attrs = append(attrs, attrS("auto_pad", "VALID"))
			if H < kH || W < kW {
				i--
				continue
			}
		default:
			attrs = append(attrs, attrS("auto_pad", ap))
		}
		if op == "AveragePool" && rng.Intn(2) == 0 {
			attrs = append(attrs, attrI("count_include_pad", 1))
		}
		inShape := []int{1, C, H, W}
		add("pool", modelDef{
			opset:   11,
			nodes:   []node{{op: op, inputs: []string{"x"}, outs: []string{"y"}, attrs: attrs}},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(inShape))
	}

	// ------------------------------------------------------------------
	// Resize: nearest+linear x coordinate_transformation_mode x nearest_mode,
	// scales and sizes, up/down/extreme sampling
	// ------------------------------------------------------------------
	ctms := []string{"asymmetric", "half_pixel", "align_corners", "pytorch_half_pixel", "tf_half_pixel_for_nn"}
	nearestModes := []string{"floor", "ceil", "round_prefer_floor", "round_prefer_ceil"}
	for i := 0; i < 14; i++ {
		N, C := 1+rng.Intn(2), 1+rng.Intn(2)
		H, W := 2+rng.Intn(6), 2+rng.Intn(6)
		mode := "nearest"
		if rng.Intn(2) == 0 {
			mode = "linear"
		}
		ctm := ctms[rng.Intn(len(ctms))]
		if ctm == "tf_half_pixel_for_nn" && mode == "linear" {
			ctm = "half_pixel" // tf_half_pixel_for_nn is nearest-only per spec
		}
		if ctm == "align_corners" && H < 2 {
			ctm = "half_pixel"
		}
		var attrs []attr
		attrs = append(attrs, attrS("coordinate_transformation_mode", ctm), attrS("mode", mode))
		if mode == "nearest" {
			attrs = append(attrs, attrS("nearest_mode", nearestModes[rng.Intn(len(nearestModes))]))
		}
		useSizes := rng.Intn(2) == 0
		inShape := []int{N, C, H, W}
		var inits []tensorInit
		var ins []string
		opset := int64(11)
		if useSizes {
			opset = 13
			oH, oW := 1+rng.Intn(11), 1+rng.Intn(11)
			inits = append(inits,
				tensorInit{name: "roi", shape: []int64{0}, dtype: 1},
				tensorInit{name: "scales", shape: []int64{0}, dtype: 1},
				iInit("sizes", int64(N), int64(C), int64(oH), int64(oW)))
			ins = []string{"x", "roi", "scales", "sizes"}
		} else {
			// random scale in [0.25, 3.0], biased to non-integer ratios
			sH := 0.25 + rng.Float32()*2.75
			sW := 0.25 + rng.Float32()*2.75
			oH := int(math.Floor(float64(float32(H) * sH)))
			oW := int(math.Floor(float64(float32(W) * sW)))
			if oH < 1 || oW < 1 {
				i--
				continue
			}
			inits = append(inits,
				tensorInit{name: "roi", shape: []int64{0}, dtype: 1},
				tensorInit{name: "scales", shape: []int64{4}, dtype: 1, f32: []float32{1, 1, sH, sW}})
			ins = []string{"x", "roi", "scales"}
		}
		add("resize", modelDef{
			opset:   opset,
			nodes:   []node{{op: "Resize", inputs: ins, outs: []string{"y"}, attrs: attrs}},
			inits:   inits,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(inShape))
	}
	// Deterministic coverage the random loop may miss: tf_half_pixel_for_nn
	// (nearest-only, no -0.5 offset) and pytorch_half_pixel with a singleton
	// output dim (source coordinate pinned to 0).
	type detResize struct {
		ctm, mode, nearest string
		inShape            []int
		scales             []float32 // nil -> use sizes
		sizes              []int64
	}
	for _, dr := range []detResize{
		{ctm: "tf_half_pixel_for_nn", mode: "nearest", nearest: "floor", inShape: []int{1, 1, 4, 5}, scales: []float32{1, 1, 2.5, 0.6}},
		{ctm: "tf_half_pixel_for_nn", mode: "nearest", nearest: "round_prefer_floor", inShape: []int{1, 2, 3, 4}, sizes: []int64{1, 2, 1, 7}},
		{ctm: "pytorch_half_pixel", mode: "nearest", nearest: "round_prefer_ceil", inShape: []int{1, 2, 4, 5}, sizes: []int64{1, 2, 1, 9}},
		{ctm: "pytorch_half_pixel", mode: "linear", inShape: []int{1, 1, 5, 6}, sizes: []int64{1, 1, 7, 1}},
	} {
		attrs := []attr{attrS("coordinate_transformation_mode", dr.ctm), attrS("mode", dr.mode)}
		if dr.nearest != "" {
			attrs = append(attrs, attrS("nearest_mode", dr.nearest))
		}
		opset := int64(11)
		inits := []tensorInit{{name: "roi", shape: []int64{0}, dtype: 1}}
		ins := []string{"x", "roi", "scales"}
		if dr.scales != nil {
			inits = append(inits, tensorInit{name: "scales", shape: []int64{4}, dtype: 1, f32: dr.scales})
		} else {
			opset = 13
			inits = append(inits,
				tensorInit{name: "scales", shape: []int64{0}, dtype: 1},
				iInit("sizes", dr.sizes...))
			ins = append(ins, "sizes")
		}
		add("resize", modelDef{
			opset:   opset,
			nodes:   []node{{op: "Resize", inputs: ins, outs: []string{"y"}, attrs: attrs}},
			inits:   inits,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(dr.inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(dr.inShape))
	}

	// ------------------------------------------------------------------
	// Slice: random ranges/steps/negative indices/omitted axes
	// ------------------------------------------------------------------
	for i := 0; i < 10; i++ {
		rank := 2 + rng.Intn(3)
		shape := randShape(rng, rank, 3, 7)
		// choose a random subset of axes (or omit axes input entirely)
		var axes []int
		omitAxes := rng.Intn(4) == 0
		if omitAxes {
			axes = make([]int, rank)
			for a := range axes {
				axes[a] = a
			}
		} else {
			perm := rng.Perm(rank)
			k := 1 + rng.Intn(rank)
			axes = perm[:k]
		}
		var starts, ends, steps []int64
		useSteps := rng.Intn(3) != 0
		for _, a := range axes {
			d := shape[a]
			starts = append(starts, int64(rng.Intn(4*d+2)-(2*d+1)))
			ends = append(ends, int64(rng.Intn(4*d+2)-(2*d+1)))
			st := int64(1)
			if useSteps {
				st = []int64{1, 2, 3, -1, -2}[rng.Intn(5)]
			}
			steps = append(steps, st)
		}
		var inits []tensorInit
		ins := []string{"x", "starts", "ends"}
		inits = append(inits, iInit("starts", starts...), iInit("ends", ends...))
		if !omitAxes {
			inits = append(inits, iInit("axes", toI64(axes)...))
			ins = append(ins, "axes")
		}
		if useSteps {
			if omitAxes {
				ins = append(ins, "") // positional placeholder for omitted axes
			}
			inits = append(inits, iInit("steps", steps...))
			ins = append(ins, "steps")
		}
		add("slice", modelDef{
			opset:   11,
			nodes:   []node{{op: "Slice", inputs: ins, outs: []string{"y"}}},
			inits:   inits,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(shape))
	}

	// ------------------------------------------------------------------
	// broadcasting chains: 2-3 binary ops on broadcastable shapes
	// ------------------------------------------------------------------
	binOps := []string{"Add", "Sub", "Mul", "Div", "Pow"}
	for i := 0; i < 8; i++ {
		rank := 1 + rng.Intn(4)
		base := randShape(rng, rank, 1, 5)
		sA := bcastShape(rng, base)
		sB := bcastShape(rng, base)
		sC := bcastShape(rng, base)
		op1 := binOps[rng.Intn(len(binOps))]
		op2 := binOps[rng.Intn(len(binOps))]
		nodes := []node{
			{op: op1, inputs: []string{"a", "b"}, outs: []string{"t"}},
			{op: op2, inputs: []string{"t", "c"}, outs: []string{"y"}},
		}
		a := randF32(rng, numEl(sA))
		b := randF32(rng, numEl(sB))
		c := randF32(rng, numEl(sC))
		// keep Pow domains mostly valid: positive bases, exponents small
		if op1 == "Pow" {
			for j := range a {
				a[j] = float32(math.Abs(float64(a[j]))) + 0.01
			}
		}
		if op2 == "Pow" {
			for j := range c {
				c[j] = float32(math.Round(float64(c[j] * 3))) // integer exponents
			}
		}
		add("bcast", modelDef{
			opset:   11,
			nodes:   nodes,
			inputs:  []valueInfo{{name: "a", dtype: 1, shape: toI64(sA)}, {name: "b", dtype: 1, shape: toI64(sB)}, {name: "c", dtype: 1, shape: toI64(sC)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, map[string]tensorData{
			"a": f32td(sA, a),
			"b": f32td(sB, b),
			"c": f32td(sC, c),
		})
	}

	// ------------------------------------------------------------------
	// Softmax / ReduceMean axis combos
	// ------------------------------------------------------------------
	for i := 0; i < 5; i++ {
		rank := 2 + rng.Intn(3)
		shape := randShape(rng, rank, 2, 6)
		axis := rng.Intn(2*rank) - rank
		opset := int64(11)
		if i < 2 {
			opset = 13 // opset-13 semantics differ from 11 when axis != -1
		}
		data := randF32(rng, numEl(shape))
		if i == 0 { // large-magnitude inputs to exercise max-subtraction
			for j := range data {
				data[j] *= 200
			}
		}
		add("softmax", modelDef{
			opset:   opset,
			nodes:   []node{{op: "Softmax", inputs: []string{"x"}, outs: []string{"y"}, attrs: []attr{attrI("axis", int64(axis))}}},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, map[string]tensorData{"x": f32td(shape, data)})
	}
	for i := 0; i < 5; i++ {
		rank := 2 + rng.Intn(3)
		shape := randShape(rng, rank, 2, 6)
		var attrs []attr
		if rng.Intn(3) != 0 {
			perm := rng.Perm(rank)
			k := 1 + rng.Intn(rank)
			axes := make([]int64, k)
			for j, a := range perm[:k] {
				if rng.Intn(2) == 0 {
					axes[j] = int64(a - rank) // negative form
				} else {
					axes[j] = int64(a)
				}
			}
			attrs = append(attrs, attrIs("axes", axes...))
		}
		attrs = append(attrs, attrI("keepdims", int64(rng.Intn(2))))
		add("reducemean", modelDef{
			opset:   11,
			nodes:   []node{{op: "ReduceMean", inputs: []string{"x"}, outs: []string{"y"}, attrs: attrs}},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(shape))
	}

	// ------------------------------------------------------------------
	// MatMul shapes
	// ------------------------------------------------------------------
	mmShapes := []func(rng *rand.Rand) ([]int, []int){
		func(r *rand.Rand) ([]int, []int) { // 2d x 2d
			m, k, n := 1+r.Intn(5), 1+r.Intn(5), 1+r.Intn(5)
			return []int{m, k}, []int{k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // batched equal
			b, m, k, n := 1+r.Intn(3), 1+r.Intn(4), 1+r.Intn(4), 1+r.Intn(4)
			return []int{b, m, k}, []int{b, k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // b broadcast
			b, m, k, n := 2+r.Intn(2), 1+r.Intn(4), 1+r.Intn(4), 1+r.Intn(4)
			return []int{b, m, k}, []int{k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // a broadcast
			b, m, k, n := 2+r.Intn(2), 1+r.Intn(4), 1+r.Intn(4), 1+r.Intn(4)
			return []int{m, k}, []int{b, k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // batch dim 1 broadcast
			b, m, k, n := 2+r.Intn(2), 1+r.Intn(3), 1+r.Intn(3), 1+r.Intn(3)
			return []int{b, m, k}, []int{1, k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // 1d x 1d
			k := 1 + r.Intn(6)
			return []int{k}, []int{k}
		},
		func(r *rand.Rand) ([]int, []int) { // 1d x 2d
			k, n := 1+r.Intn(5), 1+r.Intn(5)
			return []int{k}, []int{k, n}
		},
		func(r *rand.Rand) ([]int, []int) { // 2d x 1d
			m, k := 1+r.Intn(5), 1+r.Intn(5)
			return []int{m, k}, []int{k}
		},
	}
	for _, gen := range mmShapes {
		sA, sB := gen(rng)
		add("matmul", modelDef{
			opset:   11,
			nodes:   []node{{op: "MatMul", inputs: []string{"a", "b"}, outs: []string{"y"}}},
			inputs:  []valueInfo{{name: "a", dtype: 1, shape: toI64(sA)}, {name: "b", dtype: 1, shape: toI64(sB)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, map[string]tensorData{
			"a": f32td(sA, randF32(rng, numEl(sA))),
			"b": f32td(sB, randF32(rng, numEl(sB))),
		})
	}

	// ------------------------------------------------------------------
	// Squeeze/Unsqueeze/Reshape/Transpose/Concat variants
	// ------------------------------------------------------------------
	for i := 0; i < 8; i++ {
		switch i % 5 {
		case 0: // Transpose random perm (or default)
			rank := 2 + rng.Intn(3)
			shape := randShape(rng, rank, 1, 5)
			var attrs []attr
			if rng.Intn(3) != 0 {
				attrs = append(attrs, attrIs("perm", toI64(rng.Perm(rank))...))
			}
			add("shapeop", modelDef{
				opset:   11,
				nodes:   []node{{op: "Transpose", inputs: []string{"x"}, outs: []string{"y"}, attrs: attrs}},
				inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
				outputs: []valueInfo{{name: "y", dtype: 1}},
			}, oneInput(shape))
		case 1: // Reshape with -1 / 0
			shape := randShape(rng, 2+rng.Intn(2), 2, 5)
			total := numEl(shape)
			// pick a target: split total into 2-3 factors, maybe one -1
			d1 := 1 + rng.Intn(total)
			for total%d1 != 0 {
				d1 = 1 + rng.Intn(total)
			}
			rest := total / d1
			var tgt []int64
			if rest > 1 && rng.Intn(2) == 0 {
				d2 := 1 + rng.Intn(rest)
				for rest%d2 != 0 {
					d2 = 1 + rng.Intn(rest)
				}
				tgt = []int64{int64(d1), int64(d2), int64(rest / d2)}
			} else {
				tgt = []int64{int64(d1), int64(rest)}
			}
			if rng.Intn(2) == 0 { // replace one dim with -1
				tgt[rng.Intn(len(tgt))] = -1
			} else if rng.Intn(3) == 0 { // use 0 (copy dim) where it matches the input dim
				pos := rng.Intn(min(len(tgt), len(shape)))
				if tgt[pos] == int64(shape[pos]) {
					tgt[pos] = 0
				}
			}
			add("shapeop", modelDef{
				opset:   11,
				nodes:   []node{{op: "Reshape", inputs: []string{"x", "shape"}, outs: []string{"y"}}},
				inits:   []tensorInit{iInit("shape", tgt...)},
				inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
				outputs: []valueInfo{{name: "y", dtype: 1}},
			}, oneInput(shape))
		case 2: // Squeeze (attr axes, opset 11)
			rank := 3 + rng.Intn(2)
			shape := randShape(rng, rank, 1, 4)
			// ensure some dims are 1
			nOnes := 1 + rng.Intn(2)
			var ones []int64
			for j := 0; j < nOnes; j++ {
				p := rng.Intn(rank)
				shape[p] = 1
				if rng.Intn(2) == 0 {
					ones = append(ones, int64(p-rank)) // negative form
				} else {
					ones = append(ones, int64(p))
				}
			}
			var attrs []attr
			if rng.Intn(3) != 0 {
				attrs = append(attrs, attrIs("axes", ones...))
			}
			add("shapeop", modelDef{
				opset:   11,
				nodes:   []node{{op: "Squeeze", inputs: []string{"x"}, outs: []string{"y"}, attrs: attrs}},
				inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
				outputs: []valueInfo{{name: "y", dtype: 1}},
			}, oneInput(shape))
		case 3: // Unsqueeze (attr axes, opset 11)
			rank := 1 + rng.Intn(3)
			shape := randShape(rng, rank, 2, 4)
			newRank := rank + 1 + rng.Intn(2)
			perm := rng.Perm(newRank)
			k := newRank - rank
			axes := make([]int64, k)
			for j, a := range perm[:k] {
				axes[j] = int64(a)
			}
			add("shapeop", modelDef{
				opset:   11,
				nodes:   []node{{op: "Unsqueeze", inputs: []string{"x"}, outs: []string{"y"}, attrs: []attr{attrIs("axes", axes...)}}},
				inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
				outputs: []valueInfo{{name: "y", dtype: 1}},
			}, oneInput(shape))
		case 4: // Concat 2-4 inputs, random axis
			rank := 1 + rng.Intn(3)
			shape := randShape(rng, rank, 1, 4)
			axis := rng.Intn(2*rank) - rank
			k := 2 + rng.Intn(3)
			names := []string{"a", "b", "c", "d"}[:k]
			var inputs []valueInfo
			td := map[string]tensorData{}
			ax := axis
			if ax < 0 {
				ax += rank
			}
			for _, nm := range names {
				s := append([]int(nil), shape...)
				s[ax] = 1 + rng.Intn(3)
				inputs = append(inputs, valueInfo{name: nm, dtype: 1, shape: toI64(s)})
				td[nm] = f32td(s, randF32(rng, numEl(s)))
			}
			add("shapeop", modelDef{
				opset:   11,
				nodes:   []node{{op: "Concat", inputs: names, outs: []string{"y"}, attrs: []attr{attrI("axis", int64(axis))}}},
				inputs:  inputs,
				outputs: []valueInfo{{name: "y", dtype: 1}},
			}, td)
		}
	}

	// ------------------------------------------------------------------
	// HardSigmoid/Erf/Sigmoid on adversarial values
	// ------------------------------------------------------------------
	adversarial := func(rng *rand.Rand, n int, alpha, beta float32) []float32 {
		pool := []float32{
			0, float32(math.Copysign(0, -1)),
			1e-40, -1e-40, // subnormals
			1e-38, -1e-38, // near min normal
			1e30, -1e30,
			float32(math.Inf(1)), float32(math.Inf(-1)),
			88.0, -88.0, 100.0, -100.0, // exp overflow region
		}
		if alpha != 0 {
			pool = append(pool, -beta/alpha, (1-beta)/alpha) // HardSigmoid clip boundaries
		}
		out := make([]float32, n)
		for i := range out {
			switch rng.Intn(3) {
			case 0:
				out[i] = pool[rng.Intn(len(pool))]
			case 1:
				out[i] = (rng.Float32()*2 - 1) * 50
			default:
				out[i] = rng.Float32()*2 - 1
			}
		}
		return out
	}
	for i := 0; i < 10; i++ {
		shape := randShape(rng, 2+rng.Intn(2), 2, 5)
		alpha := 0.1 + rng.Float32()*0.4
		beta := rng.Float32()
		data := adversarial(rng, numEl(shape), alpha, beta)
		var nodes []node
		switch i % 3 {
		case 0:
			nodes = []node{{op: "HardSigmoid", inputs: []string{"x"}, outs: []string{"y"},
				attrs: []attr{attrF("alpha", alpha), attrF("beta", beta)}}}
		case 1:
			nodes = []node{{op: "Erf", inputs: []string{"x"}, outs: []string{"y"}}}
		case 2:
			nodes = []node{{op: "Sigmoid", inputs: []string{"x"}, outs: []string{"y"}}}
		}
		add("advunary", modelDef{
			opset:   11,
			nodes:   nodes,
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, map[string]tensorData{"x": f32td(shape, data)})
	}

	// ------------------------------------------------------------------
	// dynamic int64 chains: Shape -> Slice -> Concat -> Reshape
	// ------------------------------------------------------------------
	for i := 0; i < 4; i++ {
		shape := []int{1 + rng.Intn(2), 1 + rng.Intn(4), 3 + rng.Intn(5), 3 + rng.Intn(5)}
		// take dims [start:4] of the shape, concat with -1 (auto-computed, so
		// the reshape is always valid regardless of the slice length)
		start := int64(1 + rng.Intn(3))
		end := int64(4)
		var tail int64 = -1
		add("dynchain", modelDef{
			opset: 11,
			nodes: []node{
				{op: "Shape", inputs: []string{"x"}, outs: []string{"sh"}},
				{op: "Slice", inputs: []string{"sh", "starts", "ends", "axes", "steps"}, outs: []string{"hw"}},
				{op: "Concat", inputs: []string{"hw", "extra"}, outs: []string{"tgt"}, attrs: []attr{attrI("axis", 0)}},
				{op: "Reshape", inputs: []string{"x", "tgt"}, outs: []string{"y"}},
			},
			inits: []tensorInit{
				iInit("starts", start), iInit("ends", end), iInit("axes", 0), iInit("steps", 1),
				iInit("extra", tail),
			},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(shape))
	}

	// ------------------------------------------------------------------
	// BatchNormalization random channels/epsilon
	// ------------------------------------------------------------------
	for i := 0; i < 4; i++ {
		C := 1 + rng.Intn(5)
		shape := []int{1 + rng.Intn(2), C, 2 + rng.Intn(4), 2 + rng.Intn(4)}
		varData := make([]float32, C)
		for j := range varData {
			varData[j] = 0.1 + rng.Float32()*2
		}
		add("batchnorm", modelDef{
			opset: 11,
			nodes: []node{{op: "BatchNormalization", inputs: []string{"x", "scale", "bias", "mean", "var"}, outs: []string{"y"},
				attrs: []attr{attrF("epsilon", 1e-5*float32(math.Pow(10, float64(rng.Intn(3)))))}}},
			inits: []tensorInit{
				wInit("scale", []int64{int64(C)}, rng),
				wInit("bias", []int64{int64(C)}, rng),
				wInit("mean", []int64{int64(C)}, rng),
				{name: "var", shape: []int64{int64(C)}, dtype: 1, f32: varData},
			},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(shape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(shape))
	}

	// ------------------------------------------------------------------
	// mixed composed graphs: conv -> act -> pool -> reshape -> matmul
	// ------------------------------------------------------------------
	for i := 0; i < 6; i++ {
		N, C := 1, 1+rng.Intn(3)
		H, W := 5+rng.Intn(4), 5+rng.Intn(4)
		M := 1 + rng.Intn(3)
		k := 2 + rng.Intn(2)
		inShape := []int{N, C, H, W}
		act := []string{"Relu", "Sigmoid", "HardSigmoid"}[rng.Intn(3)]
		var actAttrs []attr
		if act == "HardSigmoid" {
			actAttrs = []attr{attrF("alpha", 0.2), attrF("beta", 0.5)}
		}
		oH, oW := H-k+1, W-k+1
		pk := 2
		pH, pW := oH/pk, oW/pk
		flat := M * pH * pW
		outN := 2 + rng.Intn(4)
		add("mixed", modelDef{
			opset: 11,
			nodes: []node{
				{op: "Conv", inputs: []string{"x", "w", "b"}, outs: []string{"c"},
					attrs: []attr{attrIs("kernel_shape", int64(k), int64(k))}},
				{op: act, inputs: []string{"c"}, outs: []string{"a"}, attrs: actAttrs},
				{op: "MaxPool", inputs: []string{"a"}, outs: []string{"p"},
					attrs: []attr{attrIs("kernel_shape", int64(pk), int64(pk)), attrIs("strides", int64(pk), int64(pk))}},
				{op: "Reshape", inputs: []string{"p", "flat"}, outs: []string{"f"}},
				{op: "MatMul", inputs: []string{"f", "mw"}, outs: []string{"m"}},
				{op: "Softmax", inputs: []string{"m"}, outs: []string{"y"}, attrs: []attr{attrI("axis", -1)}},
			},
			inits: []tensorInit{
				wInit("w", []int64{int64(M), int64(C), int64(k), int64(k)}, rng),
				wInit("b", []int64{int64(M)}, rng),
				iInit("flat", int64(N), int64(flat)),
				wInit("mw", []int64{int64(flat), int64(outN)}, rng),
			},
			inputs:  []valueInfo{{name: "x", dtype: 1, shape: toI64(inShape)}},
			outputs: []valueInfo{{name: "y", dtype: 1}},
		}, oneInput(inShape))
	}

	// ------------------------------------------------------------------
	// random multi-node DAGs (3-8 nodes, shared consumers, fusion motifs,
	// intermediate graph outputs) — see dag.go
	// ------------------------------------------------------------------
	genDAGCases(rng, add)

	// write cases
	for _, c := range cases {
		if *only != "" && !strings.Contains(c.name, *only) {
			continue
		}
		dir := filepath.Join(*outRoot, c.name)
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
	fmt.Printf("%d cases (seed %d)\n", len(cases), *seed)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
