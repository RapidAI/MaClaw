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

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// QueryEmbedding is the Layer 2 query vector of one classification turn.
type QueryEmbedding struct {
	Text    string
	Role    embedding.Role
	ModelID string    // "" when the embedder does not report one
	Vec     []float32 // owned by the receiver (a copy of the Layer 2 vector)

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
