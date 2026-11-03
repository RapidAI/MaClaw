package embedding

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// NPUProber is the lightweight initializer an NPU build registers next to its
// provider factory. A probe MUST actually create one provider/context and close
// it again before returning, so the verdict answers "this machine can bring the
// NPU runtime up" instead of "an environment variable is set" or "a library
// file exists". A non-nil error declines the NPU for this process and its text
// is surfaced through NPUReason()/Accel(); the device name is diagnostics only.
type NPUProber func() (device string, err error)

// NPUProbeResult is the verdict of the NPU availability probe.
type NPUProbeResult struct {
	// Ready reports whether the NPU may serve the forward pass. It covers
	// availability only: initNPU still builds the real provider and runs the
	// numeric self-check against the CPU vectors afterwards.
	Ready bool
	// Device is the probed device name (diagnostics).
	Device string
	// Reason explains a negative verdict; empty when Ready.
	Reason string
	// Probed is true when a registered prober really created and closed a
	// context, false when the verdict rests on the device scan alone (no
	// prober linked, or no NPU device at all).
	Probed bool
	// Cached is true when the value came from the process cache.
	Cached bool
	// Attempts counts how many times the probe actually ran in this process.
	Attempts int
	// Duration is the wall time the probe took when it ran.
	Duration time.Duration
}

var (
	npuProberMu sync.RWMutex
	npuProber   NPUProber

	npuProbeMu    sync.Mutex
	npuProbeCache NPUProbeResult
	npuProbeValid bool
	npuProbeRuns  int
)

// RegisterNPUProber installs the lightweight initializer ProbeNPU runs. Passing
// nil removes it, which turns the NPU verdict back into a device scan. The
// cached verdict is dropped so the next probe re-runs against the new prober.
func RegisterNPUProber(p NPUProber) {
	ResetNPUProbe()
	npuProberMu.Lock()
	npuProber = p
	npuProberMu.Unlock()
}

// RegisterNPUBackendWithProbe installs a provider factory together with the
// lightweight initializer that proves the NPU runtime can come up. This is the
// entry point an Ascend/CANN, OpenVINO-NPU or QNN build should use: the probe
// creates and closes one context at startup, and only a positive verdict lets
// initNPU build the full provider after the weights are loaded.
func RegisterNPUBackendWithProbe(f NPUFactory, p NPUProber) {
	RegisterNPUProber(p) // invalidates the cached verdict
	RegisterNPUBackend(f)
}

// ResetNPUProberForTest removes any registered prober (tests only).
func ResetNPUProberForTest() { RegisterNPUProber(nil) }

// ResetNPUProbe drops the cached verdict so the next ProbeNPU re-runs. Hosts
// call it when the environment changed — the acceleration toggle being switched
// back on, or a driver update the user expects to be picked up.
func ResetNPUProbe() {
	npuProbeMu.Lock()
	npuProbeValid = false
	npuProbeMu.Unlock()
}

func loadNPUProber() NPUProber {
	npuProberMu.RLock()
	defer npuProberMu.RUnlock()
	return npuProber
}

// ProbeNPU returns the process-wide NPU availability verdict. The probe runs at
// most once per process (until the cache is invalidated), so neither repeated
// embedder construction nor per-inference routing pays for it twice.
func ProbeNPU() NPUProbeResult {
	npuProbeMu.Lock()
	defer npuProbeMu.Unlock()
	if npuProbeValid {
		res := npuProbeCache
		res.Cached = true
		res.Attempts = npuProbeRuns
		return res
	}
	res := runNPUProbe()
	npuProbeRuns++
	res.Attempts = npuProbeRuns
	res.Cached = false
	npuProbeCache = res
	npuProbeValid = true
	return res
}

// runNPUProbe is the uncached probe: device scan, then one real initialization
// attempt through the registered prober. Called with npuProbeMu held, which is
// what keeps two concurrent embedders from initializing the NPU at once.
func runNPUProbe() NPUProbeResult {
	start := time.Now()
	waitDetect()
	det := CurrentAccelInfo()
	res := NPUProbeResult{Device: det.Device}
	if !det.NPUPresent {
		res.Reason = "no NPU device"
		res.Duration = time.Since(start)
		return res
	}
	prober := loadNPUProber()
	if prober == nil {
		if loadNPUFactory() == nil {
			res.Reason = "NPU present (" + det.Device + ") but no inference provider is linked"
		} else {
			// A provider is linked without a lightweight prober: nothing can be
			// created before the model is loaded, so the device scan is the best
			// available verdict and initNPU performs the real initialization.
			res.Ready = true
		}
		res.Duration = time.Since(start)
		return res
	}
	res.Probed = true
	device, err := callNPUProber(prober)
	if err != nil {
		res.Reason = "NPU probe failed: " + err.Error()
		log.Printf("qwen3: NPU probe failed — %v", err)
	} else {
		res.Ready = true
		if d := strings.TrimSpace(device); d != "" {
			res.Device = d
		}
	}
	res.Duration = time.Since(start)
	return res
}

// callNPUProber runs the prober with a panic barrier: a crashing driver must
// degrade to the next candidate with a recorded reason, not take the process
// down during startup.
func callNPUProber(p NPUProber) (device string, err error) {
	defer func() {
		if r := recover(); r != nil {
			device, err = "", fmt.Errorf("panic: %v", r)
		}
	}()
	return p()
}

// markNPUProbeFailed records a provider initialization failure in the cache, so
// the next embedder skips the same slow, failing attempt and the chain drops to
// the next candidate immediately.
func markNPUProbeFailed(reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	npuProbeMu.Lock()
	defer npuProbeMu.Unlock()
	res := npuProbeCache
	res.Ready = false
	res.Reason = reason
	res.Probed = true
	res.Cached = false
	res.Attempts = npuProbeRuns
	npuProbeCache = res
	npuProbeValid = true
}
