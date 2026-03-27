package embedding

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func findModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".maclaw", "models", "gemma-emb.gguf"),
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
