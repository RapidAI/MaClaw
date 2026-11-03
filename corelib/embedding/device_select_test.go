package embedding

import (
	"errors"
	"strings"
	"testing"
)

// setEmbedDeviceEnv pins both device variables for one test and restores the
// process-wide detect cache + accel preference afterwards.
func setEmbedDeviceEnv(t *testing.T, device, accel string) {
	t.Helper()
	t.Setenv(EmbedDeviceEnv, device)
	t.Setenv("MACLAW_EMBED_ACCEL", accel)
}

func TestEmbedDeviceChainDefaults(t *testing.T) {
	SetHWAccelPreferred(true)
	setEmbedDeviceEnv(t, "", "")

	chain := EmbedDeviceChain()
	if len(chain) < 2 {
		t.Fatalf("chain too short: %v", chain)
	}
	if chain[0] != DeviceNPU {
		t.Fatalf("NPU must be the first candidate, got %v", chain)
	}
	if last := chain[len(chain)-1]; last != DeviceCPU {
		t.Fatalf("CPU must close the chain, got %v", chain)
	}
}

func TestEmbedDeviceChainHonoursExplicitPins(t *testing.T) {
	SetHWAccelPreferred(true)
	cases := []struct {
		device string
		accel  string
		want   []string
	}{
		{"cpu", "", []string{DeviceCPU}},
		{"", "cpu", []string{DeviceCPU}},
		{"", "off", []string{DeviceCPU}},
		{"", "dml", []string{DeviceCPU}},
		{DeviceVulkan, "", []string{DeviceVulkan, DeviceCPU}},
		{DeviceNPU, "", []string{DeviceNPU, DeviceCPU}},
		{DeviceMetal, "", []string{DeviceMetal, DeviceCPU}},
	}
	for _, c := range cases {
		setEmbedDeviceEnv(t, c.device, c.accel)
		got := EmbedDeviceChain()
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("MACLAW_EMBED_DEVICE=%q MACLAW_EMBED_ACCEL=%q: got %v want %v",
				c.device, c.accel, got, c.want)
		}
	}
	// auto keeps the automatic order rather than pinning anything.
	setEmbedDeviceEnv(t, DeviceAuto, "")
	if got, want := EmbedDeviceChain(), npuChain(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("auto: got %v want %v", got, want)
	}
	// A pin always ends with CPU so a failed device cannot strand the embedder.
	setEmbedDeviceEnv(t, DeviceVulkan, "")
	if got := EmbedDeviceChain(); got[len(got)-1] != DeviceCPU {
		t.Fatalf("pinned chain must end with CPU, got %v", got)
	}
}

func TestEmbedDeviceChainHardwareAccelOff(t *testing.T) {
	defer SetHWAccelPreferred(true)
	SetHWAccelPreferred(false)
	setEmbedDeviceEnv(t, "", "")
	if got := EmbedDeviceChain(); len(got) != 1 || got[0] != DeviceCPU {
		t.Fatalf("hardware acceleration off must pin CPU, got %v", got)
	}
}

// TestInitNPUDeclinesWithoutProvider: an NPU machine with no linked provider
// must decline the first chain entry and record why, instead of pretending to
// run on the NPU.
func TestInitNPUDeclinesWithoutProvider(t *testing.T) {
	defer SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	defer ResetNPUBackendForTest()
	ResetNPUBackendForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: true, Device: "fake-npu", Reason: "NPU present"})

	q := &Qwen3Embedder{}
	if q.initNPU() {
		t.Fatal("no provider is registered, initNPU must decline")
	}
	if q.NPUActive() {
		t.Fatal("declined NPU must not be marked active")
	}
	if reason := q.NPUReason(); !strings.Contains(reason, "no inference provider") {
		t.Fatalf("reason should name the missing provider, got %q", reason)
	}
}

func TestInitNPUDeclinesWithoutHardware(t *testing.T) {
	defer SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	defer ResetNPUBackendForTest()
	ResetNPUBackendForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: false, Reason: "no NPU/XDNA"})

	called := false
	RegisterNPUBackend(func(q *Qwen3Embedder) (NPUForwarder, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	q := &Qwen3Embedder{}
	if q.initNPU() {
		t.Fatal("initNPU must decline when no NPU hardware is present")
	}
	if called {
		t.Fatal("provider must not be constructed on a machine without an NPU")
	}
	if reason := q.NPUReason(); reason != "no NPU device" {
		t.Fatalf("got reason %q", reason)
	}
}

func TestInitNPUProviderErrorFallsThrough(t *testing.T) {
	defer SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	defer ResetNPUBackendForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: true, Device: "fake-npu", Reason: "NPU present"})

	RegisterNPUBackend(func(q *Qwen3Embedder) (NPUForwarder, error) {
		return nil, errors.New("driver missing")
	})
	q := &Qwen3Embedder{}
	if q.initNPU() {
		t.Fatal("a provider that fails to initialise must not be adopted")
	}
	if reason := q.NPUReason(); !strings.Contains(reason, "driver missing") {
		t.Fatalf("reason should carry the provider error, got %q", reason)
	}
}

// TestInitNPURejectsWrongVectors is the gate that keeps inference results
// unchanged: a backend whose embeddings do not match the CPU path is dropped
// and the chain continues on CPU.
func TestInitNPURejectsWrongVectors(t *testing.T) {
	defer SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	defer ResetNPUBackendForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: true, Device: "fake-npu", Reason: "NPU present"})

	released := false
	RegisterNPUBackend(func(q *Qwen3Embedder) (NPUForwarder, error) {
		return &fakeNPU{filled: false, released: &released}, nil
	})
	q := loadQwen3(t)
	if q.initNPU() {
		t.Fatal("a backend returning wrong vectors must be rejected")
	}
	if q.NPUActive() {
		t.Fatal("rejected backend must not be marked active")
	}
	if !released {
		t.Fatal("rejected backend must be released")
	}
	if reason := q.NPUReason(); !strings.Contains(reason, "self-check") {
		t.Fatalf("reason should come from the self-check, got %q", reason)
	}
}

// fakeNPU is a stand-in NPU backend. filled=false leaves the output rows zero,
// which is exactly the "computes something else" case the self-check must catch.
type fakeNPU struct {
	filled   bool
	released *bool
}

func (f *fakeNPU) Name() string { return "fake-npu" }

func (f *fakeNPU) Release() {
	if f.released != nil {
		*f.released = true
	}
}

func (f *fakeNPU) Forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	if !f.filled {
		return nil // zero rows: finite but not the model's embedding
	}
	for i := range seqs {
		for j := range outs[i] {
			outs[i][j] = 1
		}
	}
	return nil
}
