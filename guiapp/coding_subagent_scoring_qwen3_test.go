package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type codingFamilyEmb struct{ id string }

func (e codingFamilyEmb) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }
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
		t.Fatalf("qwen3: role=%v baseline=%v, want RoleQuery/0.40", role, b)
	}
	if _, err := embedCodingQuery(q, "add pytest coverage"); err != nil || len(q.roles) != 1 || q.roles[0] != embedding.RoleQuery {
		t.Fatalf("qwen3 task side must use RoleQuery, got %v err=%v", q.roles, err)
	}
}
