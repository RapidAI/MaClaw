package embedding

import (
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	BackendCPUSIMD = "cpu-simd"
	BackendAMDNPU  = "amd-npu"
	BackendDMLGPU  = "dml-gpu"
	BackendNone    = "none"
)

// AccelInfo is the detect-cache / instance pin for embedding acceleration.
type AccelInfo struct {
	Backend    string `json:"backend"`
	Device     string `json:"device"`
	Reason     string `json:"reason"`
	NPUPresent bool   `json:"npu_present"`
	// GPUAvailable reports that a GPU device exists, whether or not the startup
	// gate accepted it. Hosts use it to keep the hardware-acceleration toggle
	// enabled on machines that have a GPU but no NPU.
	GPUAvailable bool `json:"gpu_available"`
}

var (
	detectOnce     sync.Once
	detectDone     atomic.Bool
	detectMu       sync.Mutex
	detectInfo     AccelInfo
	hwAccelPref    atomic.Bool
	hwAccelPrefSet atomic.Bool
)

func init() {
	hwAccelPref.Store(true)
}

// SetHWAccelPreferred records the persisted prefer-NPU flag (default true).
func SetHWAccelPreferred(v bool) {
	hwAccelPref.Store(v)
	hwAccelPrefSet.Store(true)
}

// HWAccelPreferred is the last SetHWAccelPreferred value, default true.
func HWAccelPreferred() bool {
	if !hwAccelPrefSet.Load() {
		return true
	}
	return hwAccelPref.Load()
}

// CurrentAccelInfo is the process detect cache. About badge must NOT use Backend here.
func CurrentAccelInfo() AccelInfo {
	ensureDetect()
	detectMu.Lock()
	defer detectMu.Unlock()
	return detectInfo
}

func ensureDetect() {
	detectOnce.Do(func() {
		info := detectAccel()
		detectMu.Lock()
		detectInfo = info
		detectMu.Unlock()
		detectDone.Store(true)
	})
}

func waitDetect() {
	ensureDetect()
	deadline := time.Now().Add(2 * time.Second)
	for !detectDone.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func detectAccel() AccelInfo {
	info := AccelInfo{Backend: BackendCPUSIMD, Reason: "cpu simd"}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_EMBED_ACCEL")))
	present, device, reason := detectNPU()
	info.NPUPresent = present
	info.Device = device
	if reason != "" {
		info.Reason = reason
	}
	switch env {
	case "cpu", "off":
		info.Backend = BackendCPUSIMD
		info.Reason = "MACLAW_EMBED_ACCEL=" + env
	case "dml":
		info.Backend = BackendCPUSIMD
		info.Reason = "dml requested but not default-enabled"
	case "npu":
		if !present {
			info.Backend = BackendCPUSIMD
			info.Reason = "npu requested but not present"
		}
	}
	if !present && info.Reason == "cpu simd" {
		info.Reason = "no NPU/XDNA"
	}
	return info
}

// Accel returns the instance-pinned backend (About source of truth).
func (g *GemmaEmbedder) Accel() AccelInfo {
	if g == nil {
		return CurrentAccelInfo()
	}
	return g.accelInfo
}

// ApplyAccel opens/closes the optional NPU field without dropping mmap weights.
func (g *GemmaEmbedder) ApplyAccel(enabled bool) {
	if g == nil {
		return
	}
	waitDetect()
	det := CurrentAccelInfo()
	info := AccelInfo{
		Backend:    BackendCPUSIMD,
		Device:     det.Device,
		NPUPresent: det.NPUPresent,
		Reason:     det.Reason,
	}
	if g.dim > 256 {
		info.Reason = "npu never selected for dim>256 / token-states"
		g.accelInfo = info
		return
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_EMBED_ACCEL")))
	if env == "cpu" || env == "off" {
		info.Reason = "MACLAW_EMBED_ACCEL=" + env
		g.accelInfo = info
		return
	}
	if !enabled || !det.NPUPresent {
		if !det.NPUPresent {
			info.Reason = "no NPU/XDNA"
		} else {
			info.Reason = "hw accel switch off"
		}
		g.accelInfo = info
		return
	}
	// PR6 ORT session is cuttable; keep CPU until an NPU session exists.
	info.Reason = "npu present; ORT backend not shipped"
	g.accelInfo = info
}

// ReloadSharedEmbedderAccel applies HWAccelPreferred to the process singleton
// (whatever model it is; see AccelEmbedder).
func ReloadSharedEmbedderAccel() {
	sharedEmbMu.Lock()
	defer sharedEmbMu.Unlock()
	if g, ok := sharedEmb.(AccelEmbedder); ok && g != nil && !IsNoop(sharedEmb) {
		g.ApplyAccel(HWAccelPreferred())
	}
}

// SetDetectOverrideForTest replaces the detect cache (tests only).
func SetDetectOverrideForTest(info AccelInfo) {
	detectOnce.Do(func() {})
	detectMu.Lock()
	detectInfo = info
	detectMu.Unlock()
	detectDone.Store(true)
}

// AccelEmbedder is implemented by embedders that report (and can retarget)
// their compute backend. Hosts must use this instead of asserting a concrete
// model type, so the About badge / settings toggle keep working for whichever
// single model is loaded.
type AccelEmbedder interface {
	Accel() AccelInfo
	ApplyAccel(enabled bool)
}

// BackendVulkan is the Vulkan compute backend (Windows/Linux GPU).
const BackendVulkan = "vulkan"

// BackendMetal is the Metal compute backend (macOS GPU).
const BackendMetal = "metal"

// GPUGate is the outcome of the startup correctness + speed gate for a GPU
// backend. It answers "why is the GPU (not) in use, and by how much" for
// diagnostics and support reports.
type GPUGate struct {
	Backend  string  // BackendVulkan, or "" when no GPU backend was probed
	Accepted bool    // true when the backend was adopted
	Ratio    float64 // cpuDuration/gpuDuration at probe time (>1 = GPU faster)
	CPUMs    float64
	GPUMs    float64
	MinRatio float64 // speed threshold that had to be met
	CosFloor float64 // cosine floor that had to be met
	MinCos   float64 // worst cosine measured against the CPU path
	Reason   string  // non-empty when the gate refused the backend
}

// GPUGateInfo returns the recorded gate outcome (zero value when no GPU
// backend was probed, e.g. on macOS where Metal has no gate).
func (q *Qwen3Embedder) GPUGateInfo() GPUGate {
	if q == nil {
		return GPUGate{}
	}
	q.gpuMu.Lock()
	defer q.gpuMu.Unlock()
	return q.gpuGate
}

func (q *Qwen3Embedder) setGPUGate(g GPUGate) {
	q.gpuMu.Lock()
	q.gpuGate = g
	q.gpuMu.Unlock()
}

// Accel reports the Qwen3 instance's backend. On Windows/Linux with a Vulkan
// device that passed the numeric self-check the forward runs on the GPU
// (BackendVulkan); otherwise it is CPU SIMD with Device carrying the numeric
// path (ComputeMode, e.g. "q8p:avx512:2pass" or "float").
func (q *Qwen3Embedder) Accel() AccelInfo {
	det := CurrentAccelInfo()
	if q == nil {
		return AccelInfo{Backend: BackendCPUSIMD, Device: "float", NPUPresent: det.NPUPresent, Reason: "no embedder"}
	}
	if g := q.vkGPU.Load(); g != nil {
		return AccelInfo{Backend: BackendVulkan, Device: g.dev.Info.Name, NPUPresent: det.NPUPresent,
			GPUAvailable: true, Reason: "qwen3 runs on the Vulkan GPU"}
	}
	if g := q.gpu.Load(); g != nil {
		return AccelInfo{Backend: BackendMetal, Device: g.dev.Name, NPUPresent: det.NPUPresent,
			GPUAvailable: true, Reason: "qwen3 runs on the Metal GPU"}
	}
	// A GPU that the gate refused still counts as present: the toggle must stay
	// usable so the user can retry after a driver update.
	gpuPresent := q.GPUGateInfo().Backend != ""
	info := AccelInfo{Backend: BackendCPUSIMD, Device: q.ComputeMode(), NPUPresent: det.NPUPresent,
		GPUAvailable: gpuPresent, Reason: "qwen3 runs on CPU SIMD"}
	if reason := q.GPUReason(); reason != "" {
		info.Reason = "qwen3 CPU SIMD (" + reason + ")"
	}
	if q.q8pFallback != "" {
		info.Reason = "qwen3 float path (Q8P disabled: " + q.q8pFallback + ")"
	}
	return info
}

// ApplyAccel applies the hardware-acceleration toggle to the Vulkan backend:
// off releases the GPU (the CPU path takes over), on re-runs the backend probe
// and self-check. It never blocks on a device that is already in the requested
// state, so the toggle stays cheap.
func (q *Qwen3Embedder) ApplyAccel(enabled bool) {
	if q == nil {
		return
	}
	// Serialise: two concurrent enables would both pass the "already active"
	// check below and each build a device, silently leaking the loser.
	q.accelMu.Lock()
	defer q.accelMu.Unlock()
	if q.isClosed() {
		return // nothing to toggle once the embedder is torn down
	}
	if !enabled {
		if g := q.vkGPU.Swap(nil); g != nil {
			g.shutdown()
			log.Printf("qwen3: Vulkan backend released by the hardware-acceleration toggle")
		}
		if g := q.gpu.Swap(nil); g != nil {
			g.shutdown()
			log.Printf("qwen3: Metal backend released by the hardware-acceleration toggle")
		}
		return
	}
	if q.vkGPU.Load() != nil || q.gpu.Load() != nil {
		return
	}
	q.initGPU()    // Metal (darwin only; no-op elsewhere)
	q.initVulkan() // Vulkan (windows/linux only)
	// Close may have run while the backend was being built; it would have
	// swapped an empty slot, so release anything we just created.
	if q.isClosed() {
		if g := q.vkGPU.Swap(nil); g != nil {
			g.shutdown()
		}
		if g := q.gpu.Swap(nil); g != nil {
			g.shutdown()
		}
	}
}

var (
	_ AccelEmbedder = (*GemmaEmbedder)(nil)
	_ AccelEmbedder = (*Qwen3Embedder)(nil)
)
