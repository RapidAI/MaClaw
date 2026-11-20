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
	// catchingUpHint is the result when this search stopped before every noted
	// file was reloaded. The hits are from the published index. Exact text
	// that must include those edits belongs on ripgrep.
	catchingUpHint = "索引还在跟上刚才的改动，这次命中不含未跟上的文件。精确文本用 ripgrep。"
	// dirtyCatchupBudget is how long a background pass spends reloading noted
	// files. It is not a query timeout. The query uses Request.Wait and leaves
	// the rest of the notes for this pass.
	dirtyCatchupBudget = 2 * time.Minute
)

// ProgressFunc reports noted files handled by one search.
// phase is "catchup". done is how many this call has handled. total is how
// many it owed when it started.
type ProgressFunc func(phase string, done, total int)

var symbolQuery = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:::[A-Za-z_][A-Za-z0-9_]*)?$`)

// Request is one code_search lookup. Path is a slash-separated directory or
// file inside Root. Wait bounds two things: a cold index build, and how long
// a published index spends reloading files noted since the last pass.
// Reloading one file retokenizes that file. It does not rebuild the corpus.
// Notes still owed stay dirty for the background pass. Progress may be nil.
type Request struct {
	Root     string
	Path     string
	Query    string
	TopK     int
	Wait     time.Duration
	Progress ProgressFunc
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
	storeMu    sync.Mutex
	corpora    = map[string]*corpus{}
	builds     = map[string]*buildSlot{}
	dirtyNotes = map[string]time.Time{}
	maxFiles   atomic.Int32
	buildPause func()
	pauseMu    sync.Mutex
	// dirtyGate, when set by a test, is asked before each noted file is read.
	// Returning false leaves that note dirty and the search ranks the index
	// already published.
	dirtyGate   func(done, total int) bool
	dirtyGateMu sync.Mutex
	// noteRead reads a noted file. Tests replace it to simulate a share
	// violation; a failed read must not consume the note.
	noteReadMu  sync.Mutex
	noteRead    = os.ReadFile
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

// SetDirtyGateForTest is asked before each noted file is read.
// Returning false stops this pass. The note stays dirty. Nil clears it.
func SetDirtyGateForTest(fn func(done, total int) bool) {
	dirtyGateMu.Lock()
	dirtyGate = fn
	dirtyGateMu.Unlock()
}

func currentDirtyGate() func(done, total int) bool {
	dirtyGateMu.Lock()
	defer dirtyGateMu.Unlock()
	return dirtyGate
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
	mu         sync.Mutex
	gen        uint64
	root       string
	sub        string
	files      int
	truncated  bool
	scoped     bool
	rules      []rule
	chunks     []chunk
	meta       map[string]fileSnap
	dirs       map[string]time.Time
	gitignores map[string]time.Time
	index      *bm25.Index
	// applied is the dirty-note time already reflected in this corpus, keyed
	// by noteKey. A later search skips those notes before copying meta.
	applied map[string]time.Time
	used    atomic.Int64
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
	// The published index is already searchable. This pass only reloads noted
	// files, and only until Wait. A full retokenize stays off this goroutine.
	caughtUp := applyDirtyUntil(c, time.Now().Add(wait), req.Progress)
	res := rank(c, query, sub, top)
	if !caughtUp {
		res.Hint = catchingUpHint
	}
	return res
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
	files, rules, dirs, gitignores, truncated := collectFiles(root, sub, limit)
	c := &corpus{
		root:       root,
		sub:        sub,
		truncated:  truncated,
		scoped:     scoped || truncated,
		rules:      rules,
		meta:       map[string]fileSnap{},
		dirs:       dirs,
		gitignores: gitignores,
	}
	now := time.Now()
	for _, file := range files {
		if file.skip {
			c.meta[file.rel] = fileSnap{size: file.size, mod: file.mod, readAt: now, gone: true}
			continue
		}
		c.meta[file.rel] = fileSnap{size: file.size, mod: file.mod, readAt: now}
		c.chunks = append(c.chunks, chunksFrom(file.rel, file.body)...)
	}
	c.files = liveFiles(c.meta)
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

// applyDirty reloads noted files on the background budget. A search does not
// call this. It calls applyDirtyUntil with Request.Wait.
func applyDirty(c *corpus) {
	applyDirtyUntil(c, time.Now().Add(dirtyCatchupBudget), nil)
}

// applyDirtyUntil reloads noted files until deadline. It returns false when
// notes are still owed. Those notes stay in dirtyNotes. The published chunks
// are left in place for the ones this pass did not reach. A generation
// conflict retries; the notes already published are not read again.
func applyDirtyUntil(c *corpus, deadline time.Time, progress ProgressFunc) bool {
	if c == nil {
		return true
	}
	for attempt := 0; attempt < 3; attempt++ {
		retry, caughtUp := applyDirtyOnce(c, deadline, progress)
		if !retry {
			return caughtUp
		}
	}
	return false
}

type noteEdit struct {
	abs      string
	rel      string
	noted    time.Time
	storeRel string
	snap     fileSnap
	drop     bool
	add      []chunk
	touched  bool
	// retry leaves the note owed. A failed read is not a deletion.
	retry bool
}

// applyDirtyOnce reloads noted files that still fit in deadline.
// It returns retry when a newer generation published first.
// caughtUp is false when this pass stopped with notes still owed.
// A corpus with nothing left to reload returns before it copies the file
// table. The index published here is a clone, so a search that already
// snapped the previous pointer keeps scoring that generation.
func applyDirtyOnce(c *corpus, deadline time.Time, progress ProgressFunc) (retry bool, caughtUp bool) {
	c.mu.Lock()
	root := c.root
	sub := c.sub
	c.mu.Unlock()
	pending := dirtyIn(root, sub)
	if len(pending) == 0 {
		return false, true
	}
	c.mu.Lock()
	owed := make([]dirtyNote, 0, len(pending))
	for _, n := range pending {
		if t, ok := c.applied[n.abs]; ok && !t.Before(n.noted) {
			continue
		}
		if snapCurrent(c.meta, n.rel, n.noted) {
			rememberAppliedLocked(c, n)
			continue
		}
		owed = append(owed, n)
	}
	if len(owed) == 0 {
		c.mu.Unlock()
		return false, true
	}
	gen := c.gen
	rules := append([]rule(nil), c.rules...)
	chunks := c.chunks
	base := c.index
	meta := copyMeta(c.meta)
	c.mu.Unlock()

	snaps := indexSnaps(meta)
	work := make([]dirtyNote, 0, len(owed))
	var consumed []dirtyNote
	for _, n := range owed {
		if _, snap, exists := snaps.lookup(n.rel); exists && !snap.readAt.Before(n.noted) {
			consumed = append(consumed, n)
			continue
		}
		work = append(work, n)
	}
	if len(work) == 0 {
		if rememberApplied(c, gen, consumed) {
			return true, false
		}
		return false, true
	}
	edits := make([]noteEdit, 0, len(work))
	caughtUp = true
	done := 0
	for _, n := range work {
		if !time.Now().Before(deadline) {
			caughtUp = false
			break
		}
		if gate := currentDirtyGate(); gate != nil && !gate(done, len(work)) {
			caughtUp = false
			break
		}
		edit := catchNote(n, rules, snaps)
		if edit.retry {
			// This file stays owed. Keep applying the notes behind it.
			caughtUp = false
			continue
		}
		edit.abs = n.abs
		edit.noted = n.noted
		edits = append(edits, edit)
		done++
		if progress != nil {
			progress("catchup", done, len(work))
		}
	}
	indexChanged := false
	metaChanged := false
	dropSet := map[string]struct{}{}
	var added []chunk
	for _, edit := range edits {
		if !edit.touched {
			continue
		}
		metaChanged = true
		if edit.drop {
			dropSet[pathKey(edit.rel)] = struct{}{}
			indexChanged = true
		}
		if len(edit.add) > 0 {
			added = append(added, edit.add...)
			indexChanged = true
		}
	}
	doneNotes := append([]dirtyNote(nil), consumed...)
	for _, edit := range edits {
		doneNotes = append(doneNotes, dirtyNote{abs: edit.abs, noted: edit.noted})
	}
	if !indexChanged && !metaChanged {
		if rememberApplied(c, gen, doneNotes) {
			return true, false
		}
		return false, caughtUp
	}
	var filtered []chunk
	var removed []string
	var prepared bm25.Prepared
	var next *bm25.Index
	if indexChanged {
		filtered = make([]chunk, 0, len(chunks)+len(added))
		for _, ch := range chunks {
			if _, ok := dropSet[pathKey(ch.path)]; ok {
				removed = append(removed, ch.id)
				continue
			}
			filtered = append(filtered, ch)
		}
		added = dedupeChunkIDs(filtered, added)
		if len(removed) == 0 && len(added) == 0 {
			indexChanged = false
		} else {
			prepared = bm25.Prepare(docsForChunks(added))
			filtered = append(filtered, added...)
			// Tokenize finished above. The clone is not the published index,
			// so this edit cannot change a search that already snapped it.
			next = replacedIndex(base, removed, prepared, filtered)
		}
	}
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return true, false
	}
	if indexChanged {
		c.index = next
		c.chunks = filtered
	}
	if metaChanged {
		c.meta = meta
		c.files = liveFiles(meta)
	}
	for _, n := range doneNotes {
		rememberAppliedLocked(c, n)
	}
	c.gen++
	c.mu.Unlock()
	return false, caughtUp
}

// replacedIndex returns a new index with removed ids dropped and prepared
// docs inserted. base nil builds one from fallback. The returned index is
// not visible to readers until the caller stores it.
func replacedIndex(base *bm25.Index, removed []string, prepared bm25.Prepared, fallback []chunk) *bm25.Index {
	if base == nil {
		return indexChunks(fallback)
	}
	next := base.Clone()
	next.ReplacePrepared(removed, prepared)
	return next
}

func rememberApplied(c *corpus, gen uint64, notes []dirtyNote) bool {
	if c == nil || len(notes) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return true
	}
	for _, n := range notes {
		rememberAppliedLocked(c, n)
	}
	return false
}

func rememberAppliedLocked(c *corpus, n dirtyNote) {
	if c == nil || n.abs == "" {
		return
	}
	if c.applied == nil {
		c.applied = map[string]time.Time{}
	}
	if t, ok := c.applied[n.abs]; ok && !t.Before(n.noted) {
		return
	}
	c.applied[n.abs] = n.noted
}

// dirtyIn lists notes under root and, when set, sub. It does not copy the
// corpus file table. An empty dirty set returns before any path join.
func dirtyIn(root, sub string) []dirtyNote {
	storeMu.Lock()
	defer storeMu.Unlock()
	if len(dirtyNotes) == 0 {
		return nil
	}
	out := make([]dirtyNote, 0)
	for abs, noted := range dirtyNotes {
		rel, ok := relUnder(root, abs)
		if !ok {
			continue
		}
		if sub != "" && !underPath(rel, sub) {
			continue
		}
		out = append(out, dirtyNote{abs: abs, rel: rel, noted: noted})
	}
	return out
}

func snapCurrent(meta map[string]fileSnap, rel string, noted time.Time) bool {
	if snap, ok := meta[rel]; ok {
		return !snap.readAt.Before(noted)
	}
	if runtime.GOOS != "windows" {
		return false
	}
	for key, snap := range meta {
		if strings.EqualFold(key, rel) && !snap.readAt.Before(noted) {
			return true
		}
	}
	return false
}

func catchNote(n dirtyNote, rules []rule, snaps *snapIndex) noteEdit {
	key, snap, found := snaps.lookup(n.rel)
	stamp := noteStamp(time.Now(), n.noted)
	storeRel := n.rel
	if key != "" {
		storeRel = key
	}
	info, indexable := indexableInfo(n.abs, n.rel)
	if pathSkipped(n.rel) || ignoredRel(n.rel, rules) || !indexable {
		edit := noteEdit{rel: n.rel, storeRel: storeRel}
		if found && !snap.gone {
			edit.drop = true
			edit.touched = true
		}
		if !found || !snap.gone || snap.readAt.Before(n.noted) {
			edit.snap = goneSnap(n.abs, info, stamp)
			edit.touched = true
			snaps.store(storeRel, edit.snap)
		}
		return edit
	}
	body, err := readNoted(n.abs)
	if err != nil {
		// The bytes are still the previous generation. Consuming the note
		// would drop those chunks, and noteChanged would not come back:
		// a later rewrite does not change the directory mtime.
		return noteEdit{rel: n.rel, storeRel: storeRel, retry: true}
	}
	if hasNUL(body) {
		edit := noteEdit{rel: n.rel, storeRel: storeRel, touched: true, snap: goneSnap(n.abs, info, stamp)}
		if found && !snap.gone {
			edit.drop = true
		}
		snaps.store(storeRel, edit.snap)
		return edit
	}
	edit := noteEdit{
		rel:      n.rel,
		storeRel: storeRel,
		touched:  true,
		drop:     true,
		snap:     fileSnap{size: info.Size(), mod: info.ModTime(), readAt: stamp},
		add:      chunksFrom(storeRel, string(body)),
	}
	snaps.store(storeRel, edit.snap)
	return edit
}

func docsForChunks(chunks []chunk) []bm25.Doc {
	docs := make([]bm25.Doc, len(chunks))
	for i := range chunks {
		docs[i] = bm25.Doc{ID: chunks[i].id, Text: expandIdentifiers(chunks[i].symbol + "\n" + chunks[i].text)}
	}
	return docs
}

func dedupeChunkIDs(kept, added []chunk) []chunk {
	if len(added) == 0 {
		return added
	}
	seen := make(map[string]struct{}, len(kept)+len(added))
	for _, ch := range kept {
		seen[ch.id] = struct{}{}
	}
	out := make([]chunk, len(added))
	for i, ch := range added {
		id := ch.id
		if id == "" {
			id = ch.path + "#" + itoaSmall(ch.start)
		}
		base := id
		n := i + 1
		for {
			if _, ok := seen[id]; !ok {
				break
			}
			id = base + "#" + itoaSmall(n)
			n++
		}
		seen[id] = struct{}{}
		ch.id = id
		out[i] = ch
	}
	return out
}

func noteStamp(started, noted time.Time) time.Time {
	if !started.After(noted) {
		return noted
	}
	return started
}

// indexableInfo reports a regular source file small enough to index.
// Lstat does not follow a symlink, and the size check happens before ReadFile
// so a noted log or archive is not loaded. An oversized or empty file still
// returns its info so a tombstone can remember that identity.
func indexableInfo(abs, rel string) (os.FileInfo, bool) {
	if !sourceExt(rel) {
		return nil, false
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	if info.Size() == 0 || info.Size() > maxFileBytes {
		return info, false
	}
	return info, true
}

func readNoted(abs string) ([]byte, error) {
	noteReadMu.Lock()
	fn := noteRead
	noteReadMu.Unlock()
	if fn == nil {
		fn = os.ReadFile
	}
	return fn(abs)
}

// goneSnap remembers the file that was skipped. noteChanged uses the size
// and mtime to notice when those bytes change; the directory mtime does not.
func goneSnap(abs string, info os.FileInfo, stamp time.Time) fileSnap {
	snap := fileSnap{readAt: stamp, gone: true}
	if info != nil && info.Mode().IsRegular() {
		snap.size = info.Size()
		snap.mod = info.ModTime()
		return snap
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.Mode().IsRegular() {
		return snap
	}
	snap.size = st.Size()
	snap.mod = st.ModTime()
	return snap
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

type snapHit struct {
	key  string
	snap fileSnap
}

type snapIndex struct {
	exact map[string]fileSnap
	fold  map[string]snapHit
}

func indexSnaps(meta map[string]fileSnap) *snapIndex {
	s := &snapIndex{exact: meta}
	if runtime.GOOS != "windows" {
		return s
	}
	s.fold = make(map[string]snapHit, len(meta))
	for key, snap := range meta {
		s.fold[strings.ToLower(key)] = snapHit{key: key, snap: snap}
	}
	return s
}

func (s *snapIndex) lookup(rel string) (string, fileSnap, bool) {
	if s == nil {
		return "", fileSnap{}, false
	}
	if snap, ok := s.exact[rel]; ok {
		return rel, snap, true
	}
	if s.fold == nil {
		return "", fileSnap{}, false
	}
	hit, ok := s.fold[strings.ToLower(rel)]
	if !ok {
		return "", fileSnap{}, false
	}
	return hit.key, hit.snap, true
}

func (s *snapIndex) store(rel string, snap fileSnap) {
	if s == nil {
		return
	}
	s.exact[rel] = snap
	if s.fold != nil {
		s.fold[strings.ToLower(rel)] = snapHit{key: rel, snap: snap}
	}
}

func pathKey(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
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
	// The published index is immutable. A reload clones it and swaps this
	// pointer, so ScoreWithTokens below still scores the generation that
	// owns this chunk slice.
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
		// A file write changes the parent directory mtime. That is not a new
		// tree. Rebuilding here retokenized every chunk on the same cadence
		// as the edits that made the next search slow.
		if !syncCorpusTree(c) {
			noteChanged(c)
		}
		applyDirtyUntil(c, time.Now().Add(dirtyCatchupBudget), nil)
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

// syncCorpusTree notes source files that appeared in a directory whose mtime
// changed. A new directory is walked on its own. A removed directory drops
// its chunks. A gitignore edit reloads that file's rules and notes only the
// files whose ignore status changed. None of those retokenize the corpus.
// It returns false so the caller still checks the mtime of files already
// indexed. A missing project root is left untouched.
func syncCorpusTree(c *corpus) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	root := c.root
	rules := append([]rule(nil), c.rules...)
	dirs := make(map[string]time.Time, len(c.dirs))
	for rel, mod := range c.dirs {
		dirs[rel] = mod
	}
	gitignores := make(map[string]time.Time, len(c.gitignores))
	for rel, mod := range c.gitignores {
		gitignores[rel] = mod
	}
	c.mu.Unlock()
	if len(dirs) == 0 {
		return false
	}
	// Check the root before any child. Map order could otherwise treat a
	// deleted project as a missing subdirectory and drop the index.
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return false
	}
	stamps := make(map[string]time.Time, len(dirs))
	changed := make([]string, 0)
	missing := make([]string, 0)
	giChanged := make([]string, 0)
	for rel, mod := range dirs {
		abs := root
		if rel != "" {
			abs = filepath.Join(root, filepath.FromSlash(rel))
		}
		info, err := os.Stat(abs)
		if rel == "" && (err != nil || !info.IsDir()) {
			return false
		}
		if err != nil || !info.IsDir() {
			missing = append(missing, rel)
			continue
		}
		stamps[rel] = info.ModTime()
		if !info.ModTime().Equal(mod) {
			changed = append(changed, rel)
		}
		// Rewriting .gitignore does not change the directory mtime on
		// Windows. The file's own mtime is the signal, including when the
		// directory itself is unchanged.
		if _, tracked := gitignores[rel]; tracked || !info.ModTime().Equal(mod) {
			if gitignoreStampChanged(abs, rel, gitignores) {
				giChanged = append(giChanged, rel)
			}
		}
	}
	for _, rel := range topmostDirs(missing) {
		dropCorpusTree(c, rel)
	}
	for _, rel := range giChanged {
		abs := root
		if rel != "" {
			abs = filepath.Join(root, filepath.FromSlash(rel))
		}
		reloadGitignore(c, abs, rel, dirs)
	}
	if len(changed) == 0 {
		return false
	}
	c.mu.Lock()
	rules = append([]rule(nil), c.rules...)
	meta := copyMeta(c.meta)
	c.mu.Unlock()
	known := indexSnaps(meta)
	unread := map[string]struct{}{}
	for _, rel := range changed {
		abs := root
		if rel != "" {
			abs = filepath.Join(root, filepath.FromSlash(rel))
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			unread[rel] = struct{}{}
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == ".gitignore" {
				continue
			}
			child := name
			if rel != "" {
				child = rel + "/" + name
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			if mode&os.ModeSymlink != 0 || (mode&os.ModeIrregular != 0 && !info.IsDir()) {
				continue
			}
			if info.IsDir() {
				if skipComponent(name, true) || ignored(child, true, rules) || ignoredRel(child, rules) {
					continue
				}
				if !dirKnown(dirs, child) {
					adoptCorpusDir(c, child, dirs)
				}
				continue
			}
			if !info.Mode().IsRegular() || !sourceExt(name) {
				continue
			}
			if pathSkipped(child) || ignoredRel(child, rules) {
				continue
			}
			if _, snap, ok := known.lookup(child); ok {
				if !snap.gone {
					continue
				}
				// Already skipped, and the bytes are unchanged.
				if snap.size == info.Size() && snap.mod.Equal(info.ModTime()) {
					continue
				}
			}
			NoteDirty(filepath.Join(root, filepath.FromSlash(child)))
		}
	}
	c.mu.Lock()
	for rel, mod := range stamps {
		if _, skip := unread[rel]; skip {
			continue
		}
		if _, ok := c.dirs[rel]; ok {
			c.dirs[rel] = mod
		}
	}
	c.mu.Unlock()
	return false
}

func topmostDirs(rels []string) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		covered := false
		for _, other := range rels {
			if other != rel && other != "" && underPath(rel, other) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, rel)
		}
	}
	return out
}

// dropCorpusTree removes one deleted directory from the published index.
// Kept documents stay in a clone. Nothing in the rest of the tree is tokenized.
func dropCorpusTree(c *corpus, rel string) {
	if c == nil || rel == "" {
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		c.mu.Lock()
		gen := c.gen
		base := c.index
		chunks := c.chunks
		meta := copyMeta(c.meta)
		rules := append([]rule(nil), c.rules...)
		dirs := make(map[string]time.Time, len(c.dirs))
		for key, mod := range c.dirs {
			dirs[key] = mod
		}
		c.mu.Unlock()
		removed := make([]string, 0)
		keptChunks := make([]chunk, 0, len(chunks))
		for _, ch := range chunks {
			if underPath(ch.path, rel) {
				if ch.id != "" {
					removed = append(removed, ch.id)
				}
				continue
			}
			keptChunks = append(keptChunks, ch)
		}
		now := time.Now()
		metaChanged := false
		for path, snap := range meta {
			if snap.gone || !underPath(path, rel) {
				continue
			}
			snap.gone = true
			snap.readAt = now
			meta[path] = snap
			metaChanged = true
		}
		keptRules := make([]rule, 0, len(rules))
		rulesChanged := false
		for _, rule := range rules {
			if pathEqual(rule.base, rel) || (rule.base != "" && underPath(rule.base, rel)) {
				rulesChanged = true
				continue
			}
			keptRules = append(keptRules, rule)
		}
		dirsChanged := false
		for key := range dirs {
			if pathEqual(key, rel) || underPath(key, rel) {
				delete(dirs, key)
				dirsChanged = true
			}
		}
		if len(removed) == 0 && !metaChanged && !rulesChanged && !dirsChanged {
			return
		}
		var next *bm25.Index
		if len(removed) > 0 && base != nil {
			next = replacedIndex(base, removed, bm25.Prepared{}, nil)
		}
		c.mu.Lock()
		if c.gen != gen {
			c.mu.Unlock()
			continue
		}
		if len(removed) > 0 {
			if next != nil {
				c.index = next
			}
			c.chunks = keptChunks
		}
		if metaChanged {
			c.meta = meta
			c.files = liveFiles(meta)
		}
		if rulesChanged {
			c.rules = keptRules
		}
		if dirsChanged {
			c.dirs = dirs
		}
		if c.gitignores != nil {
			for key := range c.gitignores {
				if pathEqual(key, rel) || underPath(key, rel) {
					delete(c.gitignores, key)
				}
			}
		}
		c.gen++
		c.mu.Unlock()
		return
	}
}

// adoptCorpusDir indexes a directory that was not in the tree.
// Only that directory's files are read. seen records the new stamps so this
// pass does not adopt the same directory again.
func adoptCorpusDir(c *corpus, rel string, seen map[string]time.Time) {
	if c == nil || rel == "" {
		return
	}
	c.mu.Lock()
	root := c.root
	sub := c.sub
	have := c.files
	c.mu.Unlock()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if sub != "" && !underPath(rel, sub) && !dirMayHold(rel, sub) {
		recordDir(c, abs, rel, seen)
		return
	}
	if have >= int(maxFiles.Load()) {
		c.mu.Lock()
		c.truncated = true
		c.scoped = true
		c.mu.Unlock()
		recordDir(c, abs, rel, seen)
		return
	}
	limit := int(maxFiles.Load()) - have
	files, rules, dirs, gitignores, truncated := collectFiles(root, rel, limit)
	c.mu.Lock()
	merged, added := mergeRules(c.rules, rules)
	if added > 0 {
		c.rules = merged
	}
	if c.dirs == nil {
		c.dirs = map[string]time.Time{}
	}
	if c.gitignores == nil {
		c.gitignores = map[string]time.Time{}
	}
	for key, mod := range gitignores {
		if key != rel && !underPath(key, rel) {
			continue
		}
		if _, ok := c.gitignores[key]; ok {
			continue
		}
		c.gitignores[key] = mod
	}
	for key, mod := range dirs {
		if key != rel && !underPath(key, rel) {
			continue
		}
		if _, ok := c.dirs[key]; ok {
			continue
		}
		c.dirs[key] = mod
		if seen != nil {
			seen[key] = mod
		}
	}
	if _, ok := c.dirs[rel]; !ok {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			c.dirs[rel] = info.ModTime()
			if seen != nil {
				seen[rel] = c.dirs[rel]
			}
		}
	} else if seen != nil {
		seen[rel] = c.dirs[rel]
	}
	if truncated {
		c.truncated = true
		c.scoped = true
	}
	if added > 0 || len(files) > 0 {
		c.gen++
	}
	c.mu.Unlock()
	for _, file := range files {
		if sub != "" && !underPath(file.rel, sub) {
			continue
		}
		NoteDirty(filepath.Join(root, filepath.FromSlash(file.rel)))
	}
}

func recordDir(c *corpus, abs, rel string, seen map[string]time.Time) {
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return
	}
	c.mu.Lock()
	if c.dirs == nil {
		c.dirs = map[string]time.Time{}
	}
	if _, ok := c.dirs[rel]; !ok {
		c.dirs[rel] = info.ModTime()
	}
	if seen != nil {
		seen[rel] = c.dirs[rel]
	}
	c.mu.Unlock()
}

// reloadGitignore replaces one directory's rules and notes files whose
// ignore status changed. Files that stay indexed are not read again.
func reloadGitignore(c *corpus, dirAbs, rel string, seen map[string]time.Time) {
	if c == nil {
		return
	}
	giPath := filepath.Join(dirAbs, ".gitignore")
	info, statErr := os.Stat(giPath)
	var parsed []rule
	if statErr == nil {
		if data, err := os.ReadFile(giPath); err == nil {
			parsed = parseGitignore(rel, string(data))
		}
	}
	c.mu.Lock()
	same := sameRules(rulesAt(c.rules, rel), parsed)
	if !same {
		c.rules = replaceRulesAtBase(c.rules, rel, parsed)
		c.gen++
	}
	if c.gitignores == nil {
		c.gitignores = map[string]time.Time{}
	}
	if statErr != nil {
		delete(c.gitignores, rel)
	} else {
		c.gitignores[rel] = info.ModTime()
	}
	if same {
		c.mu.Unlock()
		return
	}
	rules := append([]rule(nil), c.rules...)
	meta := copyMeta(c.meta)
	root := c.root
	sub := c.sub
	c.mu.Unlock()
	noteIgnoreChanges(c, root, dirAbs, rel, sub, rules, meta, seen)
}

func noteIgnoreChanges(c *corpus, root, dirAbs, rel, sub string, rules []rule, meta map[string]fileSnap, seen map[string]time.Time) {
	known := indexSnaps(meta)
	var walk func(abs, relPath string)
	walk = func(abs, relPath string) {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == ".gitignore" {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			if mode&os.ModeSymlink != 0 || (mode&os.ModeIrregular != 0 && !info.IsDir()) {
				continue
			}
			child := name
			if relPath != "" {
				child = relPath + "/" + name
			}
			if info.IsDir() {
				if skipComponent(name, true) || ignored(child, true, rules) || ignoredRel(child, rules) {
					noteLiveUnder(root, sub, known, child)
					continue
				}
				if !dirKnown(seen, child) {
					c.mu.Lock()
					if c.dirs == nil {
						c.dirs = map[string]time.Time{}
					}
					if _, ok := c.dirs[child]; !ok {
						c.dirs[child] = info.ModTime()
					}
					c.mu.Unlock()
					if seen != nil {
						seen[child] = info.ModTime()
					}
				}
				walk(filepath.Join(abs, name), child)
				continue
			}
			if !info.Mode().IsRegular() || !sourceExt(name) {
				continue
			}
			if sub != "" && !underPath(child, sub) {
				continue
			}
			_, snap, ok := known.lookup(child)
			live := ok && !snap.gone
			if pathSkipped(child) || ignoredRel(child, rules) {
				if live {
					NoteDirty(filepath.Join(root, filepath.FromSlash(child)))
				}
				continue
			}
			if live {
				continue
			}
			if ok && snap.size == info.Size() && snap.mod.Equal(info.ModTime()) {
				continue
			}
			NoteDirty(filepath.Join(root, filepath.FromSlash(child)))
		}
	}
	walk(dirAbs, rel)
}

func noteLiveUnder(root, sub string, known *snapIndex, dirRel string) {
	if known == nil {
		return
	}
	for path, snap := range known.exact {
		if snap.gone || !underPath(path, dirRel) {
			continue
		}
		if sub != "" && !underPath(path, sub) {
			continue
		}
		NoteDirty(filepath.Join(root, filepath.FromSlash(path)))
	}
}

func rulesAt(rules []rule, rel string) []rule {
	out := make([]rule, 0)
	for _, rule := range rules {
		if pathEqual(rule.base, rel) {
			out = append(out, rule)
		}
	}
	return out
}

func sameRules(a, b []rule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func replaceRulesAtBase(rules []rule, rel string, parsed []rule) []rule {
	out := make([]rule, 0, len(rules)+len(parsed))
	for _, rule := range rules {
		if pathEqual(rule.base, rel) {
			continue
		}
		out = append(out, rule)
	}
	out = append(out, parsed...)
	// ignored() keeps the last match. A parent pattern appended after a
	// nested negation would hide the file the child gitignore put back.
	orderRules(out)
	return out
}

// orderRules puts shallower gitignore directories first. Rules that share a
// base keep the order they had in that file, so a later negation still wins.
func orderRules(rules []rule) {
	sort.SliceStable(rules, func(i, j int) bool {
		return ruleDepth(rules[i].base) < ruleDepth(rules[j].base)
	})
}

func ruleDepth(base string) int {
	if base == "" {
		return 0
	}
	return strings.Count(base, "/") + 1
}

func mergeRules(have, extra []rule) ([]rule, int) {
	if len(extra) == 0 {
		return have, 0
	}
	out := append([]rule(nil), have...)
	added := 0
	for _, rule := range extra {
		if ruleExists(out, rule) {
			continue
		}
		out = append(out, rule)
		added++
	}
	if added == 0 {
		return have, 0
	}
	return out, added
}

func ruleExists(rules []rule, candidate rule) bool {
	for i := range rules {
		if rules[i] == candidate {
			return true
		}
	}
	return false
}

// gitignoreStampChanged reports a new, removed, or rewritten .gitignore.
// The directory mtime is not a substitute: rewriting the file leaves it unchanged.
func gitignoreStampChanged(dirAbs, rel string, stamps map[string]time.Time) bool {
	info, err := os.Stat(filepath.Join(dirAbs, ".gitignore"))
	prev, tracked := stamps[rel]
	if err != nil {
		return tracked
	}
	if !info.Mode().IsRegular() {
		return tracked
	}
	if !tracked {
		return true
	}
	return !info.ModTime().Equal(prev)
}

func dirKnown(dirs map[string]time.Time, rel string) bool {
	if _, ok := dirs[rel]; ok {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	for key := range dirs {
		if strings.EqualFold(key, rel) {
			return true
		}
	}
	return false
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
		items = append(items, item{rel: rel, snap: snap})
	}
	c.mu.Unlock()
	for _, item := range items {
		abs := filepath.Join(root, filepath.FromSlash(item.rel))
		info, err := os.Lstat(abs)
		if item.snap.gone {
			// A skipped file stays out while its size and mtime match the
			// tombstone. A later edit does not change the directory mtime,
			// so the identity on the snap is what brings it back.
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if info.Size() == item.snap.size && info.ModTime().Equal(item.snap.mod) {
				continue
			}
			NoteDirty(abs)
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() != item.snap.size || !info.ModTime().Equal(item.snap.mod) {
			NoteDirty(abs)
		}
	}
}
