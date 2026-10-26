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
	"reflect"
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
	if sameEmbedderInstance(qe.emb, emb) {
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
	// The embedding-only cache keeps the anchor cosines and drops the vector.
	// Embed once, and still hand the head the scores from that pass.
	if qe, ok := queryEmbeddingRaw(ctx); ok && len(qe.Vec) == 0 && len(qe.AnchorMaxCos) > 0 && qe.sameEmbedder(emb, text, intentEmbeddingRole) {
		vec, err = embedIntentQuery(emb, text)
		return vec, qe.AnchorMaxCos, err
	}
	vec, err = embedIntentQuery(emb, text)
	return vec, nil, err
}

func queryEmbeddingRaw(ctx context.Context) (QueryEmbedding, bool) {
	if ctx == nil {
		return QueryEmbedding{}, false
	}
	qe, ok := ctx.Value(queryEmbeddingKey{}).(QueryEmbedding)
	return qe, ok
}

// sameEmbedder is matches without a vector-length check, for a scores-only
// attachment whose vector was not retained.
func (qe QueryEmbedding) sameEmbedder(emb embedding.Embedder, text string, role embedding.Role) bool {
	if qe.Text != text || qe.Role != role || emb == nil {
		return false
	}
	if sameEmbedderInstance(qe.emb, emb) {
		return true
	}
	id := embedderModelID(emb)
	return id != "" && id == qe.ModelID
}

// HeadQueryInput returns the input vector for head h: the Layer 2 query
// vector (reused from ctx, plus its anchor scores) for ordinary heads, or —
// for heads with InputBlocks (r13 F1 last+mean) — the concatenated pooled
// views of the classification-role pass. Layer 2 already ran that pass for the
// same text, so its last+mean views are reused instead of embedding again.
func HeadQueryInput(ctx context.Context, emb embedding.Embedder, h *LocalHead, text string) (vec []float32, scores map[string]float64, err error) {
	if h == nil || len(h.InputBlocks) == 0 {
		return IntentQueryEmbeddingScores(ctx, emb, text)
	}
	key := pooledKey{emb: emb, text: text, blocks: strings.Join(h.InputBlocks, ",")}
	pooledMemo.Lock()
	if sameEmbedderInstance(pooledMemo.key.emb, key.emb) &&
		pooledMemo.key.text == key.text &&
		pooledMemo.key.blocks == key.blocks &&
		pooledMemo.vec != nil {
		vec = append([]float32(nil), pooledMemo.vec...)
		pooledMemo.Unlock()
		return vec, nil, nil
	}
	pooledMemo.Unlock()
	last, mean, reused := lookupPooledPair(emb, text, intentEmbeddingRole)
	if !reused {
		last, mean, err = embedding.EmbedPooledAs(emb, text, intentEmbeddingRole)
		if err != nil {
			return nil, nil, err
		}
		// Publish the pair as well: a Layer 2 pass later in the same turn then
		// reuses this forward instead of embedding again.
		storePooledPair(emb, text, intentEmbeddingRole, last, mean)
	}
	vec = assembleBlockVector(h.InputBlocks, last, mean)
	pooledMemo.Lock()
	pooledMemo.key, pooledMemo.vec = key, append([]float32(nil), vec...)
	pooledMemo.Unlock()
	return vec, nil, nil
}

// assembleBlockVector concatenates the requested pooled views in block order.
// Per-block normalisation stays with the caller (Predict's blockNormalize); the
// pooled vectors are already L2-normalised, so the pass is idempotent.
func assembleBlockVector(blocks []string, last, mean []float32) []float32 {
	vec := make([]float32, 0, len(blocks)*len(last))
	for _, b := range blocks {
		if b == "mean" {
			vec = append(vec, mean...)
		} else {
			vec = append(vec, last...)
		}
	}
	return vec
}

// pooledPair is the last-token and mean view of one classification-role pass.
// Layer 2 performs that pass anyway, so keeping both here saves a whole forward
// for heads with InputBlocks (the shipped r13 head).
type pooledPair struct {
	emb  embedding.Embedder
	text string
	role embedding.Role
	last []float32
	mean []float32
}

var pooledPairMemo struct {
	sync.Mutex
	entry pooledPair
}

// storePooledPair keeps copies: embedIntentQuery hands the caller the original
// last view, which must stay independent of the cached one.
func storePooledPair(emb embedding.Embedder, text string, role embedding.Role, last, mean []float32) {
	if len(last) == 0 || len(mean) == 0 {
		return
	}
	pooledPairMemo.Lock()
	pooledPairMemo.entry = pooledPair{
		emb:  emb,
		text: text,
		role: role,
		last: append([]float32(nil), last...),
		mean: append([]float32(nil), mean...),
	}
	pooledPairMemo.Unlock()
}

// SameEmbedder reports whether a and b are the same embedder instance.
// Interface equality panics when the dynamic type is not comparable (a
// value-type embedder holding a slice or map); this compares pointer identity
// instead. Exported for the local-L3 runtime, which caches one head per
// embedder.
func SameEmbedder(a, b embedding.Embedder) bool { return sameEmbedderInstance(a, b) }

// sameEmbedderInstance reports whether a and b are the same embedder object.
// Interface equality would panic when a dynamic type is not comparable (a
// value-type embedder holding a slice or map), so pointer identity is compared
// instead; non-pointer embedders never compare equal and simply re-embed.
func sameEmbedderInstance(a, b embedding.Embedder) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Kind() != reflect.Ptr || vb.Kind() != reflect.Ptr || va.Type() != vb.Type() {
		return false
	}
	return va.Pointer() == vb.Pointer()
}

func lookupPooledPair(emb embedding.Embedder, text string, role embedding.Role) (last, mean []float32, ok bool) {
	pooledPairMemo.Lock()
	defer pooledPairMemo.Unlock()
	e := pooledPairMemo.entry
	if !sameEmbedderInstance(e.emb, emb) || e.text != text || e.role != role {
		return nil, nil, false
	}
	return e.last, e.mean, true
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
