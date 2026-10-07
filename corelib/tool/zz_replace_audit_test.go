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
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
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
