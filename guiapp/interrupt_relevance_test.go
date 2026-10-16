package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

// relTestEmbedder is a deterministic fake: family comes from ModelID, the
// vector depends on whether the text carries an "Instruct:" prefix.
type relTestEmbedder struct {
	id    string
	dim   int
	texts []string
}

func (e *relTestEmbedder) Embed(t string) ([]float32, error) {
	e.texts = append(e.texts, t)
	v := make([]float32, e.dim)
	v[0] = 1
	if strings.HasPrefix(t, "Instruct: ") {
		v[1] = 1
	}
	return v, nil
}
func (e *relTestEmbedder) EmbedBatch(ts []string) ([][]float32, error) {
	out := make([][]float32, len(ts))
	for i, t := range ts {
		out[i], _ = e.Embed(t)
	}
	return out, nil
}
func (e *relTestEmbedder) Dim() int        { return e.dim }
func (e *relTestEmbedder) ModelID() string { return e.id }
func (e *relTestEmbedder) Close()          {}

func TestInterruptRelevanceModelResolution(t *testing.T) {
	t.Setenv(interruptRelevanceEnv, "")
	gemma := &relTestEmbedder{id: "embeddinggemma-300m:768", dim: 768}
	if m := interruptRelevanceModelFor(gemma); m != nil {
		t.Fatal("Gemma must keep the legacy plain cosine")
	}
	q := &relTestEmbedder{id: "Qwen3 Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1", dim: 1024}
	if got := interruptRelevanceModelFor(q); got != builtinQwen3InterruptRelevance {
		t.Fatalf("Qwen3 without a model file must use the built-in model, got %+v", got)
	}
	t.Setenv(interruptRelevanceEnv, "off")
	if m := interruptRelevanceModelFor(q); m != nil {
		t.Fatal("MACLAW_INTERRUPT_RELEVANCE=off must restore legacy cosine")
	}
	// Forced model file: bound by embedder model id.
	dir := t.TempDir()
	p := filepath.Join(dir, "rel.json")
	doc := `{"format":"maclaw-interrupt-relevance-v1","kind":"cosine","embedder_model_id":"` + q.id + `","dims":1024,"task_instruction":"T","high":0.8,"low":0.5}`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(interruptRelevanceEnv, p)
	m := interruptRelevanceModelFor(q)
	if m == nil || m.TaskInstruction != "T" {
		t.Fatalf("forced model not loaded: %+v", m)
	}
	other := &relTestEmbedder{id: "Qwen3 Embedding 0.6b:1024:prompt-v0", dim: 1024}
	if interruptRelevanceModelFor(other) != nil {
		t.Fatal("model must refuse an embedder with a different model id")
	}
}

func TestInterruptModelRelevanceUsesFormattedTaskCache(t *testing.T) {
	q := &relTestEmbedder{id: "qwen3", dim: 4}
	m := &progress.RelevanceModel{Format: progress.RelevanceModelFormat, Kind: "cosine", TaskInstruction: "T", High: 0.9, Low: 0.5}
	ih := &imInterruptHandler{}
	// Loop start embedded the formatted task once; TryInterrupt must reuse it.
	vec, _ := q.Embed(m.FormatTask("fix the bug"))
	ih.taskVecs.put(m.FormatTask("fix the bug"), vec)
	q.texts = nil
	r := ih.modelRelevance(q, m, "fix the bug", "also add tests")
	if len(q.texts) != 1 || q.texts[0] != "also add tests" {
		t.Fatalf("expected only the message to be embedded (plain), got %q", q.texts)
	}
	// u=(1,1,0,0) v=(1,0,0,0): cos=0.7071 → 0.30 + (0.7071-0.5)*0.3/0.4
	if want := 0.30 + (0.70710678-0.5)*0.3/0.4; r < want-1e-4 || r > want+1e-4 {
		t.Fatalf("relevance=%v want %v", r, want)
	}
	var _ embedding.Embedder = q
}
