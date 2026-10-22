package intent

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func (e *roleSpyEmbedder) countSeen(sub string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, s := range e.seen {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// TestClassifyContextHandsL2VectorToLocalL3 checks that an escalated turn
// embeds the message once: the local L3 gets the Layer 2 vector through the
// context, bit-identical to embedding it again.
func TestClassifyContextHandsL2VectorToLocalL3(t *testing.T) {
	emb := &roleSpyEmbedder{modelID: "spy:8"}
	const text = "please help me sort out the quarterly numbers for the team"
	var got []float32
	var reused bool
	var l3 int
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx, parent context.Context, _, user string) (string, error) {
			l3++
			v, r, err := IntentQueryEmbedding(ctx, emb, user)
			if err != nil {
				return "", err
			}
			got, reused = v, r
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}}},
		{Label: LabelCoding, Vecs: [][]float32{{0, 1, 0, 0, 0, 0, 0, 0}}},
	}
	uic.mu.Unlock()

	res := uic.ClassifyContext(context.Background(), MessageContext{Text: text})
	if l3 != 1 {
		t.Fatalf("L3 calls = %d, want 1 (result %+v)", l3, res)
	}
	if !reused {
		t.Fatalf("local L3 re-embedded instead of reusing the Layer 2 vector")
	}
	if n := emb.countSeen(text); n != 1 {
		t.Fatalf("message embedded %d times in one escalated turn, want 1", n)
	}
	want, err := embedding.EmbedAs(emb, text, intentEmbeddingRole)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("len %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reused vector differs at %d: %v vs %v", i, got[i], want[i])
		}
	}
	if res.Primary != LabelOffice || res.Layer != 3 {
		t.Fatalf("result = %+v, want office from L3", res)
	}
}

func TestClassifyEmbeddingOnlyKeepsThePassForTheHead(t *testing.T) {
	emb := &roleSpyEmbedder{modelID: "spy:8"}
	const text = "fix the nil pointer in login"
	uic := New(Config{Embedder: emb})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelCoding, Vecs: [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}}},
		{Label: LabelOffice, Vecs: [][]float32{{0, 1, 0, 0, 0, 0, 0, 0}}},
	}
	uic.mu.Unlock()

	first := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
	if len(first.QueryVec) != 8 {
		t.Fatalf("query vec len = %d", len(first.QueryVec))
	}
	if first.AnchorMaxCos[string(LabelCoding)] == 0 || first.AnchorMaxCos[string(LabelOffice)] == 0 {
		t.Fatalf("anchor scores = %v", first.AnchorMaxCos)
	}
	embedded := emb.countSeen(text)
	if embedded != 1 {
		t.Fatalf("embedded %d times, want 1", embedded)
	}
	ctx := uic.WithEmbeddingQuery(context.Background(), text, first)
	vec, scores, err := IntentQueryEmbeddingScores(ctx, emb, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 8 || scores[string(LabelCoding)] == 0 {
		t.Fatalf("reused vec %d scores %v", len(vec), scores)
	}
	if emb.countSeen(text) != 1 {
		t.Fatalf("head embedded again: %d", emb.countSeen(text))
	}

	cached := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
	if len(cached.QueryVec) != 0 {
		t.Fatal("cache must not retain the query vector")
	}
	if cached.AnchorMaxCos[string(LabelCoding)] == 0 {
		t.Fatalf("cache dropped anchor scores: %v", cached.AnchorMaxCos)
	}
	ctx = uic.WithEmbeddingQuery(context.Background(), text, cached)
	_, scores, err = IntentQueryEmbeddingScores(ctx, emb, text)
	if err != nil {
		t.Fatal(err)
	}
	if scores[string(LabelOffice)] == 0 {
		t.Fatalf("cache-hit head lost anchor scores: %v", scores)
	}
	if emb.countSeen(text) != 2 {
		t.Fatalf("cache hit embedded %d times, want one more", emb.countSeen(text))
	}
}

func TestIntentQueryEmbeddingRejectsMismatches(t *testing.T) {
	emb := &roleSpyEmbedder{modelID: "spy:8"}
	vec, _ := embedding.EmbedAs(emb, "hello there", intentEmbeddingRole)
	ctx := WithQueryEmbedding(context.Background(), newQueryEmbedding(emb, "hello there", vec))

	if _, reused, _ := IntentQueryEmbedding(ctx, emb, "hello there"); !reused {
		t.Fatal("same embedder + text should reuse")
	}
	// Different text: embed.
	if _, reused, _ := IntentQueryEmbedding(ctx, emb, "something else"); reused {
		t.Fatal("different text must not reuse")
	}
	// Different instance, same ModelID: reuse (same model, same space).
	twin := &roleSpyEmbedder{modelID: "spy:8"}
	if _, reused, _ := IntentQueryEmbedding(ctx, twin, "hello there"); !reused {
		t.Fatal("same ModelID should reuse")
	}
	// Different model: embed.
	other := &roleSpyEmbedder{modelID: "other:8"}
	if _, reused, _ := IntentQueryEmbedding(ctx, other, "hello there"); reused {
		t.Fatal("different ModelID must not reuse")
	}
	// No ModelID and a different instance: embed.
	plain := &plainEmbedder{}
	if _, reused, _ := IntentQueryEmbedding(ctx, plain, "hello there"); reused {
		t.Fatal("unidentified embedder must not reuse")
	}
	// No vector attached: embed.
	if _, reused, _ := IntentQueryEmbedding(context.Background(), emb, "hello there"); reused {
		t.Fatal("no attached vector must not reuse")
	}
	// The receiver owns its copy.
	a, _ := QueryEmbeddingFromContext(ctx)
	a.Vec[0] = -1
	b, _ := QueryEmbeddingFromContext(ctx)
	if b.Vec[0] == -1 {
		t.Fatal("QueryEmbeddingFromContext must return a copy")
	}
}
