package intent

import (
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// roleSpyEmbedder is a prompt-aware embedder that records the text it was
// asked to embed (after prompting) and exposes a ModelID.
type roleSpyEmbedder struct {
	mu      sync.Mutex
	modelID string
	seen    []string
	batches int
}

func (e *roleSpyEmbedder) vec(text string) []float32 {
	v := make([]float32, 8)
	for i, r := range text {
		v[i%8] += float32(r%97) + 1
	}
	return v
}
func (e *roleSpyEmbedder) Embed(text string) ([]float32, error) {
	e.mu.Lock()
	e.seen = append(e.seen, text)
	e.mu.Unlock()
	return e.vec(text), nil
}
func (e *roleSpyEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.batches++
	e.seen = append(e.seen, texts...)
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.vec(t)
	}
	return out, nil
}
func (e *roleSpyEmbedder) EmbedWithRole(text string, role embedding.Role) ([]float32, error) {
	return e.Embed(embedding.ApplyPrompt(text, role))
}
func (e *roleSpyEmbedder) EmbedBatchWithRole(texts []string, role embedding.Role) ([][]float32, error) {
	prompted := make([]string, len(texts))
	for i, t := range texts {
		prompted[i] = embedding.ApplyPrompt(t, role)
	}
	return e.EmbedBatch(prompted)
}
func (e *roleSpyEmbedder) Dim() int        { return 8 }
func (e *roleSpyEmbedder) Close()          {}
func (e *roleSpyEmbedder) ModelID() string { return e.modelID }

// plainEmbedder has no prompt support and no ModelID.
type plainEmbedder struct{ roleSpyEmbedder }

func (p *plainEmbedder) asEmbedder() embedding.Embedder { return plainOnly{p} }

type plainOnly struct{ p *plainEmbedder }

func (o plainOnly) Embed(t string) ([]float32, error)          { return o.p.Embed(t) }
func (o plainOnly) EmbedBatch(t []string) ([][]float32, error) { return o.p.EmbedBatch(t) }
func (o plainOnly) Dim() int                                   { return 8 }
func (o plainOnly) Close()                                     {}

func testAnchors() []intentAnchor {
	return []intentAnchor{
		{Label: LabelCoding, Texts: []string{"write a script", "build an app"}},
		{Label: LabelBugFix, Texts: []string{"fix the crash"}},
		{Label: LabelUnknown},
	}
}

func TestIntentEmbeddingUsesClassificationPromptForAnchorsAndQueries(t *testing.T) {
	t.Setenv("MACLAW_INTENT_ANCHOR_CACHE", "0")
	embedding.SetPromptsEnabled(true)
	emb := &roleSpyEmbedder{modelID: "spy:8:prompt-v1"}
	warmed, err := warmupAnchors(emb, testAnchors())
	if err != nil {
		t.Fatal(err)
	}
	classifyByEmbedding(emb, warmed, "please repair the login")
	prefix := embedding.PromptPrefix(embedding.RoleClassification)
	if len(emb.seen) != 4 {
		t.Fatalf("expected 3 anchor texts + 1 query, got %d: %q", len(emb.seen), emb.seen)
	}
	for _, s := range emb.seen {
		if !strings.HasPrefix(s, prefix) {
			t.Fatalf("text embedded without classification prompt: %q", s)
		}
	}
	if got := intentScoreCalibrationFor(emb); got != classificationPromptScoreCalibration {
		t.Fatalf("prompted embedder must use the prompt calibration, got %+v", got)
	}
}

func TestIntentEmbeddingRawSpaceKeepsIdentityCalibration(t *testing.T) {
	t.Setenv("MACLAW_INTENT_ANCHOR_CACHE", "0")
	p := &plainEmbedder{}
	emb := p.asEmbedder()
	if got := intentScoreCalibrationFor(emb); got != identityScoreCalibration {
		t.Fatalf("plain embedder must keep raw calibration, got %+v", got)
	}
	warmed, err := warmupAnchors(emb, testAnchors())
	if err != nil {
		t.Fatal(err)
	}
	classifyByEmbedding(emb, warmed, "please repair the login")
	for _, s := range p.seen {
		if strings.HasPrefix(s, "task:") {
			t.Fatalf("plain embedder received a prompted text: %q", s)
		}
	}
	// Prompts disabled process-wide: a prompt-aware embedder stays raw too.
	embedding.SetPromptsEnabled(false)
	defer embedding.SetPromptsEnabled(true)
	if got := intentScoreCalibrationFor(&roleSpyEmbedder{modelID: "x"}); got != identityScoreCalibration {
		t.Fatalf("prompts disabled must keep raw calibration, got %+v", got)
	}
}

func TestIntentScoreCalibrationApply(t *testing.T) {
	c := classificationPromptScoreCalibration
	if c.apply(0) != 0 || c.apply(-0.3) != 0 {
		t.Fatal("non-positive scores must stay 0")
	}
	if got := c.apply(1); got > 1 {
		t.Fatalf("calibrated score must not exceed 1, got %v", got)
	}
	if !(c.apply(0.80) > c.apply(0.79)) {
		t.Fatal("calibration must be monotone")
	}
	if identityScoreCalibration.apply(0.731) != 0.731 {
		t.Fatal("identity calibration changed a score")
	}
}

func TestIntentAnchorDiskCacheRoundTripAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_INTENT_ANCHOR_CACHE", "")
	t.Setenv("MACLAW_INTENT_ANCHOR_CACHE_DIR", dir)
	embedding.SetPromptsEnabled(true)

	first := &roleSpyEmbedder{modelID: "spy:8:prompt-v1"}
	w1, err := warmupAnchors(first, testAnchors())
	if err != nil || first.batches != 2 {
		t.Fatalf("cold warmup: err=%v batches=%d", err, first.batches)
	}
	second := &roleSpyEmbedder{modelID: "spy:8:prompt-v1"}
	w2, err := warmupAnchors(second, testAnchors())
	if err != nil {
		t.Fatal(err)
	}
	if second.batches != 0 {
		t.Fatalf("warm start must come from disk cache, embedded %d batches", second.batches)
	}
	for i := range w1 {
		if w1[i].Label != w2[i].Label || len(w1[i].Vecs) != len(w2[i].Vecs) {
			t.Fatalf("cache mismatch at %d", i)
		}
		for j := range w1[i].Vecs {
			for k := range w1[i].Vecs[j] {
				if w1[i].Vecs[j][k] != w2[i].Vecs[j][k] {
					t.Fatalf("cached vector differs at %d/%d/%d", i, j, k)
				}
			}
		}
	}
	// A different vector space (e.g. prompt regime -> different ModelID) must miss.
	other := &roleSpyEmbedder{modelID: "spy:8:raw"}
	if _, err := warmupAnchors(other, testAnchors()); err != nil || other.batches != 2 {
		t.Fatalf("ModelID change must re-embed: err=%v batches=%d", err, other.batches)
	}
	// Edited anchor text must miss.
	edited := testAnchors()
	edited[1].Texts = []string{"fix the crash on startup"}
	again := &roleSpyEmbedder{modelID: "spy:8:prompt-v1"}
	if _, err := warmupAnchors(again, edited); err != nil || again.batches != 2 {
		t.Fatalf("anchor edit must re-embed: err=%v batches=%d", err, again.batches)
	}
	// Embedders without a ModelID are never cached.
	p := &plainEmbedder{}
	if key := intentAnchorCacheKey(p.asEmbedder(), testAnchors()); key != "" {
		t.Fatalf("unidentified embedder must not get a cache key, got %q", key)
	}
}
