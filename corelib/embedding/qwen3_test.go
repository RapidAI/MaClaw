package embedding

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qwen3TestModel returns the Qwen3-Embedding GGUF path or skips.
// MACLAW_QWEN3_MODEL overrides; default is <models dir>/Qwen3-Embedding-0.6B-Q8_0.gguf.
func qwen3TestModel(t testing.TB) string {
	t.Helper()
	p := os.Getenv("MACLAW_QWEN3_MODEL")
	if p == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, ".maclaw", "models", Qwen3ModelFilename)
		}
	}
	if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
		t.Skipf("Qwen3 embedding model not found (%s)", p)
	}
	return p
}

var qwen3Shared *Qwen3Embedder

func loadQwen3(t testing.TB) *Qwen3Embedder {
	t.Helper()
	if qwen3Shared != nil {
		return qwen3Shared
	}
	e, err := NewQwen3Embedder(qwen3TestModel(t), 0)
	if err != nil {
		t.Fatalf("NewQwen3Embedder: %v", err)
	}
	qwen3Shared = e
	return e
}

func TestQwen3PreTokenize(t *testing.T) {
	cases := map[string][]string{
		"Hello world":            {"Hello", " world"},
		"I'll go, won't I?":      {"I", "'ll", " go", ",", " won", "'t", " I", "?"},
		"price 1234.5元":          {"price", " ", "1", "2", "3", "4", ".", "5", "元"},
		"a  b":                   {"a", " ", " b"},
		"x\n\n  y":               {"x", "\n\n", " ", " y"},
		"end   ":                 {"end", "   "},
		"打开报告.docx":              {"打开报告", ".docx"},
		" ?!\n":                  {" ?!\n"},
		"tab\there":              {"tab", "\there"},
	}
	for in, want := range cases {
		got := qwen2PreTokenize(in)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("qwen2PreTokenize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQwen3PromptFormat(t *testing.T) {
	prev := PromptsEnabled()
	defer SetPromptsEnabled(prev)
	SetPromptsEnabled(true)
	if got := ApplyQwen3Prompt("打开计算器", RoleClassification); got != "Instruct: Classify the intent of this assistant user request\nQuery: 打开计算器" {
		t.Fatalf("classification prompt = %q", got)
	}
	if got := ApplyQwen3Prompt("doc body", RoleDocument); got != "doc body" {
		t.Fatalf("documents must not get an instruction, got %q", got)
	}
	if got := ApplyQwen3Prompt("q", RoleNone); got != "q" {
		t.Fatalf("RoleNone = %q", got)
	}
	SetPromptsEnabled(false)
	if got := ApplyQwen3Prompt("q", RoleQuery); got != "q" {
		t.Fatalf("prompts disabled = %q", got)
	}
}

// Token ids checked against the HuggingFace tokenizer of Qwen/Qwen3-Embedding-0.6B.
func TestQwen3TokenizerMatchesHF(t *testing.T) {
	e := loadQwen3(t)
	cases := map[string][]int{
		"Hello world": {9707, 1879, 151643},
	}
	for in, want := range cases {
		got := e.Tokenize(in)
		if len(got) != len(want) {
			t.Fatalf("Tokenize(%q) = %v, want %v", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("Tokenize(%q) = %v, want %v", in, got, want)
			}
		}
	}
}

func TestQwen3EmbedBasics(t *testing.T) {
	e := loadQwen3(t)
	if e.Dim() != 1024 {
		t.Fatalf("Dim = %d", e.Dim())
	}
	if !strings.Contains(e.ModelID(), "qwen3-instruct-v1") || !strings.Contains(e.ModelID(), ":1024:") {
		t.Fatalf("ModelID = %q", e.ModelID())
	}
	a, err := e.EmbedWithRole("帮我打开计算器", RoleClassification)
	if err != nil {
		t.Fatal(err)
	}
	var n float64
	for _, v := range a {
		n += float64(v) * float64(v)
	}
	if math.Abs(n-1) > 1e-3 {
		t.Fatalf("not normalised: %v", n)
	}
	b, _ := e.EmbedWithRole("launch the calculator app", RoleClassification)
	c, _ := e.EmbedWithRole("今天天气怎么样", RoleClassification)
	if dot(a, b) <= dot(a, c) {
		t.Fatalf("semantic order wrong: calc-calc %.3f <= calc-weather %.3f", dot(a, b), dot(a, c))
	}
	// Batch (single-threaded kernels, pooled scratch) must agree with Embed.
	vs, err := e.EmbedBatchWithRole([]string{"帮我打开计算器", "launch the calculator app", "今天天气怎么样"}, RoleClassification)
	if err != nil {
		t.Fatal(err)
	}
	if d := dot(vs[0], a); d < 0.9999 {
		t.Fatalf("batch vs single cos = %.6f", d)
	}
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func BenchmarkQwen3EmbedShort(b *testing.B) {
	e := loadQwen3(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.EmbedWithRole("帮我把这个文件发给张三", RoleClassification)
	}
}

func BenchmarkQwen3EmbedShortSerial(b *testing.B) {
	e := loadQwen3(b)
	txt := ApplyQwen3Prompt("帮我把这个文件发给张三", RoleClassification)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.EmbedConcurrent(txt)
	}
}
