package yolo

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// .yolow file format:
//
// Header (32 bytes):
//   magic:     [4]byte = "YOLW"
//   version:   uint32  = 1
//   inputSize: uint32  = 640
//   nc:        uint32  = number of classes
//   regMax:    uint32  = 16
//   numLayers: uint32  = number of weight tensors
//   reserved:  [8]byte = 0
//
// For each layer:
//   nameLen: uint32
//   name:    [nameLen]byte (UTF-8 layer name)
//   ndim:    uint32
//   shape:   [ndim]uint32
//   data:    [product(shape)]float32 (little-endian)

const yolowMagic = "YOLW"

// LoadModel loads a YOLO11 model from a .yolow file.
func LoadModel(path string) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open weights: %w", err)
	}
	defer f.Close()

	var magic [4]byte
	if err := binary.Read(f, binary.LittleEndian, &magic); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if string(magic[:]) != yolowMagic {
		return nil, fmt.Errorf("invalid magic: %q", magic)
	}

	var version, inputSize, nc, regMax, numLayers uint32
	binary.Read(f, binary.LittleEndian, &version)
	binary.Read(f, binary.LittleEndian, &inputSize)
	binary.Read(f, binary.LittleEndian, &nc)
	binary.Read(f, binary.LittleEndian, &regMax)
	binary.Read(f, binary.LittleEndian, &numLayers)

	var reserved [8]byte
	binary.Read(f, binary.LittleEndian, &reserved)

	weights := make(map[string]*Tensor, numLayers)
	for i := uint32(0); i < numLayers; i++ {
		name, tensor, err := readWeightTensor(f)
		if err != nil {
			return nil, fmt.Errorf("read layer %d: %w", i, err)
		}
		weights[name] = tensor
	}

	model, err := buildModel(weights, int(inputSize), int(nc), int(regMax))
	if err != nil {
		return nil, fmt.Errorf("build model: %w", err)
	}
	return model, nil
}

func readWeightTensor(r io.Reader) (string, *Tensor, error) {
	var nameLen uint32
	if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
		return "", nil, err
	}
	nameBuf := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBuf); err != nil {
		return "", nil, err
	}

	var ndim uint32
	if err := binary.Read(r, binary.LittleEndian, &ndim); err != nil {
		return "", nil, err
	}
	shape := make([]int, ndim)
	size := 1
	for i := uint32(0); i < ndim; i++ {
		var s uint32
		binary.Read(r, binary.LittleEndian, &s)
		shape[i] = int(s)
		size *= int(s)
	}

	data := make([]float32, size)
	if err := binary.Read(r, binary.LittleEndian, data); err != nil {
		return "", nil, fmt.Errorf("read data for %s: %w", string(nameBuf), err)
	}

	return string(nameBuf), NewTensorFrom(data, shape...), nil
}

// ── model builder ──

func buildModel(w map[string]*Tensor, inputSize, nc, regMax int) (*Model, error) {
	m := &Model{InputSize: inputSize, NC: nc}

	// Backbone
	m.B0 = convBN(w, "model.0", 2, 1, true, 1)
	m.B1 = convBN(w, "model.1", 2, 1, true, 1)
	m.B2 = buildC3k2(w, "model.2")
	m.B3 = convBN(w, "model.3", 2, 1, true, 1)
	m.B4 = buildC3k2(w, "model.4")
	m.B5 = convBN(w, "model.5", 2, 1, true, 1)
	m.B6 = buildC3k2(w, "model.6")
	m.B7 = convBN(w, "model.7", 2, 1, true, 1)
	m.B8 = buildC3k2(w, "model.8")
	m.B9 = buildSPPF(w, "model.9")
	m.B10 = buildC2PSA(w, "model.10")

	// Neck
	m.N13 = buildC3k2(w, "model.13")
	m.N16 = buildC3k2(w, "model.16")
	m.N17 = convBN(w, "model.17", 2, 1, true, 1)
	m.N19 = buildC3k2(w, "model.19")
	m.N20 = convBN(w, "model.20", 2, 1, true, 1)
	m.N22 = buildC3k2(w, "model.22")

	// Detect head
	m.Head = buildDetectHead(w, "model.23", nc, regMax)

	return m, nil
}

// ── conv builder ──

// convBN builds a Conv2dBNSiLU with fused BatchNorm.
// Key pattern: prefix+".conv.weight", prefix+".bn.weight", etc.
func convBN(w map[string]*Tensor, prefix string, stride, padding int, silu bool, groups int) *Conv2dBNSiLU {
	convW := w[prefix+".conv.weight"]
	bnGamma := w[prefix+".bn.weight"]
	bnBeta := w[prefix+".bn.bias"]
	bnMean := w[prefix+".bn.running_mean"]
	bnVar := w[prefix+".bn.running_var"]

	outC := convW.Shape[0]
	inCPerGroup := convW.Shape[1]
	kh := convW.Shape[2]
	kw := convW.Shape[3]
	inC := inCPerGroup * groups

	eps := float32(1e-3) // ultralytics uses eps=0.001

	fusedW := convW.Clone()
	fusedB := make([]float32, outC)

	for oc := 0; oc < outC; oc++ {
		scale := bnGamma.Data[oc] / float32(math.Sqrt(float64(bnVar.Data[oc]+eps)))
		fusedB[oc] = -bnMean.Data[oc]*scale + bnBeta.Data[oc]
		wOff := oc * inCPerGroup * kh * kw
		for i := 0; i < inCPerGroup*kh*kw; i++ {
			fusedW.Data[wOff+i] *= scale
		}
	}

	c := &Conv2dBNSiLU{
		Weight: fusedW, Bias: fusedB,
		OutC: outC, InC: inC, KH: kh, KW: kw,
		Stride: stride, Padding: padding,
		Groups: groups, UseSiLU: silu,
	}
	c.InitWinograd()
	return c
}

// convNoBN builds a Conv2d without BatchNorm (detect head final layers).
func convNoBN(w map[string]*Tensor, prefix string) *Conv2dBNSiLU {
	convW := w[prefix+".weight"]
	convB := w[prefix+".bias"]
	return &Conv2dBNSiLU{
		Weight: convW, Bias: convB.Data,
		OutC: convW.Shape[0], InC: convW.Shape[1],
		KH: convW.Shape[2], KW: convW.Shape[3],
		Stride: 1, Padding: 0, Groups: 1, UseSiLU: false,
	}
}

// ── C3k2 builder ──

func buildC3k2(w map[string]*Tensor, prefix string) *C3k2 {
	c := &C3k2{
		CV1: convBN(w, prefix+".cv1", 1, 0, true, 1),
		CV2: convBN(w, prefix+".cv2", 1, 0, true, 1),
	}
	// Count C3k sub-modules by checking for weight keys
	for i := 0; ; i++ {
		key := fmt.Sprintf("%s.m.%d.cv1.conv.weight", prefix, i)
		if _, ok := w[key]; !ok {
			break
		}
		c.Modules = append(c.Modules, buildC3k(w, fmt.Sprintf("%s.m.%d", prefix, i)))
	}
	return c
}

func buildC3k(w map[string]*Tensor, prefix string) *C3k {
	c := &C3k{
		CV1: convBN(w, prefix+".cv1", 1, 0, true, 1),
		CV2: convBN(w, prefix+".cv2", 1, 0, true, 1),
		CV3: convBN(w, prefix+".cv3", 1, 0, true, 1),
	}
	// Count bottlenecks
	for i := 0; ; i++ {
		key := fmt.Sprintf("%s.m.%d.cv1.conv.weight", prefix, i)
		if _, ok := w[key]; !ok {
			break
		}
		bn := &Bottleneck{
			CV1:      convBN(w, fmt.Sprintf("%s.m.%d.cv1", prefix, i), 1, 1, true, 1),
			CV2:      convBN(w, fmt.Sprintf("%s.m.%d.cv2", prefix, i), 1, 1, true, 1),
			Shortcut: true,
		}
		c.Bottlenecks = append(c.Bottlenecks, bn)
	}
	return c
}

// ── SPPF builder ──

func buildSPPF(w map[string]*Tensor, prefix string) *SPPF {
	return &SPPF{
		CV1: convBN(w, prefix+".cv1", 1, 0, true, 1),
		CV2: convBN(w, prefix+".cv2", 1, 0, true, 1),
		K:   5,
	}
}

// ── C2PSA builder ──

func buildC2PSA(w map[string]*Tensor, prefix string) *C2PSA {
	c := &C2PSA{
		CV1: convBN(w, prefix+".cv1", 1, 0, true, 1),
		CV2: convBN(w, prefix+".cv2", 1, 0, true, 1),
	}
	for i := 0; ; i++ {
		key := fmt.Sprintf("%s.m.%d.attn.qkv.conv.weight", prefix, i)
		if _, ok := w[key]; !ok {
			break
		}
		c.Blocks = append(c.Blocks, buildPSABlock(w, fmt.Sprintf("%s.m.%d", prefix, i)))
	}
	return c
}

func buildPSABlock(w map[string]*Tensor, prefix string) *PSABlock {
	// Determine num_heads from QKV weight shape
	qkvW := w[prefix+".attn.qkv.conv.weight"]
	outC := qkvW.Shape[0] // 2 * halfC
	halfC := outC / 2
	numHeads := 1
	// YOLO11 uses num_heads = halfC / 64 (head_dim=64)
	if halfC >= 64 {
		numHeads = halfC / 64
	}

	// PE is depthwise conv: groups = channels
	peW := w[prefix+".attn.pe.conv.weight"]
	peGroups := peW.Shape[0] // [C, 1, 3, 3] → groups = C

	return &PSABlock{
		Attn: &Attention{
			QKV:      convBN(w, prefix+".attn.qkv", 1, 0, false, 1),
			Proj:     convBN(w, prefix+".attn.proj", 1, 0, false, 1),
			PE:       convBN(w, prefix+".attn.pe", 1, 1, false, peGroups),
			NumHeads: numHeads,
		},
		FFN0: convBN(w, prefix+".ffn.0", 1, 0, true, 1),
		FFN1: convBN(w, prefix+".ffn.1", 1, 0, false, 1),
	}
}

// ── Detect head builder ──

func buildDetectHead(w map[string]*Tensor, prefix string, nc, regMax int) *DetectHead {
	nScales := 3
	head := &DetectHead{
		CV2:    make([][]Layer, nScales),
		CV3:    make([][]Layer, nScales),
		NC:     nc,
		RegMax: regMax,
		Stride: []int{8, 16, 32},
	}

	for i := 0; i < nScales; i++ {
		// Box regression: 2 Conv+BN+SiLU + 1 Conv (no BN)
		head.CV2[i] = []Layer{
			convBN(w, fmt.Sprintf("%s.cv2.%d.0", prefix, i), 1, 1, true, 1),
			convBN(w, fmt.Sprintf("%s.cv2.%d.1", prefix, i), 1, 1, true, 1),
			convNoBN(w, fmt.Sprintf("%s.cv2.%d.2", prefix, i)),
		}

		// Classification: 2 DWConv+Conv pairs + 1 Conv (no BN)
		dwGroups0 := w[fmt.Sprintf("%s.cv3.%d.0.0.conv.weight", prefix, i)].Shape[0]
		dwGroups1 := w[fmt.Sprintf("%s.cv3.%d.1.0.conv.weight", prefix, i)].Shape[0]

		head.CV3[i] = []Layer{
			buildDWSepConv(w,
				fmt.Sprintf("%s.cv3.%d.0.0", prefix, i), dwGroups0,
				fmt.Sprintf("%s.cv3.%d.0.1", prefix, i)),
			buildDWSepConv(w,
				fmt.Sprintf("%s.cv3.%d.1.0", prefix, i), dwGroups1,
				fmt.Sprintf("%s.cv3.%d.1.1", prefix, i)),
			convNoBN(w, fmt.Sprintf("%s.cv3.%d.2", prefix, i)),
		}
	}

	return head
}

// DWSepConv is a depthwise separable convolution: DWConv(groups=C) + Conv(1x1).
type DWSepConv struct {
	DW *Conv2dBNSiLU
	PW *Conv2dBNSiLU
}

func (d *DWSepConv) Forward(x *Tensor) *Tensor {
	return d.PW.Forward(d.DW.Forward(x))
}

func buildDWSepConv(w map[string]*Tensor, dwPrefix string, dwGroups int, pwPrefix string) Layer {
	return &DWSepConv{
		DW: convBN(w, dwPrefix, 1, 1, true, dwGroups),
		PW: convBN(w, pwPrefix, 1, 0, true, 1),
	}
}
