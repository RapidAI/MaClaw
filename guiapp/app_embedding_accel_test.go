package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type accelTestEmbedder struct {
	appEmbeddingTestEmbedder
	applied []bool
}

func (e *accelTestEmbedder) Accel() embedding.AccelInfo {
	return embedding.AccelInfo{Backend: embedding.BackendCPUSIMD, Device: "q8p:test:2pass", Reason: "qwen3 runs on CPU SIMD"}
}
func (e *accelTestEmbedder) ApplyAccel(v bool) { e.applied = append(e.applied, v) }

// The HW-accel paths used to assert *embedding.GemmaEmbedder, which never
// matches once Qwen3 is the single model: the badge then showed global NPU
// detection and the toggle silently did nothing. They now use the
// embedding.AccelEmbedder interface.
func TestEmbedAccelUsesLiveInstanceViaInterface(t *testing.T) {
	var _ embedding.AccelEmbedder = (*embedding.Qwen3Embedder)(nil)
	emb := &accelTestEmbedder{}
	app := &App{}
	got, err := app.sharedEmbeddingEmbedder("m.gguf", func(string) (embedding.Embedder, error) { return emb, nil })
	if err != nil || got != emb {
		t.Fatalf("load: %v %v", got, err)
	}
	if len(emb.applied) != 1 {
		t.Fatalf("ApplyAccel must run on load for any AccelEmbedder, calls=%v", emb.applied)
	}
	if info := app.GetEmbedAccelInfo(); info.Device != "q8p:test:2pass" {
		t.Fatalf("badge must come from the live instance, got %+v", info)
	}
}
