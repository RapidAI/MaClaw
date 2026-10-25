// Package rerank scores a short candidate list against one query.
// A nil reranker and every failure leave the caller on its fusion order.
// This package does not call a chat model.
package rerank

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// DefaultBudget is the hot-path limit for one local scoring call.
const DefaultBudget = 80 * time.Millisecond

// MaxPassageRunes caps the text a caller hands to a scorer.
const MaxPassageRunes = 512

// Passage is one candidate. Scores come back in this order, not by ID.
type Passage struct {
	ID   string
	Text string
}

// Reranker scores passages for relevance to query.
// The returned slice must have one finite score per passage.
type Reranker interface {
	Rerank(ctx context.Context, query string, passages []Passage) ([]float64, error)
}

// Outcome is a successful score vector. Applied is false when the caller
// should keep its original order.
type Outcome struct {
	Scores  []float64
	Applied bool
}

// Apply runs reranker within budget. A nil reranker, a canceled context,
// a timeout, a length mismatch, or a non-finite score is not applied.
func Apply(ctx context.Context, r Reranker, query string, passages []Passage, budget time.Duration) Outcome {
	if r == nil || len(passages) == 0 {
		return Outcome{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}
	}
	if budget <= 0 {
		budget = DefaultBudget
	}
	cctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	type scored struct {
		scores []float64
		err    error
	}
	done := make(chan scored, 1)
	go func() {
		scores, err := r.Rerank(cctx, query, passages)
		done <- scored{scores: scores, err: err}
	}()

	var result scored
	select {
	case result = <-done:
	case <-cctx.Done():
		// A canceled caller does not take a late score. A deadline only drops
		// the call when the vector is not already buffered.
		if ctx.Err() != nil {
			return Outcome{}
		}
		select {
		case result = <-done:
		default:
			return Outcome{}
		}
	}
	if ctx.Err() != nil {
		return Outcome{}
	}
	if result.err != nil || len(result.scores) != len(passages) {
		return Outcome{}
	}
	for _, score := range result.scores {
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return Outcome{}
		}
	}
	return Outcome{Scores: result.scores, Applied: true}
}

// Truncate keeps at most MaxPassageRunes. The cut is on runes, not bytes.
func Truncate(text string) string {
	return TruncateTo(text, MaxPassageRunes)
}

// TruncateTo keeps at most maxRunes. A non-positive limit returns empty text.
func TruncateTo(text string, maxRunes int) string {
	if text == "" || maxRunes <= 0 {
		return ""
	}
	// A byte length inside the rune budget cannot contain more runes than that.
	if len(text) <= maxRunes {
		return text
	}
	n := 0
	for i := range text {
		if n == maxRunes {
			return text[:i]
		}
		n++
	}
	return text
}

// Prepared holds one query's terms. Build it once for a candidate list.
type Prepared struct {
	terms []queryTerm
}

// Prepare segments the query into dictionary words. Overlapping character
// bigrams are not terms: a pair that crosses a word boundary matches text
// the query did not ask for.
func Prepare(query string) Prepared {
	return Prepared{terms: queryTerms(query)}
}

// Window keeps at most MaxPassageRunes around the query.
func (p Prepared) Window(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= MaxPassageRunes {
		return text
	}
	terms := p.terms
	hits := make([][]int, len(terms))
	type anchor struct {
		term int
		at   int
	}
	var anchors []anchor
	for i, term := range terms {
		found := termHits(runes, term)
		if len(found) == 0 {
			continue
		}
		hits[i] = found
		for _, at := range found {
			anchors = append(anchors, anchor{term: i, at: at})
		}
	}
	if len(anchors) == 0 {
		return string(runes[:MaxPassageRunes])
	}
	bestCov := -1
	bestSpan := 0
	bestLen := 0
	bestAt := 0
	bestStart := 0
	n := len(runes)
	for _, anchor := range anchors {
		termLen := len(terms[anchor.term].runes)
		// Lead-in first. The left edge is the placement that can still hold a
		// second term past the forward room. Equal coverage keeps the lead-in.
		starts := [2]int{windowStart(anchor.at, n), alignedStart(anchor.at, n)}
		for i, start := range starts {
			if i == 1 && start == starts[0] {
				continue
			}
			cov, span := coveredSpan(hits, terms, start, start+MaxPassageRunes)
			if cov > bestCov || (cov == bestCov && (span < bestSpan || (span == bestSpan && (termLen > bestLen || (termLen == bestLen && anchor.at < bestAt))))) {
				bestCov = cov
				bestSpan = span
				bestLen = termLen
				bestAt = anchor.at
				bestStart = start
			}
		}
	}
	bestStart = refineStart(hits, terms, bestAt, bestLen, bestStart, bestCov, n)
	return string(runes[bestStart : bestStart+MaxPassageRunes])
}

// Window keeps at most MaxPassageRunes around the query. The retrieval
// snippet can be a whole card or node, so a prefix cut drops the match that
// put the row in the pool. The chosen span covers the most query terms.
// A hit keeps a quarter-passage lead-in when that covers as many terms.
// Between that lead-in and the left edge, the start slides toward the
// lead-in while the winning hit and the same coverage still fit. With no
// hit, the leading runes stay.
func Window(text, query string) string {
	return Prepare(query).Window(text)
}

func windowStart(at, n int) int {
	start := at - MaxPassageRunes/4
	if start < 0 {
		start = 0
	}
	if start > n-MaxPassageRunes {
		start = n - MaxPassageRunes
	}
	return start
}

// alignedStart puts the hit on the left edge. windowStart leaves only three
// quarters of the passage after the hit, so two terms that still fit in
// MaxPassageRunes can fall in different biased spans.
func alignedStart(at, n int) int {
	if at > n-MaxPassageRunes {
		return n - MaxPassageRunes
	}
	return at
}

// refineStart moves a winning passage toward the quarter-passage lead-in.
// The left edge keeps every term, but it drops context in front of the
// cluster even when a start between the two placements still covers them.
func refineStart(hits [][]int, terms []queryTerm, at, termLen, known, bestCov, n int) int {
	preferred := windowStart(at, n)
	if preferred >= known {
		return known
	}
	if coveredTerms(hits, terms, preferred, preferred+MaxPassageRunes) == bestCov &&
		anchorInSpan(at, termLen, preferred) {
		return preferred
	}
	best := known
	for i, found := range hits {
		length := len(terms[i].runes)
		for _, h := range found {
			s := h + length - MaxPassageRunes
			if s <= preferred || s >= best || s > n-MaxPassageRunes {
				continue
			}
			if !anchorInSpan(at, termLen, s) || coveredTerms(hits, terms, s, s+MaxPassageRunes) != bestCov {
				continue
			}
			best = s
		}
	}
	return best
}

func anchorInSpan(at, termLen, start int) bool {
	return at >= start && at+termLen <= start+MaxPassageRunes
}

type queryTerm struct {
	runes []rune
	word  bool
}

func queryTerms(query string) []queryTerm {
	segs := bm25.Segment(query)
	phrases := cjkPhrases(query)
	multi := len(phrases) > 0
	for _, tok := range segs {
		if len([]rune(tok)) >= 2 {
			multi = true
			break
		}
	}
	seen := make(map[string]struct{})
	var terms []queryTerm
	add := func(token string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		runes := []rune(token)
		// A single CJK fragment is noise once a longer word exists ("要", "奇").
		// A single ASCII token is itself a word ("R", "C") and must still match.
		if len(runes) == 1 && isCJKQueryRune(runes[0]) && multi {
			return
		}
		seen[token] = struct{}{}
		word := !isCJKQueryRune(runes[0])
		terms = append(terms, queryTerm{runes: runes, word: word})
	}
	for _, tok := range segs {
		add(tok)
	}
	for _, phrase := range phrases {
		add(phrase)
	}
	return terms
}

// cjkPhrases keeps each intact CJK run that can sit inside one passage.
// Dictionary cuts of a longer name are fragments; the intact run outranks a
// bag of those fragments. A run longer than the passage cannot be covered.
func cjkPhrases(query string) []string {
	var phrases []string
	var run []rune
	flush := func() {
		if len(run) >= 2 && len(run) <= MaxPassageRunes {
			phrases = append(phrases, string(run))
		}
		run = nil
	}
	for _, r := range query {
		if isCJKQueryRune(r) {
			run = append(run, unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return phrases
}

// maxTermHits caps how many occurrences of one term become window anchors.
// Every hit is kept up to this cap, including those in the middle. Beyond it,
// evenly spaced samples still cover the whole text.
const maxTermHits = 256

func termHits(text []rune, term queryTerm) []int {
	if len(term.runes) == 0 || len(term.runes) > len(text) {
		return nil
	}
	var hits []int
	for i := 0; i+len(term.runes) <= len(text); i++ {
		if matchTermAt(text, i, term) {
			hits = append(hits, i)
		}
	}
	if len(hits) <= maxTermHits {
		return hits
	}
	sampled := make([]int, maxTermHits)
	last := len(hits) - 1
	for i := range sampled {
		sampled[i] = hits[i*last/(maxTermHits-1)]
	}
	return sampled
}

func coveredTerms(hits [][]int, terms []queryTerm, start, end int) int {
	cov, _ := coveredSpan(hits, terms, start, end)
	return cov
}

// coveredSpan reports how many terms land in [start, end) and how far the
// outermost of those hits stretches. Equal coverage keeps the tighter stretch
// so an earlier loose pair does not beat a later adjacent pair.
func coveredSpan(hits [][]int, terms []queryTerm, start, end int) (cov, span int) {
	lo := end
	hi := start
	for i, term := range terms {
		termLo, termHi, ok := termExtent(hits[i], start, end, len(term.runes))
		if !ok {
			continue
		}
		cov++
		if termLo < lo {
			lo = termLo
		}
		if termHi > hi {
			hi = termHi
		}
	}
	if cov == 0 {
		return 0, 0
	}
	return cov, hi - lo
}

func termExtent(hits []int, start, end, termLen int) (lo, hi int, ok bool) {
	i := sort.Search(len(hits), func(i int) bool { return hits[i] >= start })
	if i >= len(hits) || hits[i]+termLen > end {
		return 0, 0, false
	}
	lo = hits[i]
	hi = hits[i] + termLen
	for i++; i < len(hits); i++ {
		if hits[i] >= end || hits[i]+termLen > end {
			break
		}
		hi = hits[i] + termLen
	}
	return lo, hi, true
}

func matchTermAt(text []rune, i int, term queryTerm) bool {
	if term.word {
		if i > 0 && isWordRune(text[i-1]) {
			return false
		}
		end := i + len(term.runes)
		if end < len(text) && isWordRune(text[end]) {
			return false
		}
	}
	for j, r := range term.runes {
		if unicode.ToLower(text[i+j]) != r {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	if isCJKQueryRune(r) {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isCJKQueryRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai)
}

// MinMax scales scores onto [0, 1]. A flat vector becomes zeros so a bonus
// cannot invent an order the scorer did not express.
func MinMax(scores []float64) []float64 {
	out := make([]float64, len(scores))
	if len(scores) == 0 {
		return out
	}
	lo, hi := scores[0], scores[0]
	for _, score := range scores[1:] {
		if score < lo {
			lo = score
		}
		if score > hi {
			hi = score
		}
	}
	span := hi - lo
	if span == 0 {
		return out
	}
	for i, score := range scores {
		out[i] = (score - lo) / span
	}
	return out
}
