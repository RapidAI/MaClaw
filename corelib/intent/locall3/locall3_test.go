package locall3

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

type fakeEmb struct {
	dim int
	id  string
}

func (f fakeEmb) Embed(string) ([]float32, error) {
	v := make([]float32, f.dim)
	v[0] = 1
	return v, nil
}
func (f fakeEmb) EmbedBatch(t []string) ([][]float32, error) {
	out := make([][]float32, len(t))
	for i := range t {
		out[i], _ = f.Embed(t[i])
	}
	return out, nil
}
func (f fakeEmb) Dim() int        { return f.dim }
func (f fakeEmb) Close()          {}
func (f fakeEmb) ModelID() string { return f.id }

// writeHead writes a linear head over dim inputs whose top-1 for the e0
// vector is "file_ops" with p≈0.97.
func writeHead(t *testing.T, dir, name, model string, dim int, extra map[string]any) string {
	t.Helper()
	labels := []string{"file_ops", "web_search", "unknown"}
	w2 := make([][]float64, len(labels))
	for i := range w2 {
		w2[i] = make([]float64, dim)
	}
	w2[0][0] = 4
	m := map[string]any{"labels": labels, "w2": w2, "b2": []float64{0, 0, 0},
		"temperature": 1.0, "tau": 0.8, "tau_s": 0.99, "l2norm": true,
		"sensitive": []string{"web_search"}, "embed_role": "classification"}
	if model != "" {
		m["embed_model"] = model
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

var qwen = fakeEmb{dim: 1024, id: "Qwen3-Embedding-0.6B:1024:x:y"}
var gemma = fakeEmb{dim: 768, id: "embeddinggemma-300m:768:x"}

func remoteSpy(calls *int) intent.LLMClassifyContextFunc {
	return func(context.Context, context.Context, string, string) (string, error) {
		*calls++
		return `{"top":[{"skill":"web_search","score":0.9}]}`, nil
	}
}

func TestAttachPicksQwen3HeadAndRunsLocally(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.LocalTauSEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, "head_mlp_gemmacls_GK.json", "", 768, nil)
	writeHead(t, dir, "head_mlp_qwen3go_GK.json", "", 1024, nil)
	rt := NewWithMode(dir, intent.LocalL3On)
	h, err := rt.Attach(qwen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h.ID, "head_mlp_qwen3go_GK.json@") || h.TauS != intent.DefaultLocalTauS {
		t.Fatalf("head %s tau_s %.2f", h.ID, h.TauS)
	}
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	out, err := fn(context.Background(), context.Background(), "sys", "open the file")
	if err != nil || calls != 0 || !strings.Contains(out, "file_ops") {
		t.Fatalf("out=%s err=%v remote calls=%d", out, err, calls)
	}
}

func TestAttachPrefersXFHead(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, "head_mlp_qwen3go_GK.json", "", 1024, nil)
	writeHead(t, dir, "head_mlp_qwen3go_xf.json", "qwen3", 1024, nil)
	h, err := NewWithMode(dir, intent.LocalL3On).Attach(qwen)
	if err != nil || !strings.HasPrefix(h.ID, "head_mlp_qwen3go_xf.json@") {
		t.Fatalf("h=%v err=%v", h, err)
	}
}

func TestGemmaFallbackUsesGemmaHead(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, "head_mlp_gemmacls_GK.json", "", 768, nil)
	writeHead(t, dir, "head_mlp_qwen3go_GK.json", "", 1024, nil)
	h, err := NewWithMode(dir, intent.LocalL3On).Attach(gemma)
	if err != nil || !strings.HasPrefix(h.ID, "head_mlp_gemmacls_GK.json@") || h.TauS != 0.95 {
		t.Fatalf("h=%v err=%v", h, err)
	}
}

func TestMismatchRefused(t *testing.T) {
	dir := t.TempDir()
	// Only a Gemma head available for a Qwen3 embedder.
	g := writeHead(t, dir, "head_mlp_gemmacls_GK.json", "", 768, nil)
	// A head that claims qwen3 but has the Gemma width.
	bad := writeHead(t, dir, "head_wrongdim.json", "qwen3", 768, nil)
	// A head with the right width but trained on the other family.
	other := writeHead(t, dir, "head_other.json", "gemma", 1024, nil)
	t.Setenv(intent.LocalHeadEnv, strings.Join([]string{g, bad, other}, ","))
	rt := NewWithMode("", intent.LocalL3On)
	if _, err := rt.Attach(qwen); !errors.Is(err, intent.ErrHeadMismatch) {
		t.Fatalf("want ErrHeadMismatch, got %v", err)
	}
	if h, _ := rt.Active(); h != nil {
		t.Fatal("mismatched head must not be active")
	}
	// on → remote stays L3; strict → never remote.
	calls := 0
	if _, err := rt.ContextFunc(remoteSpy(&calls))(context.Background(), nil, "", "x"); err != nil || calls != 1 {
		t.Fatalf("on-mode should use remote: err=%v calls=%d", err, calls)
	}
	st := NewWithMode("", intent.LocalL3Strict)
	st.Attach(qwen)
	if _, err := st.ContextFunc(remoteSpy(&calls))(context.Background(), nil, "", "x"); !errors.Is(err, ErrNotReady) || calls != 1 {
		t.Fatalf("strict: err=%v calls=%d", err, calls)
	}
	if st.LLMFunc(func(string, string) (string, error) { return "", nil }) != nil {
		t.Fatal("strict mode must drop the remote LLMFunc")
	}
}

func TestRollbackOffUsesRemote(t *testing.T) {
	t.Setenv(intent.LocalL3Env, "0")
	dir := t.TempDir()
	writeHead(t, dir, "head_mlp_qwen3go_GK.json", "", 1024, nil)
	rt := New(dir)
	if rt.Mode() != intent.LocalL3Off {
		t.Fatalf("mode %s", rt.Mode())
	}
	if h, err := rt.Attach(qwen); h != nil || err != nil {
		t.Fatal("off: attach is a no-op")
	}
	calls := 0
	rt.ContextFunc(remoteSpy(&calls))(context.Background(), nil, "", "x")
	if calls != 1 {
		t.Fatal("off must call the remote tree LLM")
	}
}

func TestHumanCalibratedTauSKept(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.LocalTauSEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, "head_mlp_qwen3go_GK.json", "", 1024, map[string]any{
		"tau_s": 0.97, "calibration": map[string]any{"source": "human_labels"}})
	h, err := NewWithMode(dir, intent.LocalL3On).Attach(qwen)
	if err != nil || h.TauS != 0.97 {
		t.Fatalf("h=%v err=%v", h, err)
	}
	t.Setenv(intent.LocalTauSEnv, "0.99")
	h2, _ := NewWithMode(dir, intent.LocalL3On).Attach(qwen)
	if h2.TauS != 0.99 {
		t.Fatalf("env override: %v", h2.TauS)
	}
}

func TestEmbedderFamily(t *testing.T) {
	if embedding.EmbedderFamily(qwen) != embedding.FamilyQwen3 || embedding.EmbedderFamily(gemma) != embedding.FamilyGemma {
		t.Fatal("family by ModelID")
	}
	if embedding.EmbedderFamily(embedding.NoopEmbedder{}) != "" {
		t.Fatal("noop")
	}
}
