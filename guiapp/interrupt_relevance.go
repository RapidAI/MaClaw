package guiapp

// interrupt_relevance.go — embedder-specific interrupt relevance for the IM
// interrupt scheduler (see corelib/progress/relevance.go).
//
// Gemma (and any non-Qwen3 embedder) keeps the legacy plain cosine on the
// scheduler's original scale. For Qwen3 the handler uses a RelevanceModel:
//   1. <models>/interrupt_heads/interrupt_relevance_qwen3go.json when present
//      (release asset, fetched GitHub → hub with size+sha verification like
//      the intent / tool heads) and bound to the live embedder's model id;
//   2. otherwise the built-in Qwen3 model (instruction-formatted cosine with
//      bands calibrated for Qwen3).
// MACLAW_INTERRUPT_RELEVANCE=off restores the legacy plain cosine for every
// embedder; MACLAW_INTERRUPT_RELEVANCE=<path> forces a model file.

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

const (
	interruptRelevanceEnv      = "MACLAW_INTERRUPT_RELEVANCE"
	interruptRelevanceDirName  = "interrupt_heads"
	interruptRelevanceFileName = "interrupt_relevance_qwen3go.json"
)

// builtinQwen3InterruptRelevance is used for Qwen3 when no model file is
// installed. nil → legacy plain cosine.
var builtinQwen3InterruptRelevance = defaultQwen3InterruptRelevance()

type relevanceFileCache struct {
	mu    sync.Mutex
	path  string
	mtime time.Time
	size  int64
	model *progress.RelevanceModel
}

var interruptRelevanceFiles relevanceFileCache

func (c *relevanceFileCache) load(path string) *progress.RelevanceModel {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == path && c.mtime.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.model
	}
	m, err := progress.LoadRelevanceModel(path)
	if err != nil {
		log.Printf("[interrupt] relevance model %s rejected: %v", path, err)
		m = nil
	}
	c.path, c.mtime, c.size, c.model = path, fi.ModTime(), fi.Size(), m
	return m
}

// relevanceModelMatches reports whether m was built for emb's vector space.
func relevanceModelMatches(m *progress.RelevanceModel, emb embedding.Embedder) bool {
	if m == nil || emb == nil {
		return false
	}
	if m.Dims > 0 && m.Dims != emb.Dim() {
		return false
	}
	want := strings.TrimSpace(m.EmbedderModelID)
	if want == "" {
		return true
	}
	if id, ok := emb.(interface{ ModelID() string }); ok {
		return id.ModelID() == want
	}
	return false
}

// interruptRelevanceModelFor resolves the relevance model for emb (nil →
// legacy plain cosine).
func interruptRelevanceModelFor(emb embedding.Embedder) *progress.RelevanceModel {
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	v := strings.TrimSpace(os.Getenv(interruptRelevanceEnv))
	switch strings.ToLower(v) {
	case "off", "0", "false", "legacy", "cosine":
		return nil
	}
	if v != "" && !isTruthyEnv(v) {
		if m := interruptRelevanceFiles.load(v); relevanceModelMatches(m, emb) {
			return m
		}
		return nil
	}
	if embedding.EmbedderFamily(emb) != embedding.FamilyQwen3 {
		return nil
	}
	if dir := embedding.DefaultModelsDir(); dir != "" {
		p := filepath.Join(dir, interruptRelevanceDirName, interruptRelevanceFileName)
		if m := interruptRelevanceFiles.load(p); relevanceModelMatches(m, emb) {
			return m
		}
	}
	if m := builtinQwen3InterruptRelevance; m != nil && (m.Dims == 0 || m.Dims == emb.Dim()) {
		return m
	}
	return nil
}

func isTruthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// taskVecCache keeps model-formatted task vectors keyed by the formatted
// text, so the vector computed at loop start (EmbedText) is reused by
// TryInterrupt even though the tracker buffer stores only a bare vector.
type taskVecCache struct {
	mu sync.Mutex
	m  map[string][]float32
}

func (c *taskVecCache) get(k string) []float32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

func (c *taskVecCache) put(k string, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string][]float32, 8)
	}
	if _, ok := c.m[k]; !ok && len(c.m) >= 64 {
		// A new task past the cap drops the older vectors. Refreshing a key
		// that is already cached must keep the rest.
		c.m = make(map[string][]float32, 8)
	}
	c.m[k] = v
}

// modelRelevance scores task ↔ message with model m (formatted embeddings).
// Returns -1 when anything is unavailable.
func (ih *imInterruptHandler) modelRelevance(emb embedding.Embedder, m *progress.RelevanceModel, taskDesc, messageText string) float64 {
	// Key the cache on the same user-authored text EmbedText stored. The
	// tracker keeps the raw task, picker block included, for the LLM.
	taskDesc = stripHostAttachmentSections(taskDesc)
	messageText = stripHostAttachmentSections(messageText)
	if taskDesc == "" || messageText == "" {
		return -1
	}
	tk := m.FormatTask(taskDesc)
	u := ih.taskVecs.get(tk)
	if len(u) == 0 {
		vec, err := emb.Embed(tk)
		if err != nil || len(vec) == 0 {
			return -1
		}
		ih.taskVecs.put(tk, vec)
		u = vec
	}
	v, err := emb.Embed(m.FormatMessage(messageText))
	if err != nil || len(v) == 0 {
		return -1
	}
	return m.Relevance(u, v)
}

// defaultQwen3InterruptRelevance returns the built-in Qwen3 model (filled in
// by interrupt_relevance_qwen3_builtin.go).
func defaultQwen3InterruptRelevance() *progress.RelevanceModel { return qwen3InterruptRelevanceBuiltin }
