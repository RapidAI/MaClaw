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

	if len(idx.docs) == 0 {
		return nil
	}

	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
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

	if len(idx.docs) == 0 || len(allowed) == 0 {
		return nil
	}

	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

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
		doc := idx.docs[docPos]
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
func Tokenize(text string) []string {
	if text == "" {
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
	return addCJKFallbackNgrams(tokens, lower)
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
