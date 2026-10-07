package tool

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func testToolHeadJSON(t *testing.T, mutate func(m map[string]any)) []byte {
	t.Helper()
	tools := []string{"bash", "read_file", "web_search"}
	m := map[string]any{
		"format":              ToolHeadFormat,
		"embedder_model_id":   "test-model:4",
		"embed_role":          "classification",
		"dims":                4,
		"l2norm":              true,
		"label_table_version": ToolLabelTableVersion(tools),
		"tools":               tools,
		"trained":             []bool{true, true, false},
		"W":                   [][]float64{{1, 0, 0, 0}, {0, 2, 0, 0}, {0, 0, 0, 0}},
		"b":                   []float64{0, -1, -20},
		"theta":               0.5,
		"top_k":               5,
	}
	if mutate != nil {
		mutate(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestToolHeadParsePredictTopK(t *testing.T) {
	h, err := ParseToolHead(testToolHeadJSON(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if h.Dim() != 4 || len(h.Tools) != 3 || h.Theta != 0.5 || h.TopKDefault != 5 {
		t.Fatalf("bad head: dim %d tools %v", h.Dim(), h.Tools)
	}
	// Unnormalised input: l2norm must make it equivalent to (1,1,0,0)/sqrt2.
	p, err := h.Predict([]float32{3, 3, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	x := 1 / math.Sqrt2
	want := []float64{1 / (1 + math.Exp(-x)), 1 / (1 + math.Exp(-(2*x - 1))), 0}
	for k := range want {
		if math.Abs(p[k]-want[k]) > 1e-6 {
			t.Fatalf("p[%d]=%v want %v", k, p[k], want[k])
		}
	}
	top := h.TopK(p, 5, 0)
	if len(top) != 2 || top[0].Tool != "bash" || top[1].Tool != "read_file" {
		t.Fatalf("TopK = %+v (untrained web_search must never appear)", top)
	}
	if got := h.TopK(p, 5, 0.65); len(got) != 1 || got[0].Tool != "bash" {
		t.Fatalf("TopK θ=0.65 = %+v", got)
	}
	if got := h.TopK(p, 1, 0); len(got) != 1 {
		t.Fatalf("TopK k=1 = %+v", got)
	}
}

func TestToolHeadRefusesMismatch(t *testing.T) {
	h, err := ParseToolHead(testToolHeadJSON(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.CheckEmbedder("test-model:4", 4); err != nil {
		t.Fatalf("matching embedder refused: %v", err)
	}
	if err := h.CheckEmbedder("test-model:4", 768); !errors.Is(err, ErrToolHeadMismatch) {
		t.Fatalf("dim mismatch not refused: %v", err)
	}
	if err := h.CheckEmbedder("EmbeddingGemma:768", 4); !errors.Is(err, ErrToolHeadMismatch) {
		t.Fatalf("model mismatch not refused: %v", err)
	}
	if err := h.CheckEmbedderOf(struct{}{}); !errors.Is(err, ErrToolHeadMismatch) {
		t.Fatalf("embedder without ModelID not refused: %v", err)
	}
	if err := h.CheckEmbedderOf(fakeIDEmbedder{"test-model:4", 4}); err != nil {
		t.Fatalf("CheckEmbedderOf matching: %v", err)
	}
	if err := h.CheckEmbedderOf(fakeIDEmbedder{"test-model:4", 1024}); !errors.Is(err, ErrToolHeadMismatch) {
		t.Fatalf("CheckEmbedderOf dim mismatch not refused: %v", err)
	}
	if _, err := h.Predict(make([]float32, 768)); !errors.Is(err, ErrToolHeadMismatch) {
		t.Fatalf("Predict with wrong dim: %v", err)
	}
}

type fakeIDEmbedder struct {
	id  string
	dim int
}

func (f fakeIDEmbedder) ModelID() string { return f.id }
func (f fakeIDEmbedder) Dim() int        { return f.dim }

func TestToolHeadParseRejectsBadFiles(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"format":        func(m map[string]any) { m["format"] = "v0" },
		"no model id":   func(m map[string]any) { m["embedder_model_id"] = "" },
		"dims":          func(m map[string]any) { m["dims"] = 5 },
		"zero dims":     func(m map[string]any) { m["dims"] = 0 },
		"rows":          func(m map[string]any) { m["b"] = []float64{0, 0} },
		"label version": func(m map[string]any) { m["label_table_version"] = "core3@000000000000" },
		"dup tool": func(m map[string]any) {
			tl := []string{"bash", "bash", "web_search"}
			m["tools"] = tl
			m["label_table_version"] = ToolLabelTableVersion(tl)
		},
		"trained len": func(m map[string]any) { m["trained"] = []bool{true} },
	}
	for name, mut := range cases {
		if _, err := ParseToolHead(testToolHeadJSON(t, mut)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// TestToolHeadRealFileParity loads the exported head (MACLAW_TOOL_HEAD) and
// checks Go probabilities against Python's on recorded vectors
// (MACLAW_TOOL_HEAD_PARITY: jsonl {"vec": [...], "p": [...]}) when set.
func TestToolHeadRealFileParity(t *testing.T) {
	path := os.Getenv("MACLAW_TOOL_HEAD")
	if path == "" {
		t.Skip("MACLAW_TOOL_HEAD not set")
	}
	h, err := LoadToolHead(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s model=%q dims=%d tools=%d labels=%s", h.ID, h.EmbedderModelID, h.Dim(), len(h.Tools), h.LabelTableVersion)
	if err := h.CheckEmbedder(h.EmbedderModelID, 768); err == nil {
		t.Fatal("768-d (Gemma) embedder must be refused")
	}
	pp := os.Getenv("MACLAW_TOOL_HEAD_PARITY")
	if pp == "" {
		return
	}
	f, err := os.Open(pp)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	n, maxd := 0, 0.0
	for sc.Scan() {
		var r struct {
			Vec []float32 `json:"vec"`
			P   []float64 `json:"p"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		p, err := h.Predict(r.Vec)
		if err != nil {
			t.Fatal(err)
		}
		for k := range p {
			if d := math.Abs(p[k] - r.P[k]); d > maxd {
				maxd = d
			}
		}
		n++
	}
	t.Logf("parity n=%d max|Δp|=%.2e", n, maxd)
	if n == 0 || maxd > 1e-4 {
		t.Fatalf("parity failed: n=%d max|Δp|=%g", n, maxd)
	}
}

// TestToolHeadQwen3EmbedderBind checks the exported head binds to the real
// Qwen3 embedder (MACLAW_QWEN3_MODEL) and scores a query embedded with the
// intent L2/L3 role.
func TestToolHeadQwen3EmbedderBind(t *testing.T) {
	path, model := os.Getenv("MACLAW_TOOL_HEAD"), os.Getenv("MACLAW_QWEN3_MODEL")
	if path == "" || model == "" {
		t.Skip("MACLAW_TOOL_HEAD / MACLAW_QWEN3_MODEL not set")
	}
	h, err := LoadToolHead(path)
	if err != nil {
		t.Fatal(err)
	}
	q, err := embedding.NewQwen3Embedder(model, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err := h.CheckEmbedderOf(q); err != nil {
		t.Fatalf("exported head refused by the real Qwen3 embedder: %v", err)
	}
	v, err := q.EmbedWithRole("帮我搜一下今天上海的天气", embedding.RoleClassification)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Predict(v)
	if err != nil {
		t.Fatal(err)
	}
	top := h.TopK(p, 3, 0)
	t.Logf("model=%s top3=%+v", q.ModelID(), top)
	if len(top) == 0 || (top[0].Tool != "web_search" && top[0].Tool != "web_fetch") {
		t.Fatalf("weather query top tool = %+v, want web_search/web_fetch", top)
	}
}

func BenchmarkToolHeadPredict49x1024(b *testing.B) {
	tools := make([]string, 49)
	W := make([][]float64, 49)
	bias := make([]float64, 49)
	for k := range tools {
		tools[k] = "t" + strings.Repeat("x", k)
		W[k] = make([]float64, 1024)
		for i := range W[k] {
			W[k][i] = math.Sin(float64(k*1024 + i))
		}
	}
	js, _ := json.Marshal(map[string]any{"format": ToolHeadFormat, "embedder_model_id": "m", "dims": 1024, "l2norm": true,
		"label_table_version": ToolLabelTableVersion(tools), "tools": tools, "W": W, "b": bias})
	h, err := ParseToolHead(js)
	if err != nil {
		b.Fatal(err)
	}
	v := make([]float32, 1024)
	for i := range v {
		v[i] = float32(math.Cos(float64(i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, _ := h.Predict(v)
		_ = h.TopK(p, 5, 0.5)
	}
}
