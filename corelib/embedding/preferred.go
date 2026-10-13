package embedding

// preferred.go — the single-model embedder factory for hosts (guiapp).
//
// Product rule (2026-10-07): ship ONE embedding model, Qwen3-Embedding-0.6B.
// There is no automatic Gemma fallback: when Qwen3 is missing, unloadable,
// refused by the low-memory guard or backing off after a failure, the result
// is a NoopEmbedder and hosts degrade to rules + BM25 (+ remote L3) while the
// background fetch / retry runs. The EmbeddingGemma engine code stays in the
// package (gemma*.go) but nothing in this factory loads it.
//
// Environment (read on every call):
//
//	MACLAW_EMBED_MODEL       ignored: Qwen3 is the only model (any value, including "gemma")
//	MACLAW_EMBED_MODEL_PATH  Qwen3 GGUF file or directory (see ResolveQwen3ModelPath)
//	MACLAW_EMBED_MEM_GUARD   "0" disables the low-memory guard (CheckModelMemory)

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
	Embedder Embedder // never nil; NoopEmbedder when nothing could be loaded
	Family   string   // FamilyQwen3 or "" (noop)
	Path     string   // file the embedder was loaded from
	QwenPath string   // Qwen3 candidate path
	QwenErr  error    // why Qwen3 was not used (nil when it was)
	// Tried lists the Qwen3 locations considered (path resolution log).
	Tried []Qwen3PathCandidate
}

// String is a one-line description for startup logs.
func (p PreferredLoad) String() string {
	switch {
	case p.Family == FamilyQwen3:
		return fmt.Sprintf("qwen3 %s (dim=%d)", p.Path, p.Embedder.Dim())
	default:
		return fmt.Sprintf("noop — local embedder unavailable, degraded to rules+BM25 (qwen3: %v)", p.QwenErr)
	}
}

// PreferredOptions tunes LoadPreferredEmbedder.
type PreferredOptions struct {
	// SmokeTest embeds "test" after loading; a failure counts as a load
	// failure (degrades to NoopEmbedder; there is no fallback model).
	SmokeTest bool
}

// modelLoaders is swappable in tests so the factory can be exercised without
// real GGUFs.
type modelLoaders struct {
	qwen3 func(path string) (Embedder, error)
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
}

// PreferredQwen3Path returns the Qwen3 GGUF candidate for a model-slot path:
// MACLAW_EMBED_MODEL_PATH if set, else Qwen3ModelFilename in the same dir.
func PreferredQwen3Path(slotPath string) string {
	if p := strings.TrimSpace(os.Getenv(EmbedModelPathEnv)); p != "" {
		return p
	}
	if strings.TrimSpace(slotPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(slotPath), Qwen3ModelFilename)
}

// PreferredModelFileExists reports whether the model file the factory would
// load is present (used by hosts to decide whether a download is needed).
func PreferredModelFileExists(slotPath string) bool {
	p, _ := ResolveQwen3ModelPath(slotPath)
	return p != ""
}

func regularNonEmpty(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// LoadPreferredEmbedder loads the single embedding model, Qwen3 (there is no
// other model). slotPath is the host's model
// slot (embedding.DefaultModelPath()); the Qwen3 file is located with
// ResolveQwen3ModelPath. When no Qwen3 file exists, or a retry is not yet due
// after a failure, the result is a NoopEmbedder with a nil error (callers
// treat that as "download needed / degraded"). A non-nil error means a model
// file existed but failed to load (or the low-memory guard refused it).
func LoadPreferredEmbedder(slotPath string, opt PreferredOptions) (PreferredLoad, error) {
	return loadPreferredWith(preferredLoaders, slotPath, opt)
}

func loadPreferredWith(ld modelLoaders, slotPath string, opt PreferredOptions) (PreferredLoad, error) {
	res := PreferredLoad{Embedder: NoopEmbedder{}}
	path, tried := ResolveQwen3ModelPath(slotPath)
	res.Tried = tried
	if path == "" {
		res.QwenPath = PreferredQwen3Path(slotPath)
		res.QwenErr = fmt.Errorf("model file missing (tried: %s)", describeCandidates(tried))
		return res, nil
	}
	res.QwenPath = path
	key := fileGateKey(path)
	if err := qwen3Gate.allow(key); err != nil {
		res.QwenErr = err
		return res, nil
	}
	if err := CheckModelMemory(path); err != nil {
		qwen3Gate.fail(key, err)
		res.QwenErr = err
		return res, fmt.Errorf("qwen3 %s: %w", path, err)
	}
	emb, err := tryLoad(path, func(p string) (Embedder, error) { return ld.qwen3(p) }, opt.SmokeTest)
	if err != nil {
		qwen3Gate.fail(key, err)
		res.QwenErr = err
		return res, fmt.Errorf("qwen3 %s: %w", path, err)
	}
	qwen3Gate.success(key)
	res.Embedder, res.Family, res.Path = emb, FamilyQwen3, path
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
// FamilyGemma is only reported for legacy / foreign embedders (by ModelID or
// width) so stale Gemma-space heads and vectors can be recognised and refused.
func EmbedderFamily(e Embedder) string {
	if _, ok := e.(*Qwen3Embedder); ok {
		return FamilyQwen3
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
