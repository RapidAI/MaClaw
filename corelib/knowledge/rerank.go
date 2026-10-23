package knowledge

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/rerank"
)

const (
	knowledgeRerankPool = 48
	knowledgeRerankTie  = 0.02
)

type rerankNoteKey struct{}

type rerankNoteBox struct {
	note string
}

func contextWithRerankNote(ctx context.Context) (context.Context, *rerankNoteBox) {
	if ctx == nil {
		ctx = context.Background()
	}
	box := &rerankNoteBox{}
	return context.WithValue(ctx, rerankNoteKey{}, box), box
}

func setRerankNote(ctx context.Context, note string) {
	box, _ := ctx.Value(rerankNoteKey{}).(*rerankNoteBox)
	if box == nil {
		return
	}
	box.note = note
}

// SetReranker attaches a local scorer. Nil leaves Search on fusion order.
func (s *SQLiteStore) SetReranker(r rerank.Reranker) {
	if s == nil {
		return
	}
	s.rerankerMu.Lock()
	s.reranker = r
	s.rerankerMu.Unlock()
}

func (s *SQLiteStore) currentReranker() rerank.Reranker {
	if s == nil {
		return nil
	}
	s.rerankerMu.RLock()
	defer s.rerankerMu.RUnlock()
	return s.reranker
}

func lexicalCandidateLimit(limit int) int {
	candidateLimit := limit * 3
	if candidateLimit < limit {
		candidateLimit = limit
	}
	if candidateLimit > 100 {
		candidateLimit = 100
	}
	return candidateLimit
}

// knowledgeScanLimit is the per-index SQL limit. Widening never fetches fewer
// rows than the historical limit; it only raises a short list to the rerank pool.
func knowledgeScanLimit(finalLimit int, widen bool) int {
	limit := lexicalCandidateLimit(finalLimit)
	if widen && limit < knowledgeRerankPool {
		return knowledgeRerankPool
	}
	return limit
}

// knowledgeRerankRetrieval chooses the vector half of a wide pool.
// Explicit fts and vector routes keep their contract. A strict name stays
// lexical. Auto and hybrid keep a vector half even when the lexical hit is
// already strong, so the scorer sees both lists.
func knowledgeRerankRetrieval(opts SearchOptions, results []SearchResult, hasEmbedder bool) knowledgeRetrievalDecision {
	decision := knowledgeRetrievalPlan(opts, results, hasEmbedder)
	if !hasEmbedder || QuerySuppressesEmbedding(opts.Query) || opts.Retrieval == knowledgeRetrievalFTS || opts.Retrieval == knowledgeRetrievalVector {
		return decision
	}
	return knowledgeRetrievalDecision{runEmbedding: true, fuse: true}
}

// knowledgeRerankPreGate is the part of the gate known before the score check.
// The lexical best-score check and the final pool-length check stay in Search.
func knowledgeRerankPreGate(opts SearchOptions) bool {
	return opts.Limit > 0 && opts.Limit < knowledgeRerankPool && !QuerySuppressesEmbedding(opts.Query)
}

func knowledgeQueryIsQuestion(query string) bool {
	if strings.Contains(query, "?") || strings.Contains(query, "？") {
		return true
	}
	for _, word := range []string{"为什么", "怎么", "对比", "区别"} {
		if strings.Contains(query, word) {
			return true
		}
	}
	lowered := strings.ToLower(query)
	for _, word := range []string{"why", "how", "compare", "difference"} {
		if containsASCIIWord(lowered, word) {
			return true
		}
	}
	return false
}

func containsASCIIWord(text, word string) bool {
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], word)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isASCIILetter(text[i-1])
		end := i + len(word)
		afterOK := end >= len(text) || !isASCIILetter(text[end])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func bestNonNodeSearchScore(results []SearchResult) float64 {
	best := 0.0
	for _, result := range results {
		if result.ResultType != "node" && result.Score > best {
			best = result.Score
		}
	}
	return best
}

func knowledgePassages(query string, results []SearchResult) []rerank.Passage {
	focus := rerank.Prepare(query)
	passages := make([]rerank.Passage, len(results))
	for i, result := range results {
		passages[i] = rerank.Passage{
			ID:   searchResultEntityKey(result),
			Text: focus.Window(knowledgePassageBody(result)),
		}
	}
	return passages
}

func knowledgePassageText(result SearchResult, query string) string {
	return rerank.Window(knowledgePassageBody(result), query)
}

func knowledgePassageBody(result SearchResult) string {
	fields := []string{
		result.CardTitle,
		result.Claim,
		result.Summary,
		result.NodeTitle,
		result.Subject,
		result.Predicate,
		result.Object,
	}
	var b strings.Builder
	write := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(part)
	}
	for _, field := range fields {
		write(field)
	}
	body := b.String()
	// Fact snippets repeat the triple, and card snippets repeat the claim.
	// A novel snippet, including an FTS window, still has to be searchable.
	snippet := strings.TrimSpace(result.Snippet)
	if snippet != "" && !strings.Contains(body, snippet) {
		if body != "" {
			body += " "
		}
		body += snippet
	}
	return body
}

type knowledgeRanked struct {
	result SearchResult
	blend  float64
}

// orderKnowledgeResultsByRerank sorts by min-max relevance plus a bounded
// project and trust bonus. A gap under knowledgeRerankTie keeps the
// historical type-and-score order.
func orderKnowledgeResultsByRerank(opts SearchOptions, results []SearchResult, scores []float64) []SearchResult {
	norms := rerank.MinMax(scores)
	items := make([]knowledgeRanked, len(results))
	for i, result := range results {
		blend := 0.0
		if i < len(norms) {
			blend = norms[i]
		}
		if opts.ProjectPath != "" && result.Source.ProjectPath == opts.ProjectPath {
			blend += 0.05
		}
		if result.Source.SourceTrust > 0 {
			blend += result.Source.SourceTrust * 0.05
		}
		items[i] = knowledgeRanked{result: result, blend: blend}
	}
	sort.SliceStable(items, func(i, j int) bool {
		// Band first so a near-tie chain cannot disagree with itself.
		// Inside one band, keep the historical type-and-score order.
		bi := knowledgeBlendBand(items[i].blend)
		bj := knowledgeBlendBand(items[j].blend)
		if bi != bj {
			return bi > bj
		}
		return searchResultLess(items[i].result, items[j].result)
	})
	out := make([]SearchResult, len(items))
	for i := range items {
		out[i] = items[i].result
	}
	return out
}

func knowledgeBlendBand(score float64) int {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return int(math.Floor(score/knowledgeRerankTie + 1e-9))
}
