package embedding

import (
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

// ReloadSharedGemmaAccel applies HWAccelPreferred to the process singleton.
func ReloadSharedGemmaAccel() {
	sharedGemmaMu.Lock()
	defer sharedGemmaMu.Unlock()
	if g, ok := sharedGemma.(*GemmaEmbedder); ok && g != nil {
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
