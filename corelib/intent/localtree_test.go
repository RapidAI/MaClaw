package intent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type stubEmbedder struct{ dim int }

func (s stubEmbedder) Embed(string) ([]float32, error) {
	v := make([]float32, s.dim)
	v[0] = 1
	return v, nil
}
func (s stubEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i], _ = s.Embed(texts[i])
	}
	return out, nil
}
func (s stubEmbedder) Dim() int     { return s.dim }
func (s stubEmbedder) Close() {}

func TestLocalTreeFuncNilHead(t *testing.T) {
	fn := LocalTreeFunc(nil, nil, nil)
	_, err := fn(context.Background(), context.Background(), "", "x")
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("want not-loaded error, got %v", err)
	}
}

func TestLocalTreeFuncProducesTreeJSONOrUncertain(t *testing.T) {
	h, err := ParseLocalHead(tinyHeadJSON(t, []string{"search", "ssh", "unknown"}, 4, 3, 0.5, 0.9, []string{"ssh"}))
	if err != nil {
		t.Fatal(err)
	}
	emb := stubEmbedder{dim: h.Dim()}
	fn := LocalTreeFunc(emb, h, nil)
	out, err := fn(context.Background(), context.Background(), "", "hello")
	if err != nil {
		if !strings.Contains(err.Error(), "local head uncertain") {
			t.Fatalf("unexpected: %v", err)
		}
		return
	}
	var parsed struct {
		Top []struct {
			Skill string  `json:"skill"`
			Score float64 `json:"score"`
		} `json:"top"`
	}
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil || len(parsed.Top) == 0 {
		t.Fatalf("bad tree json %q: %v", out, jerr)
	}
}

func TestLocalTreeFuncFallback(t *testing.T) {
	// High τ so the tiny head almost always abstains → fallback runs.
	h, err := ParseLocalHead(tinyHeadJSON(t, []string{"search", "ssh", "unknown"}, 4, 3, 0.99, 0.99, []string{"ssh"}))
	if err != nil {
		t.Fatal(err)
	}
	emb := stubEmbedder{dim: h.Dim()}
	saw := false
	fn := LocalTreeFunc(emb, h, func(ctx context.Context, text string, cands []string) (string, error) {
		saw = true
		return "search", nil
	})
	out, err := fn(context.Background(), context.Background(), "", "hello")
	if !saw {
		t.Fatal("fallback not called")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"skill":"search"`) {
		t.Fatalf("got %s", out)
	}
}
