// Package bm25 provides a reusable in-memory BM25 index with gse-based
// Chinese/English tokenization.
package bm25

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

// global gse segmenter (initialized once)
var (
	seg     gse.Segmenter
	segOnce sync.Once
	segDone chan struct{} // closed when dictionary loading finishes
	// segMu serializes Cut: gse's DAG/HMM paths use package-level Segmenter state
	// that is not safe for concurrent Cut from parallel import FTS prep.
	segMu sync.Mutex
)

func init() {
	segDone = make(chan struct{})
}

// PrewarmDict starts loading the gse dictionary in a background goroutine.
// Call this early at startup so the dictionary is ready by the time the first
// BM25 query arrives. Safe to call multiple times (sync.Once guarded).
func PrewarmDict() {
	go initSeg()
}

func initSeg() {
	segOnce.Do(func() {
		// Suppress gse's verbose "Load the gse dictionary" log output.
		origOutput := log.Writer()
		log.SetOutput(io.Discard)
		seg.LoadDict()
		log.SetOutput(origOutput)
		close(segDone)
	})
}

// waitSeg blocks until the gse dictionary is loaded.
func waitSeg() {
	initSeg() // ensure started
	<-segDone
}

// Doc represents a document in the index.
type Doc struct {
	ID   string
	Text string // combined text to index
}

// Index is a thread-safe in-memory BM25 index.
type Index struct {
	mu       sync.RWMutex
	docs     []indexedDoc
	docIndex map[string]int
	df       map[string]int
	avgDL    float64
	k1       float64
	b        float64
	docsHash string // hash of the last Rebuild input, used by RebuildIfChanged
}

type indexedDoc struct {
	id     string
	tf     map[string]int
	length int
}

// New creates an empty BM25 index with standard parameters.
func New() *Index {
	waitSeg()
	return &Index{k1: 1.2, b: 0.75}
}

// Rebuild reconstructs the entire index from a slice of documents.
func (idx *Index) Rebuild(docs []Doc) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.rebuildLocked(docs)
	idx.docsHash = hashDocs(docs)
}

// RebuildIfChanged rebuilds the index only if the docs have changed since the
// last Rebuild/RebuildIfChanged call. Returns true if a rebuild occurred.
func (idx *Index) RebuildIfChanged(docs []Doc) bool {
	h := hashDocs(docs)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if h == idx.docsHash {
		return false
	}
	idx.rebuildLocked(docs)
	idx.docsHash = h
	return true
}

func (idx *Index) rebuildLocked(docs []Doc) {
	idx.docs = make([]indexedDoc, len(docs))
	idx.docIndex = make(map[string]int, len(docs))
	totalLen := 0
	for i, d := range docs {
		doc := tokenizeDoc(d)
		idx.docs[i] = doc
		idx.docIndex[doc.id] = i
		totalLen += doc.length
	}
	if len(docs) > 0 {
		idx.avgDL = float64(totalLen) / float64(len(docs))
	} else {
		idx.avgDL = 1
	}
	idx.recalcDocFreqLocked()
}

// Add appends a single document to the index.
func (idx *Index) Add(d Doc) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc := tokenizeDoc(d)
	if idx.docIndex == nil {
		idx.docIndex = make(map[string]int, len(idx.docs)+1)
		for i, existing := range idx.docs {
			idx.docIndex[existing.id] = i
		}
	}
	idx.docIndex[doc.id] = len(idx.docs)
	idx.docs = append(idx.docs, doc)
	idx.recalcAvgDL()
	idx.recalcDocFreqLocked()
	idx.docsHash = "" // invalidate cache
}

// Remove removes a document by ID.
func (idx *Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for i, d := range idx.docs {
		if d.id == id {
			idx.docs = append(idx.docs[:i], idx.docs[i+1:]...)
			idx.rebuildDocIndexLocked()
			idx.recalcAvgDL()
			idx.docsHash = "" // invalidate cache
			return
		}
	}
}

// Update replaces a document in the index. If not found, appends it.
func (idx *Index) Update(d Doc) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc := tokenizeDoc(d)
	for i, existing := range idx.docs {
		if existing.id == d.ID {
			idx.docs[i] = doc
			if idx.docIndex == nil {
				idx.rebuildDocIndexLocked()
			} else {
				idx.docIndex[doc.id] = i
			}
			idx.recalcAvgDL()
			idx.docsHash = "" // invalidate cache
			return
		}
	}
	idx.docs = append(idx.docs, doc)
	idx.recalcAvgDL()
	idx.recalcDocFreqLocked()
	idx.docsHash = "" // invalidate cache
}

// Score computes BM25 scores for all documents against the query string.
// Returns a map of document ID → BM25 score (only positive scores included).
func (idx *Index) Score(query string) map[string]float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.scoreQueryLocked(query, nil)
}

// ScoreWithTokens is like Score but accepts pre-tokenized query tokens.
// This avoids re-tokenizing the same query when both Score and external code
// need the tokens (e.g. experience matching in Router).
func (idx *Index) ScoreWithTokens(queryTokens []string) map[string]float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.docs) == 0 || len(queryTokens) == 0 {
		return nil
	}

	// Deduplicate query tokens.
	seen := make(map[string]struct{}, len(queryTokens))
	unique := make([]string, 0, len(queryTokens))
	for _, qt := range queryTokens {
		if _, ok := seen[qt]; !ok {
			seen[qt] = struct{}{}
			unique = append(unique, qt)
		}
	}

	n := float64(len(idx.docs))
	idf := make(map[string]float64, len(unique))
	for _, term := range unique {
		freq := idx.df[term]
		if freq == 0 {
			continue
		}
		idf[term] = math.Log((n-float64(freq)+0.5)/(float64(freq)+0.5) + 1.0)
	}

	scores := make(map[string]float64, len(idx.docs))
	for _, doc := range idx.docs {
		var s float64
		dl := float64(doc.length)
		for _, qt := range unique {
			tfVal := float64(doc.tf[qt])
			if tfVal == 0 {
				continue
			}
			num := tfVal * (idx.k1 + 1)
			denom := tfVal + idx.k1*(1-idx.b+idx.b*dl/idx.avgDL)
			s += idf[qt] * num / denom
		}
		if s > 0 {
			scores[doc.id] = s
		}
	}
	return scores
}

// ScoreSubset computes BM25 scores only for documents whose IDs are in allowed.
// Document frequency and average length still use the full index so scores stay
// comparable with Score(); only the final scoring loop is pruned.
func (idx *Index) ScoreSubset(query string, allowed map[string]struct{}) map[string]float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if len(allowed) == 0 {
		return nil
	}
	return idx.scoreQueryLocked(query, allowed)
}

// scoreQueryLocked scores query against the index. allowed nil scores every
// document. A strict name is a conjunction of its real spans, so overlapping
// n-grams cannot rank a different person who only shares 中国人 or 奇安信.
func (idx *Index) scoreQueryLocked(query string, allowed map[string]struct{}) map[string]float64 {
	if len(idx.docs) == 0 {
		return nil
	}
	terms, requireAll := scoreQueryTerms(query)
	if len(terms) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	idf := make(map[string]float64, len(terms))
	for _, term := range terms {
		freq := idx.df[term]
		if freq == 0 {
			if requireAll {
				return map[string]float64{}
			}
			continue
		}
		idf[term] = math.Log((n-float64(freq)+0.5)/(float64(freq)+0.5) + 1.0)
	}

	scoreDoc := func(doc indexedDoc) float64 {
		if requireAll {
			for _, term := range terms {
				if doc.tf[term] == 0 {
					return 0
				}
			}
		}
		var s float64
		dl := float64(doc.length)
		for _, term := range terms {
			tfVal := float64(doc.tf[term])
			if tfVal == 0 {
				continue
			}
			num := tfVal * (idx.k1 + 1)
			denom := tfVal + idx.k1*(1-idx.b+idx.b*dl/idx.avgDL)
			s += idf[term] * num / denom
		}
		return s
	}

	if allowed == nil {
		scores := make(map[string]float64)
		for _, doc := range idx.docs {
			if s := scoreDoc(doc); s > 0 {
				scores[doc.id] = s
			}
		}
		return scores
	}

	scores := make(map[string]float64, len(allowed))
	docIndex := idx.docIndex
	if docIndex == nil {
		docIndex = make(map[string]int, len(idx.docs))
		for i, doc := range idx.docs {
			docIndex[doc.id] = i
		}
	}
	for id := range allowed {
		docPos, ok := docIndex[id]
		if !ok || docPos < 0 || docPos >= len(idx.docs) {
			continue
		}
		if s := scoreDoc(idx.docs[docPos]); s > 0 {
			scores[id] = s
		}
	}
	return scores
}

// scoreQueryTerms returns the terms to score. Strict names contribute only
// their content anchors and every anchor must hit. Other queries keep the
// overlapping n-gram recall used for ordinary words.
func scoreQueryTerms(query string) (terms []string, requireAll bool) {
	if anchors, strict := QueryAnchors(query); strict && len(anchors) > 0 {
		return uniqueTerms(anchors), true
	}
	return uniqueTerms(Tokenize(query)), false
}

func uniqueTerms(tokens []string) []string {
	seen := make(map[string]struct{}, len(tokens))
	unique := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		unique = append(unique, token)
	}
	return unique
}

func (idx *Index) rebuildDocIndexLocked() {
	idx.docIndex = make(map[string]int, len(idx.docs))
	for i, doc := range idx.docs {
		idx.docIndex[doc.id] = i
	}
}

func (idx *Index) recalcDocFreqLocked() {
	idx.df = make(map[string]int)
	for _, doc := range idx.docs {
		for term := range doc.tf {
			idx.df[term]++
		}
	}
}

func (idx *Index) recalcAvgDL() {
	if len(idx.docs) == 0 {
		idx.avgDL = 1
		return
	}
	total := 0
	for _, d := range idx.docs {
		total += d.length
	}
	idx.avgDL = float64(total) / float64(len(idx.docs))
}

// ---------------------------------------------------------------------------
// Tokenization (exported for reuse)
// ---------------------------------------------------------------------------

// Tokenize splits text into lowercase tokens using gse for CJK and simple
// splitting for Latin scripts. Punctuation-only tokens are discarded.
// CJK runs also receive overlapping character n-grams so an index can recall
// words the dictionary missed. Those n-grams are not identity evidence; use
// ContentAnchors for that.
func Tokenize(text string) []string {
	tokens := Segment(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return addCJKFallbackNgrams(tokens, strings.ToLower(text))
}

// Segment returns dictionary and HMM cuts only. It does not add overlapping
// character n-grams.
func Segment(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	waitSeg()

	lower := strings.ToLower(text)
	segMu.Lock()
	segments := seg.Cut(lower, true)
	segMu.Unlock()

	var tokens []string
	for _, s := range segments {
		s = strings.TrimSpace(s)
		if s == "" || isAllPunct(s) {
			continue
		}
		tokens = append(tokens, s)
	}
	return tokens
}

// ShortEntityMention reports whether text is a short Han name or title rather
// than a question or a mixed sentence. Fragment overlap is not enough to
// identify this kind of query with a stored person.
func ShortEntityMention(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "?？") {
		return false
	}
	han := 0
	other := 0
	for _, r := range text {
		switch {
		case isCJKUnified(r):
			han++
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
		default:
			other++
		}
	}
	// Shape check first so English and long text never pay for segmentation.
	if han < 2 || han > 16 || other != 0 {
		return false
	}
	return !hasQuestionCue(text)
}

// hasQuestionCue reports interrogative wording. Single-character cues count
// only as their own word or a final particle, so 几何 stays a noun while
// 马勇是谁 and 马勇有几本书 stay questions.
func hasQuestionCue(text string) bool {
	for _, cue := range []string{"什么", "怎么", "如何", "哪些", "为什么", "为何", "是否", "有没有"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 0 {
		switch runes[len(runes)-1] {
		case '吗', '呢', '吧', '啊':
			return true
		}
	}
	for _, seg := range Segment(text) {
		switch seg {
		case "吗", "呢", "谁", "哪", "几", "啥", "咋":
			return true
		}
		if countSegment(seg) {
			return true
		}
	}
	return false
}

// countSegment reports a how-many phrase such as 几本 or 几本书. 几何 is a
// noun and must not match.
func countSegment(seg string) bool {
	runes := []rune(seg)
	if len(runes) < 2 || runes[0] != '几' {
		return false
	}
	switch runes[1] {
	case '个', '本', '种', '次', '条', '项', '位', '名', '岁', '点', '号', '张', '件':
		return true
	}
	return false
}

// ContentAnchors returns the non-overlapping spans a short entity query must
// actually contain. Dictionary words stay whole. Consecutive unknown Han
// characters are joined, so 奇强 remains one span instead of the separate
// characters 奇 and 强. Overlapping n-grams are intentionally absent: a
// document that contains 中国人 and 奇安信 does not contain 中国人奇强.
func ContentAnchors(text string) []string {
	anchors, _ := contentAnchors(text)
	return anchors
}

// QueryAnchors classifies a short Han query. strict is set when the query is a
// composite name or an out-of-vocabulary span. A single dictionary word such
// as 学历 is not strict: semantic recall may still relate it to a synonym.
// Callers must not fall back to overlapping character OR when strict is true.
func QueryAnchors(text string) (anchors []string, strict bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	if cached, ok := loadQueryAnchorCache(text); ok {
		return cached.anchors, cached.strict
	}
	anchors, strict = classifyQueryAnchors(text)
	storeQueryAnchorCache(text, queryAnchorEntry{anchors: anchors, strict: strict})
	return anchors, strict
}

func classifyQueryAnchors(text string) (anchors []string, strict bool) {
	if !ShortEntityMention(text) {
		return nil, false
	}
	anchors, mergedOOV := contentAnchors(text)
	if len(anchors) == 0 {
		for _, run := range cjkRuns(text) {
			if len([]rune(run)) >= 2 {
				anchors = append(anchors, run)
			}
		}
		return anchors, len(anchors) > 0
	}
	if len(anchors) > 1 || mergedOOV {
		return anchors, true
	}
	// A single span of four or more characters is a name or title, not a
	// common word. Require that span itself instead of its character pieces.
	return anchors, len([]rune(anchors[0])) >= 4
}

type queryAnchorEntry struct {
	anchors []string
	strict  bool
}

var queryAnchorCache = struct {
	sync.Mutex
	items map[string]queryAnchorEntry
}{items: map[string]queryAnchorEntry{}}

func loadQueryAnchorCache(text string) (queryAnchorEntry, bool) {
	queryAnchorCache.Lock()
	defer queryAnchorCache.Unlock()
	entry, ok := queryAnchorCache.items[text]
	if !ok {
		return queryAnchorEntry{}, false
	}
	entry.anchors = append([]string(nil), entry.anchors...)
	return entry, true
}

func storeQueryAnchorCache(text string, entry queryAnchorEntry) {
	entry.anchors = append([]string(nil), entry.anchors...)
	queryAnchorCache.Lock()
	defer queryAnchorCache.Unlock()
	if len(queryAnchorCache.items) >= 128 {
		queryAnchorCache.items = map[string]queryAnchorEntry{}
	}
	queryAnchorCache.items[text] = entry
}

func contentAnchors(text string) ([]string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	var anchors []string
	mergedOOV := false
	seen := make(map[string]struct{})
	add := func(span string, merged bool) {
		span = strings.TrimSpace(span)
		if len([]rune(span)) < 2 || hanSpanIsStop(span) {
			return
		}
		if _, ok := seen[span]; ok {
			return
		}
		seen[span] = struct{}{}
		anchors = append(anchors, span)
		if merged {
			mergedOOV = true
		}
	}
	for _, run := range cjkRuns(text) {
		segments := Segment(run)
		if len(segments) == 0 {
			add(run, true)
			continue
		}
		rest := run
		var pending []rune
		flush := func() {
			if len(pending) >= 2 {
				add(string(pending), true)
			}
			pending = nil
		}
		for _, seg := range segments {
			idx := strings.Index(rest, seg)
			if idx < 0 {
				flush()
				continue
			}
			for _, r := range rest[:idx] {
				if isCJKUnified(r) && !hanStop(r) {
					pending = append(pending, r)
					continue
				}
				flush()
			}
			rest = rest[idx+len(seg):]
			if len([]rune(seg)) >= 2 {
				flush()
				add(seg, false)
				continue
			}
			for _, r := range seg {
				if !isCJKUnified(r) || hanStop(r) {
					flush()
					continue
				}
				pending = append(pending, r)
			}
		}
		for _, r := range rest {
			if isCJKUnified(r) && !hanStop(r) {
				pending = append(pending, r)
				continue
			}
			flush()
		}
		flush()
	}
	return anchors, mergedOOV
}

func hanStop(r rune) bool {
	switch r {
	case '的', '了', '是', '在', '有', '和', '与', '或', '不', '也',
		'都', '就', '而', '及', '等', '这', '那', '你', '我', '他',
		'她', '它', '们', '个', '为', '到', '把', '被', '让', '从',
		'对', '吗', '呢', '吧', '啊', '哦':
		return true
	}
	return false
}

func hanSpanIsStop(span string) bool {
	saw := false
	for _, r := range span {
		if !isCJKUnified(r) {
			return false
		}
		if !hanStop(r) {
			return false
		}
		saw = true
	}
	return saw
}

func addCJKFallbackNgrams(tokens []string, text string) []string {
	seen := make(map[string]struct{}, len(tokens)+8)
	out := make([]string, 0, len(tokens)+8)
	add := func(token string) {
		if token == "" || isAllPunct(token) {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	for _, token := range tokens {
		add(token)
	}

	for _, run := range cjkRuns(text) {
		runes := []rune(run)
		if len(runes) < 3 || len(runes) > 24 {
			continue
		}
		for n := 2; n <= 3; n++ {
			for i := 0; i+n <= len(runes); i++ {
				add(string(runes[i : i+n]))
			}
		}
	}
	return out
}

func cjkRuns(text string) []string {
	var runs []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, string(current))
			current = nil
		}
	}
	for _, r := range text {
		if isCJKUnified(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func isCJKUnified(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff
}

func tokenizeDoc(d Doc) indexedDoc {
	tokens := Tokenize(d.Text)
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return indexedDoc{id: d.ID, tf: tf, length: len(tokens)}
}

func isAllPunct(s string) bool {
	for _, r := range s {
		if !unicode.IsPunct(r) && !unicode.IsSymbol(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// hashDocs computes a deterministic hash over a slice of Docs.
// Docs are sorted by ID first to ensure order-independence.
func hashDocs(docs []Doc) string {
	if len(docs) == 0 {
		return ""
	}
	sorted := make([]Doc, len(docs))
	copy(sorted, docs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	h := sha256.New()
	for _, d := range sorted {
		h.Write([]byte(d.ID))
		h.Write([]byte{0})
		h.Write([]byte(d.Text))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
