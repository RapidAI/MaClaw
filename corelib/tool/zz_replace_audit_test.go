package tool

// LOCAL EVAL HARNESS (local branch only, Qwen3-vs-Gemma replacement audit):
// dumps the raw BM25 channel of Track A (exactly as Router.Route builds it:
// buildSearchText docs, bm25.Tokenize query) and the buildEmbeddingText of
// every tool, so the cosine channel and alpha fusion can be recomputed offline
// for any embedder / role / alpha without re-running the router.

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestZZReplaceAuditBM25Dump(t *testing.T) {
	in := os.Getenv("ZZ_IN")
	if in == "" {
		t.Skip("ZZ_IN not set")
	}
	var defs []map[string]interface{}
	b, err := os.ReadFile(os.Getenv("ZZ_TOOLS"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &defs); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(nil)
	docs := []bm25.Doc{}
	tf, _ := os.Create(os.Getenv("ZZ_TOOLTEXT_OUT"))
	tenc := json.NewEncoder(tf)
	tenc.SetEscapeHTML(false)
	for _, d := range defs {
		name := ExtractToolName(d)
		desc := ExtractToolDescription(d)
		docs = append(docs, bm25.Doc{ID: name, Text: r.buildSearchText(name, desc)})
		_ = tenc.Encode(map[string]any{"id": name, "text": r.buildEmbeddingText(name, desc), "desc": desc})
	}
	tf.Close()
	r.bm25Index.RebuildIfChanged(docs)
	f, _ := os.Open(in)
	defer f.Close()
	out, _ := os.Create(os.Getenv("ZZ_OUT"))
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var q struct{ ID, Text string }
		_ = json.Unmarshal(sc.Bytes(), &q)
		_ = enc.Encode(map[string]any{"id": q.ID, "bm25": r.bm25Index.ScoreWithTokens(bm25.Tokenize(q.Text))})
	}
}

type zzRoleSpy struct {
	dim   int
	id    string
	roles []embedding.Role
}

func (z *zzRoleSpy) Embed(t string) ([]float32, error) { return z.EmbedWithRole(t, embedding.RoleNone) }
func (z *zzRoleSpy) EmbedBatch(ts []string) ([][]float32, error) {
	return z.EmbedBatchWithRole(ts, embedding.RoleNone)
}
func (z *zzRoleSpy) EmbedWithRole(t string, r embedding.Role) ([]float32, error) {
	z.roles = append(z.roles, r)
	v := make([]float32, z.dim)
	v[0] = 1
	return v, nil
}
func (z *zzRoleSpy) EmbedBatchWithRole(ts []string, r embedding.Role) ([][]float32, error) {
	out := make([][]float32, len(ts))
	for i, t := range ts {
		out[i], _ = z.EmbedWithRole(t, r)
	}
	return out, nil
}
func (z *zzRoleSpy) Dim() int        { return z.dim }
func (z *zzRoleSpy) Close()          {}
func (z *zzRoleSpy) ModelID() string { return z.id }

func TestToolDocRoleQwen3Classification(t *testing.T) {
	t.Setenv(ToolDocRoleEnv, "")
	t.Setenv("HOME", t.TempDir())
	q := &zzRoleSpy{dim: 1024, id: "Qwen3 Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1"}
	g := &zzRoleSpy{dim: 768, id: "Embeddinggemma 300m:768:prompt-v1"}
	if toolDocRole(q) != embedding.RoleClassification || toolDocRole(g) != embedding.RoleNone {
		t.Fatal("doc role: qwen3 → classification, gemma → none")
	}
	if !strings.Contains(toolEmbeddingCacheModelID(q), "tooldoc-role=classification") || strings.Contains(toolEmbeddingCacheModelID(g), "tooldoc-role") {
		t.Fatal("cache key must change with the doc role (switch re-embeds once)")
	}
	c := NewToolEmbeddingCache(q)
	if _, err := c.Get("read_file: read a file"); err != nil {
		t.Fatal(err)
	}
	if len(q.roles) != 1 || q.roles[0] != embedding.RoleClassification {
		t.Fatalf("roles %v", q.roles)
	}
	t.Setenv(ToolDocRoleEnv, "none")
	if toolDocRole(q) != embedding.RoleNone {
		t.Fatal("rollback env ignored")
	}
}
