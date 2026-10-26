package embedding

import (
	"math"
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// vkTestModel returns the Qwen3 GGUF path when it exists on this machine.
func vkTestModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("MACLAW_EMBED_MODEL_PATH"); p != "" {
		return p
	}
	p, _ := ResolveQwen3ModelPath("")
	if p == "" {
		t.Skip("no Qwen3 GGUF available")
	}
	return p
}

// TestVulkanSelfCheck verifies the Vulkan backend produces the same embedding
// as the CPU path. The embedder's own init already runs this comparison; this
// test makes the numbers visible and fails loudly on drift.
func TestVulkanSelfCheck(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	texts := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"hello",
		"what is the weather like today",
	}
	for _, txt := range texts {
		ids := q.Tokenize(txt)
		got := make([]float32, q.hp.Dim)
		if err := q.vkGPU.Load().forward(q, [][]int{ids}, [][]float32{got}); err != nil {
			t.Fatalf("forward %q: %v", txt, err)
		}
		s := q.getScratch(len(ids) + 8)
		ref, err := q.forwardFrom(ids, nil, nil, s, 1)
		q.putScratch(s)
		if err != nil {
			t.Fatalf("cpu forward %q: %v", txt, err)
		}
		// forward returns the raw last row; forwardFrom already applies the
		// output norm, so normalise only the GPU side.
		gn := make([]float32, q.hp.Dim)
		tensor.RMSNorm(gn, got, q.outputNorm, q.hp.RMSNormEps)
		cos := cosineF32(gn, ref)
		t.Logf("%-40q tokens=%2d cos=%.8f", txt, len(ids), cos)
		if cos < qwen3VKSelfCheckMinCos {
			t.Errorf("%q: cos %.8f < %.4f (Vulkan output diverges from CPU)", txt, cos, qwen3VKSelfCheckMinCos)
		}
		if math.IsNaN(cos) {
			t.Errorf("%q: NaN cosine", txt)
		}
	}
}

// TestVulkanEmbedMatchesCPU compares the public Embed path with the CPU path
// for the same text, which is what retrieval actually consumes.
func TestVulkanEmbedMatchesCPU(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	if q.vkGPU.Load() == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	const txt = "帮我把这个文件发给张三"
	gpu, err := q.Embed(txt)
	if err != nil {
		t.Fatalf("gpu Embed: %v", err)
	}
	// Disable the GPU so the same call takes the CPU path.
	saved := q.vkGPU.Swap(nil)
	defer q.vkGPU.Store(saved)
	cpu, err := q.Embed(txt)
	if err != nil {
		t.Fatalf("cpu Embed: %v", err)
	}
	cos := cosineF32(gpu, cpu)
	t.Logf("Embed cos(gpu, cpu) = %.8f", cos)
	if cos < 0.999 {
		t.Errorf("cos %.8f < 0.999", cos)
	}
}

// TestVulkanBatchMatchesSingle checks that packing several sequences into one
// forward (the EmbedBatch path) gives each sequence the same vector as running
// it alone — i.e. the segment handling and causal mask are right.
func TestVulkanBatchMatchesSingle(t *testing.T) {
	path := vkTestModel(t)
	q, err := NewQwen3Embedder(path, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer q.Close()
	g := q.vkGPU.Load()
	if g == nil {
		t.Skipf("Vulkan backend not active: %s", q.gpuReason)
	}
	texts := []string{"hello", "帮我把这个文件发给张三", "what is the weather like today"}
	ids := make([][]int, len(texts))
	for i, t2 := range texts {
		ids[i] = q.Tokenize(t2)
	}
	// Each sequence alone.
	single := make([][]float32, len(texts))
	for i := range ids {
		single[i] = make([]float32, q.hp.Dim)
		if err := g.forward(q, ids[i:i+1], single[i:i+1]); err != nil {
			t.Fatalf("single forward: %v", err)
		}
	}
	// All sequences packed into one forward.
	packed := make([][]float32, len(ids))
	for i := range packed {
		packed[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, ids, packed); err != nil {
		t.Fatalf("packed forward: %v", err)
	}
	for i := range texts {
		cos := cosineF32(single[i], packed[i])
		t.Logf("%-40q cos(single, packed)=%.8f", texts[i], cos)
		if cos < 0.9999 {
			t.Errorf("%q: packed cos %.8f < 0.9999 (batch isolation broken)", texts[i], cos)
		}
	}
}
