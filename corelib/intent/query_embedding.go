package intent

// query_embedding.go — hand the Layer 2 query vector to Layer 3.
//
// ClassifyContext embeds the user message once for Layer 2. When it then
// escalates to Layer 3, a *local* L3 (for example a calibrated classification
// head running on the same embedder) would otherwise embed the same text with
// the same role again — on Qwen3-Embedding that second forward pass is about
// half of an escalated turn. ClassifyContext therefore attaches the Layer 2
// vector to the context it passes to LLMClassifyContextFunc. Remote L3
// callbacks never look at it, so their behaviour is unchanged.
//
// Local L3 implementations call IntentQueryEmbedding(ctx, emb, text): it
// returns the attached vector when it was produced by the same embedder
// (same instance, or the same non-empty ModelID), with the Layer 2 role, for
// exactly this text; otherwise it embeds text itself. Both branches run the
// identical EmbedAs(emb, text, intentEmbeddingRole) computation, so the
// result is the same vector either way (the embedders are deterministic).

import (
	"context"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// QueryEmbedding is the Layer 2 query vector of one classification turn.
type QueryEmbedding struct {
	Text    string
	Role    embedding.Role
	ModelID string    // "" when the embedder does not report one
	Vec     []float32 // owned by the receiver (a copy of the Layer 2 vector)
	// AnchorMaxCos is the raw per-label max anchor cosine of the same Layer 2
	// pass (nil when unavailable); read-only. Used by heads with L2 anchor
	// features (LocalHead.AnchorFeatureLabels) — no extra embedding call.
	AnchorMaxCos map[string]float64

	emb embedding.Embedder
}

type queryEmbeddingKey struct{}

// WithQueryEmbedding returns ctx carrying qe for Layer 3 callbacks.
func WithQueryEmbedding(ctx context.Context, qe QueryEmbedding) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, queryEmbeddingKey{}, qe)
}

// QueryEmbeddingFromContext returns the Layer 2 query vector attached by
// ClassifyContext, if any. The returned Vec is a fresh copy.
func QueryEmbeddingFromContext(ctx context.Context) (QueryEmbedding, bool) {
	if ctx == nil {
		return QueryEmbedding{}, false
	}
	qe, ok := ctx.Value(queryEmbeddingKey{}).(QueryEmbedding)
	if !ok || len(qe.Vec) == 0 {
		return QueryEmbedding{}, false
	}
	qe.Vec = append([]float32(nil), qe.Vec...)
	return qe, true
}

// IntentQueryEmbedding returns text's vector in the Layer 2 space of emb,
// reusing the vector ClassifyContext already computed for this turn when it
// matches (same text, Layer 2 role, same embedder); otherwise it embeds.
// reused reports which branch was taken.
func IntentQueryEmbedding(ctx context.Context, emb embedding.Embedder, text string) (vec []float32, reused bool, err error) {
	if qe, ok := QueryEmbeddingFromContext(ctx); ok && qe.matches(emb, text, intentEmbeddingRole) {
		return qe.Vec, true, nil
	}
	vec, err = embedIntentQuery(emb, text)
	return vec, false, err
}

func (qe QueryEmbedding) matches(emb embedding.Embedder, text string, role embedding.Role) bool {
	if qe.Text != text || qe.Role != role || emb == nil {
		return false
	}
	if emb.Dim() > 0 && len(qe.Vec) != emb.Dim() {
		return false
	}
	if qe.emb != nil && qe.emb == emb {
		return true
	}
	id := embedderModelID(emb)
	return id != "" && id == qe.ModelID
}

func embedderModelID(emb embedding.Embedder) string {
	if identified, ok := emb.(interface{ ModelID() string }); ok {
		return strings.TrimSpace(identified.ModelID())
	}
	return ""
}

// newQueryEmbedding wraps a Layer 2 vector (copied) for WithQueryEmbedding.
func newQueryEmbedding(emb embedding.Embedder, text string, vec []float32) QueryEmbedding {
	return QueryEmbedding{
		Text:    text,
		Role:    intentEmbeddingRole,
		ModelID: embedderModelID(emb),
		Vec:     append([]float32(nil), vec...),
		emb:     emb,
	}
}

// IntentQueryEmbeddingScores is IntentQueryEmbedding that also returns the
// Layer 2 per-label raw anchor cosines when the attached vector is reused
// (nil otherwise).
func IntentQueryEmbeddingScores(ctx context.Context, emb embedding.Embedder, text string) (vec []float32, scores map[string]float64, err error) {
	if qe, ok := QueryEmbeddingFromContext(ctx); ok && qe.matches(emb, text, intentEmbeddingRole) {
		return qe.Vec, qe.AnchorMaxCos, nil
	}
	vec, err = embedIntentQuery(emb, text)
	return vec, nil, err
}

// HeadQueryInput returns the input vector for head h: the Layer 2 query
// vector (reused from ctx, plus its anchor scores) for ordinary heads, or —
// for heads with InputBlocks (r13 F1 last+mean) — the concatenated pooled
// views of one classification-role pass (embedding.EmbedPooledAs; the Layer
// 2 vector itself is not used or changed).
func HeadQueryInput(ctx context.Context, emb embedding.Embedder, h *LocalHead, text string) (vec []float32, scores map[string]float64, err error) {
	if h == nil || len(h.InputBlocks) == 0 {
		return IntentQueryEmbeddingScores(ctx, emb, text)
	}
	key := pooledKey{emb: emb, text: text, blocks: strings.Join(h.InputBlocks, ",")}
	pooledMemo.Lock()
	if pooledMemo.key == key && pooledMemo.vec != nil {
		vec = append([]float32(nil), pooledMemo.vec...)
		pooledMemo.Unlock()
		return vec, nil, nil
	}
	pooledMemo.Unlock()
	last, mean, err := embedding.EmbedPooledAs(emb, text, intentEmbeddingRole)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		pooledMemo.Lock()
		pooledMemo.key, pooledMemo.vec = key, append([]float32(nil), vec...)
		pooledMemo.Unlock()
	}()
	vec = make([]float32, 0, len(h.InputBlocks)*len(last))
	for _, b := range h.InputBlocks {
		if b == "mean" {
			vec = append(vec, mean...)
		} else {
			vec = append(vec, last...)
		}
	}
	return vec, nil, nil
}

// pooledMemo keeps the last pooled head input so the composite-override
// probe and the L3 call of the same turn embed once (deterministic embedder,
// so a hit returns exactly what a fresh pass would).
type pooledKey struct {
	emb    embedding.Embedder
	text   string
	blocks string
}

var pooledMemo struct {
	sync.Mutex
	key pooledKey
	vec []float32
}
