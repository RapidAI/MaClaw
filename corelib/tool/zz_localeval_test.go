package tool

// LOCAL EVAL HARNESS (local branch only): Track-A / tool-head / combined
// intent→tool latency. Not for upstream.
//
// Env:
//   ZZ_IN, ZZ_OUT, ZZ_TOOLS, ZZ_GGUF (required)
//   ZZ_MODE=trackA|toolhead|combined   (default trackA)
//   ZZ_TOOL_HEAD=heads/tool_head_ovr_gemma.json  (toolhead / combined inject)
//   ZZ_HEAD=intent MLP head.json + ZZ_L3=localhead + ZZ_TAU_S  (combined)
//   ZZ_WARMUP_QUERIES=2  (discarded after tool-cache warm; default 2)

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// zzModelProbe reports which GGUF files are mmap'd into this process (every
// embedder mmaps its weights), the embedder type, the Q8P ISA and whether the
// process-wide shared Gemma singleton was ever loaded. With
// ZZ_EXPECT_SINGLE_GGUF=1 it fails if any GGUF other than ZZ_GGUF is mapped
// or a GemmaEmbedder is in use while ZZ_GGUF is not Gemma.
func zzModelProbe(t *testing.T, stage string, emb embedding.Embedder) {
	t.Helper()
	maps, _ := os.ReadFile("/proc/self/maps")
	seen := map[string]bool{}
	for _, line := range splitLines(maps) {
		f := fieldsBytes(line)
		if len(f) >= 6 {
			path := string(f[len(f)-1])
			if len(path) > 5 && path[len(path)-5:] == ".gguf" {
				seen[path] = true
			}
		}
	}
	var files []string
	for k := range seen {
		files = append(files, k)
	}
	sort.Strings(files)
	_, isGemma := emb.(*embedding.GemmaEmbedder)
	fmt.Fprintf(os.Stderr, "ZZ probe[%s] embedder=%T isGemma=%v isa=%s 2pass_env=%q sharedEmbReady=%v mapped_gguf=%v\n",
		stage, emb, isGemma, tensor.Q8PISAName(), os.Getenv("MACLAW_QWEN3_Q8P_2PASS"), embedding.SharedEmbedderReady(), files)
	if os.Getenv("ZZ_EXPECT_SINGLE_GGUF") == "1" {
		want := os.Getenv("ZZ_GGUF")
		if len(files) != 1 || files[0] != want {
			t.Fatalf("single-model check failed at %s: mapped=%v want only %s", stage, files, want)
		}
		if embedding.SharedEmbedderReady() {
			t.Fatalf("single-model check failed at %s: shared Gemma singleton loaded", stage)
		}
	}
}

type zzToolIn struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type zzToolHead struct {
	Tools []string    `json:"tools"`
	W     [][]float64 `json:"W"`
	B     []float64   `json:"b"`
}

func zzToolRSSMB() float64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range splitLines(b) {
		if len(line) >= 6 && string(line[:6]) == "VmRSS:" {
			fields := fieldsBytes(line)
			if len(fields) < 2 {
				return 0
			}
			kb, _ := strconv.ParseFloat(string(fields[1]), 64)
			return kb / 1024
		}
	}
	return 0
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

func fieldsBytes(b []byte) [][]byte {
	var out [][]byte
	i := 0
	for i < len(b) {
		for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
			i++
		}
		if i >= len(b) {
			break
		}
		j := i
		for j < len(b) && b[j] != ' ' && b[j] != '\t' {
			j++
		}
		out = append(out, b[i:j])
		i = j
	}
	return out
}

func zzLoadToolHead(t *testing.T, path string) *zzToolHead {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var h zzToolHead
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Tools) == 0 || len(h.W) != len(h.Tools) || len(h.B) != len(h.Tools) {
		t.Fatalf("bad tool head shape tools=%d W=%d B=%d", len(h.Tools), len(h.W), len(h.B))
	}
	return &h
}

func zzToolHeadTop(h *zzToolHead, vec []float32, k int) []struct {
	N string
	S float64
} {
	// L2-normalize then sigmoid(Wx+b); sort desc.
	var norm float64
	for _, x := range vec {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	type kv struct {
		N string
		S float64
	}
	out := make([]kv, len(h.Tools))
	for i, name := range h.Tools {
		dot := h.B[i]
		row := h.W[i]
		n := len(vec)
		if len(row) < n {
			n = len(row)
		}
		for j := 0; j < n; j++ {
			dot += row[j] * (float64(vec[j]) / norm)
		}
		out[i] = kv{name, 1 / (1 + math.Exp(-dot))}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].S > out[j].S || (out[i].S == out[j].S && out[i].N < out[j].N)
	})
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	res := make([]struct {
		N string
		S float64
	}, len(out))
	for i := range out {
		res[i].N, res[i].S = out[i].N, out[i].S
	}
	return res
}

func zzTopKV(norm map[string]float64, k int) []map[string]any {
	type kv struct {
		N string
		S float64
	}
	var l []kv
	for n, s := range norm {
		l = append(l, kv{n, s})
	}
	sort.Slice(l, func(i, j int) bool {
		return l[i].S > l[j].S || (l[i].S == l[j].S && l[i].N < l[j].N)
	})
	if k > 0 && len(l) > k {
		l = l[:k]
	}
	out := make([]map[string]any, len(l))
	for i, x := range l {
		out[i] = map[string]any{"n": x.N, "s": x.S}
	}
	return out
}

func TestZZToolRetrieval(t *testing.T) {
	in := os.Getenv("ZZ_IN")
	if in == "" {
		t.Skip("ZZ_IN not set")
	}
	mode := os.Getenv("ZZ_MODE")
	if mode == "" {
		mode = "trackA"
	}
	var defs []map[string]interface{}
	b, err := os.ReadFile(os.Getenv("ZZ_TOOLS"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &defs); err != nil {
		t.Fatal(err)
	}

	gguf := os.Getenv("ZZ_GGUF")
	tLoad := time.Now()
	emb, err := embedding.NewEmbedderFromFile(gguf, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "ZZ embedder load %.0fms rss=%.0fMB mode=%s\n",
		float64(time.Since(tLoad).Microseconds())/1000, zzToolRSSMB(), mode)

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

	// Synchronous tool-embedding warm (production GetBatch path).
	tWarm := time.Now()
	_ = r.hybrid.FuseScores("warmup query for tool cache", map[string]float64{}, embTexts)
	fmt.Fprintf(os.Stderr, "ZZ tool cache warm %.0fms tools=%d rss=%.0fMB\n",
		float64(time.Since(tWarm).Microseconds())/1000, len(embTexts), zzToolRSSMB())

	var toolHead *zzToolHead
	if hp := os.Getenv("ZZ_TOOL_HEAD"); hp != "" {
		toolHead = zzLoadToolHead(t, hp)
		fmt.Fprintf(os.Stderr, "ZZ tool head loaded tools=%d dim=%d\n", len(toolHead.Tools), len(toolHead.W[0]))
	} else if mode == "toolhead" {
		t.Fatal("ZZ_TOOL_HEAD required for mode=toolhead")
	}

	var uic *intent.UnifiedIntentClassifier
	if mode == "combined" {
		headPath := os.Getenv("ZZ_HEAD")
		if headPath == "" {
			t.Fatal("ZZ_HEAD required for mode=combined")
		}
		h, err := intent.LoadLocalHead(headPath)
		if err != nil {
			t.Fatal(err)
		}
		if v := os.Getenv("ZZ_TAU_S"); v != "" {
			if ts, e := strconv.ParseFloat(v, 64); e == nil && ts > h.Tau {
				h.TauS = ts
			}
		}
		cfg := intent.Config{
			Embedder:       emb,
			LLMContextFunc: intent.LocalTreeFunc(emb, h, nil),
		}
		uic = intent.New(cfg)
		for !uic.Ready() {
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Fprintf(os.Stderr, "ZZ intent ready rss=%.0fMB head=%s\n", zzToolRSSMB(), headPath)
	}
	zzModelProbe(t, "after_setup", emb)

	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, err := os.Create(os.Getenv("ZZ_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	var queries []zzToolIn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var q zzToolIn
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			t.Fatal(err)
		}
		queries = append(queries, q)
	}

	warmN := 2
	if v := os.Getenv("ZZ_WARMUP_QUERIES"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 {
			warmN = n
		}
	}
	if warmN > len(queries) {
		warmN = len(queries)
	}
	for i := 0; i < warmN; i++ {
		q := queries[i]
		_ = r.hybrid.FuseScores(q.Text, r.bm25Index.ScoreWithTokens(bm25.Tokenize(q.Text)), embTexts)
		if uic != nil {
			_ = uic.ClassifyContext(context.Background(), intent.MessageContext{Text: q.Text})
		}
		if toolHead != nil {
			if vec, err := r.hybrid.queryCache.Get(q.Text); err == nil {
				_ = zzToolHeadTop(toolHead, vec, 5)
			}
		}
	}
	runtime.GC()
	fmt.Fprintf(os.Stderr, "ZZ query warm done n=%d rss=%.0fMB\n", warmN, zzToolRSSMB())

	reuseNote := "double_embed: intent uses RoleClassification; Track A queryCache uses Embed() (RoleNone). No L2-vector reuse into tool router."
	for i, q := range queries {
		rec := map[string]any{
			"id":           q.ID,
			"mode":         mode,
			"vector_reuse": false,
			"reuse_note":   reuseNote,
			"warm":         i < warmN,
			"rss_mb":       zzToolRSSMB(),
		}

		switch mode {
		case "trackA":
			t0 := time.Now()
			scores := r.bm25Index.ScoreWithTokens(bm25.Tokenize(q.Text))
			bmHits := len(scores)
			tBM := time.Now()
			scores = r.hybrid.FuseScores(q.Text, scores, embTexts)
			norm := normalizeRetrievalScores(scores)
			ms := float64(time.Since(t0).Microseconds()) / 1000
			rec["bm25_hits"] = bmHits
			rec["bm25_ms"] = float64(tBM.Sub(t0).Microseconds()) / 1000
			rec["fuse_ms"] = float64(time.Since(tBM).Microseconds()) / 1000
			rec["ms"] = ms
			rec["tool_ms"] = ms
			rec["top"] = zzTopKV(norm, 20)

		case "toolhead":
			t0 := time.Now()
			vec, err := r.hybrid.queryCache.Get(q.Text)
			if err != nil {
				t.Fatal(err)
			}
			tEmb := time.Now()
			top := zzToolHeadTop(toolHead, vec, 20)
			ms := float64(time.Since(t0).Microseconds()) / 1000
			rec["embed_ms"] = float64(tEmb.Sub(t0).Microseconds()) / 1000
			rec["head_ms"] = float64(time.Since(tEmb).Microseconds()) / 1000
			rec["ms"] = ms
			rec["tool_ms"] = ms
			tops := make([]map[string]any, len(top))
			for j, x := range top {
				tops[j] = map[string]any{"n": x.N, "s": x.S}
			}
			rec["top"] = tops

		case "combined":
			// Production-shaped: classify intent, then Track A (re-embeds).
			// Optional Gemma tool-head inject on Track A query vector.
			t0 := time.Now()
			cres := uic.ClassifyContext(context.Background(), intent.MessageContext{Text: q.Text})
			intentMS := float64(time.Since(t0).Microseconds()) / 1000

			t1 := time.Now()
			scores := r.bm25Index.ScoreWithTokens(bm25.Tokenize(q.Text))
			bmHits := len(scores)
			scores = r.hybrid.FuseScores(q.Text, scores, embTexts)
			norm := normalizeRetrievalScores(scores)
			trackAMS := float64(time.Since(t1).Microseconds()) / 1000

			var headMS float64
			var headTop []map[string]any
			if toolHead != nil {
				t2 := time.Now()
				vec, err := r.hybrid.queryCache.Get(q.Text) // cache hit after FuseScores
				if err == nil {
					top := zzToolHeadTop(toolHead, vec, 5)
					headTop = make([]map[string]any, len(top))
					for j, x := range top {
						headTop[j] = map[string]any{"n": x.N, "s": x.S}
					}
				}
				headMS = float64(time.Since(t2).Microseconds()) / 1000
			}
			totalMS := float64(time.Since(t0).Microseconds()) / 1000
			rec["intent_ms"] = intentMS
			rec["tool_ms"] = trackAMS
			rec["trackA_ms"] = trackAMS
			rec["head_ms"] = headMS
			rec["ms"] = totalMS
			rec["cls_primary"] = string(cres.Primary)
			rec["cls_conf"] = cres.Confidence
			rec["cls_layer"] = cres.Layer
			rec["cls_degraded"] = cres.Degraded
			rec["cls_tools"] = cres.ToolNames
			rec["bm25_hits"] = bmHits
			rec["top"] = zzTopKV(norm, 20)
			if headTop != nil {
				rec["head_top"] = headTop
			}

		default:
			t.Fatalf("bad ZZ_MODE %s", mode)
		}

		_ = enc.Encode(rec)
		if i%50 == 0 {
			fmt.Fprintf(os.Stderr, "ZZ %s %d/%d rss=%.0fMB last_ms=%.1f\n",
				mode, i, len(queries), zzToolRSSMB(), rec["ms"])
		}
	}
	zzModelProbe(t, "end", emb)
	runtime.GC()
	fmt.Fprintf(os.Stderr, "ZZ %s done n=%d rss=%.0fMB vector_reuse=false\n", mode, len(queries), zzToolRSSMB())
}
