package tool

// LOCAL EVAL HARNESS (local branch only): Track-A retrieval ranking exactly as
// Router.Route computes it (BM25 over buildSearchText + EmbeddingGemma cosine
// over buildEmbeddingText, alpha fusion, max-normalisation), without the
// usage tracker / core keep rules, over the real 49 first-party tools.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestZZToolRetrieval(t *testing.T) {
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
	emb, err := embedding.NewGemmaEmbedder(os.Getenv("ZZ_GGUF"), embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter(nil)
	r.SetEmbedder(emb)
	docs := []bm25.Doc{}
	embTexts := map[string]string{}
	for _, d := range defs {
		name := ExtractToolName(d)
		desc := ExtractToolDescription(d)
		docs = append(docs, bm25.Doc{ID: name, Text: r.buildSearchText(name, desc)})
		embTexts[name] = r.buildEmbeddingText(name, desc)
	}
	r.bm25Index.RebuildIfChanged(docs)
	f, _ := os.Open(in)
	defer f.Close()
	out, _ := os.Create(os.Getenv("ZZ_OUT"))
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var q struct{ ID, Text string }
		_ = json.Unmarshal(sc.Bytes(), &q)
		t0 := time.Now()
		scores := r.bm25Index.ScoreWithTokens(bm25.Tokenize(q.Text))
		bmHits := len(scores)
		scores = r.hybrid.FuseScores(q.Text, scores, embTexts)
		norm := normalizeRetrievalScores(scores)
		type kv struct {
			N string  `json:"n"`
			S float64 `json:"s"`
		}
		var l []kv
		for n, s := range norm {
			l = append(l, kv{n, s})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].S > l[j].S || (l[i].S == l[j].S && l[i].N < l[j].N) })
		ms := float64(time.Since(t0).Microseconds()) / 1000
		if len(l) > 20 {
			l = l[:20]
		}
		_ = enc.Encode(map[string]any{"id": q.ID, "top": l, "ms": ms, "bm25_hits": bmHits})
	}
	fmt.Fprintln(os.Stderr, "ZZ tool retrieval done")
}
