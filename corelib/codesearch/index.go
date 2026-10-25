// Package codesearch ranks source chunks for natural-language code search.
// Declaration chunks, BM25, and a small code-aware rerank answer "where is
// this behavior" queries. Exact regex search and symbol graphs stay separate.
package codesearch

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

const (
	defaultTopK      = 8
	maxTopK          = 20
	candidateLimit   = 50
	defaultWait      = 1500 * time.Millisecond
	maxResultBytes   = 8000
	definitionBoost  = 0.75
	fileCoherence    = 0.35
	noiseScale       = 0.6
	warmingFreshness = "warming"
	indexedFreshness = "indexed"
	warmingHint      = "索引还在构建。这一轮改用 code_navigation 或 ripgrep。"
	symbolHint       = "下一步用 code_navigation，operation 用 node 或 callers。"
)

var symbolQuery = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:::[A-Za-z_][A-Za-z0-9_]*)?$`)

// Request is one code_search lookup. Path is a slash-separated directory or
// file inside Root. Wait is how long this call blocks for a cold index.
type Request struct {
	Root  string
	Path  string
	Query string
	TopK  int
	Wait  time.Duration
}

// Hit is one ranked chunk.
type Hit struct {
	Path      string  `json:"path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Symbol    string  `json:"symbol,omitempty"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
}

// Result is the tool payload. Warming and an indexed miss are different:
// warming has a hint and no hits, an indexed miss has freshness indexed.
type Result struct {
	Freshness    string `json:"freshness"`
	Scoped       bool   `json:"scoped"`
	IndexedFiles int    `json:"indexed_files"`
	Hint         string `json:"hint,omitempty"`
	Hits         []Hit  `json:"hits"`
	OmittedHits  int    `json:"omitted_hits,omitempty"`
}

// Encode serializes a result and drops trailing hits until the JSON fits.
func Encode(res Result) string {
	if res.Hits == nil {
		res.Hits = []Hit{}
	}
	raw := func() string {
		b, err := json.Marshal(res)
		if err != nil {
			return `{"freshness":"indexed","scoped":false,"indexed_files":0,"hits":[]}`
		}
		return string(b)
	}
	for len(res.Hits) > 1 && len(raw()) > maxResultBytes {
		res.Hits = res.Hits[:len(res.Hits)-1]
		res.OmittedHits++
	}
	if len(res.Hits) == 1 && len(raw()) > maxResultBytes {
		hit := res.Hits[0]
		hit.Snippet = cutRunes(hit.Snippet, 200)
		res.Hits = []Hit{hit}
	}
	return raw()
}

var (
	storeMu     sync.Mutex
	corpora     = map[string]*corpus{}
	builds      = map[string]*buildSlot{}
	dirtyNotes  = map[string]time.Time{}
	maxFiles    atomic.Int32
	buildPause  func()
	pauseMu     sync.Mutex
	reconcileOn sync.Once
	// testIndexBuilds counts BM25 rebuilds so a test can see a deleted file
	// stop invalidating the corpus.
	testIndexBuilds atomic.Int32
)

func init() {
	maxFiles.Store(100000)
	reconcileOn.Do(func() {
		go reconcileLoop()
	})
}

// SetMaxFilesForTest caps how many source files one corpus keeps.
func SetMaxFilesForTest(n int) int {
	prev := int(maxFiles.Swap(int32(n)))
	return prev
}

// SetBuildPauseForTest runs fn at the start of each index build. Nil clears it.
func SetBuildPauseForTest(fn func()) {
	pauseMu.Lock()
	buildPause = fn
	pauseMu.Unlock()
}

type buildSlot struct {
	done chan struct{}
}

type fileSnap struct {
	size   int64
	mod    time.Time
	readAt time.Time
	// gone keeps a deletion visible to the next search. Without it, a consumed
	// dirty note has no readAt to compare and every later search rebuilds.
	gone bool
}

type chunk struct {
	id     string
	path   string
	start  int
	end    int
	symbol string
	text   string
	stems  []string
	noise  bool
}

type corpus struct {
	mu        sync.Mutex
	gen       uint64
	root      string
	sub       string
	files     int
	truncated bool
	scoped    bool
	rules     []rule
	chunks    []chunk
	meta      map[string]fileSnap
	dirs      map[string]time.Time
	index     *bm25.Index
	used      atomic.Int64
}

// noteKey folds Windows paths so a write recorded as D:\Repo\A.go and a corpus
// rooted at d:\repo share one dirty note. ReadFile accepts the folded path.
func noteKey(abs string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(abs)
	}
	return abs
}

// Warm starts an index build for a local project and marks a finished corpus
// as used. It does not wait. A coding session calls it when the project is
// bound, and again while the session is still running, so a later search is
// not the first time the tree is read and the 15-minute idle eviction does
// not drop a project the session is still using.
func Warm(root string) {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	key := rootKey(root)
	storeMu.Lock()
	if c := corpora[key]; c != nil {
		c.used.Store(time.Now().UnixNano())
		storeMu.Unlock()
		return
	}
	startBuildLocked(key, root, "", false)
	storeMu.Unlock()
}

// KeepWarm builds root in the background and refreshes its idle clock until
// stop is called. stop is safe to call more than once. An empty or missing
// root does nothing and returns a stop that is also safe to call.
func KeepWarm(root string) (stop func()) {
	noop := func() {}
	root = strings.TrimSpace(root)
	if root == "" {
		return noop
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return noop
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return noop
	}
	Warm(abs)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(warmHoldInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				Warm(abs)
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// NoteDirty marks a file this process wrote. The next search of a corpus that
// contains it reloads that file instead of rebuilding the tree.
func NoteDirty(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	abs = noteKey(filepath.Clean(abs))
	storeMu.Lock()
	dirtyNotes[abs] = time.Now()
	if len(dirtyNotes) > 8192 {
		pruneNotesLocked()
	}
	storeMu.Unlock()
}

func pruneNotesLocked() {
	const capN = 8192
	cutoff := time.Now().Add(-10 * time.Minute)
	for p, noted := range dirtyNotes {
		if noted.Before(cutoff) {
			delete(dirtyNotes, p)
		}
	}
	if len(dirtyNotes) <= capN {
		return
	}
	type pair struct {
		p string
		t time.Time
	}
	all := make([]pair, 0, len(dirtyNotes))
	for p, noted := range dirtyNotes {
		all = append(all, pair{p: p, t: noted})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	drop := len(all) - capN
	for i := 0; i < drop; i++ {
		delete(dirtyNotes, all[i].p)
	}
}

// Search ranks chunks for query. A cold corpus waits at most Wait, then
// returns freshness warming so the caller can use another tool this turn.
func Search(req Request) Result {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return Result{Freshness: indexedFreshness, Hits: []Hit{}}
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return Result{Freshness: indexedFreshness, Hits: []Hit{}}
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Result{Freshness: indexedFreshness, Hits: []Hit{}}
	}
	sub := cleanSub(req.Path)
	wait := req.Wait
	if wait <= 0 {
		wait = defaultWait
	}
	top := req.TopK
	if top <= 0 {
		top = defaultTopK
	}
	if top > maxTopK {
		top = maxTopK
	}
	key := rootKey(root)
	c, warming := ensure(key, root, "", false, wait)
	if warming || c == nil {
		return warmingResult()
	}
	if c.truncated && sub != "" {
		skey := key + "\x00" + foldPathKey(sub)
		sc, warming := ensure(skey, root, sub, true, wait)
		if warming || sc == nil {
			return warmingResult()
		}
		c = sc
	}
	c.used.Store(time.Now().UnixNano())
	applyDirty(c)
	return rank(c, query, sub, top)
}

func warmingResult() Result {
	return Result{Freshness: warmingFreshness, Hits: []Hit{}, Hint: warmingHint}
}

func cleanSub(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.Trim(p, "/")
	if p == "." {
		return ""
	}
	return p
}

func rootKey(root string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(root)
	}
	return root
}

func ensure(key, root, sub string, scoped bool, wait time.Duration) (*corpus, bool) {
	storeMu.Lock()
	if c := corpora[key]; c != nil {
		storeMu.Unlock()
		return c, false
	}
	slot := startBuildLocked(key, root, sub, scoped)
	storeMu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-slot.done:
		storeMu.Lock()
		c := corpora[key]
		storeMu.Unlock()
		if c == nil {
			return nil, true
		}
		return c, false
	case <-timer.C:
		return nil, true
	}
}

// startBuildLocked launches the one build for key. The caller holds storeMu.
func startBuildLocked(key, root, sub string, scoped bool) *buildSlot {
	if slot := builds[key]; slot != nil {
		return slot
	}
	slot := &buildSlot{done: make(chan struct{})}
	builds[key] = slot
	go build(key, root, sub, scoped, slot)
	return slot
}

func currentPause() func() {
	pauseMu.Lock()
	defer pauseMu.Unlock()
	return buildPause
}

func build(key, root, sub string, scoped bool, slot *buildSlot) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("codesearch: index build failed for %s: %v", root, rec)
		}
		storeMu.Lock()
		if builds[key] == slot {
			delete(builds, key)
		}
		storeMu.Unlock()
		close(slot.done)
	}()
	if fn := currentPause(); fn != nil {
		fn()
	}
	c := buildCorpus(root, sub, scoped)
	applyDirty(c)
	c.used.Store(time.Now().UnixNano())
	storeMu.Lock()
	corpora[key] = c
	storeMu.Unlock()
}

func buildCorpus(root, sub string, scoped bool) *corpus {
	limit := int(maxFiles.Load())
	files, rules, dirs, truncated := collectFiles(root, sub, limit)
	c := &corpus{
		root:      root,
		sub:       sub,
		files:     len(files),
		truncated: truncated,
		scoped:    scoped || truncated,
		rules:     rules,
		meta:      map[string]fileSnap{},
		dirs:      dirs,
	}
	now := time.Now()
	for _, file := range files {
		c.meta[file.rel] = fileSnap{size: file.size, mod: file.mod, readAt: now}
		c.chunks = append(c.chunks, chunksFrom(file.rel, file.body)...)
	}
	c.index = indexChunks(c.chunks)
	return c
}

func chunksFrom(rel, body string) []chunk {
	raw := chunkSource(rel, body)
	out := make([]chunk, 0, len(raw))
	for _, item := range raw {
		if strings.TrimSpace(item.text) == "" {
			continue
		}
		id := item.path + "#" + itoaSmall(item.start)
		out = append(out, chunk{
			id:     id,
			path:   item.path,
			start:  item.start,
			end:    item.end,
			symbol: item.symbol,
			text:   item.text,
			stems:  bm25.Tokenize(expandIdentifiers(item.symbol)),
			noise:  noisePath(item.path),
		})
	}
	return out
}

func indexChunks(chunks []chunk) *bm25.Index {
	testIndexBuilds.Add(1)
	docs := make([]bm25.Doc, 0, len(chunks))
	seen := map[string]int{}
	for i := range chunks {
		id := chunks[i].id
		if _, ok := seen[id]; ok {
			id = id + "#" + itoaSmall(i)
			chunks[i].id = id
		}
		seen[id] = i
		docs = append(docs, bm25.Doc{ID: id, Text: expandIdentifiers(chunks[i].symbol + "\n" + chunks[i].text)})
	}
	idx := bm25.New()
	idx.Rebuild(docs)
	return idx
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func noisePath(rel string) bool {
	slash := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasSuffix(slash, "_test.go") || strings.HasSuffix(slash, ".d.ts") {
		return true
	}
	for _, seg := range strings.Split(slash, "/") {
		switch seg {
		case "testdata", "examples", "legacy", "compat":
			return true
		}
	}
	return false
}

type dirtyNote struct {
	abs   string
	rel   string
	noted time.Time
}

func applyDirty(c *corpus) {
	if c == nil {
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		if !applyDirtyOnce(c) {
			return
		}
	}
}

// applyDirtyOnce reloads noted files. It returns true when a newer corpus
// generation won the swap and the caller should retry.
func applyDirtyOnce(c *corpus) bool {
	c.mu.Lock()
	gen := c.gen
	root := c.root
	sub := c.sub
	rules := c.rules
	chunks := append([]chunk(nil), c.chunks...)
	meta := copyMeta(c.meta)
	c.mu.Unlock()

	notes := notesFor(root, meta)
	if len(notes) == 0 {
		return false
	}
	indexChanged := false
	metaChanged := false
	for _, n := range notes {
		// Notes are stored for the whole root. A scope corpus must not read
		// or rebuild for a file outside that scope; doing so pulls the file
		// into this index and changes every score.
		if sub != "" && !underPath(n.rel, sub) {
			continue
		}
		key, snap, found := lookupSnap(meta, n.rel)
		stamp := noteStamp(time.Now(), n.noted)
		info, indexable := indexableInfo(n.abs, n.rel)
		if pathSkipped(n.rel) || ignoredRel(n.rel, rules) || !indexable {
			if found && !snap.gone {
				chunks = dropChunks(chunks, n.rel)
				indexChanged = true
			}
			storeRel := n.rel
			if key != "" {
				storeRel = key
			}
			if !found || !snap.gone || snap.readAt.Before(n.noted) {
				meta[storeRel] = fileSnap{readAt: stamp, gone: true}
				metaChanged = true
			}
			continue
		}
		body, err := os.ReadFile(n.abs)
		if err != nil || hasNUL(body) {
			if found && !snap.gone {
				chunks = dropChunks(chunks, n.rel)
				indexChanged = true
			}
			storeRel := n.rel
			if key != "" {
				storeRel = key
			}
			meta[storeRel] = fileSnap{readAt: stamp, gone: true}
			metaChanged = true
			continue
		}
		chunks = dropChunks(chunks, n.rel)
		storeRel := n.rel
		if key != "" {
			delete(meta, key)
			storeRel = key
		}
		meta[storeRel] = fileSnap{size: info.Size(), mod: info.ModTime(), readAt: stamp}
		chunks = append(chunks, chunksFrom(storeRel, string(body))...)
		indexChanged = true
		metaChanged = true
	}
	if !indexChanged && !metaChanged {
		return false
	}
	var idx *bm25.Index
	if indexChanged {
		idx = indexChunks(chunks)
	}
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return true
	}
	if indexChanged {
		c.chunks = chunks
		c.index = idx
	}
	c.meta = meta
	c.files = liveFiles(meta)
	c.gen++
	c.mu.Unlock()
	return false
}

func noteStamp(started, noted time.Time) time.Time {
	if !started.After(noted) {
		return noted
	}
	return started
}

// indexableInfo reports a regular source file small enough to index.
// Lstat does not follow a symlink, and the size check happens before ReadFile
// so a noted log or archive is not loaded.
func indexableInfo(abs, rel string) (os.FileInfo, bool) {
	if !sourceExt(rel) {
		return nil, false
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	if info.Size() == 0 || info.Size() > maxFileBytes {
		return nil, false
	}
	return info, true
}

func dropChunks(chunks []chunk, rel string) []chunk {
	next := make([]chunk, 0, len(chunks))
	for _, ch := range chunks {
		if pathEqual(ch.path, rel) {
			continue
		}
		next = append(next, ch)
	}
	return next
}

func copyMeta(in map[string]fileSnap) map[string]fileSnap {
	out := make(map[string]fileSnap, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func liveFiles(meta map[string]fileSnap) int {
	n := 0
	for _, snap := range meta {
		if !snap.gone {
			n++
		}
	}
	return n
}

func notesFor(root string, meta map[string]fileSnap) []dirtyNote {
	storeMu.Lock()
	defer storeMu.Unlock()
	var notes []dirtyNote
	for abs, noted := range dirtyNotes {
		rel, ok := relUnder(root, abs)
		if !ok {
			continue
		}
		// Equal counts as consumed. On Windows time.Now often returns the same
		// instant for the note and the reload that follows it, and After would
		// rebuild the corpus on every later search.
		if _, snap, exists := lookupSnap(meta, rel); exists && !snap.readAt.Before(noted) {
			continue
		}
		notes = append(notes, dirtyNote{abs: abs, rel: rel, noted: noted})
	}
	return notes
}

func lookupSnap(meta map[string]fileSnap, rel string) (string, fileSnap, bool) {
	if snap, ok := meta[rel]; ok {
		return rel, snap, true
	}
	if runtime.GOOS != "windows" {
		return "", fileSnap{}, false
	}
	for key, snap := range meta {
		if strings.EqualFold(key, rel) {
			return key, snap, true
		}
	}
	return "", fileSnap{}, false
}

func pathEqual(a, b string) bool {
	a = filepath.ToSlash(a)
	b = filepath.ToSlash(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func relUnder(root, abs string) (string, bool) {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if root == "" || abs == "" {
		return "", false
	}
	if runtime.GOOS == "windows" {
		rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
		absSlash := filepath.ToSlash(abs)
		if strings.EqualFold(absSlash, rootSlash) {
			return "", false
		}
		if len(absSlash) <= len(rootSlash) || absSlash[len(rootSlash)] != '/' || !strings.EqualFold(absSlash[:len(rootSlash)], rootSlash) {
			return "", false
		}
		rel := absSlash[len(rootSlash)+1:]
		if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", false
		}
		return rel, true
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", false
	}
	return rel, true
}

type scored struct {
	idx   int
	bm    float64
	final float64
}

func rank(c *corpus, query, sub string, top int) Result {
	if c == nil {
		return Result{Freshness: indexedFreshness, Hits: []Hit{}}
	}
	tokens := bm25.Tokenize(expandIdentifiers(query))
	c.mu.Lock()
	chunks := c.chunks
	idx := c.index
	scoped := c.scoped
	files := c.files
	c.mu.Unlock()
	if idx == nil {
		return Result{Freshness: indexedFreshness, Hits: []Hit{}}
	}
	scores := idx.ScoreWithTokens(tokens)
	cands := make([]scored, 0, len(scores))
	for i, ch := range chunks {
		if sub != "" && !underPath(ch.path, sub) {
			continue
		}
		bm := scores[ch.id]
		if bm <= 0 {
			continue
		}
		cands = append(cands, scored{idx: i, bm: bm, final: bm})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].bm == cands[j].bm {
			return cands[i].idx < cands[j].idx
		}
		return cands[i].bm > cands[j].bm
	})
	if len(cands) > candidateLimit {
		cands = cands[:candidateLimit]
	}
	perFile := map[string]int{}
	best := map[string]int{}
	for n, cand := range cands {
		path := chunks[cand.idx].path
		perFile[path]++
		if prev, ok := best[path]; !ok || cands[prev].bm < cand.bm {
			best[path] = n
		}
	}
	for n := range cands {
		ch := chunks[cands[n].idx]
		if definitionHit(ch.stems, tokens) {
			cands[n].final += definitionBoost
		}
		if perFile[ch.path] >= 2 && best[ch.path] == n {
			cands[n].final += fileCoherence
		}
		if ch.noise {
			cands[n].final *= noiseScale
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].final == cands[j].final {
			a, b := chunks[cands[i].idx], chunks[cands[j].idx]
			if a.path == b.path {
				return a.start < b.start
			}
			return a.path < b.path
		}
		return cands[i].final > cands[j].final
	})
	cands = diversify(cands, chunks, top)
	hits := make([]Hit, 0, len(cands))
	for _, cand := range cands {
		ch := chunks[cand.idx]
		snippet, start, end := focusSnippet(ch.text, query, tokens, ch.start)
		hits = append(hits, Hit{
			Path:      ch.path,
			StartLine: start,
			EndLine:   end,
			Symbol:    ch.symbol,
			Score:     cand.final,
			Snippet:   snippet,
		})
	}
	res := Result{
		Freshness:    indexedFreshness,
		Scoped:       scoped,
		IndexedFiles: files,
		Hits:         hits,
	}
	if symbolLike(query) {
		res.Hint = symbolHint
	}
	return res
}

// definitionHit boosts a declaration when its name covers every Latin query
// token. A longer query word may sit on a shorter piece ("parser" on "parse").
// One shared word such as "config" does not boost loadConfig for "parse config".
func definitionHit(stems, query []string) bool {
	if len(stems) == 0 || len(query) == 0 {
		return false
	}
	have := make(map[string]struct{}, len(stems))
	for _, stem := range stems {
		have[stem] = struct{}{}
	}
	latin := 0
	for _, tok := range query {
		if len([]rune(tok)) < 2 || !latinToken(tok) {
			continue
		}
		latin++
		if !stemCovers(have, tok) {
			return false
		}
	}
	return latin > 0
}

func stemCovers(have map[string]struct{}, tok string) bool {
	if _, ok := have[tok]; ok {
		return true
	}
	if len(tok) < 5 {
		return false
	}
	for stem := range have {
		if len(stem) >= 4 && strings.HasPrefix(tok, stem) {
			return true
		}
	}
	return false
}

func latinToken(tok string) bool {
	for _, r := range tok {
		if r > unicode.MaxASCII || !unicode.IsLetter(r) {
			return false
		}
	}
	return tok != ""
}

func diversify(cands []scored, chunks []chunk, top int) []scored {
	if top < 1 || len(cands) <= 1 {
		return cands
	}
	out := make([]scored, 0, top)
	rest := make([]scored, 0)
	per := map[string]int{}
	for _, cand := range cands {
		path := chunks[cand.idx].path
		if per[path] >= 2 {
			rest = append(rest, cand)
			continue
		}
		per[path]++
		out = append(out, cand)
		if len(out) == top {
			return out
		}
	}
	for _, cand := range rest {
		out = append(out, cand)
		if len(out) == top {
			break
		}
	}
	return out
}

func focusSnippet(text, query string, tokens []string, chunkStart int) (string, int, int) {
	lines := splitLines(text)
	if chunkStart < 1 {
		chunkStart = 1
	}
	if len(lines) == 0 {
		return "", chunkStart, chunkStart
	}
	best := bestSnippetLine(lines, query, tokens)
	start := best - 5
	if start < 0 {
		start = 0
	}
	end := start + maxSnippetLn
	if end > len(lines) {
		end = len(lines)
		start = end - maxSnippetLn
		if start < 0 {
			start = 0
		}
	}
	if best < start {
		start = best
		end = start + maxSnippetLn
		if end > len(lines) {
			end = len(lines)
		}
	}
	// cutRunes keeps the head of the window. A long lead then drops the
	// matching line while the reported range still claims to contain it.
	// Drop the tail first, then the lead, and keep that line inside the budget.
	for end > best+1 && snippetRunes(lines[start:end]) > maxSnippetRun {
		end--
	}
	for start < best && snippetRunes(lines[start:end]) > maxSnippetRun {
		start++
	}
	for end < len(lines) && end-start < maxSnippetLn && snippetRunes(lines[start:end+1]) <= maxSnippetRun {
		end++
	}
	body := strings.Join(lines[start:end], "\n")
	if snippetRunes(lines[start:end]) > maxSnippetRun {
		body = cutAroundNeedle(lines[best], snippetNeedles(query, tokens))
		return body, chunkStart + best, chunkStart + best
	}
	return body, chunkStart + start, chunkStart + end - 1
}

func snippetRunes(lines []string) int {
	n := 0
	for i, line := range lines {
		if i > 0 {
			n++
		}
		n += len([]rune(line))
	}
	return n
}

func cutAroundNeedle(line string, needles []string) string {
	runes := []rune(line)
	if len(runes) <= maxSnippetRun {
		return line
	}
	lower := strings.ToLower(line)
	atByte := -1
	if len(lower) == len(line) {
		for _, needle := range needles {
			if i := strings.Index(lower, needle); i >= 0 && (atByte < 0 || i < atByte) {
				atByte = i
			}
		}
	}
	if atByte < 0 {
		return string(runes[:maxSnippetRun])
	}
	at := len([]rune(line[:atByte]))
	start := at - maxSnippetRun/2
	if start < 0 {
		start = 0
	}
	end := start + maxSnippetRun
	if end > len(runes) {
		end = len(runes)
		start = end - maxSnippetRun
		if start < 0 {
			start = 0
		}
	}
	if at < start || at >= end {
		start = at
		end = start + maxSnippetRun
		if end > len(runes) {
			end = len(runes)
		}
	}
	return string(runes[start:end])
}

func bestSnippetLine(lines []string, query string, tokens []string) int {
	needles := snippetNeedles(query, tokens)
	best, score := 0, 0
	for i, line := range lines {
		lower := strings.ToLower(line)
		s := 0
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				s += len(needle)
			}
		}
		if s > score {
			score = s
			best = i
		}
	}
	return best
}

func snippetNeedles(query string, tokens []string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len([]rune(s)) < 2 {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(query)
	for _, tok := range tokens {
		add(tok)
	}
	return out
}

func symbolLike(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" || strings.ContainsAny(query, " \t\r\n") {
		return false
	}
	for _, r := range query {
		if r >= 0x4e00 && r <= 0x9fff {
			return false
		}
	}
	return symbolQuery.MatchString(query)
}

func reconcileLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		reconcileOnce()
	}
}

func reconcileOnce() {
	evictIdle(time.Now())
	storeMu.Lock()
	list := make([]*corpus, 0, len(corpora))
	for _, c := range corpora {
		list = append(list, c)
	}
	storeMu.Unlock()
	for _, c := range list {
		if dirsChanged(c) {
			refreshCorpus(c)
			continue
		}
		noteChanged(c)
	}
}

const corpusIdle = 15 * time.Minute

// warmHoldInterval stays under corpusIdle so a live coding session refreshes
// the corpus before idle eviction can drop it.
const warmHoldInterval = 4 * time.Minute

func evictIdle(now time.Time) {
	cutoff := now.Add(-corpusIdle).UnixNano()
	storeMu.Lock()
	defer storeMu.Unlock()
	for key, c := range corpora {
		used := c.used.Load()
		if used != 0 && used < cutoff {
			delete(corpora, key)
		}
	}
}

func dirsChanged(c *corpus) bool {
	c.mu.Lock()
	root := c.root
	dirs := make(map[string]time.Time, len(c.dirs))
	for rel, mod := range c.dirs {
		dirs[rel] = mod
	}
	c.mu.Unlock()
	if len(dirs) == 0 {
		return false
	}
	for rel, mod := range dirs {
		abs := root
		if rel != "" {
			abs = filepath.Join(root, filepath.FromSlash(rel))
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() || !info.ModTime().Equal(mod) {
			return true
		}
	}
	return false
}

func refreshCorpus(c *corpus) {
	c.mu.Lock()
	root, sub := c.root, c.sub
	c.mu.Unlock()
	fresh := buildCorpus(root, sub, sub != "")
	c.mu.Lock()
	c.files = fresh.files
	c.truncated = fresh.truncated
	c.scoped = fresh.scoped
	c.rules = fresh.rules
	c.chunks = fresh.chunks
	c.meta = fresh.meta
	c.dirs = fresh.dirs
	c.index = fresh.index
	c.sub = fresh.sub
	c.gen++
	c.mu.Unlock()
}

func noteChanged(c *corpus) {
	c.mu.Lock()
	type item struct {
		rel  string
		snap fileSnap
	}
	items := make([]item, 0, len(c.meta))
	root := c.root
	for rel, snap := range c.meta {
		if snap.gone {
			continue
		}
		items = append(items, item{rel: rel, snap: snap})
	}
	c.mu.Unlock()
	for _, item := range items {
		abs := filepath.Join(root, filepath.FromSlash(item.rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() != item.snap.size || !info.ModTime().Equal(item.snap.mod) {
			NoteDirty(abs)
		}
	}
}
