package guiapp

// app_local_intent.go — single shared embedder + local intent L3 wiring.
//
// Every guiapp embedder load goes through loadAppEmbedder (via
// sharedEmbeddingEmbedder, which already keeps one runtime per App), so the
// process holds exactly one model: Qwen3-Embedding-0.6B (there is no other
// local model and no fallback model — when Qwen3 is unavailable the host degrades to rules + BM25 / remote L3 while the
// background fetch and the factory's retry backoff run). The intent classifiers (desktop
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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/intent/locall3"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// headAttachLog suppresses repeated attach lines. Startup wires the same
// embedder from the sync path and again from activateEmbedderAsync while the
// head file is still downloading; without this the log shows the same failure
// twice before the file lands.
var headAttachLog = struct {
	sync.Mutex
	intent string
	tool   string
}{}

func logHeadAttach(slot *string, line string) {
	headAttachLog.Lock()
	defer headAttachLog.Unlock()
	if *slot == line {
		return
	}
	*slot = line
	log.Print(line)
}

// loadAppEmbedder is the only embedder loader guiapp uses. path is the
// model slot (embedding.DefaultModelPath()); the Qwen3 file is located with
// embedding.ResolveQwen3ModelPath (MACLAW_EMBED_MODEL_PATH, slot dir, models
// dir, exe dir).
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

// attachClassificationHeads binds both local heads. Safe to call more than
// once; a repeat with the same embedder does not reload the head file.
func (a *App) attachClassificationHeads(emb embedding.Embedder) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[intent] local L3 attach failed: panic %v", r)
		}
	}()
	a.attachLocalL3(emb)
	a.attachToolHead(emb)
}

// attachLocalL3 binds the head matching emb; call whenever the shared
// embedder is (re)wired into the classifiers.
func (a *App) attachLocalL3(emb embedding.Embedder) {
	if emb == nil || embedding.IsNoop(emb) {
		return
	}
	rt := a.localL3Runtime()
	if h, cur := rt.Active(); h != nil && cur == emb {
		log.Printf("[intent] local L3 attach skipped (already attached): head=%s", h.ID)
		return
	}
	if _, err := rt.Attach(emb); err != nil {
		// "no local head file" is the steady state until the release download
		// finishes. Strict mode still fails closed; otherwise say we are waiting
		// so a startup log is not a hard failure that later succeeds.
		if rt.Mode() != intent.LocalL3Strict && strings.Contains(err.Error(), "no local head file found") {
			logHeadAttach(&headAttachLog.intent, fmt.Sprintf("[intent] local L3 waiting for head file: %v", err))
			return
		}
		if rt.Mode() == intent.LocalL3Strict {
			logHeadAttach(&headAttachLog.intent, fmt.Sprintf("[intent] local L3 attach failed (strict: intent L3 fails closed): %v", err))
		} else {
			logHeadAttach(&headAttachLog.intent, fmt.Sprintf("[intent] local L3 attach failed; remote tree LLM stays L3: %v", err))
		}
		return
	}
	logHeadAttach(&headAttachLog.intent, fmt.Sprintf("[intent] %s", rt.Describe()))
}

// Tool-head env (first-party OvR injection into the tool router).
const (
	toolHeadEnv      = "MACLAW_TOOL_HEAD"     // path to tool_head_ovr_*.json; empty = auto
	toolHeadDisable  = "MACLAW_TOOL_HEAD_OFF" // 1/true → never load
	toolHeadsDirName = "tool_heads"           // default dir under models/
	// shippedToolHeadName is the tool head pinned in release_assets.json; it is
	// the only one auto-loaded (legacy tool heads are removed at startup).
	shippedToolHeadName = "tool_head_ovr_qwen3go_r5.json"
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
		for _, n := range []string{shippedToolHeadName} {
			p := filepath.Join(dir, n)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

// attachToolHead binds the first-party tool head that matches emb onto the
// tool router (intent scope ∪ head top-k ∪ Track A). No-op when the router or
// embedder is missing. A missing file or a head that refuses the embedder
// (wrong model id / dim) is logged; log_detail off still keeps that line.
func (a *App) attachToolHead(emb embedding.Embedder) {
	if a == nil {
		return
	}
	if a.toolRouter == nil || emb == nil || embedding.IsNoop(emb) {
		log.Printf("[tool] tool head attach deferred: router=%v embedder=%v (retried when the embedder is wired)", a.toolRouter != nil, emb != nil && !embedding.IsNoop(emb))
		return
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(toolHeadDisable))) {
	case "1", "true", "on", "yes":
		_ = a.toolRouter.SetToolHead(nil, nil, 0, 0)
		log.Printf("[tool] tool head disabled (%s=1)", toolHeadDisable)
		return
	}
	var last error
	cands := toolHeadCandidates(embedding.DefaultModelsDir())
	if cur := a.toolRouter.ToolHead(); cur != nil {
		for _, p := range cands {
			if filepath.Base(p) == shippedToolHeadName && cur.ID == pinnedHeadAssetID(shippedToolHeadName) {
				if err := cur.CheckEmbedderOf(emb); err == nil {
					log.Printf("[tool] tool head attach skipped (already attached): %s", cur.ID)
					return
				}
			}
		}
	}
	if len(cands) == 0 {
		last = fmt.Errorf("no %s under %s/{%s,%s,.}", shippedToolHeadName, embedding.DefaultModelsDir(), toolHeadsDirName, intent.LocalHeadsDirName)
	}
	for _, p := range cands {
		h, err := tool.LoadToolHead(p)
		if err != nil {
			last = err
			continue
		}
		if err := a.toolRouter.SetToolHead(h, emb, 0, 0); err != nil {
			last = err
			continue
		}
		logHeadAttach(&headAttachLog.tool, fmt.Sprintf("[tool] tool head attached: %s (θ=%.2f k=%d, embedder=%s)", h.ID, h.Theta, h.TopKDefault, embedding.EmbedderFamily(emb)))
		return
	}
	// Do not clear a head that a concurrent fetch callback just installed.
	// Stat runs outside the router lock, so SetToolHead(nil) here can land
	// after a successful attach and drop the live head.
	if last != nil {
		logHeadAttach(&headAttachLog.tool, fmt.Sprintf("[tool] tool head attach failed: %v", last))
		return
	}
	if a.toolRouter.ToolHead() != nil {
		return
	}
	logHeadAttach(&headAttachLog.tool, fmt.Sprintf("[tool] tool head waiting for %s", shippedToolHeadName))
}
