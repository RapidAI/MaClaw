package tool

// LOCAL EVAL HARNESS (local branch only, §16 Qwen3 tool head): Track-A
// retrieval ranking exactly as Router.Route computes it (BM25 over
// buildSearchText + embedding cosine over buildEmbeddingText, alpha fusion,
// max-normalisation), with a selectable query-side role so a Qwen3-only Track A
// can reuse the intent L2/L3 RoleClassification query vector. Same as
// TestZZToolRetrieval at 8b4e2f2f plus ZZ_EMBEDDER / ZZ_QROLE / ZZ_QVEC_OUT.

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

func TestZZToolRetrievalRole(t *testing.T) {
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
	// ZZ_EMBEDDER=qwen3 → Qwen3-Embedding (pure Go). Tool descriptions are
	// embedded exactly as Router does (Embed, no instruct). ZZ_QROLE selects
	// the query side: "" = Router default (Embed, no instruct, i.e. an extra
	// embed per turn); "classification" = reuse the intent L2/L3 query vector
	// (RoleClassification instruct; zero extra embed); "query" = retrieval
	// instruct. ZZ_QVEC_OUT dumps the query vectors used.
	var emb embedding.Embedder
	if os.Getenv("ZZ_EMBEDDER") == "qwen3" {
		q, err := embedding.NewQwen3Embedder(os.Getenv("ZZ_GGUF"), 0)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(os.Stderr, "qwen3 model id:", q.ModelID(), "dim", q.Dim())
		emb = q
	} else {
		g, err := embedding.NewGemmaEmbedder(os.Getenv("ZZ_GGUF"), embedding.DefaultEmbeddingDim)
		if err != nil {
			t.Fatal(err)
		}
		emb = g
	}
	qr := &zzQueryRoleEmbedder{Embedder: emb, queries: map[string]bool{}}
	switch os.Getenv("ZZ_QROLE") {
	case "classification":
		qr.role = embedding.RoleClassification
	case "query":
		qr.role = embedding.RoleQuery
	}
	r := NewRouter(nil)
	r.SetEmbedder(qr)
	var qvecOut *json.Encoder
	if p := os.Getenv("ZZ_QVEC_OUT"); p != "" {
		qf, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer qf.Close()
		qvecOut = json.NewEncoder(qf)
	}
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
		qr.queries[q.Text] = true
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
		if qvecOut != nil {
			v, _ := qr.Embed(q.Text) // cached path not used: recompute for the dump
			_ = qvecOut.Encode(map[string]any{"id": q.ID, "vec": v})
		}
	}
	fmt.Fprintln(os.Stderr, "ZZ tool retrieval done")
}

// zzQueryRoleEmbedder embeds registered query texts with role (when the
// underlying embedder is a RoleEmbedder and role != RoleNone) and everything
// else (tool descriptions) unchanged.
type zzQueryRoleEmbedder struct {
	embedding.Embedder
	role    embedding.Role
	queries map[string]bool
}

func (z *zzQueryRoleEmbedder) Embed(text string) ([]float32, error) {
	if z.role != embedding.RoleNone && z.queries[text] {
		if re, ok := z.Embedder.(embedding.RoleEmbedder); ok {
			return re.EmbedWithRole(text, z.role)
		}
	}
	return z.Embedder.Embed(text)
}
