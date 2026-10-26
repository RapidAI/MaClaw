package intent

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// shippedArtifacts returns the installed head and embedding model, skipping
// when either is absent (CI).
func shippedArtifacts(t *testing.T) (headPath, modelPath string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
	}
	models := filepath.Join(home, ".maclaw", "models")
	headPath = filepath.Join(models, "intent_heads", "head_mlp_qwen3go_r13f1.json")
	modelPath = filepath.Join(models, embedding.Qwen3ModelFilename)
	for _, p := range []string{headPath, modelPath} {
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			t.Skipf("missing shipped artifact %s", p)
		}
	}
	return headPath, modelPath
}

// TestShippedHeadOnShippedModel exercises the production input pipeline end to
// end: the head is fed the concatenated per-block vector HeadQueryInput builds
// (last-token + mean, block-normalised), not the bare Dim()-sized embedding.
// Synthetic head tests cannot catch a wiring break here.
func TestShippedHeadOnShippedModel(t *testing.T) {
	headPath, modelPath := shippedArtifacts(t)
	h, err := LoadLocalHead(headPath)
	if err != nil {
		t.Fatalf("load head: %v", err)
	}
	emb, err := embedding.NewQwen3Embedder(modelPath, 0)
	if err != nil {
		t.Fatalf("load embedder: %v", err)
	}
	defer emb.Close()

	const text = "帮我把这个文件发给张三，然后打开设置页面确认一下"
	vec, scores, err := HeadQueryInput(context.Background(), emb, h, text)
	if err != nil {
		t.Fatalf("HeadQueryInput: %v", err)
	}
	// The network input is the concatenation of all blocks; Dim() is one block.
	if len(vec) != h.dim {
		t.Fatalf("query vector dim %d != head network dim %d", len(vec), h.dim)
	}
	probs, err := h.PredictWithAnchors(vec, scores)
	if err != nil {
		t.Fatalf("PredictWithAnchors: %v", err)
	}
	if len(probs) != len(h.Labels) {
		t.Fatalf("got %d probs for %d labels", len(probs), len(h.Labels))
	}
	var sum float64
	for i, p := range probs {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
			t.Fatalf("prob[%d] = %v", i, p)
		}
		sum += p
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Fatalf("probabilities sum to %v, want 1", sum)
	}
	label, prob, decision, threshold := h.Decide(probs)
	if label == "" || prob <= 0 || prob > 1 || threshold <= 0 {
		t.Fatalf("Decide = %q p=%v decision=%q tau=%v", label, prob, decision, threshold)
	}
	t.Logf("label=%s p=%.3f decision=%s tau=%.3f (q8p=%v twoPass=%v)",
		label, prob, decision, threshold, h.w1q != nil, h.headQ8P2)
}

// TestShippedHeadQ8PAgreesOnEmbeddings checks that the int8 path chosen for the
// shipped head agrees with float32 on real embedding inputs, not just on the
// synthetic probe vectors used at load time.
func TestShippedHeadQ8PAgreesOnEmbeddings(t *testing.T) {
	headPath, modelPath := shippedArtifacts(t)
	emb, err := embedding.NewQwen3Embedder(modelPath, 0)
	if err != nil {
		t.Fatalf("load embedder: %v", err)
	}
	defer emb.Close()

	texts := []string{
		"帮我把这个文件发给张三",
		"查询 api2 服务器状态",
		"把这份季度报告整理成PDF",
		"read the config and restart the service",
		"summarize the meeting notes",
	}
	v := func(h *LocalHead) [][]float64 {
		out := make([][]float64, len(texts))
		for i, text := range texts {
			vec, scores, err := HeadQueryInput(context.Background(), emb, h, text)
			if err != nil {
				t.Fatalf("HeadQueryInput %q: %v", text, err)
			}
			p, err := h.PredictWithAnchors(vec, scores)
			if err != nil {
				t.Fatalf("PredictWithAnchors %q: %v", text, err)
			}
			out[i] = p
		}
		return out
	}

	t.Setenv("MACLAW_HEAD_Q8P", "0")
	floatHead, err := LoadLocalHead(headPath)
	if err != nil {
		t.Fatalf("load float head: %v", err)
	}
	want := v(floatHead)

	t.Setenv("MACLAW_HEAD_Q8P", "2")
	int8Head, err := LoadLocalHead(headPath)
	if err != nil {
		t.Fatalf("load int8 head: %v", err)
	}
	got := v(int8Head)

	var worst float64
	for i := range want {
		for j := range want[i] {
			if d := math.Abs(want[i][j] - got[i][j]); d > worst {
				worst = d
			}
		}
	}
	t.Logf("max probability delta on real embeddings: %.2g", worst)
	if worst > 1e-2 {
		t.Fatalf("int8 deviates from float32 by %.2g on real embeddings", worst)
	}
}
