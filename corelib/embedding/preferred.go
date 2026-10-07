package embedding

// preferred.go — the single-model embedder factory for hosts (guiapp).
//
// Product rule (2026-10-07): ship ONE embedding model. Qwen3-Embedding-0.6B is
// tried first; EmbeddingGemma is loaded only if Qwen3 fails to load (file
// missing, not a qwen3 GGUF, load error, or smoke embed failure). On the Qwen3
// success path the Gemma file is never opened, so a process can never hold
// both models through this factory.
//
// Environment (read on every call):
//
//	MACLAW_EMBED_MODEL       unset / "auto" / "qwen3"  → Qwen3 first, Gemma fallback
//	                         "gemma"                    → Gemma only (explicit pin / rollback)
//	MACLAW_EMBED_MODEL_PATH  Qwen3 GGUF path (default: Qwen3ModelFilename next to the Gemma file)

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Model families returned by PreferredLoad.Family / EmbedderFamily.
const (
	FamilyQwen3 = "qwen3"
	FamilyGemma = "gemma"
)

// PreferredLoad reports which single model LoadPreferredEmbedder loaded and why.
type PreferredLoad struct {
	Embedder  Embedder // never nil; NoopEmbedder when nothing could be loaded
	Family    string   // FamilyQwen3, FamilyGemma or "" (noop)
	Path      string   // file the embedder was loaded from
	QwenPath  string   // Qwen3 candidate path ("" when pinned to Gemma)
	QwenErr   error    // why Qwen3 was not used (nil when it was, or when pinned to Gemma)
	GemmaPath string
	Fallback  bool // Qwen3 was attempted and failed, Gemma was then tried
}

// String is a one-line description for startup logs.
func (p PreferredLoad) String() string {
	switch {
	case p.Family == FamilyQwen3:
		return fmt.Sprintf("qwen3 %s (dim=%d)", p.Path, p.Embedder.Dim())
	case p.Family == FamilyGemma && p.Fallback:
		return fmt.Sprintf("gemma %s (dim=%d) — fallback, qwen3 unavailable: %v", p.Path, p.Embedder.Dim(), p.QwenErr)
	case p.Family == FamilyGemma:
		return fmt.Sprintf("gemma %s (dim=%d) — pinned by %s=gemma", p.Path, p.Embedder.Dim(), EmbedModelEnv)
	default:
		return fmt.Sprintf("noop (qwen3: %v; gemma %s missing)", p.QwenErr, p.GemmaPath)
	}
}

// PreferredOptions tunes LoadPreferredEmbedder.
type PreferredOptions struct {
	// SmokeTest embeds "test" after loading; a failure counts as a load
	// failure (Qwen3 → fall back to Gemma; Gemma → error).
	SmokeTest bool
	// GemmaDim is the Gemma output width (<=0 → DefaultEmbeddingDim).
	GemmaDim int
}

// modelLoaders is swappable in tests so the factory can be exercised without
// real GGUFs and the "Gemma never opened" property can be asserted.
type modelLoaders struct {
	qwen3 func(path string) (Embedder, error)
	gemma func(path string, dim int) (Embedder, error)
}

var preferredLoaders = modelLoaders{
	qwen3: func(path string) (Embedder, error) {
		arch, err := gguf.PeekArchitecture(path)
		if err != nil {
			return nil, err
		}
		if arch != "qwen3" {
			return nil, fmt.Errorf("unsupported architecture %q (want qwen3)", arch)
		}
		e, err := NewQwen3Embedder(path, 0)
		if err != nil {
			return nil, err
		}
		return e, nil
	},
	gemma: func(path string, dim int) (Embedder, error) {
		e, err := NewGemmaEmbedder(path, dim)
		if err != nil {
			return nil, err
		}
		return e, nil
	},
}

// PreferredQwen3Path returns the Qwen3 GGUF candidate for a Gemma model path:
// MACLAW_EMBED_MODEL_PATH if set, else Qwen3ModelFilename in the same dir.
func PreferredQwen3Path(gemmaPath string) string {
	if p := strings.TrimSpace(os.Getenv(EmbedModelPathEnv)); p != "" {
		return p
	}
	if strings.TrimSpace(gemmaPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(gemmaPath), Qwen3ModelFilename)
}

// GemmaPinned reports whether MACLAW_EMBED_MODEL explicitly pins Gemma.
func GemmaPinned() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(EmbedModelEnv)), "gemma")
}

// PreferredModelFileExists reports whether any model file the factory would
// load is present (used by hosts to decide whether a download is needed).
func PreferredModelFileExists(gemmaPath string) bool {
	if !GemmaPinned() && regularNonEmpty(PreferredQwen3Path(gemmaPath)) {
		return true
	}
	return regularNonEmpty(gemmaPath)
}

func regularNonEmpty(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// LoadPreferredEmbedder loads exactly one embedding model: Qwen3 first, Gemma
// only when Qwen3 fails (see file comment). gemmaPath is the host's Gemma
// model slot (embedding.DefaultModelPath()). When neither file exists the
// result is a NoopEmbedder with a nil error (callers treat that as "download
// needed", matching NewDefaultEmbedder). A non-nil error means the Gemma
// fallback file existed but failed to load.
func LoadPreferredEmbedder(gemmaPath string, opt PreferredOptions) (PreferredLoad, error) {
	return loadPreferredWith(preferredLoaders, gemmaPath, opt)
}

func loadPreferredWith(ld modelLoaders, gemmaPath string, opt PreferredOptions) (PreferredLoad, error) {
	res := PreferredLoad{Embedder: NoopEmbedder{}, GemmaPath: gemmaPath}
	dim := opt.GemmaDim
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	if !GemmaPinned() {
		res.QwenPath = PreferredQwen3Path(gemmaPath)
		emb, err := tryLoad(res.QwenPath, func(p string) (Embedder, error) { return ld.qwen3(p) }, opt.SmokeTest)
		if err == nil {
			res.Embedder, res.Family, res.Path = emb, FamilyQwen3, res.QwenPath
			return res, nil
		}
		res.QwenErr = err
		res.Fallback = true
	}
	if !regularNonEmpty(gemmaPath) {
		return res, nil
	}
	emb, err := tryLoad(gemmaPath, func(p string) (Embedder, error) { return ld.gemma(p, dim) }, opt.SmokeTest)
	if err != nil {
		return res, fmt.Errorf("gemma %s: %w", gemmaPath, err)
	}
	res.Embedder, res.Family, res.Path = emb, FamilyGemma, gemmaPath
	return res, nil
}

func tryLoad(path string, load func(string) (Embedder, error), smoke bool) (emb Embedder, err error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("no model path")
	}
	if !regularNonEmpty(path) {
		return nil, fmt.Errorf("model file missing: %s", path)
	}
	defer func() {
		if r := recover(); r != nil {
			if emb != nil {
				emb.Close()
			}
			emb, err = nil, fmt.Errorf("load panic: %v", r)
		}
	}()
	emb, err = load(path)
	if err != nil {
		return nil, err
	}
	if emb == nil || IsNoop(emb) {
		return nil, fmt.Errorf("loader returned no embedder")
	}
	if smoke {
		vec, serr := emb.Embed("test")
		if serr != nil || len(vec) == 0 {
			emb.Close()
			return nil, fmt.Errorf("smoke test failed: err=%v len=%d", serr, len(vec))
		}
	}
	return emb, nil
}

// EmbedderFamily classifies a live embedder: by concrete type, then by
// ModelID, then by width (1024 → qwen3, 768 → gemma). "" when unknown.
func EmbedderFamily(e Embedder) string {
	switch e.(type) {
	case *Qwen3Embedder:
		return FamilyQwen3
	case *GemmaEmbedder:
		return FamilyGemma
	}
	if e == nil || IsNoop(e) {
		return ""
	}
	if m, ok := e.(interface{ ModelID() string }); ok {
		id := strings.ToLower(m.ModelID())
		switch {
		case strings.Contains(id, "qwen3"):
			return FamilyQwen3
		case strings.Contains(id, "gemma"):
			return FamilyGemma
		}
	}
	switch e.Dim() {
	case 1024:
		return FamilyQwen3
	case 768:
		return FamilyGemma
	}
	return ""
}

// ComputeMode names the numeric path a Qwen3 embedder runs on, e.g.
// "q8p:avx512:2pass", "q8p:avx2:1pass", "q8p:sdot:1pass", "q8p:avx512:2pass(qkv,o)"
// or "float". Probabilities near τ can flip between modes (avx2 single-pass
// vs two-pass changed 4/150 primaries), so shadow logs record it and heads
// recalibrated from those logs carry it (calibration.compute).
func (q *Qwen3Embedder) ComputeMode() string {
	if q == nil || !q.q8p {
		return "float"
	}
	t := qwen3Q8PTwoPass
	pass := "1pass"
	switch {
	case t.qkv && t.o && t.gu && t.down:
		pass = "2pass"
	case t.qkv || t.o || t.gu || t.down:
		var parts []string
		for _, p := range []struct {
			on   bool
			name string
		}{{t.qkv, "qkv"}, {t.o, "o"}, {t.gu, "gu"}, {t.down, "down"}} {
			if p.on {
				parts = append(parts, p.name)
			}
		}
		pass = "2pass(" + strings.Join(parts, ",") + ")"
	}
	return "q8p:" + tensor.Q8PISAName() + ":" + pass
}

// EmbedderComputeMode returns e's ComputeMode when it has one, else its family
// ("gemma"), else "".
func EmbedderComputeMode(e Embedder) string {
	if m, ok := e.(interface{ ComputeMode() string }); ok && m != nil {
		return m.ComputeMode()
	}
	return EmbedderFamily(e)
}
