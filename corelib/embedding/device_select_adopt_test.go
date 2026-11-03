package embedding

import (
	"math"
	"testing"
)

// passthroughNPU stands in for a correct NPU backend: it reproduces the CPU
// last-token hidden state exactly. The self-check must adopt it and the
// embeddings must stay bit-comparable with the CPU path.
type passthroughNPU struct{ released *bool }

func (f *passthroughNPU) Name() string { return "fake-npu-passthrough" }

func (f *passthroughNPU) Release() {
	if f.released != nil {
		*f.released = true
	}
}

// Forward runs the CPU forward and inverts the output RMSNorm (divide by the
// norm weight), so poolVectors — which norms again — lands on the same
// direction the CPU path produces.
func (f *passthroughNPU) Forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	for i, ids := range seqs {
		s := q.getScratch(len(ids) + 8)
		h, err := q.forwardFrom(ids, nil, nil, s, 1)
		if err != nil {
			q.putScratch(s)
			return err
		}
		for j := range outs[i] {
			g := float32(1)
			if j < len(q.outputNorm) && q.outputNorm[j] != 0 {
				g = q.outputNorm[j]
			}
			outs[i][j] = h[j] / g
		}
		q.putScratch(s)
	}
	return nil
}

// TestInitNPUAdoptsMatchingBackend proves the happy path of the chain: with an
// NPU present and a provider that reproduces the CPU vectors, the backend is
// adopted, reported as the NPU, and the embeddings do not change.
func TestInitNPUAdoptsMatchingBackend(t *testing.T) {
	defer SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, Reason: "restored"})
	defer ResetNPUBackendForTest()
	ResetNPUBackendForTest()
	SetHWAccelPreferred(true)
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: true, Device: "fake-npu", Reason: "NPU present"})

	// Force the CPU path for the load, then adopt the NPU explicitly, so the
	// comparison has a pure CPU reference.
	t.Setenv(EmbedDeviceEnv, DeviceCPU)
	t.Setenv("MACLAW_QWEN3_VULKAN", "0")
	t.Setenv("MACLAW_QWEN3_METAL", "0")
	q, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatalf("NewQwen3Embedder: %v", err)
	}
	defer q.Close()

	const text = "请把这份文件发给张三"
	cpu, err := q.Embed(text)
	if err != nil {
		t.Fatalf("cpu embed: %v", err)
	}

	released := false
	RegisterNPUBackend(func(q *Qwen3Embedder) (NPUForwarder, error) {
		return &passthroughNPU{released: &released}, nil
	})
	if !q.initNPU() {
		t.Fatalf("matching NPU backend must be adopted, reason: %s", q.NPUReason())
	}
	if !q.NPUActive() {
		t.Fatal("adopted backend must be marked active")
	}
	if got := q.Accel().Backend; got != BackendAMDNPU {
		t.Fatalf("Accel().Backend = %q, want %q", got, BackendAMDNPU)
	}

	onNPU, err := q.Embed(text)
	if err != nil {
		t.Fatalf("npu embed: %v", err)
	}
	if len(onNPU) != len(cpu) {
		t.Fatalf("vector width changed: %d vs %d", len(onNPU), len(cpu))
	}
	for i := range cpu {
		if d := math.Abs(float64(onNPU[i] - cpu[i])); d > 1e-5 {
			t.Fatalf("component %d differs by %g (npu %g vs cpu %g)", i, d, onNPU[i], cpu[i])
		}
	}

	// Dropping the backend must fall back to the CPU path, not to a stale one.
	q.releaseNPU()
	if q.NPUActive() {
		t.Fatal("releaseNPU must clear the backend")
	}
	if !released {
		t.Fatal("releaseNPU must release the provider backend")
	}
	back, err := q.Embed(text)
	if err != nil {
		t.Fatalf("embed after release: %v", err)
	}
	for i := range cpu {
		if d := math.Abs(float64(back[i] - cpu[i])); d > 1e-5 {
			t.Fatalf("CPU fallback component %d differs by %g", i, d)
		}
	}
}
