package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/rerank"
)

const (
	memoryRerankMinCandidates = 15
	memoryRerankHead          = 32
	memoryRerankBand          = 2.0
)

type recallRerankMeta struct {
	applied bool
	pool    int
}

// SetReranker attaches a local scorer. Nil leaves recall on the stream score.
func (s *Store) SetReranker(r rerank.Reranker) {
	if s == nil {
		return
	}
	s.rerankerMu.Lock()
	s.reranker = r
	s.rerankerMu.Unlock()
}

func (s *Store) currentReranker() rerank.Reranker {
	if s == nil {
		return nil
	}
	s.rerankerMu.RLock()
	defer s.rerankerMu.RUnlock()
	return s.reranker
}

// blendRelevanceRerank adds a bounded relevance bonus to the head.
// The caller has already decided the query is complex. A gap larger than
// memoryRerankBand in the existing score cannot be overturned.
func blendRelevanceRerank(ctx context.Context, rr rerank.Reranker, query string, candidates []recallScored) ([]recallScored, recallRerankMeta) {
	if rr == nil || len(candidates) <= memoryRerankMinCandidates {
		return candidates, recallRerankMeta{}
	}
	// Graph expansion can leave a higher base score past the prefix. Score the
	// actual top of the list, not whatever happens to sit in the first slots.
	ordered := append([]recallScored(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].score > ordered[j].score
	})
	head := memoryRerankHead
	if head > len(ordered) {
		head = len(ordered)
	}
	focus := rerank.Prepare(query)
	passages := make([]rerank.Passage, head)
	for i := 0; i < head; i++ {
		passages[i] = rerank.Passage{ID: ordered[i].entry.ID, Text: focus.Window(memoryPassageText(ordered[i].entry))}
	}
	outcome := rerank.Apply(ctx, rr, query, passages, rerank.DefaultBudget)
	if !outcome.Applied {
		return candidates, recallRerankMeta{}
	}
	norms := rerank.MinMax(outcome.Scores)
	out := ordered
	for i := 0; i < head && i < len(norms); i++ {
		out[i].score += memoryRerankBand * norms[i]
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})
	return out, recallRerankMeta{applied: true, pool: head}
}

// memoryPassageText keeps a compact form that is not already in the body.
// The compact form is a short keyword line, so it goes first and the rune
// cap cannot drop it behind a long entry.
func memoryPassageText(entry Entry) string {
	content := strings.TrimSpace(entry.Content)
	compact := strings.TrimSpace(entry.CompactForm)
	if compact == "" || compact == content || strings.Contains(content, compact) {
		if content != "" {
			return content
		}
		return compact
	}
	if content == "" {
		return compact
	}
	return compact + " " + content
}
