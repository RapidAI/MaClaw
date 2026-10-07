package guiapp

// app_local_intent.go — single shared embedder + local intent L3 wiring.
//
// Every guiapp embedder load goes through loadAppEmbedder (via
// sharedEmbeddingEmbedder, which already keeps one runtime per App), so the
// process holds exactly one model: Qwen3-Embedding-0.6B when it loads, else
// EmbeddingGemma (only when Qwen3 fails). The intent classifiers (desktop
// UIC, per-device hardware UICs), tool router (Track A hybrid retrieval),
// tool builder, memory/knowledge stores and interrupt relevance all receive
// that one instance.
//
// Layer 3 of every UIC is a.intentL3ContextFunc(): the local MLP head that
// matches the live embedder (intent.LocalTreeFunc), selected by family + dim
// from MACLAW_INTENT_LOCAL_HEAD or <models>/intent_heads/. Rollback:
// MACLAW_INTENT_LOCAL_L3=0 restores the remote tree LLM; "strict" never
// calls the remote LLM even before the head is attached.

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/intent/locall3"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// loadAppEmbedder is the only embedder loader guiapp uses. path is the
// Gemma model slot (embedding.DefaultModelPath() / the download target);
// the Qwen3 file next to it (or MACLAW_EMBED_MODEL_PATH) is tried first.
func loadAppEmbedder(smoke bool) func(string) (embedding.Embedder, error) {
	return func(path string) (embedding.Embedder, error) {
		res, err := locall3.LoadEmbedder(path, smoke)
		if err != nil {
			log.Printf("[embedding] load failed: %v (qwen3: %v)", err, res.QwenErr)
			if !smoke {
				// Same contract as the NewDefaultEmbedder loader this
				// replaces: an unloadable file is "no model" (noop), which
				// lets the caller fall back to the download/verify path.
				return embedding.NoopEmbedder{}, nil
			}
			return nil, err
		}
		log.Printf("[embedding] shared embedder: %s", res)
		return res.Embedder, nil
	}
}

// localL3Runtime returns the App's local-L3 runtime (created once).
func (a *App) localL3Runtime() *locall3.Runtime {
	a.localL3Once.Do(func() {
		a.localL3 = locall3.New(locall3.DefaultHeadDir(embedding.DefaultModelsDir()))
		log.Printf("[intent] %s", a.localL3.Describe())
	})
	return a.localL3
}

// intentL3ContextFunc is the Layer 3 callback for every guiapp UIC.
func (a *App) intentL3ContextFunc() intent.LLMClassifyContextFunc {
	return a.localL3Runtime().ContextFunc(a.buildUICLLMContextFunc())
}

// intentL3Func is the non-context Layer 3 callback (nil in strict mode).
func (a *App) intentL3Func() intent.LLMClassifyFunc {
	return a.localL3Runtime().LLMFunc(a.buildUICLLMFunc())
}

// attachLocalL3 binds the head matching emb; call whenever the shared
// embedder is (re)wired into the classifiers.
func (a *App) attachLocalL3(emb embedding.Embedder) {
	if emb == nil || embedding.IsNoop(emb) {
		return
	}
	rt := a.localL3Runtime()
	if _, err := rt.Attach(emb); err != nil {
		if rt.Mode() == intent.LocalL3Strict {
			log.Printf("[intent] local L3 unavailable (strict: intent L3 fails closed): %v", err)
		} else {
			log.Printf("[intent] local L3 unavailable, remote tree LLM stays L3: %v", err)
		}
		return
	}
	log.Printf("[intent] %s", rt.Describe())
}

// Tool-head env (first-party OvR injection into the tool router).
const (
	toolHeadEnv      = "MACLAW_TOOL_HEAD"     // path to tool_head_ovr_*.json; empty = auto
	toolHeadDisable  = "MACLAW_TOOL_HEAD_OFF" // 1/true → never load
	toolHeadsDirName = "tool_heads"           // default dir under models/
)

func toolHeadCandidates(modelsDir string) []string {
	if v := strings.TrimSpace(os.Getenv(toolHeadEnv)); v != "" {
		return []string{v}
	}
	var out []string
	for _, dir := range []string{
		filepath.Join(modelsDir, toolHeadsDirName),
		filepath.Join(modelsDir, intent.LocalHeadsDirName),
		modelsDir,
	} {
		for _, n := range []string{"tool_head_ovr_qwen3go.json", "tool_head_ovr_qwen3.json", "tool_head_ovr_gemma.json"} {
			p := filepath.Join(dir, n)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

// attachToolHead binds the first-party tool head that matches emb onto the
// tool router (intent scope ∪ head top-k ∪ Track A). No-op when disabled,
// no matching file, or the head refuses the embedder (wrong model id / dim).
func (a *App) attachToolHead(emb embedding.Embedder) {
	if a == nil || a.toolRouter == nil || emb == nil || embedding.IsNoop(emb) {
		return
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(toolHeadDisable))) {
	case "1", "true", "on", "yes":
		_ = a.toolRouter.SetToolHead(nil, nil, 0, 0)
		log.Printf("[tool] tool head disabled (%s=1)", toolHeadDisable)
		return
	}
	var last error
	for _, p := range toolHeadCandidates(embedding.DefaultModelsDir()) {
		h, err := tool.LoadToolHead(p)
		if err != nil {
			last = err
			continue
		}
		if err := a.toolRouter.SetToolHead(h, emb, 0, 0); err != nil {
			last = err
			continue
		}
		log.Printf("[tool] tool head attached: %s (θ=%.2f k=%d, embedder=%s)", h.ID, h.Theta, h.TopKDefault, embedding.EmbedderFamily(emb))
		return
	}
	_ = a.toolRouter.SetToolHead(nil, nil, 0, 0)
	if last != nil {
		log.Printf("[tool] tool head not attached: %v", last)
	}
}
