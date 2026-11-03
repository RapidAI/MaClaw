package embedding

// device_select.go — one place that decides which hardware runs the Qwen3
// forward pass.
//
// Before this file the choice was implicit: NewQwen3Embedder called initGPU
// (Metal) then initVulkan (Vulkan) and whatever failed left a reason behind.
// The order was fixed, there was no way to ask for a specific device, and the
// NPU was only ever *detected* (accel_detect_windows.go) for the About badge —
// it could never be used even on a machine that has one.
//
// The policy here is a single ordered chain, evaluated once at load time (and
// again whenever the hardware-acceleration toggle flips):
//
//	NPU  ->  platform GPU (Metal on macOS, Vulkan on Windows/Linux)  ->  CPU SIMD
//
// The first entry that comes up AND passes its numeric self-check against the
// CPU vectors serves every forward; the rest of the chain is skipped. CPU SIMD
// closes the chain unconditionally: it is the reference implementation the
// other backends are validated against, so no selection failure can leave the
// embedder without a backend, and no backend is ever adopted on faith.

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Device identifiers used by EmbedDeviceChain.
const (
	DeviceAuto   = "auto"
	DeviceNPU    = "npu"
	DeviceVulkan = "vulkan"
	DeviceMetal  = "metal"
	DeviceCPU    = "cpu"
)

// EmbedDeviceEnv pins the device chain, e.g. MACLAW_EMBED_DEVICE=npu (or
// auto|cpu|vulkan|metal). The legacy MACLAW_EMBED_ACCEL values (cpu/off, npu,
// dml) keep their old meaning when the new variable is unset.
const EmbedDeviceEnv = "MACLAW_EMBED_DEVICE"

// EmbedDeviceChain returns the ordered device candidates for this process.
// The last element is always DeviceCPU.
func EmbedDeviceChain() []string {
	if env := strings.ToLower(strings.TrimSpace(os.Getenv(EmbedDeviceEnv))); env != "" && env != DeviceAuto {
		return pinChain(env)
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_EMBED_ACCEL"))) {
	case "cpu", "off", "dml":
		// dml was never default-enabled; it pins the CPU path as it always did.
		return []string{DeviceCPU}
	case DeviceNPU:
		return npuChain()
	}
	if !HWAccelPreferred() {
		// The user turned hardware acceleration off: never pay a device
		// upload and probe just to release the backend afterwards.
		return []string{DeviceCPU}
	}
	return npuChain()
}

// npuChain is the default priority: NPU first, then the platform GPU, then CPU.
func npuChain() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{DeviceNPU, DeviceMetal, DeviceCPU}
	case "windows", "linux":
		return []string{DeviceNPU, DeviceVulkan, DeviceCPU}
	default:
		return []string{DeviceCPU}
	}
}

// pinChain honours an explicit MACLAW_EMBED_DEVICE pin: the requested device is
// tried first and CPU still closes the chain.
func pinChain(dev string) []string {
	switch dev {
	case DeviceCPU:
		return []string{DeviceCPU}
	case DeviceNPU, DeviceVulkan, DeviceMetal:
		return []string{dev, DeviceCPU}
	default:
		return npuChain() // unknown value: keep the automatic order
	}
}

// NPUForwarder is the compute surface an NPU backend provides. It mirrors the
// Metal/Vulkan forward(q, seqs, outs) contract: fill outs[i] with the last row
// of the final hidden state for seqs[i], BEFORE the output RMSNorm (poolVectors
// applies that).
type NPUForwarder interface {
	Forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error
	Release()
	Name() string
}

// NPUFactory builds an NPU forwarder for an already-loaded model.
type NPUFactory func(q *Qwen3Embedder) (NPUForwarder, error)

var (
	npuFactoryMu sync.RWMutex
	npuFactory   NPUFactory
)

// RegisterNPUBackend installs the NPU provider for this process. No build in
// this tree registers one today — the shipped backends are Metal, Vulkan and
// CPU SIMD — so an NPU machine declines the first chain entry and falls
// through to its GPU/CPU. This is the seam an Ascend/CANN, OpenVINO-NPU or QNN
// build plugs into; passing nil removes the provider again.
func RegisterNPUBackend(f NPUFactory) {
	// A new provider changes the answer the NPU probe can give, so drop the
	// cached verdict (taken before the factory lock to keep the lock order
	// probe → factory, never the other way round).
	ResetNPUProbe()
	npuFactoryMu.Lock()
	npuFactory = f
	npuFactoryMu.Unlock()
}

// ResetNPUBackendForTest removes any registered provider (tests only).
func ResetNPUBackendForTest() { RegisterNPUBackend(nil) }

func loadNPUFactory() NPUFactory {
	npuFactoryMu.RLock()
	defer npuFactoryMu.RUnlock()
	return npuFactory
}

// qwen3NPUDevice is an adopted NPU backend: the provider forwarder plus the
// name it reported, for diagnostics.
type qwen3NPUDevice struct {
	name string
	f    NPUForwarder
}

func (d *qwen3NPUDevice) forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	return d.f.Forward(q, seqs, outs)
}

func (d *qwen3NPUDevice) release() {
	if d != nil && d.f != nil {
		d.f.Release()
	}
}

// initBackends walks EmbedDeviceChain and adopts the first backend that comes
// up. Each entry either succeeds or records why it declined (GPUReason /
// NPUReason), so Accel() can explain the choice afterwards.
func (q *Qwen3Embedder) initBackends() {
	for _, dev := range EmbedDeviceChain() {
		switch dev {
		case DeviceNPU:
			if q.initNPU() {
				return
			}
		case DeviceMetal:
			if q.initGPU() {
				return
			}
		case DeviceVulkan:
			if q.initVulkan() {
				return
			}
		default:
			return // CPU SIMD: nothing to build, the reference path
		}
	}
}

// initNPU tries to bring the forward pass up on the NPU. It returns true only
// when a backend was adopted, which requires a positive availability probe
// (device scan plus one real provider initialization, see npu_probe.go), a
// registered provider AND a numeric self-check against the CPU vectors: an NPU
// that computes different embeddings is worse than no NPU at all.
func (q *Qwen3Embedder) initNPU() bool {
	if q == nil {
		return false
	}
	if !HWAccelPreferred() {
		q.setNPUReason("hardware acceleration off")
		return false
	}
	// The probe is cached, so a machine without a usable NPU declines here once
	// per process instead of retrying the initialization on every embedder.
	probe := ProbeNPU()
	if !probe.Ready {
		q.setNPUReason(probe.Reason)
		return false
	}
	f := loadNPUFactory()
	if f == nil {
		q.setNPUReason("NPU present (" + probe.Device + ") but no inference provider is linked")
		return false
	}
	ng, err := f(q)
	if err != nil || ng == nil {
		if err == nil {
			err = fmt.Errorf("provider returned no backend")
		}
		q.setNPUReason("provider init: " + err.Error())
		markNPUProbeFailed(q.NPUReason())
		return false
	}
	d := &qwen3NPUDevice{name: strings.TrimSpace(ng.Name()), f: ng}
	if d.name == "" {
		d.name = probe.Device
	}
	if err := q.npuSelfCheck(d); err != nil {
		d.release()
		q.setNPUReason("self-check: " + err.Error())
		markNPUProbeFailed(q.NPUReason())
		log.Printf("qwen3: NPU disabled — %s", q.NPUReason())
		return false
	}
	q.setNPUReason("")
	q.npu.Store(d)
	log.Printf("qwen3: NPU backend active (%s)", d.name)
	return true
}

// npuSelfCheck compares the candidate NPU vectors against the CPU path on every
// probe text. All texts are compared (not just the first) because a backend
// that packs sequences can corrupt some inputs while leaving others correct.
// q.npu must still be nil, otherwise EmbedBatch would route to the NPU itself.
func (q *Qwen3Embedder) npuSelfCheck(d *qwen3NPUDevice) error {
	texts := []string{
		"NPU self-check: send this file to zhangsan",
		"查询 api2 服务器状态",
	}
	seqs := make([][]int, len(texts))
	for i, t := range texts {
		seqs[i] = q.Tokenize(t)
		if len(seqs[i]) == 0 {
			return fmt.Errorf("empty probe text %d", i)
		}
	}
	hid := make([][]float32, len(seqs))
	for i := range hid {
		hid[i] = make([]float32, q.hp.Dim)
	}
	if err := d.forward(q, seqs, hid); err != nil {
		return err
	}
	cpu, err := q.EmbedBatch(texts) // production CPU path: the reference
	if err != nil {
		return err
	}
	if len(cpu) != len(hid) {
		return fmt.Errorf("cpu probe produced %d vectors, want %d", len(cpu), len(hid))
	}
	width := q.dim
	if width <= 0 || width > q.hp.Dim {
		width = q.hp.Dim
	}
	minCos := 1.0
	worst := 0
	for i := range hid {
		if !allFinite(hid[i]) {
			return fmt.Errorf("non-finite NPU output for probe text %d", i)
		}
		if len(cpu[i]) < width {
			return fmt.Errorf("cpu probe vector %d is %d wide, want %d", i, len(cpu[i]), width)
		}
		n := make([]float32, q.hp.Dim)
		tensor.RMSNorm(n, hid[i], q.outputNorm, q.hp.RMSNormEps)
		c := cosineF32(n[:width], cpu[i][:width])
		if math.IsNaN(c) || c < minCos {
			minCos, worst = c, i
		}
	}
	if !(minCos >= qwen3VKSelfCheckMinCos) {
		return fmt.Errorf("worst cos %.6f < %.4f (probe text %d)", minCos, qwen3VKSelfCheckMinCos, worst)
	}
	return nil
}

// setNPUReason records why an NPU backend is or is not in use.
func (q *Qwen3Embedder) setNPUReason(reason string) {
	q.npuMu.Lock()
	q.npuReason = reason
	q.npuMu.Unlock()
}

// NPUReason returns the last recorded NPU-backend reason (thread-safe).
func (q *Qwen3Embedder) NPUReason() string {
	if q == nil {
		return ""
	}
	q.npuMu.Lock()
	defer q.npuMu.Unlock()
	return q.npuReason
}

// NPUActive reports whether an NPU backend serves the forward pass.
func (q *Qwen3Embedder) NPUActive() bool {
	return q != nil && q.npu.Load() != nil
}

// releaseNPU drops the adopted NPU backend, if any.
func (q *Qwen3Embedder) releaseNPU() {
	if q == nil {
		return
	}
	if ng := q.npu.Swap(nil); ng != nil {
		ng.release()
		log.Printf("qwen3: NPU backend released")
	}
}
