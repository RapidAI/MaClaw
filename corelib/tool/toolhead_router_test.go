package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

type thFakeEmb struct {
	id  string
	dim int
	v   []float32
}

func (f thFakeEmb) Embed(string) ([]float32, error) { return append([]float32(nil), f.v...), nil }
func (f thFakeEmb) EmbedBatch(t []string) ([][]float32, error) {
	out := make([][]float32, len(t))
	for i := range t {
		out[i], _ = f.Embed(t[i])
	}
	return out, nil
}
func (f thFakeEmb) Dim() int        { return f.dim }
func (f thFakeEmb) Close()          {}
func (f thFakeEmb) ModelID() string { return f.id }

func writeMiniToolHead(t *testing.T, dir string, modelID string, dim int, tools []string, wRow0 []float64) string {
	t.Helper()
	trained := make([]bool, len(tools))
	W := make([][]float64, len(tools))
	b := make([]float64, len(tools))
	for i := range tools {
		trained[i] = true
		W[i] = make([]float64, dim)
		if i == 0 {
			copy(W[i], wRow0)
		}
	}
	h := map[string]any{
		"format": ToolHeadFormat, "embedder_model_id": modelID, "embed_role": "classification",
		"dims": dim, "l2norm": true, "label_table_version": ToolLabelTableVersion(tools),
		"tools": tools, "trained": trained, "W": W, "b": b, "theta": 0.3, "top_k": 5,
	}
	bjson, _ := json.Marshal(h)
	p := filepath.Join(dir, "tool_head_ovr_test.json")
	os.WriteFile(p, bjson, 0o644)
	return p
}

func TestSetToolHeadRefusesMismatch(t *testing.T) {
	dir := t.TempDir()
	p := writeMiniToolHead(t, dir, "qwen3:1024", 4, []string{"web_search", "read_file"}, []float64{1, 0, 0, 0})
	h, err := LoadToolHead(p)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter(nil)
	emb := thFakeEmb{id: "gemma:768", dim: 4, v: []float32{1, 0, 0, 0}}
	if err := r.SetToolHead(h, emb, 0, 0); err == nil {
		t.Fatal("want mismatch")
	}
	if r.ToolHead() != nil {
		t.Fatal("head must not be installed on mismatch")
	}
	embOK := thFakeEmb{id: "qwen3:1024", dim: 4, v: []float32{1, 0, 0, 0}}
	if err := r.SetToolHead(h, embOK, 0, 0); err != nil {
		t.Fatal(err)
	}
	if r.ToolHead() == nil {
		t.Fatal("want head")
	}
}

func TestPutQuerySeedsFuseScores(t *testing.T) {
	emb := thFakeEmb{id: "qwen3:1024", dim: 4, v: []float32{0, 1, 0, 0}}
	h := NewHybridRetriever(emb)
	// Seed a classification vector that differs from Embed()'s return.
	seed := []float32{1, 0, 0, 0}
	h.PutQuery("q", seed)
	got, err := h.queryCache.Get("q")
	if err != nil || len(got) != 4 || got[0] != 1 {
		t.Fatalf("cache should return seeded vec, got %v err=%v", got, err)
	}
	// FuseScoresWithQueryVec should not call Embed when vec is provided.
	scores := h.FuseScoresWithQueryVec("q", seed, map[string]float64{"web_search": 1}, map[string]string{"web_search": "search the web"})
	if scores["web_search"] <= 0 {
		t.Fatalf("fused=%v", scores)
	}
}

func TestToolHeadCandidatesAndAbstainInject(t *testing.T) {
	dir := t.TempDir()
	p := writeMiniToolHead(t, dir, "qwen3:1024", 4, []string{"web_search", "read_file"}, []float64{5, 0, 0, 0})
	h, err := LoadToolHead(p)
	if err != nil {
		t.Fatal(err)
	}
	emb := thFakeEmb{id: "qwen3:1024", dim: 4, v: []float32{1, 0, 0, 0}}
	r := NewRouter(nil)
	if err := r.SetToolHead(h, emb, 0.3, 5); err != nil {
		t.Fatal(err)
	}
	vec := []float32{1, 0, 0, 0}
	got := r.ToolHeadCandidates(vec)
	if len(got) == 0 || got[0].Tool != "web_search" {
		t.Fatalf("candidates=%v", got)
	}
	// UIC abstained and the cached label has no vector. The live PreResolved
	// vector is still the head's input, and condKeep must record the hit.
	opts := RouteOptions{SkipUnifiedClassifier: true, PreResolved: &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.3, Degraded: true, QueryVec: vec,
	}}
	if len(routeToolHeadVector(intent.ClassificationResult{}, opts)) != 4 {
		t.Fatal("pre-resolved vector dropped")
	}
	keep := map[string]bool{}
	r.mu.Lock()
	r.applyToolHeadCondKeep(keep, routeToolHeadVector(intent.ClassificationResult{}, opts))
	r.mu.Unlock()
	if !keep["web_search"] {
		t.Fatalf("abstain path did not inject, keep=%v", keep)
	}
}

func TestToolHeadInjectionUsesQueryVec(t *testing.T) {
	dir := t.TempDir()
	tools := []string{"web_search", "read_file"}
	p := writeMiniToolHead(t, dir, "qwen3:1024", 4, tools, []float64{5, 0, 0, 0}) // web_search high for e0
	h, _ := LoadToolHead(p)
	emb := thFakeEmb{id: "qwen3:1024", dim: 4, v: []float32{1, 0, 0, 0}}
	r := NewRouter(nil)
	r.SetEmbedder(emb)
	if err := r.SetToolHead(h, emb, 0.3, 5); err != nil {
		t.Fatal(err)
	}
	probs, err := h.Predict([]float32{1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	top := h.TopK(probs, 5, 0.3)
	if len(top) == 0 || top[0].Tool != "web_search" {
		t.Fatalf("top=%v probs=%v", top, probs)
	}
	_ = embedding.IsNoop
}
