package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type codingFamilyEmb struct{ id string }

func (e codingFamilyEmb) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }

type codingRecEmb struct {
	codingRoleEmb
	embedded []string
	batched  [][]string
}

func (e *codingRecEmb) Embed(t string) ([]float32, error) {
	e.embedded = append(e.embedded, t)
	return []float32{1, 0}, nil
}
func (e *codingRecEmb) EmbedBatch(t []string) ([][]float32, error) {
	e.batched = append(e.batched, append([]string(nil), t...))
	return e.codingRoleEmb.EmbedBatch(t)
}
func (e codingFamilyEmb) EmbedBatch(t []string) ([][]float32, error) {
	out := make([][]float32, len(t))
	for i := range t {
		out[i] = []float32{1, 0}
	}
	return out, nil
}
func (e codingFamilyEmb) Dim() int        { return 2 }
func (e codingFamilyEmb) Close()          {}
func (e codingFamilyEmb) ModelID() string { return e.id }

type codingRoleEmb struct {
	codingFamilyEmb
	roles []embedding.Role
}

func (e *codingRoleEmb) EmbedWithRole(t string, r embedding.Role) ([]float32, error) {
	e.roles = append(e.roles, r)
	return []float32{0, 1}, nil
}
func (e *codingRoleEmb) EmbedBatchWithRole(t []string, r embedding.Role) ([][]float32, error) {
	return e.EmbedBatch(t)
}

func TestCodingEmbeddingPolicyPerFamily(t *testing.T) {
	g := codingFamilyEmb{"embeddinggemma-300m"}
	if role, b := codingEmbeddingPolicy(g); role || b != codingSubAgentEmbeddingBaseline {
		t.Fatalf("gemma: role=%v baseline=%v, want plain/0.2 (unchanged)", role, b)
	}
	qp := codingFamilyEmb{"Qwen3 Embedding 0.6b:1024"}
	if role, b := codingEmbeddingPolicy(qp); role || b != codingSubAgentEmbeddingBaselineQwen3Plain {
		t.Fatalf("qwen3 without prompts: role=%v baseline=%v", role, b)
	}
	q := &codingRoleEmb{codingFamilyEmb: codingFamilyEmb{"Qwen3 Embedding 0.6b:1024"}}
	if role, b := codingEmbeddingPolicy(q); !role || b != codingSubAgentEmbeddingBaselineQwen3 {
		t.Fatalf("qwen3: role=%v baseline=%v, want instructed/0.50", role, b)
	}
	if p := codingEmbeddingPolicyFor(q); p.bm25Weight != codingSubAgentBM25WeightQwen3 {
		t.Fatalf("qwen3 bm25 weight %v", p.bm25Weight)
	}
	if p := codingEmbeddingPolicyFor(g); p.bm25Weight != 1 || p.instructed {
		t.Fatalf("gemma policy changed: %+v", p)
	}
	r := &codingRecEmb{codingRoleEmb: codingRoleEmb{codingFamilyEmb: codingFamilyEmb{"Qwen3 Embedding 0.6b:1024"}}}
	if _, err := embedCodingQuery(r, "add pytest coverage"); err != nil || len(r.embedded) != 1 ||
		r.embedded[0] != "Instruct: "+codingSubAgentQwen3TaskInstruction+"\nQuery: add pytest coverage" {
		t.Fatalf("qwen3 task side must carry the coding instruction, got %q err=%v", r.embedded, err)
	}
	scoreAndSelectTopKWithEmbedDocs("add pytest coverage", []string{"pytest python tests"}, []string{codingQwen3EmbedDoc("pytest", " python tests")}, r, 5, 0.15)
	if len(r.batched) != 1 || r.batched[0][0] != "Tool: pytest\nDescription: python tests" {
		t.Fatalf("qwen3 docs must use the structured embed doc, got %q", r.batched)
	}
	gr := &codingRecEmb{codingRoleEmb: codingRoleEmb{codingFamilyEmb: codingFamilyEmb{"embeddinggemma-300m"}}}
	scoreAndSelectTopKWithEmbedDocs("add pytest coverage", []string{"pytest python tests"}, []string{"Tool: pytest"}, gr, 5, 0.15)
	if len(gr.batched) != 1 || gr.batched[0][0] != "pytest python tests" || gr.embedded[0] != "add pytest coverage" {
		t.Fatalf("gemma must stay plain, got %q / %q", gr.batched, gr.embedded)
	}
}
