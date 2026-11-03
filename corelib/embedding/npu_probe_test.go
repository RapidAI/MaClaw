package embedding

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// probeRig isolates the process-wide probe state (prober, provider, detect
// cache, acceleration preference) for one test and restores it afterwards.
func probeRig(t *testing.T, present bool, device string) {
	t.Helper()
	t.Cleanup(func() {
		ResetNPUProberForTest()
		ResetNPUBackendForTest()
		ResetNPUProbe()
		SetHWAccelPreferred(true)
		SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	})
	ResetNPUBackendForTest()
	ResetNPUProberForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{
		Backend:    BackendCPUSIMD,
		NPUPresent: present,
		Device:     device,
		Reason:     "probe test",
	})
}

func unusedNPUFactory(t *testing.T) NPUFactory {
	t.Helper()
	return func(*Qwen3Embedder) (NPUForwarder, error) {
		return nil, errors.New("provider not exercised by this test")
	}
}

// TestNPUProbeCreatesAndClosesOneContext is the core requirement: availability
// is decided by really initializing the runtime once (create + close), and the
// verdict is then reused instead of probing again.
func TestNPUProbeCreatesAndClosesOneContext(t *testing.T) {
	probeRig(t, true, "fake-npu")

	var opened, closed int32
	RegisterNPUBackendWithProbe(unusedNPUFactory(t), func() (string, error) {
		atomic.AddInt32(&opened, 1)
		defer atomic.AddInt32(&closed, 1)
		return "fake-npu-probe", nil
	})

	first := ProbeNPU()
	if !first.Ready {
		t.Fatalf("probe must accept a runtime that initializes, reason: %s", first.Reason)
	}
	if !first.Probed {
		t.Fatal("a positive verdict must come from a real initialization attempt")
	}
	if first.Device != "fake-npu-probe" {
		t.Fatalf("probed device = %q, want the name the prober reported", first.Device)
	}
	if got := atomic.LoadInt32(&opened); got != 1 {
		t.Fatalf("prober ran %d times, want exactly 1", got)
	}
	if got := atomic.LoadInt32(&closed); got != 1 {
		t.Fatalf("prober closed %d contexts, want 1 (create and close once)", got)
	}

	for i := 0; i < 3; i++ {
		res := ProbeNPU()
		if !res.Cached {
			t.Fatalf("probe %d must be served from the cache", i+2)
		}
		if !res.Ready {
			t.Fatalf("cached verdict must stay positive, reason: %s", res.Reason)
		}
	}
	if got := atomic.LoadInt32(&opened); got != 1 {
		t.Fatalf("cached probes re-ran the initializer %d times, want 1", got)
	}
}

// TestNPUProbeFailureRecordsReasonAndDegrades: a runtime that refuses to come up
// must record the reason, decline the NPU and let the chain continue — and the
// negative verdict must be cached too, so the next embedder does not pay for the
// same failing initialization.
func TestNPUProbeFailureRecordsReasonAndDegrades(t *testing.T) {
	probeRig(t, true, "fake-npu")

	var runs int32
	RegisterNPUBackendWithProbe(unusedNPUFactory(t), func() (string, error) {
		atomic.AddInt32(&runs, 1)
		return "", errors.New("driver missing (probe)")
	})

	res := ProbeNPU()
	if res.Ready {
		t.Fatal("a failing initialization must not report the NPU as ready")
	}
	if !res.Probed {
		t.Fatal("the failing verdict must come from a real initialization attempt")
	}
	if !strings.Contains(res.Reason, "driver missing (probe)") {
		t.Fatalf("reason must carry the runtime error, got %q", res.Reason)
	}

	q := &Qwen3Embedder{}
	if q.initNPU() {
		t.Fatal("initNPU must decline when the probe failed")
	}
	if q.NPUActive() {
		t.Fatal("a declined probe must not mark the NPU active")
	}
	if reason := q.NPUReason(); !strings.Contains(reason, "driver missing (probe)") {
		t.Fatalf("NPUReason must explain the probe failure, got %q", reason)
	}

	second := ProbeNPU()
	if !second.Cached || second.Ready {
		t.Fatalf("second probe must reuse the cached failure, got %+v", second)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("prober ran %d times, want 1 (failure is cached)", got)
	}
}

// TestNPUProbePanicDegrades: a crashing driver must become a recorded reason,
// not a startup panic.
func TestNPUProbePanicDegrades(t *testing.T) {
	probeRig(t, true, "fake-npu")

	RegisterNPUProber(func() (string, error) {
		panic("driver crashed during init")
	})
	res := ProbeNPU()
	if res.Ready {
		t.Fatal("a panicking initialization must not report the NPU as ready")
	}
	if !strings.Contains(res.Reason, "panic") || !strings.Contains(res.Reason, "driver crashed") {
		t.Fatalf("reason must record the panic, got %q", res.Reason)
	}
	q := &Qwen3Embedder{}
	if q.initNPU() {
		t.Fatal("initNPU must decline after a panicking probe")
	}
}

// TestNPUProbeSkipsInitializerWithoutDevice: without an NPU device the
// initializer must not even run.
func TestNPUProbeSkipsInitializerWithoutDevice(t *testing.T) {
	probeRig(t, false, "")

	var runs int32
	RegisterNPUProber(func() (string, error) {
		atomic.AddInt32(&runs, 1)
		return "should-not-run", nil
	})
	res := ProbeNPU()
	if res.Ready || res.Probed {
		t.Fatalf("no device must decline without initializing, got %+v", res)
	}
	if res.Reason != "no NPU device" {
		t.Fatalf("reason = %q, want \"no NPU device\"", res.Reason)
	}
	if got := atomic.LoadInt32(&runs); got != 0 {
		t.Fatalf("prober ran %d times on a machine without an NPU, want 0", got)
	}
}

// TestNPUProbeDeviceScanOnlyWithoutProber: a provider linked without a
// lightweight prober can only be verified after the weights exist, so the probe
// reports a device-scan verdict and says so via Probed=false.
func TestNPUProbeDeviceScanOnlyWithoutProber(t *testing.T) {
	probeRig(t, true, "fake-npu")

	RegisterNPUBackend(unusedNPUFactory(t))
	res := ProbeNPU()
	if !res.Ready {
		t.Fatalf("device present with a linked provider must stay a candidate, reason: %s", res.Reason)
	}
	if res.Probed {
		t.Fatal("no prober is registered, so nothing was initialized")
	}
}

// TestInitNPUProviderFailureCachedAcrossEmbedders: the provider error is cached
// as well, so building a second embedder does not repeat the failed init.
func TestInitNPUProviderFailureCachedAcrossEmbedders(t *testing.T) {
	probeRig(t, true, "fake-npu")

	var builds int32
	RegisterNPUBackend(func(*Qwen3Embedder) (NPUForwarder, error) {
		atomic.AddInt32(&builds, 1)
		return nil, errors.New("driver missing")
	})

	first := &Qwen3Embedder{}
	if first.initNPU() {
		t.Fatal("a provider that fails to initialise must not be adopted")
	}
	second := &Qwen3Embedder{}
	if second.initNPU() {
		t.Fatal("the cached failure must decline the second embedder too")
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("provider was built %d times, want 1 (failure cached)", got)
	}
	if reason := second.NPUReason(); !strings.Contains(reason, "driver missing") {
		t.Fatalf("cached reason must carry the provider error, got %q", reason)
	}
}

// TestNPUProbeReRunsAfterReRegistration: a new prober invalidates the cache, so
// an NPU build that installs its provider later is not stuck on the old verdict.
func TestNPUProbeReRunsAfterReRegistration(t *testing.T) {
	probeRig(t, true, "fake-npu")

	var runs int32
	RegisterNPUProber(func() (string, error) {
		atomic.AddInt32(&runs, 1)
		return "first", nil
	})
	if res := ProbeNPU(); !res.Ready || res.Device != "first" {
		t.Fatalf("first probe = %+v", res)
	}
	RegisterNPUProber(func() (string, error) {
		atomic.AddInt32(&runs, 1)
		return "second", nil
	})
	res := ProbeNPU()
	if res.Cached {
		t.Fatal("re-registering a prober must invalidate the cached verdict")
	}
	if res.Device != "second" {
		t.Fatalf("probe device = %q, want the newly registered prober's device", res.Device)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Fatalf("prober ran %d times, want 2", got)
	}
}

// TestEmbedChainFallsThroughToCPUWhenProbeFails ties the probe to the ordered
// chain: a failed NPU probe must not stop the loader, which continues to the
// next candidate and ends on CPU SIMD.
func TestEmbedChainFallsThroughToCPUWhenProbeFails(t *testing.T) {
	probeRig(t, true, "fake-npu")
	t.Setenv(EmbedDeviceEnv, DeviceAuto)
	t.Setenv("MACLAW_EMBED_ACCEL", "")
	t.Setenv("MACLAW_QWEN3_VULKAN", "0")
	t.Setenv("MACLAW_QWEN3_METAL", "0")

	RegisterNPUBackendWithProbe(unusedNPUFactory(t), func() (string, error) {
		return "", errors.New("probe refused the device")
	})
	q, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatalf("NewQwen3Embedder: %v", err)
	}
	defer q.Close()

	if q.NPUActive() {
		t.Fatal("a failed probe must leave the NPU inactive")
	}
	info := q.Accel()
	if info.Backend != BackendCPUSIMD {
		t.Fatalf("backend = %q, want %q after the NPU and GPU declined", info.Backend, BackendCPUSIMD)
	}
	if !strings.Contains(info.Reason, "probe refused the device") {
		t.Fatalf("reason must explain the NPU decline, got %q", info.Reason)
	}
}

// TestNPUProbeIsNotRepeatedPerInference pins the caching requirement: the probe
// runs once per process, so neither repeated inference nor a second embedder
// pays for the device initialization again.
func TestNPUProbeIsNotRepeatedPerInference(t *testing.T) {
	probeRig(t, true, "fake-npu")
	t.Setenv(EmbedDeviceEnv, DeviceAuto)
	t.Setenv("MACLAW_EMBED_ACCEL", "")
	t.Setenv("MACLAW_QWEN3_VULKAN", "0")
	t.Setenv("MACLAW_QWEN3_METAL", "0")

	var runs int32
	RegisterNPUBackendWithProbe(unusedNPUFactory(t), func() (string, error) {
		atomic.AddInt32(&runs, 1)
		return "", errors.New("driver missing (inference)")
	})
	q, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatalf("NewQwen3Embedder: %v", err)
	}
	defer q.Close()
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("prober ran %d times at load, want 1", got)
	}
	attemptsBefore := ProbeNPU().Attempts
	for i := 0; i < 3; i++ {
		if _, err := q.Embed("hello"); err != nil {
			t.Fatalf("Embed %d: %v", i, err)
		}
	}
	if _, err := q.EmbedBatch([]string{"hello", "world"}); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("prober ran %d times after inference, want 1 (the verdict is cached)", got)
	}
	if res := ProbeNPU(); !res.Cached || res.Ready {
		t.Fatalf("cached verdict = %+v, want a cached failure", res)
	} else if res.Attempts != attemptsBefore {
		t.Fatalf("probe attempts grew from %d to %d during inference, want no re-probe", attemptsBefore, res.Attempts)
	}
	second, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatalf("second NewQwen3Embedder: %v", err)
	}
	defer second.Close()
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("prober ran %d times across two embedders, want 1", got)
	}
}
