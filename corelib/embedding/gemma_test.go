package embedding

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func findModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no gemma embedding model found")
	return ""
}

func TestGemmaEmbedder_Load(t *testing.T) {
	modelPath := findModel(t)
	t.Logf("loading model: %s", modelPath)

	start := time.Now()
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}
	defer emb.Close()
	t.Logf("model loaded in %v, dim=%d", time.Since(start), emb.Dim())
}

func TestGemmaEmbedder_Embed(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	texts := []string{"hello world", "你好世界", "embedding test"}
	for _, text := range texts {
		start := time.Now()
		vec, err := emb.Embed(text)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Embed(%q) failed: %v", text, err)
		}
		if len(vec) != 256 {
			t.Fatalf("Embed(%q) returned %d dims, want 256", text, len(vec))
		}

		// Check L2 norm ~= 1.0
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		norm = math.Sqrt(norm)
		t.Logf("Embed(%q): dim=%d norm=%.4f time=%v first5=%v",
			text, len(vec), norm, elapsed, vec[:5])

		if math.Abs(norm-1.0) > 0.01 {
			t.Errorf("L2 norm = %f, want ~1.0", norm)
		}

		// Check not all zeros
		allZero := true
		for _, v := range vec {
			if v != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Error("all-zero vector")
		}
	}
}

func TestGemmaEmbedder_Similarity(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	pairs := [][2]string{
		{"hello world", "hi there"},
		{"hello world", "quantum physics"},
		{"cat", "dog"},
		{"cat", "airplane"},
	}

	for _, pair := range pairs {
		v1, err := emb.Embed(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		v2, err := emb.Embed(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		sim := cosine(v1, v2)
		t.Logf("cosine(%q, %q) = %.4f", pair[0], pair[1], sim)
	}
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestGemmaEmbedder_PrintVec(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	vec, err := emb.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}
	// Print first 10 values for comparison with C++ version
	fmt.Printf("Go embedding first 10 values for 'hello world':\n")
	for i := 0; i < 10 && i < len(vec); i++ {
		fmt.Printf("  [%d] = %.8f\n", i, vec[i])
	}
}

func findModelBench(b *testing.B) string {
	b.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	b.Skip("no gemma embedding model found")
	return ""
}

// BenchmarkEmbed_Short benchmarks single short text embedding.
func BenchmarkEmbed_Short(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.Embed("hello world")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer() // Close/unmap is not Embed
}

// BenchmarkEmbed_Medium benchmarks a medium-length text (~50 tokens).
func BenchmarkEmbed_Medium(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	text := "The quick brown fox jumps over the lazy dog. This is a medium length sentence that should produce around fifty tokens for benchmarking the embedding model inference performance."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.Embed(text)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer() // Close/unmap is not Embed
}

// BenchmarkEmbedBatch benchmarks batch embedding of 8 texts.
func BenchmarkEmbedBatch(b *testing.B) {
	modelPath := findModelBench(b)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		b.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	texts := []string{
		"hello world",
		"machine learning",
		"natural language processing",
		"deep neural networks",
		"transformer architecture",
		"attention mechanism",
		"embedding vectors",
		"semantic similarity",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := emb.EmbedBatch(texts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestEmbedBatchDoesNotMutateMatMulParallel(t *testing.T) {
	tensor.SetMatMulMaxParallel(7)
	defer tensor.SetMatMulMaxParallel(0)
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if _, err := emb.EmbedBatch([]string{"hello", "world", "test"}); err != nil {
		t.Fatal(err)
	}
	if tensor.MatMulMaxParallelForTest() != 7 {
		t.Fatalf("EmbedBatch mutated process-global cap to %d", tensor.MatMulMaxParallelForTest())
	}
}

func TestEmbedBatchMatchesSerialCosine(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	texts := []string{"hello world", "machine learning", "attention mechanism"}
	batched, err := emb.EmbedBatch(texts)
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		serial, err := emb.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		cos := cosine32(batched[i], serial)
		if cos < 0.999 {
			t.Fatalf("%q packed-vs-serial cosine=%g want >=0.999", text, cos)
		}
	}
}

func TestEmbedTokenStates_WidthIsModelDim(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	states, seq, dim, err := emb.EmbedTokenStates("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if dim != 768 {
		t.Fatalf("EmbedTokenStates dim=%d want model dim 768 (not MRL 256)", dim)
	}
	if seq <= 0 {
		t.Fatal("empty token sequence")
	}
	if len(states) != seq*dim {
		t.Fatalf("states len=%d want seq*dim=%d", len(states), seq*dim)
	}
	pooled, err := emb.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(pooled) != 256 {
		t.Fatalf("Embed dim=%d want 256", len(pooled))
	}
}

func TestFusionOffVsOnCosine(t *testing.T) {
	path := findModel(t)
	on, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer on.Close()
	off, err := NewGemmaEmbedder(path, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer off.Close()
	off.fusionOff = true
	raw, err := os.ReadFile(filepath.Join("testdata", "embed_gate_zh.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			texts = append(texts, line)
		}
	}
	if len(texts) == 0 {
		t.Fatal("empty gate corpus")
	}
	for _, text := range texts {
		a, err := on.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		bvec, err := off.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != 256 || len(bvec) != 256 {
			t.Fatalf("%q dim on=%d off=%d want 256", text, len(a), len(bvec))
		}
		var na, nb float64
		for i := range a {
			na += float64(a[i]) * float64(a[i])
			nb += float64(bvec[i]) * float64(bvec[i])
		}
		na, nb = math.Sqrt(na), math.Sqrt(nb)
		if math.Abs(na-1) > 1e-3 || math.Abs(nb-1) > 1e-3 {
			t.Fatalf("%q L2 on=%.6f off=%.6f want 1±1e-3", text, na, nb)
		}
		cos := cosine32(a, bvec)
		t.Logf("%q dim=%d L2_on=%.4f L2_off=%.4f cosine=%.6f", text, len(a), na, nb, cos)
		if cos < 0.999 {
			t.Fatalf("%q cosine=%g want >=0.999", text, cos)
		}
	}
}

func cosine32(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
