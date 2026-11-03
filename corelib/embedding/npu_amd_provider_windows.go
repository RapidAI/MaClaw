//go:build windows && amd64

// npu_amd_provider_windows.go — AMD XDNA NPU provider for the embedding chain.
//
// Contract (device_select.go): a provider is a candidate that either serves
// whole forward passes or declines with a reason. NPUForwarder.Forward fills
// outs[i] with the last row of the final hidden state BEFORE the output
// RMSNorm — poolVectors applies that norm, so a backend that pre-norms would be
// rejected by the self-check for no good reason.
//
// The IPU backend cannot execute an ONNX graph at all: it accepts only a
// compiled .amodel, because the Phoenix overlay kernels are pre-compiled for
// fixed shapes. So the interesting logic here is discovery and honest
// reporting, not the infer call.
//
// Where a .amodel can come from:
//
//  1. MACLAW_NPU_AMODEL — an explicit path, for a model compiled offline on a
//     machine with the full Vitis AI / Ryzen AI toolchain.
//  2. <dir of the GGUF>/npu/qwen3-embedding-npu.amodel, then beside the GGUF,
//     so a packaged build is picked up with no environment variable.
//
// Compilation itself cannot happen on this host: it needs the AIE toolchain
// (voe / vitis-ai-quantization / onnxruntime-vitisai), none of which is
// installable from PyPI. That is why this file is careful to distinguish
// "no runtime" from "runtime fine, no compiled model".

package embedding

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	amodelEnvVar   = "MACLAW_NPU_AMODEL"
	amodelFileName = "qwen3-embedding-npu.amodel"
	amodelDirName  = "npu"
	// amodelSeqLen is the fixed sequence length the graph is compiled for. The
	// IPU overlay has static shapes, so the exporter must use the same value.
	amodelSeqLen = 512
)

// AMDNPUCompiledModelHint explains the one missing piece, in user terms.
//
// Corrected after checking AMD's docs: Ryzen AI 1.3 DOES support Phoenix
// (7840HS included), so the toolchain exists for this chip. What is missing is
// the compiled model, and the compiler ships only inside AMD's Ryzen AI
// installer — it is not on PyPI (voe, vitis-ai-quantization and
// onnxruntime-vitisai all 404 there), which is why it cannot be pip-installed.
const AMDNPUCompiledModelHint = "AMD IPU 只接受离线编译的 .amodel 图（ONNX 无法直接执行）；" +
	"编译工具链随 AMD Ryzen AI 1.3 官方安装包提供（支持 Phoenix/7840HS），但不在 PyPI 上，" +
	"需手动下载安装后量化并编译出 .amodel，再用 " + amodelEnvVar + " 指定"

// amdNPUAMD is the AMD XDNA implementation of NPUForwarder.
//
// It is a whole-model backend: either every token of the sequence runs on the
// NPU or the backend is absent. There is no partial offload, because splitting
// a transformer across two devices costs more in transfers than the NPU saves.
type amdNPUAMD struct {
	ctx   *rmlIPUContext
	model *rmlIPUModel

	mu       sync.Mutex // one infer at a time: the overlay is a single resource
	seqCap   int        // fixed sequence length the graph was compiled for
	dim      int        // output hidden dimension
	inFloats []float32  // host staging buffer for the ID input
	outVec   []float32  // host staging buffer for the hidden output

	closed atomic.Bool
}

// amodelCandidates returns the paths worth trying, in priority order.
func amodelCandidates(modelPath string) []string {
	var out []string
	if p := strings.TrimSpace(os.Getenv(amodelEnvVar)); p != "" {
		out = append(out, p)
	}
	if modelPath != "" {
		dir := filepath.Dir(modelPath)
		out = append(out,
			filepath.Join(dir, amodelDirName, amodelFileName),
			filepath.Join(dir, amodelFileName),
		)
	}
	return out
}

// findAmodel returns the first candidate that exists as a regular file.
func findAmodel(modelPath string) (string, []string) {
	tried := amodelCandidates(modelPath)
	for _, p := range tried {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, tried
		}
	}
	return "", tried
}

// newAMDNPU builds the NPU forwarder for an already-loaded model. q supplies
// the GGUF location (so the .amodel can sit beside it) and the hidden size.
func newAMDNPU(q *Qwen3Embedder) (NPUForwarder, error) {
	modelPath := ""
	if q != nil {
		modelPath = q.modelName
	}
	path, tried := findAmodel(modelPath)
	if path == "" {
		if len(tried) == 0 {
			return nil, fmt.Errorf("%s", AMDNPUCompiledModelHint)
		}
		return nil, fmt.Errorf("%s（已查找：%s）", AMDNPUCompiledModelHint, strings.Join(tried, "; "))
	}

	ctx, err := openIPUContext()
	if err != nil {
		return nil, err
	}
	graph, err := ctx.loadGraph(path)
	if err != nil {
		ctx.close()
		return nil, fmt.Errorf("加载 %s 失败: %w", filepath.Base(path), err)
	}
	model, err := ctx.newModel(graph)
	if err != nil {
		ctx.releaseGraph(graph)
		ctx.close()
		// This is the failure an ONNX file produces; name it explicitly so the
		// user does not go looking for a driver problem.
		return nil, fmt.Errorf("%s 不是可执行的 IPU 模型: %w", filepath.Base(path), err)
	}
	d := &amdNPUAMD{ctx: ctx, model: model}
	if err := d.initTensors(); err != nil {
		d.Release()
		return nil, err
	}
	if q != nil && q.hp.Dim > 0 && d.dim != q.hp.Dim {
		d.Release()
		return nil, fmt.Errorf("编译模型的输出维度 %d 与模型 hidden %d 不一致", d.dim, q.hp.Dim)
	}
	log.Printf("qwen3: NPU (AMD XDNA) loaded %s, seq=%d dim=%d",
		filepath.Base(path), d.seqCap, d.dim)
	return d, nil
}

// initTensors allocates the input/output tensors and wires them to the model.
//
// The graph declares [1, seq] float32 token IDs in and [1, dim] float32 hidden
// out. IDs are float32 because the IPU path here does not accept integer
// tensors; the exporter casts inside the graph.
func (d *amdNPUAMD) initTensors() error {
	rt := d.ctx.rt
	m := d.model

	inShape := []uint32{1, amodelSeqLen}
	in, _, inElems, err := rt.createF32Tensor(d.ctx.ctx, inShape, rmlLayoutNC, rmlAccessReadWrite)
	if err != nil {
		return fmt.Errorf("分配 NPU 输入张量: %w", err)
	}
	m.inTensor, m.inElems = in, inElems
	d.seqCap = amodelSeqLen
	d.inFloats = make([]float32, inElems)

	if err := rt.setInputInfo(m.model, "", inShape); err != nil {
		return err
	}
	if err := rt.setInput(m.model, "", in); err != nil {
		return err
	}

	// Output shape is only known once the inputs are fixed, so ask the model.
	outShape, err := rt.outputShape(m.model)
	if err != nil {
		return err
	}
	out, _, outElems, err := rt.createF32Tensor(d.ctx.ctx, outShape, rmlLayoutNC, rmlAccessReadWrite)
	if err != nil {
		return fmt.Errorf("分配 NPU 输出张量: %w", err)
	}
	m.outTensor, m.outElems = out, outElems
	d.outVec = make([]float32, outElems)
	d.dim = int(outShape[len(outShape)-1])

	if err := rt.setOutput(m.model, "", out); err != nil {
		return err
	}
	return rt.prepare(m.model)
}

// Forward runs every sequence in seqs on the NPU, writing the final hidden row
// (pre-RMSNorm) into outs[i].
func (d *amdNPUAMD) Forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	if d == nil || d.closed.Load() {
		return fmt.Errorf("NPU device closed")
	}
	for i, ids := range seqs {
		if len(ids) == 0 {
			return fmt.Errorf("NPU embed: sequence %d is empty", i)
		}
		if len(ids) > d.seqCap {
			// The graph has a fixed length; truncating would silently change the
			// vector, so decline and let the chain fall back.
			return fmt.Errorf("NPU embed: %d tokens exceed compiled seq %d", len(ids), d.seqCap)
		}
		if outs[i] == nil || len(outs[i]) < d.dim {
			return fmt.Errorf("NPU embed: output %d is %d wide, want %d", i, len(outs[i]), d.dim)
		}
	}
	for i, ids := range seqs {
		vec, err := d.embedOne(ids)
		if err != nil {
			return err
		}
		copy(outs[i], vec)
	}
	return nil
}

// embedOne runs one sequence through the IPU.
func (d *amdNPUAMD) embedOne(ids []int) ([]float32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Zero-pad to the compiled length. The graph is responsible for masking
	// padded positions; token ID 0 is a valid pad for every Qwen tokenizer.
	for i := range d.inFloats {
		d.inFloats[i] = 0
	}
	for i, id := range ids {
		d.inFloats[i] = float32(id)
	}

	rt := d.ctx.rt
	m := d.model

	inView, inPtr, err := rt.mapF32Tensor(m.inTensor, m.inElems)
	if err != nil {
		return nil, err
	}
	copy(inView, d.inFloats)
	if err := rt.unmapF32Tensor(m.inTensor, inPtr); err != nil {
		return nil, err
	}

	if err := rt.inferModel(m.model); err != nil {
		return nil, err
	}

	outView, outPtr, err := rt.mapF32Tensor(m.outTensor, m.outElems)
	if err != nil {
		return nil, err
	}
	copy(d.outVec, outView)
	if err := rt.unmapF32Tensor(m.outTensor, outPtr); err != nil {
		return nil, err
	}
	return d.outVec, nil
}

// Name reports the device for the acceleration diagnostics line.
func (d *amdNPUAMD) Name() string {
	if d == nil || d.ctx == nil {
		return "AMD XDNA NPU"
	}
	if v := d.ctx.versionString(); v != "" {
		return "AMD XDNA NPU (IPU " + v + ")"
	}
	return "AMD XDNA NPU"
}

// Release drops the device. Safe to call twice and on nil.
func (d *amdNPUAMD) Release() {
	if d == nil || d.closed.Swap(true) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.model != nil {
		d.model.close()
		d.model = nil
	}
	if d.ctx != nil {
		d.ctx.close()
		d.ctx = nil
	}
}

// RegisterAMDNPUProvider installs the AMD XDNA provider together with its
// lightweight prober. Prefer this in a host that wants the strict verdict
// ("ready" only when a compiled model exists).
func RegisterAMDNPUProvider() {
	RegisterNPUBackendWithProbe(newAMDNPU, probeAMDNPU)
}

// init registers the provider factory only — not the prober.
//
// The prober would give the strictest verdict, but it answers for the whole
// process and would override the prober a test installs for its own purposes.
// Registering just the factory keeps the chain honest in production: ProbeNPU
// reports the device scan, initNPU then calls newAMDNPU, and a missing .amodel
// surfaces as a real reason in NPUReason() before the chain falls through to
// Vulkan/CPU. The failure is cached by markNPUProbeFailed, so the stat() calls
// happen once per process, not per embedder.
func init() {
	RegisterNPUBackend(newAMDNPU)
}

// probeAMDNPU creates and releases one IPU context, which is exactly what the
// prober contract asks for: proof that the runtime comes up in this process.
//
// It deliberately does NOT check for a compiled model. The probe runs before
// any embedder exists, so it has no model path to search beside; looking only
// at the env var would wrongly decline a .amodel that sits next to the GGUF.
// Model discovery belongs to newAMDNPU, which knows the path, and a missing
// model there is cached by markNPUProbeFailed so the search costs one stat
// pass per process rather than one per embedder.
func probeAMDNPU() (string, error) {
	ctx, err := openIPUContext()
	if err != nil {
		return "", fmt.Errorf("AMD IPU runtime unavailable: %w", err)
	}
	device := "AMD XDNA NPU"
	if v := ctx.versionString(); v != "" {
		device += " (IPU " + v + ")"
	}
	ctx.close()
	return device, nil
}
