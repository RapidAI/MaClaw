package agent

import (
	"bytes"
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp/syntax"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxSearchIndexCacheRoot = 4
	searchIndexCacheTTL     = 10 * time.Minute
	maxDirtySearchFiles     = 1000
	maxDirtySearchRatio     = 0.25
	maxDirtySearchScanTime  = 3 * time.Second
)

var maxIndexedSearchFiles = 100000

type localSearchIndex struct {
	key          string
	root         string
	glob         string
	exclude      string
	fileType     string
	hidden       bool
	builtAt      time.Time
	usedAt       time.Time
	files        []string
	fileSet      map[string]bool
	fileIDs      map[string]int
	fileMeta     map[string]localSearchFileMeta
	postings     map[string][]int
	complete     bool
	kind         string
	reconciledAt time.Time
	// unlisted directories could not be listed. The snapshot omits files
	// there. A query reads those directories again when listing works.
	unlisted []string
	// unread files were listed, but a content read failed. The snapshot has
	// no trigrams for them, or the trigrams are from an older read. A query
	// reads those files again. Reconcile drops a path once a later read
	// indexes it.
	unread []string
}

type localSearchFileMeta struct {
	size      int64
	modTime   time.Time
	signature uint64
}

type searchCandidateStats struct {
	indexed        bool
	indexedFiles   int
	candidateFiles int
	dirtyFiles     int
	rebuilt        bool
	walkStopped    bool
	fallbackReason string
	candidateTime  time.Duration
	scanTime       time.Duration
	totalTime      time.Duration
}

var searchIndexCache = struct {
	sync.Mutex
	byRoot map[string]*localSearchIndex
}{
	byRoot: make(map[string]*localSearchIndex),
}

func indexedSearchCandidates(base, pattern, globPattern, excludePattern, fileType string, fixedString, includeHidden bool) ([]string, bool, searchCandidateStats) {
	return indexedSearchCandidatesCtx(context.Background(), base, pattern, globPattern, excludePattern, fileType, fixedString, includeHidden, false)
}

func indexedSearchCandidatesCtx(ctx context.Context, base, pattern, globPattern, excludePattern, fileType string, fixedString, includeHidden, noIgnore bool) ([]string, bool, searchCandidateStats) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		// A file path is a filter on the published corpus, not a separate
		// search mode. The agent names one file after another; those calls
		// have to see the corpus a directory search already built. Git walk
		// and gitignore reads stay in the warmer. A cold corpus, a file the
		// corpus does not list, or a pattern the index cannot filter still
		// reads the file.
		if err == nil && !info.IsDir() {
			warmSearchIndexForPath(base, includeHidden)
			if files, fileStats, handled := indexedFileCandidates(ctx, base, info, pattern, globPattern, excludePattern, fileType, fixedString, includeHidden); handled {
				if fileStats.indexed {
					return files, true, fileStats
				}
				return nil, false, fileStats
			}
		}
		return nil, false, searchCandidateStats{fallbackReason: "base_not_directory"}
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return nil, false, searchCandidateStats{fallbackReason: "bad_root"}
	}
	root = filepath.Clean(root)
	// TODO|FIXME has no shared literal. Each branch still has its own
	// trigrams, so the index is the OR of those branches. A branch that can
	// match with no trigram makes the OR unusable; the query full-scans, and
	// the corpus build still starts so the next literal search is not cold.
	query := trigramQueryForPattern(pattern, fixedString)
	if !query.selective() {
		if err := ctx.Err(); err != nil {
			return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
		}
		scheduleSearchIndexes(root, globPattern, excludePattern, fileType, includeHidden)
		return nil, false, searchCandidateStats{fallbackReason: "no_required_literal"}
	}

	idx, reason := lookupSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	if idx == nil && reason == "" {
		// A published corpus already lists every file this query can return
		// when it is a safe ancestor, or this same directory with the same
		// gitignore and only extra user filters.
		idx = publishedCorpusCoveringDir(root, includeHidden, excludePattern, noIgnore)
	}
	if idx == nil {
		if err := ctx.Err(); err != nil {
			return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
		}
		if reason == "" {
			scheduleSearchIndexes(root, globPattern, excludePattern, fileType, includeHidden)
			reason = "index_unavailable"
		}
		return nil, false, searchCandidateStats{fallbackReason: reason}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
	}
	// The corpus never listed this directory. It can be read now, so the
	// corpus is not the candidate set. The caller walks the directory.
	// The probe and the supplement share this deadline. Time spent proving
	// the directory still cannot be listed is not another walk budget.
	deadline := time.Now().Add(maxSearchWalkTime)
	if queryInsideListableUnlisted(ctx, idx, root, deadline) {
		scheduleSearchIndexes(root, globPattern, excludePattern, fileType, includeHidden)
		return nil, false, searchCandidateStats{fallbackReason: "unlisted_directory"}
	}
	stats := searchCandidateStats{indexed: true, indexedFiles: indexedSearchFileCount(idx)}

	candidates := candidateIDsForTrigramQuery(idx, query)
	files := filterIndexedCandidateFiles(idx, root, globPattern, excludePattern, fileType, candidates)
	extra, extraErr := filesUnderListableUnlisted(ctx, idx, root, globPattern, excludePattern, fileType, includeHidden, deadline)
	if extraErr != nil && extraErr != errSearchWalkDeadline {
		if ctx.Err() != nil {
			return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
		}
		return nil, false, searchCandidateStats{fallbackReason: "ignore_pattern:" + extraErr.Error()}
	}
	if len(extra) > 0 {
		files = append(files, extra...)
	}
	if extraErr == errSearchWalkDeadline {
		stats.walkStopped = true
	}
	// A listed file that could not be read is unknown, not a trigram miss.
	files = append(files, unreadSearchPaths(idx, root, globPattern, excludePattern, fileType)...)
	noted := notedSearchPaths(root, globPattern, excludePattern, fileType, includeHidden)
	if ctx.Err() != nil {
		return nil, false, searchCandidateStats{fallbackReason: "cancelled"}
	}
	// Nested rules run after the note lock is released. Reading a .gitignore
	// under that lock would stall the write hook.
	files, ignoreErr := dropTreeIgnoredPaths(ctx, root, files)
	if ignoreErr != nil {
		return nil, false, searchCandidateStats{fallbackReason: "ignore_pattern:" + ignoreErr.Error()}
	}
	noted, ignoreErr = dropTreeIgnoredPaths(ctx, root, noted)
	if ignoreErr != nil {
		return nil, false, searchCandidateStats{fallbackReason: "ignore_pattern:" + ignoreErr.Error()}
	}
	stats.candidateFiles = len(files)
	stats.dirtyFiles = len(noted)
	files = append(files, noted...)
	files = dedupeSearchPaths(files)
	return files, true, stats
}

// indexedFileCandidates answers one file from a published corpus.
// handled is false when the caller must read the file itself.
// A negative answer (handled, indexed, no paths) means the corpus lists the
// file, its size and mtime still match, the trigrams are absent, and a later
// read has not failed. A failed re-read leaves the old trigrams in place, so
// that file is opened even when the snapshot metadata still matches.
func indexedFileCandidates(ctx context.Context, base string, info os.FileInfo, pattern, globPattern, excludePattern, fileType string, fixedString, includeHidden bool) ([]string, searchCandidateStats, bool) {
	if info == nil || ctx.Err() != nil {
		return nil, searchCandidateStats{}, false
	}
	query := trigramQueryForPattern(pattern, fixedString)
	if !query.selective() {
		return nil, searchCandidateStats{}, false
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, searchCandidateStats{}, false
	}
	abs = filepath.Clean(abs)
	idx, id := publishedCorpusForFile(abs, includeHidden, excludePattern)
	if idx == nil {
		return nil, searchCandidateStats{}, false
	}
	stats := searchCandidateStats{indexed: true, indexedFiles: indexedSearchFileCount(idx)}
	baseName := filepath.Base(abs)
	if !matchesSearchFilters(baseName, false, globPattern, excludePattern, fileType) {
		return []string{}, stats, true
	}
	decide := func(read bool) ([]string, searchCandidateStats, bool) {
		excluded, ignoreErr := treeIgnoreExcludes(ctx, baseName, false)
		if ignoreErr != nil {
			return nil, searchCandidateStats{fallbackReason: "ignore_pattern:" + ignoreErr.Error()}, true
		}
		if excluded || !read {
			return []string{}, stats, true
		}
		stats.candidateFiles = 1
		return []string{base}, stats, true
	}
	if searchNoteCovers(abs) {
		stats.dirtyFiles = 1
		return decide(true)
	}
	// Unread means the stored trigrams are missing or stale. Matching size
	// and mtime does not make them current: the corpus recorded that it
	// could not read the file. The directory search opens that path again.
	if searchUnreadCovers(idx, abs) || trigramCandidatesInclude(idx, query, id) || searchFileMetaStale(idx, id, info) {
		return decide(true)
	}
	// A clean miss never reads the file. A broken ignore pattern still has
	// to fail the search, the same way the directory walk does.
	return decide(false)
}

// publishedCorpusForFile returns the published corpus that lists path and
// that file's id in it. A longer cleaned root wins only when it lists the
// file. Equal-length roots prefer the exclude key of this query, and the
// same match class prefers the later reconcile. Selection does not walk up
// to .git or read a gitignore. The id is resolved once; file id 0 is a live
// file.
func publishedCorpusForFile(path string, includeHidden bool, excludePattern string) (*localSearchIndex, int) {
	now := time.Now()
	searchIndexCache.Lock()
	cands := make([]*localSearchIndex, 0, len(searchIndexCache.byRoot))
	for _, idx := range searchIndexCache.byRoot {
		if idx == nil || !idx.complete || idx.kind != searchIndexCorpusKind || idx.hidden != includeHidden {
			continue
		}
		if idx.glob != "" || idx.fileType != "" {
			continue
		}
		if _, usable := searchIndexUsable(idx, now); !usable {
			continue
		}
		if !searchPathUnder(idx.root, path) || searchPathEqual(idx.root, path) {
			continue
		}
		cands = append(cands, idx)
	}
	searchIndexCache.Unlock()

	var best *localSearchIndex
	bestID := 0
	bestLen := -1
	for _, idx := range cands {
		id, ok := fileIDInIndex(idx, path)
		if !ok {
			continue
		}
		n := len(filepath.Clean(idx.root))
		sameLen := best != nil && n == bestLen
		matches := idx.exclude == excludePattern
		bestMatches := best != nil && best.exclude == excludePattern
		excludeBetter := sameLen && matches && !bestMatches
		// Same depth and the same exclude match class: the later reconcile
		// has the newer metadata. Map order must not decide. A matching
		// exclude is not replaced by a newer corpus that does not match.
		newer := sameLen && matches == bestMatches && idx.reconciledAt.After(best.reconciledAt)
		if best == nil || n > bestLen || excludeBetter || newer {
			best = idx
			bestID = id
			bestLen = n
		}
	}
	if best != nil {
		searchIndexCache.Lock()
		best.usedAt = now
		searchIndexCache.Unlock()
	}
	return best, bestID
}

// publishedCorpusCoveringDir returns a published corpus that can answer a
// directory search. A strict ancestor has to be gitignore-only: those
// omissions are the same files parent rules drop again at query time. This
// same directory can reuse that gitignore corpus when the query exclude is
// the corpus exclude plus optional user filters, or an empty corpus. A
// user-exclude corpus, a changed gitignore, a git boundary, or a directory
// the build skipped is not covered, and the caller builds a corpus instead.
func publishedCorpusCoveringDir(dir string, includeHidden bool, queryExclude string, noIgnore bool) *localSearchIndex {
	now := time.Now()
	searchIndexCache.Lock()
	cands := make([]*localSearchIndex, 0, len(searchIndexCache.byRoot))
	for _, idx := range searchIndexCache.byRoot {
		if idx == nil || !idx.complete || idx.kind != searchIndexCorpusKind || idx.hidden != includeHidden {
			continue
		}
		if idx.glob != "" || idx.fileType != "" {
			continue
		}
		if _, usable := searchIndexUsable(idx, now); !usable {
			continue
		}
		cands = append(cands, idx)
	}
	searchIndexCache.Unlock()

	var best *localSearchIndex
	bestLen := -1
	for _, idx := range cands {
		if !corpusCoversSearchDir(idx, dir, queryExclude, noIgnore) {
			continue
		}
		n := len(filepath.Clean(idx.root))
		if best == nil || n > bestLen {
			best = idx
			bestLen = n
			continue
		}
		if n < bestLen {
			continue
		}
		// Same root, two exclude keys. The gitignore corpus is the smaller
		// complete set. An empty-exclude corpus still answers, but it drags
		// ignored trees through the posting scan.
		if (idx.exclude != "") != (best.exclude != "") {
			if idx.exclude != "" {
				best = idx
			}
			continue
		}
		if idx.reconciledAt.After(best.reconciledAt) {
			best = idx
		}
	}
	if best != nil {
		searchIndexCache.Lock()
		best.usedAt = now
		searchIndexCache.Unlock()
	}
	return best
}

func corpusCoversSearchDir(idx *localSearchIndex, dir, queryExclude string, noIgnore bool) bool {
	if idx == nil || idx.root == "" || dir == "" {
		return false
	}
	if !searchPathUnder(idx.root, dir) {
		return false
	}
	if searchPathEqual(idx.root, dir) {
		return sameRootCorpusCovers(idx, queryExclude, noIgnore)
	}
	// node_modules, vendor, hidden directories, and the other skip names are
	// not in this corpus. Naming one of them as the search root still reads it.
	if corpusBuildSkippedRel(idx, dir) {
		return false
	}
	// A junction or symlink is not a directory to the build, so the walk
	// never entered it. Naming that path still follows the link.
	if corpusReparseSkippedRel(idx, dir) {
		return false
	}
	if idx.exclude != "" && (noIgnore || !excludeIsGitignoreOnly(idx.exclude)) {
		return false
	}
	if idx.exclude != "" && gitBoundaryBetween(idx.root, dir) {
		return false
	}
	return !corpusSkippedDir(idx, dir)
}

// sameRootCorpusCovers reports that this directory's corpus still lists
// every file the query can return. no_ignore needs the empty-exclude corpus.
// A gitignore-only exclude matches when the query is that exclude, or user
// globs joined in front of it. A changed gitignore is not a suffix, and a
// corpus built with a user exclude is not gitignore-only.
func sameRootCorpusCovers(idx *localSearchIndex, queryExclude string, noIgnore bool) bool {
	if noIgnore {
		return idx.exclude == ""
	}
	if idx.exclude == "" || idx.exclude == queryExclude {
		return true
	}
	if !excludeIsGitignoreOnly(idx.exclude) {
		return false
	}
	return strings.HasSuffix(queryExclude, idx.exclude)
}

func excludeIsGitignoreOnly(exclude string) bool {
	return exclude == "" || strings.HasPrefix(exclude, "\x00")
}

// gitBoundaryBetween reports a repository root strictly between root and dir,
// including dir itself. The ancestor build applied its gitignore across that
// boundary, so its file set is not the nested repository's file set.
func gitBoundaryBetween(root, dir string) bool {
	current := filepath.Clean(dir)
	root = filepath.Clean(root)
	for i := 0; i < 32; i++ {
		if searchPathEqual(current, root) {
			return false
		}
		if searchDirHasGit(current) {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
		current = parent
	}
	return true
}

// corpusBuildSkippedRel reports a skip-directory name between the corpus root
// and dir. The build never enters those names. An exclude rule is separate:
// corpusSkippedDir checks that. An empty exclude still skips these names.
func corpusBuildSkippedRel(idx *localSearchIndex, dir string) bool {
	if idx == nil {
		return false
	}
	rel, ok := searchIndexRelativePath(idx.root, dir)
	if !ok || rel == "" || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		if shouldSkipSearchDir(part, idx.hidden) {
			return true
		}
	}
	return false
}

// corpusReparseSkippedRel reports a path component the build could not
// descend into. On Windows a directory junction is ModeIrregular and IsDir
// is false, and a directory symlink is ModeSymlink. The build lists neither.
// An Lstat error fails closed so this corpus is not used.
func corpusReparseSkippedRel(idx *localSearchIndex, dir string) bool {
	if idx == nil {
		return false
	}
	rel, ok := searchIndexRelativePath(idx.root, dir)
	if !ok || rel == "" || rel == "." {
		return false
	}
	current := idx.root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() {
			return true
		}
	}
	return false
}

// corpusSkippedDir reports that the build's exclude walk never entered dir.
// A search inside that directory still has to see the files.
func corpusSkippedDir(idx *localSearchIndex, dir string) bool {
	if idx == nil || idx.exclude == "" {
		return false
	}
	rel, ok := searchIndexRelativePath(idx.root, dir)
	if !ok || rel == "." {
		return true
	}
	rel = filepath.ToSlash(rel)
	for rel != "" && rel != "." {
		excluded, err := matchGlob(idx.exclude, rel, true)
		if err != nil || excluded {
			return true
		}
		slash := strings.LastIndex(rel, "/")
		if slash < 0 {
			break
		}
		rel = rel[:slash]
	}
	return false
}

// searchFileIDsFromSlice keys a file id by searchPathKey. Tombstone slots are
// omitted. File id 0 is live, so a missing key is comma-ok false, not id 0.
func searchFileIDsFromSlice(files []string) map[string]int {
	ids := make(map[string]int, len(files))
	for id, path := range files {
		if path == "" {
			continue
		}
		ids[searchPathKey(path)] = id
	}
	return ids
}

func searchFileIDMatches(idx *localSearchIndex, id int, abs string) bool {
	return idx != nil && id >= 0 && id < len(idx.files) && idx.files[id] != "" && searchPathEqual(idx.files[id], abs)
}

func fileIDInIndex(idx *localSearchIndex, abs string) (int, bool) {
	if idx == nil || abs == "" {
		return 0, false
	}
	// A built corpus map is complete. A missing key is not listed, including a
	// different case on Windows. A key whose slot no longer names this path
	// falls back to the slice.
	if idx.fileIDs != nil {
		if id, ok := idx.fileIDs[searchPathKey(abs)]; ok {
			if searchFileIDMatches(idx, id, abs) {
				return id, true
			}
			return fileIDInIndexScan(idx, abs)
		}
		return 0, false
	}
	return fileIDInIndexScan(idx, abs)
}

func fileIDInIndexScan(idx *localSearchIndex, abs string) (int, bool) {
	if idx.fileSet[abs] {
		for id, path := range idx.files {
			if path == abs {
				return id, true
			}
		}
	}
	if runtime.GOOS != "windows" {
		return 0, false
	}
	for id, path := range idx.files {
		if path != "" && strings.EqualFold(path, abs) {
			return id, true
		}
	}
	return 0, false
}

// trigramCandidatesInclude reports whether one file id satisfies the query.
// It probes that id in the sorted postings and does not build the candidate
// list for the rest of the corpus.
func trigramCandidatesInclude(idx *localSearchIndex, query trigramQuery, id int) bool {
	// An empty clause matches every live id, the same set candidateIDsForTrigramQuery
	// returns. A tombstone keeps its slot so posting ids stay aligned, and it is
	// not a live file.
	if idx == nil || id < 0 || id >= len(idx.files) || idx.files[id] == "" {
		return false
	}
	for _, clause := range query.clauses {
		if len(clause) == 0 || postingClauseHasID(idx, clause, id) {
			return true
		}
	}
	return false
}

func postingClauseHasID(idx *localSearchIndex, trigrams []string, id int) bool {
	if len(trigrams) == 0 {
		return true
	}
	ordered := trigrams
	if len(trigrams) > 1 {
		ordered = orderTrigramsByPosting(idx, trigrams)
	}
	for _, trigram := range ordered {
		ids := idx.postings[trigram]
		i := sort.SearchInts(ids, id)
		if i >= len(ids) || ids[i] != id {
			return false
		}
	}
	return true
}

func searchUnreadCovers(idx *localSearchIndex, path string) bool {
	if idx == nil || path == "" {
		return false
	}
	for _, unread := range idx.unread {
		if unread != "" && searchPathEqual(unread, path) {
			return true
		}
	}
	return false
}

func searchNoteCovers(path string) bool {
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	if _, ok := notedSearchDirty.paths[path]; ok {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	for noted := range notedSearchDirty.paths {
		if strings.EqualFold(noted, path) {
			return true
		}
	}
	return false
}

func searchFileMetaStale(idx *localSearchIndex, id int, info os.FileInfo) bool {
	if idx == nil || info == nil || id < 0 || id >= len(idx.files) {
		return true
	}
	meta, ok := idx.fileMeta[idx.files[id]]
	if !ok {
		return true
	}
	return meta.size != info.Size() || !meta.modTime.Equal(info.ModTime())
}

func searchIndexRelativePath(root, path string) (string, bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if searchPathUnder(root, path) {
		if searchPathEqual(root, path) {
			return ".", true
		}
		rel := path[len(searchPathPrefix(root)):]
		if rel == "" {
			return ".", true
		}
		return rel, true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return rel, true
}
func filterIndexedCandidateFiles(idx *localSearchIndex, root, globPattern, excludePattern, fileType string, candidates []int) []string {
	files := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if id < 0 || id >= len(idx.files) {
			continue
		}
		path := idx.files[id]
		if path == "" {
			continue
		}
		rel, ok := searchIndexRelativePath(root, path)
		if !ok {
			continue
		}
		if !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
			continue
		}
		files = append(files, path)
	}
	return files
}

func indexedCandidatesFromTrigrams(idx *localSearchIndex, queryTrigrams []string) []int {
	if len(queryTrigrams) == 0 {
		if idx == nil {
			return nil
		}
		ids := make([]int, 0, len(idx.files))
		for id, path := range idx.files {
			if path != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	candidates := append([]int(nil), idx.postings[queryTrigrams[0]]...)
	for _, trigram := range queryTrigrams[1:] {
		candidates = intersectSortedSearchIDs(candidates, idx.postings[trigram])
		if len(candidates) == 0 {
			break
		}
	}
	return candidates
}

func intersectSortedSearchIDs(a, b []int) []int {
	out := a[:0]
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

func cachedSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	return cachedSearchIndexCtx(context.Background(), root, globPattern, excludePattern, fileType, includeHidden)
}

func cachedSearchIndexCtx(ctx context.Context, root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	idx, reason := lookupSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	if idx != nil {
		return idx, true
	}
	if reason == "" {
		scheduleSearchIndexes(root, globPattern, excludePattern, fileType, includeHidden)
	}
	return nil, false
}

func rebuildCachedSearchIndex(root string) (*localSearchIndex, bool) {
	return rebuildCachedSearchIndexForScope(root, "", "", "", false)
}

func rebuildCachedSearchIndexForScope(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	return rebuildCachedSearchIndexForScopeCtx(context.Background(), root, globPattern, excludePattern, fileType, includeHidden)
}

func rebuildCachedSearchIndexForScopeCtx(ctx context.Context, root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	idx, ok := buildLocalSearchIndexCtx(ctx, root, globPattern, excludePattern, fileType, includeHidden)
	if !ok || (ctx != nil && ctx.Err() != nil) {
		return nil, false
	}
	key := idx.key
	kind := searchIndexCorpusKind
	if strings.TrimSpace(globPattern) != "" || strings.TrimSpace(fileType) != "" {
		key = searchIndexScopedKey(root, globPattern, excludePattern, fileType, includeHidden)
		kind = searchIndexScopedKind
	}
	publishSearchIndex(key, idx, kind)
	return idx, true
}

// maxSearchIndexBuildTime caps the wall-clock time for building a search
// index. If the directory tree is too large (e.g. user home directory),
// this prevents the search from blocking for minutes.
const maxSearchIndexBuildTime = 30 * time.Second

func buildLocalSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	return buildLocalSearchIndexCtx(context.Background(), root, globPattern, excludePattern, fileType, includeHidden)
}

func buildLocalSearchIndexCtx(ctx context.Context, root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	now := time.Now()
	deadline := now.Add(maxSearchIndexBuildTime)
	if d, hasDeadline := ctx.Deadline(); hasDeadline {
		deadline = d
	}
	fileLimit := maxIndexedSearchFiles
	idx := &localSearchIndex{
		key:      searchIndexCacheKey(root, globPattern, excludePattern, fileType, includeHidden),
		root:     root,
		glob:     globPattern,
		exclude:  excludePattern,
		fileType: fileType,
		hidden:   includeHidden,
		builtAt:  now,
		usedAt:   now,
		fileSet:  make(map[string]bool),
		fileMeta: make(map[string]localSearchFileMeta),
		postings: make(map[string][]int),
	}
	truncated := false
	var blocked []string
	var unread []string
	noted := notedSearchKeySet(root, globPattern, excludePattern, fileType, includeHidden)
	walkCount := 0
	err := searchIndexWalk(ctx, root, func(path string, d os.DirEntry, err error) error {
		// Check deadline every 512 entries to avoid per-entry syscall overhead.
		walkCount++
		if walkCount&511 == 0 {
			if ctx.Err() != nil || time.Now().After(deadline) {
				truncated = true
				return filepath.SkipAll
			}
		}
		if err != nil {
			if searchPathEqual(path, root) {
				return err
			}
			blocked = append(blocked, path)
			return nil
		}
		if path != root && isSearchSymlink(d) {
			if notedPath(noted, path) {
				noteSearchBuildIgnored(ctx, path)
			}
			return nil
		}
		if path != root && d.IsDir() && shouldSkipSearchDir(d.Name(), includeHidden) {
			return filepath.SkipDir
		}
		if path != root {
			if !includeHidden && isHiddenSearchPath(d.Name()) {
				return nil
			}
			rel, ok := searchIndexRelativePath(root, path)
			if !ok {
				return nil
			}
			if excludePattern != "" {
				excluded, err := matchGlob(excludePattern, rel, d.IsDir())
				if err != nil {
					return err
				}
				if excluded && d.IsDir() {
					return filepath.SkipDir
				}
				if excluded {
					return nil
				}
			}
			if !d.IsDir() && !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		if len(idx.files) >= fileLimit {
			truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			if searchUnreadableCandidate(path) {
				unread = append(unread, path)
			}
			return nil
		}
		if info.Size() > MaxSearchFileSize {
			if notedPath(noted, path) {
				noteSearchBuildIgnored(ctx, path)
			}
			return nil
		}
		if ctx.Err() != nil {
			truncated = true
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// A directory junction is not a file. Recording it makes the next
			// search open a directory and fail the whole query.
			if !searchDirReparse(path, info) {
				unread = append(unread, path)
			}
			return nil
		}
		if bytes.Contains(data[:min(len(data), 8000)], []byte{0}) {
			if notedPath(noted, path) {
				noteSearchBuildIgnored(ctx, path)
			}
			return nil
		}
		id := len(idx.files)
		idx.files = append(idx.files, path)
		idx.fileSet[path] = true
		idx.fileMeta[path] = localSearchFileMeta{size: info.Size(), modTime: info.ModTime(), signature: searchContentSignature(data)}
		for trigram := range contentTrigramsBytes(data) {
			idx.postings[trigram] = append(idx.postings[trigram], id)
		}
		return nil
	})
	noteSearchBuildBlocked(ctx, blocked)
	if ctx.Err() != nil {
		markSearchBuild(ctx, truncated, true)
		return nil, false
	}
	if err != nil || truncated || len(idx.files) == 0 {
		markSearchBuild(ctx, truncated, false)
		if err == nil && !truncated && len(idx.files) == 0 && len(blocked) == 0 {
			markSearchBuildEmpty(ctx)
		}
		return nil, false
	}
	idx.unlisted = cleanSearchUnlisted(blocked)
	idx.unread = cleanSearchUnread(unread)
	idx.fileIDs = searchFileIDsFromSlice(idx.files)
	return idx, true
}

// queryInsideListableUnlisted reports that dir is an unlisted directory, or
// sits inside one, and listing dir itself now succeeds. The corpus omitted
// files under that unlisted directory, so a finished walk of dir is the
// candidate set. Listing an ancestor is a different fact: a child that still
// cannot be listed keeps the files already indexed, and a listable child of
// an unlistable directory is walked.
func queryInsideListableUnlisted(ctx context.Context, idx *localSearchIndex, dir string, deadline time.Time) bool {
	if !searchInsideUnlisted(idx, dir) {
		return false
	}
	return searchDirListable(ctx, dir, deadline)
}

// searchInsideUnlisted reports that dir is an unlisted directory or sits
// inside one. Listing dir is a separate question.
func searchInsideUnlisted(idx *localSearchIndex, dir string) bool {
	if idx == nil || dir == "" {
		return false
	}
	for _, blocked := range idx.unlisted {
		if blocked != "" && searchPathUnder(blocked, dir) {
			return true
		}
	}
	return false
}

// filesUnderListableUnlisted lists files the corpus omitted because a
// directory could not be read. The walk includes that directory when the
// query is the directory or sits inside it, and it includes an unlisted
// child when the query is an ancestor. A listing error keeps the files
// already read. A directory that cannot be opened adds nothing, and the
// corpus keeps the files it already has. A cancelled walk drops the batch.
// The search walk deadline keeps the batch and reports that the tree stopped.
func filesUnderListableUnlisted(ctx context.Context, idx *localSearchIndex, root, globPattern, excludePattern, fileType string, includeHidden bool, deadline time.Time) ([]string, error) {
	if idx == nil || len(idx.unlisted) == 0 {
		return nil, nil
	}
	var files []string
	// deadline is the query's walk deadline, already shared with the
	// listability probe. A deadline that is already past still visits the
	// first entries: the full scan checks on this same 512 cadence.
	walkCount := 0
	var stopped error
	for _, walkRoot := range unlistedWalkTargets(idx, root) {
		var batch []string
		err := walkDirCtx(ctx, walkRoot, func(path string, d os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			walkCount++
			if walkCount&511 == 0 {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !time.Now().Before(deadline) {
					return errSearchWalkDeadline
				}
			}
			// The build, reconcile, and full scan treat a listing error as
			// unknown and keep going. Returning it aborts the rest of this
			// tree, and the batch below would then hide files already read.
			if walkErr != nil {
				return nil
			}
			if path != walkRoot && isSearchSymlink(d) {
				return nil
			}
			if path != walkRoot && d.IsDir() && shouldSkipSearchDir(d.Name(), includeHidden) {
				return filepath.SkipDir
			}
			if path != walkRoot && !includeHidden && isHiddenSearchPath(d.Name()) {
				return nil
			}
			// The same directory prune as the full scan. The walk budget has
			// to be spent on files this query can return. The search root
			// itself stays, so naming an ignored directory still lists it.
			if !searchPathEqual(path, root) {
				rel, relOK := searchIndexRelativePath(root, path)
				if !relOK {
					return nil
				}
				if excludePattern != "" {
					excluded, matchErr := matchGlob(excludePattern, rel, d.IsDir())
					if matchErr != nil {
						return matchErr
					}
					if excluded && d.IsDir() {
						return filepath.SkipDir
					}
				}
				if ignored, ignoreErr := treeIgnoreExcludes(ctx, rel, d.IsDir()); ignoreErr != nil {
					return ignoreErr
				} else if ignored {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					return nil
				}
				if !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
					return nil
				}
				info, infoErr := d.Info()
				if infoErr != nil || info.Size() > MaxSearchFileSize {
					return nil
				}
				batch = append(batch, path)
				return nil
			}
			return nil
		})
		if err == errSearchWalkDeadline {
			stopped = err
			files = append(files, batch...)
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		files = append(files, batch...)
	}
	return files, stopped
}

// unlistedWalkTargets is the directories a query has to list again.
// An ancestor query lists each unlisted child. The unlisted directory
// itself, and a search inside it, lists the directory the query named.
func unlistedWalkTargets(idx *localSearchIndex, root string) []string {
	if idx == nil || root == "" {
		return nil
	}
	var targets []string
	seen := make(map[string]struct{}, len(idx.unlisted))
	add := func(path string) {
		if path == "" {
			return
		}
		key := searchPathKey(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, path)
	}
	for _, blocked := range idx.unlisted {
		if blocked == "" {
			continue
		}
		if searchPathUnder(root, blocked) {
			add(blocked)
			continue
		}
		if searchPathUnder(blocked, root) {
			add(root)
		}
	}
	return targets
}

// unreadSearchPaths is the files a query has to open because a content
// read failed. An indexed path stays on the list when that read was a
// later change, so the old trigrams are not the candidate set. Glob and
// exclude still apply. The caller skips a file that is still unreadable,
// and reads one that is readable now.
func unreadSearchPaths(idx *localSearchIndex, root, globPattern, excludePattern, fileType string) []string {
	if idx == nil || len(idx.unread) == 0 {
		return nil
	}
	files := make([]string, 0, len(idx.unread))
	for _, path := range idx.unread {
		if path == "" {
			continue
		}
		rel, ok := searchIndexRelativePath(root, path)
		if !ok || !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
			continue
		}
		files = append(files, path)
	}
	if len(files) == 0 {
		return nil
	}
	return files
}

func pathInSearchIndex(idx *localSearchIndex, path string) bool {
	if idx == nil || path == "" {
		return false
	}
	if idx.fileSet[path] {
		return true
	}
	_, ok := fileIDInIndex(idx, path)
	return ok
}

func searchIndexCacheKey(root, globPattern, excludePattern, fileType string, includeHidden bool) string {
	return strings.Join([]string{
		filepath.Clean(root),
		strings.TrimSpace(globPattern),
		strings.TrimSpace(excludePattern),
		strings.TrimSpace(strings.ToLower(fileType)),
		boolSearchCacheKeyPart(includeHidden),
	}, "\x00")
}

func boolSearchCacheKeyPart(v bool) string {
	if v {
		return "hidden"
	}
	return "nohidden"
}

func shouldRebuildSearchIndex(idx *localSearchIndex, dirtyCount int) bool {
	if idx == nil || dirtyCount <= 0 {
		return false
	}
	if dirtyCount > maxDirtySearchFiles {
		return true
	}
	indexedCount := indexedSearchFileCount(idx)
	if indexedCount == 0 {
		return true
	}
	return float64(dirtyCount)/float64(indexedCount) >= maxDirtySearchRatio
}

func indexedSearchFileCount(idx *localSearchIndex) int {
	if idx == nil {
		return 0
	}
	if idx.fileSet != nil {
		return len(idx.fileSet)
	}
	n := 0
	for _, path := range idx.files {
		if path != "" {
			n++
		}
	}
	if n > 0 {
		return n
	}
	return len(idx.files)
}

func pruneSearchIndexCacheLocked(now time.Time) {
	for key, idx := range searchIndexCache.byRoot {
		if now.Sub(idx.builtAt) > searchIndexCacheTTL {
			delete(searchIndexCache.byRoot, key)
			stopSearchIndexReconciler(key)
			forgetSearchBuildSlot(key)
		}
	}
	for len(searchIndexCache.byRoot) > maxSearchIndexCacheRoot {
		var oldestKey string
		var oldestUsedAt time.Time
		for key, idx := range searchIndexCache.byRoot {
			if oldestKey == "" || idx.usedAt.Before(oldestUsedAt) {
				oldestKey = key
				oldestUsedAt = idx.usedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(searchIndexCache.byRoot, oldestKey)
		stopSearchIndexReconciler(oldestKey)
		forgetSearchBuildSlot(oldestKey)
	}
}

func (idx *localSearchIndex) dirtyCandidateFiles(globPattern, excludePattern, fileType string, includeHidden bool) []string {
	return idx.dirtyCandidateFilesCtx(context.Background(), globPattern, excludePattern, fileType, includeHidden)
}

func (idx *localSearchIndex) dirtyCandidateFilesCtx(ctx context.Context, globPattern, excludePattern, fileType string, includeHidden bool) []string {
	files, _ := idx.dirtyCandidateFilesWithStatusCtx(ctx, globPattern, excludePattern, fileType, includeHidden)
	return files
}

func (idx *localSearchIndex) dirtyCandidateFilesBudgetedCtx(ctx context.Context, globPattern, excludePattern, fileType string, includeHidden bool) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	dirtyCtx, cancel := context.WithTimeout(ctx, maxDirtySearchScanTime)
	defer cancel()
	return idx.dirtyCandidateFilesWithStatusCtx(dirtyCtx, globPattern, excludePattern, fileType, includeHidden)
}

func (idx *localSearchIndex) dirtyCandidateFilesWithStatusCtx(ctx context.Context, globPattern, excludePattern, fileType string, includeHidden bool) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	var files []string
	if ctx.Err() != nil {
		return files, false
	}
	complete := true
	walkCount := 0
	err := walkDirCtx(ctx, idx.root, func(path string, d os.DirEntry, err error) error {
		walkCount++
		if walkCount&511 == 0 && ctx.Err() != nil {
			complete = false
			return filepath.SkipAll
		}
		if err != nil {
			if path == idx.root {
				complete = false
			}
			return nil
		}
		if path != idx.root && isSearchSymlink(d) {
			return nil
		}
		if path != idx.root && d.IsDir() && shouldSkipSearchDir(d.Name(), includeHidden) {
			return filepath.SkipDir
		}
		if path != idx.root && !includeHidden && isHiddenSearchPath(d.Name()) {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, ok := searchIndexRelativePath(idx.root, path)
		if !ok {
			return nil
		}
		if !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSearchFileSize {
			return nil
		}
		if idx.fileSet[path] && idx.fileMetadataUnchanged(path, info) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		complete = false
	}
	if ctx.Err() != nil {
		complete = false
	}
	return files, complete
}

func (idx *localSearchIndex) fileMetadataUnchanged(path string, info os.FileInfo) bool {
	if idx == nil || info == nil {
		return false
	}
	meta, ok := idx.fileMeta[path]
	if !ok {
		return info.ModTime().Before(idx.builtAt)
	}
	if meta.size != info.Size() || !meta.modTime.Equal(info.ModTime()) {
		return false
	}
	if meta.modTime.Before(idx.builtAt) {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != meta.size {
		return false
	}
	return meta.signature == searchContentSignature(data)
}

func searchContentSignature(data []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return h.Sum64()
}

func literalSearchTerms(pattern string) []string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil
	}
	return literalSearchTermsFromText(strings.Join(requiredLiteralFragments(re), " "))
}

func literalSearchTermsForQuery(pattern string, fixedString bool) []string {
	if fixedString {
		return literalSearchTermsFromText(pattern)
	}
	return literalSearchTerms(pattern)
}

func literalSearchTermsFromText(text string) []string {
	type searchTerm struct {
		text string
		size int
	}
	var terms []searchTerm
	seen := make(map[string]bool)
	start := -1
	runeCount := 0
	hasCaseFold := false
	flush := func(end int) {
		if start < 0 {
			return
		}
		part := text[start:end]
		if runeCount >= 3 && !seen[part] {
			key := part
			if hasCaseFold {
				key = strings.ToLower(part)
			}
			if !seen[key] {
				seen[key] = true
				seen[part] = true
				terms = append(terms, searchTerm{text: part, size: runeCount})
			} else {
				seen[part] = true
			}
		}
		start = -1
		runeCount = 0
		hasCaseFold = false
	}
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			if start < 0 {
				start = i
			}
			if (r >= 'A' && r <= 'Z') || (r > unicode.MaxASCII && r <= 0x024F && unicode.IsUpper(r)) {
				hasCaseFold = true
			}
			runeCount++
			continue
		}
		flush(i)
	}
	flush(len(text))
	sort.Slice(terms, func(i, j int) bool {
		return terms[i].size > terms[j].size
	})
	if len(terms) > 3 {
		terms = terms[:3]
	}
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		out = append(out, term.text)
	}
	return out
}

func requiredLiteralFragments(re *syntax.Regexp) []string {
	if re == nil {
		return nil
	}
	switch re.Op {
	case syntax.OpLiteral:
		return []string{string(re.Rune)}
	case syntax.OpCapture, syntax.OpPlus:
		return requiredLiteralFragments(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min < 1 {
			return nil
		}
		return requiredLiteralFragments(re.Sub[0])
	case syntax.OpConcat:
		var out []string
		for _, sub := range re.Sub {
			out = append(out, requiredLiteralFragments(sub)...)
		}
		return out
	case syntax.OpAlternate:
		return commonLiteralFragments(re.Sub)
	default:
		return nil
	}
}

func commonLiteralFragments(subs []*syntax.Regexp) []string {
	if len(subs) == 0 {
		return nil
	}
	if shared := commonLiteralEdges(subs); len(shared) > 0 {
		return shared
	}
	common := make(map[string]bool)
	for _, fragment := range requiredLiteralFragments(subs[0]) {
		common[fragment] = true
	}
	for _, sub := range subs[1:] {
		next := make(map[string]bool)
		for _, fragment := range requiredLiteralFragments(sub) {
			if common[fragment] {
				next[fragment] = true
			}
		}
		common = next
		if len(common) == 0 {
			return nil
		}
	}
	out := make([]string, 0, len(common))
	for fragment := range common {
		out = append(out, fragment)
	}
	return out
}

func commonLiteralEdges(subs []*syntax.Regexp) []string {
	literals := make([]string, 0, len(subs))
	for _, sub := range subs {
		literal, ok := exactLiteralString(sub)
		if !ok {
			return nil
		}
		literals = append(literals, literal)
	}
	var out []string
	if prefix := commonStringPrefix(literals); len([]rune(prefix)) >= 3 {
		out = append(out, prefix)
	}
	if suffix := commonStringSuffix(literals); len([]rune(suffix)) >= 3 && suffix != outFirst(out) {
		out = append(out, suffix)
	}
	return out
}

func exactLiteralString(re *syntax.Regexp) (string, bool) {
	if re == nil {
		return "", false
	}
	switch re.Op {
	case syntax.OpLiteral:
		return string(re.Rune), true
	case syntax.OpCapture:
		return exactLiteralString(re.Sub[0])
	case syntax.OpConcat:
		var b strings.Builder
		for _, sub := range re.Sub {
			literal, ok := exactLiteralString(sub)
			if !ok {
				return "", false
			}
			b.WriteString(literal)
		}
		return b.String(), true
	default:
		return "", false
	}
}

func commonStringPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := []rune(items[0])
	for _, item := range items[1:] {
		runes := []rune(item)
		n := 0
		for n < len(prefix) && n < len(runes) && unicode.ToLower(prefix[n]) == unicode.ToLower(runes[n]) {
			n++
		}
		prefix = prefix[:n]
		if len(prefix) == 0 {
			return ""
		}
	}
	return string(prefix)
}

func commonStringSuffix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	suffix := []rune(items[0])
	for _, item := range items[1:] {
		runes := []rune(item)
		n := 0
		for n < len(suffix) && n < len(runes) && unicode.ToLower(suffix[len(suffix)-1-n]) == unicode.ToLower(runes[len(runes)-1-n]) {
			n++
		}
		suffix = suffix[len(suffix)-n:]
		if len(suffix) == 0 {
			return ""
		}
	}
	return string(suffix)
}

func outFirst(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

// maxTrigramQueryClauses caps AND-distribution of alternations. Past this the
// query matches every file instead of dropping a branch.
const maxTrigramQueryClauses = 32

// trigramQuery is a disjunction of trigram conjunctions. all means the
// pattern can match a file that contains none of the tracked trigrams, so
// the index must not filter. An empty clause list matches nothing.
//
// prefix and suffix are the first and last two runes of every match. exact
// means the match is that fixed literal, so a neighbor can join across the
// edge. A plus or a star is not exact: extra copies can sit in the middle.
type trigramQuery struct {
	all     bool
	clauses [][]string
	prefix  string
	suffix  string
	exact   bool
}

func (q trigramQuery) selective() bool {
	return !q.all && len(q.clauses) > 0
}

func trigramQueryAll() trigramQuery {
	return trigramQuery{all: true}
}

// trigramQueryEmpty is the empty string. Concatenation keeps the other side,
// including its edges. A star does not: the match may start with something else.
func trigramQueryEmpty() trigramQuery {
	return trigramQuery{all: true, exact: true}
}

func trigramQueryNone(q trigramQuery) bool {
	return !q.all && len(q.clauses) == 0
}

func trigramQueryAnd(a, b trigramQuery) trigramQuery {
	if a.all {
		return b
	}
	if b.all {
		return a
	}
	if len(a.clauses) == 0 || len(b.clauses) == 0 {
		return trigramQuery{}
	}
	if len(a.clauses)*len(b.clauses) > maxTrigramQueryClauses {
		return trigramQueryAll()
	}
	out := make([][]string, 0, len(a.clauses)*len(b.clauses))
	for _, left := range a.clauses {
		for _, right := range b.clauses {
			merged := make([]string, 0, len(left)+len(right))
			merged = append(merged, left...)
			merged = append(merged, right...)
			out = append(out, dedupeTrigrams(merged))
		}
	}
	return trigramQuery{clauses: uniqueTrigramClauses(out)}
}

func trigramQueryOr(a, b trigramQuery) trigramQuery {
	if a.all || b.all {
		return trigramQueryAll()
	}
	if len(a.clauses) == 0 {
		return b
	}
	if len(b.clauses) == 0 {
		return a
	}
	if len(a.clauses)+len(b.clauses) > maxTrigramQueryClauses {
		return trigramQueryAll()
	}
	clauses := make([][]string, 0, len(a.clauses)+len(b.clauses))
	clauses = append(clauses, a.clauses...)
	clauses = append(clauses, b.clauses...)
	return trigramQuery{clauses: uniqueTrigramClauses(clauses)}
}

func uniqueTrigramClauses(clauses [][]string) [][]string {
	if len(clauses) < 2 {
		return clauses
	}
	seen := make(map[string]struct{}, len(clauses))
	out := clauses[:0]
	for _, clause := range clauses {
		key := strings.Join(clause, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clause)
	}
	return out
}

func dedupeTrigrams(items []string) []string {
	if len(items) < 2 {
		return items
	}
	sort.Strings(items)
	out := items[:0]
	var prev string
	for i, item := range items {
		if i > 0 && item == prev {
			continue
		}
		prev = item
		out = append(out, item)
	}
	return out
}

func trigramQueryForPattern(pattern string, fixedString bool) trigramQuery {
	if fixedString {
		return trigramQueryFromLiteral(pattern)
	}
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return trigramQueryAll()
	}
	return trigramQueryFromRegexp(re.Simplify())
}

func trigramQueryFromLiteral(text string) trigramQuery {
	q := trigramQuery{
		exact:  true,
		prefix: capSearchEdge(text, false),
		suffix: capSearchEdge(text, true),
	}
	seen := contentTrigrams(text)
	if len(seen) == 0 {
		q.all = true
		return q
	}
	clause := make([]string, 0, len(seen))
	for trigram := range seen {
		clause = append(clause, trigram)
	}
	sort.Strings(clause)
	q.clauses = [][]string{clause}
	return q
}

// trigramEdgeRunes is the overlap a boundary trigram can take from one side.
const trigramEdgeRunes = 2

func capSearchEdge(s string, suffix bool) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= trigramEdgeRunes {
		return s
	}
	if suffix {
		runes = runes[len(runes)-trigramEdgeRunes:]
	} else {
		runes = runes[:trigramEdgeRunes]
	}
	return string(runes)
}

// trigramQueryConcat is regexp concatenation. Clauses from each side stay
// required, and so do trigrams that cross the two-rune edges. Short pieces
// such as the three literals in a{3} only become a trigram at that boundary.
func trigramQueryConcat(a, b trigramQuery) trigramQuery {
	if trigramQueryNone(a) {
		return a
	}
	if trigramQueryNone(b) {
		return b
	}
	merged := trigramQueryAnd(a, b)
	if boundary := a.suffix + b.prefix; boundary != "" {
		merged = trigramQueryAnd(merged, trigramQueryFromLiteral(boundary))
	}
	merged.exact = a.exact && b.exact
	if a.exact {
		merged.prefix = capSearchEdge(a.prefix+b.prefix, false)
	} else {
		merged.prefix = a.prefix
	}
	if b.exact {
		merged.suffix = capSearchEdge(a.suffix+b.suffix, true)
	} else {
		merged.suffix = b.suffix
	}
	return merged
}

func trigramQueryFromRegexp(re *syntax.Regexp) trigramQuery {
	if re == nil {
		return trigramQueryAll()
	}
	switch re.Op {
	case syntax.OpNoMatch:
		return trigramQuery{}
	case syntax.OpEmptyMatch:
		return trigramQueryEmpty()
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL, syntax.OpCharClass,
		syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary, syntax.OpStar, syntax.OpQuest:
		return trigramQueryAll()
	case syntax.OpLiteral:
		return trigramQueryFromLiteral(string(re.Rune))
	case syntax.OpCapture:
		if len(re.Sub) == 0 {
			return trigramQueryAll()
		}
		return trigramQueryFromRegexp(re.Sub[0])
	case syntax.OpPlus:
		return trigramQueryFromRepeat(re.Sub, 1, -1)
	case syntax.OpRepeat:
		return trigramQueryFromRepeat(re.Sub, re.Min, re.Max)
	case syntax.OpConcat:
		if len(re.Sub) == 0 {
			return trigramQueryEmpty()
		}
		query := trigramQueryFromRegexp(re.Sub[0])
		for _, sub := range re.Sub[1:] {
			// A match-nothing piece clears the rest. An unfilterable piece
			// does not: .*.*Target and a{3} still have a required trigram.
			query = trigramQueryConcat(query, trigramQueryFromRegexp(sub))
			if trigramQueryNone(query) {
				return query
			}
		}
		return query
	case syntax.OpAlternate:
		if len(re.Sub) == 0 {
			return trigramQuery{}
		}
		query := trigramQueryFromRegexp(re.Sub[0])
		for _, sub := range re.Sub[1:] {
			query = trigramQueryOr(query, trigramQueryFromRegexp(sub))
			if query.all {
				return query
			}
		}
		return query
	default:
		return trigramQueryAll()
	}
}

func trigramQueryFromRepeat(subs []*syntax.Regexp, min, max int) trigramQuery {
	if min < 1 || len(subs) == 0 || subs[0] == nil {
		return trigramQueryAll()
	}
	if text, ok := exactRegexpLiteral(subs[0]); ok && text != "" {
		q := trigramQueryFromLiteral(repeatLiteralForTrigrams(text, min))
		// max < 0 is unbounded. Extra copies can be inserted, so the minimum
		// string is not the whole match.
		if max != min {
			q.exact = false
		}
		return q
	}
	q := trigramQueryFromRegexp(subs[0])
	q.exact = false
	return q
}

func exactRegexpLiteral(re *syntax.Regexp) (string, bool) {
	if re == nil {
		return "", false
	}
	switch re.Op {
	case syntax.OpLiteral:
		return string(re.Rune), true
	case syntax.OpCapture:
		if len(re.Sub) != 1 {
			return "", false
		}
		return exactRegexpLiteral(re.Sub[0])
	case syntax.OpConcat:
		var b strings.Builder
		for _, sub := range re.Sub {
			part, ok := exactRegexpLiteral(sub)
			if !ok {
				return "", false
			}
			b.WriteString(part)
		}
		return b.String(), true
	default:
		return "", false
	}
}

// repeatLiteralForTrigrams returns the shortest prefix of strings.Repeat(text, min)
// that still contains every trigram of the full repeat. A periodic string
// repeats its windows after one period plus two runes.
func repeatLiteralForTrigrams(text string, min int) string {
	if min < 1 || text == "" {
		return ""
	}
	runeLen := utf8.RuneCountInString(text)
	if runeLen == 0 {
		return ""
	}
	copies := (runeLen + 2 + runeLen - 1) / runeLen
	if copies < 1 {
		copies = 1
	}
	if copies > min {
		copies = min
	}
	return strings.Repeat(text, copies)
}

func candidateIDsForTrigramQuery(idx *localSearchIndex, query trigramQuery) []int {
	if idx == nil || len(query.clauses) == 0 {
		return nil
	}
	if len(query.clauses) == 1 {
		return indexedCandidatesFromTrigrams(idx, orderTrigramsByPosting(idx, query.clauses[0]))
	}
	seen := make(map[int]struct{})
	ids := make([]int, 0)
	limit := len(idx.files)
	for _, clause := range query.clauses {
		if len(clause) == 0 {
			return indexedCandidatesFromTrigrams(idx, nil)
		}
		for _, id := range indexedCandidatesFromTrigrams(idx, orderTrigramsByPosting(idx, clause)) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if limit > 0 && len(ids) >= limit {
			break
		}
	}
	return ids
}

func orderTrigramsByPosting(idx *localSearchIndex, trigrams []string) []string {
	if idx == nil || len(trigrams) < 2 {
		return trigrams
	}
	ordered := append([]string(nil), trigrams...)
	sort.Slice(ordered, func(i, j int) bool {
		return len(idx.postings[ordered[i]]) < len(idx.postings[ordered[j]])
	})
	return ordered
}

type fileWarmKey struct {
	dir    string
	hidden bool
}

type fileWarmMemo struct {
	root    string
	exclude string
	at      time.Time
}

// fileWarmMemos remembers two facts. byDir is the corpus a file's directory
// warms, keyed by searchPathKey so Windows case variants share one entry.
// byRoot is the default exclude already resolved for a git root. A later
// directory in that same root still walks to .git, because a submodule
// boundary can sit in between, and then reuses the exclude without reading
// gitignore again. A directory memo skips both the walk and the read while
// its corpus is published, a build is in flight, or a failed build is still
// inside its retry cooldown. Entries expire with the index cache.
// busy is the directory whose discovery goroutine has not finished. The
// directory memo is written only after the git root is known, so this set
// covers the walk itself.
var fileWarmMemos = struct {
	sync.Mutex
	byDir  map[fileWarmKey]fileWarmMemo
	byRoot map[fileWarmKey]fileWarmMemo
	busy   map[fileWarmKey]struct{}
}{
	byDir:  map[fileWarmKey]fileWarmMemo{},
	byRoot: map[fileWarmKey]fileWarmMemo{},
	busy:   map[fileWarmKey]struct{}{},
}

func fileWarmDirectory(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return canonicalWarmDir(filepath.Dir(filepath.Clean(abs)))
}

func canonicalWarmDir(dir string) string {
	if dir == "" {
		return ""
	}
	return searchPathKey(dir)
}

func fileWarmMapKey(dir string, hidden bool) (fileWarmKey, bool) {
	dir = canonicalWarmDir(dir)
	if dir == "" {
		return fileWarmKey{}, false
	}
	return fileWarmKey{dir: dir, hidden: hidden}, true
}

func loadWarmMemo(memos map[fileWarmKey]fileWarmMemo, dir string, hidden bool) (fileWarmMemo, bool) {
	key, ok := fileWarmMapKey(dir, hidden)
	if !ok {
		return fileWarmMemo{}, false
	}
	fileWarmMemos.Lock()
	defer fileWarmMemos.Unlock()
	memo, ok := memos[key]
	if !ok || time.Since(memo.at) > searchIndexCacheTTL {
		return fileWarmMemo{}, false
	}
	return memo, true
}

func loadFileWarm(dir string, hidden bool) (fileWarmMemo, bool) {
	return loadWarmMemo(fileWarmMemos.byDir, dir, hidden)
}

func loadRootWarm(root string, hidden bool) (fileWarmMemo, bool) {
	return loadWarmMemo(fileWarmMemos.byRoot, root, hidden)
}

func storeWarmMemo(memos map[fileWarmKey]fileWarmMemo, dir, root, exclude string, hidden bool) {
	key, ok := fileWarmMapKey(dir, hidden)
	if !ok || root == "" {
		return
	}
	fileWarmMemos.Lock()
	defer fileWarmMemos.Unlock()
	now := time.Now()
	if len(memos) > 1024 {
		for itemKey, memo := range memos {
			if now.Sub(memo.at) > searchIndexCacheTTL {
				delete(memos, itemKey)
			}
		}
	}
	memos[key] = fileWarmMemo{root: root, exclude: exclude, at: now}
	for len(memos) > 1024 {
		var oldest fileWarmKey
		var oldestAt time.Time
		first := true
		for itemKey, memo := range memos {
			if itemKey == key {
				continue
			}
			if first || memo.at.Before(oldestAt) {
				oldest = itemKey
				oldestAt = memo.at
				first = false
			}
		}
		if first {
			return
		}
		delete(memos, oldest)
	}
}

func storeFileWarm(dir string, hidden bool, root, exclude string) {
	storeWarmMemo(fileWarmMemos.byDir, dir, root, exclude, hidden)
}

func storeRootWarm(root string, hidden bool, exclude string) {
	// Keep the walked root in the value. The cache key is case-sensitive,
	// and the map key is searchPathKey.
	storeWarmMemo(fileWarmMemos.byRoot, root, root, exclude, hidden)
}

func searchCorpusWarmSettled(root, exclude string, hidden bool) bool {
	key := searchIndexCorpusKey(root, exclude, hidden)
	// publishedComplete matches the short-circuit in scheduleSearchIndexBuild.
	// An in-flight build and a failed build inside searchIndexBuildRetry will
	// not start another build, so another git walk would only reread gitignore.
	return publishedComplete(key) || searchBuildInFlight(key) || reconcileRebuildCoolingDown(key)
}

func beginFileWarm(dir string, hidden bool) bool {
	key, ok := fileWarmMapKey(dir, hidden)
	if !ok {
		return true
	}
	fileWarmMemos.Lock()
	defer fileWarmMemos.Unlock()
	if _, busy := fileWarmMemos.busy[key]; busy {
		return false
	}
	fileWarmMemos.busy[key] = struct{}{}
	return true
}

func endFileWarm(dir string, hidden bool) {
	key, ok := fileWarmMapKey(dir, hidden)
	if !ok {
		return
	}
	fileWarmMemos.Lock()
	defer fileWarmMemos.Unlock()
	delete(fileWarmMemos.busy, key)
}

func fileWarmDiscoveryRunning(dir string, hidden bool) bool {
	key, ok := fileWarmMapKey(dir, hidden)
	if !ok {
		return false
	}
	fileWarmMemos.Lock()
	defer fileWarmMemos.Unlock()
	_, busy := fileWarmMemos.busy[key]
	return busy
}

// warmSearchIndexForPath schedules the corpus a later directory search of this
// file would use. The nearest git root is that corpus. Outside a repository
// the file's own directory is the corpus. A volume root is not indexed.
// It reports whether a discovery goroutine was started.
func warmSearchIndexForPath(path string, includeHidden bool) bool {
	dir := fileWarmDirectory(path)
	if memo, ok := loadFileWarm(dir, includeHidden); ok {
		if searchCorpusWarmSettled(memo.root, memo.exclude, includeHidden) {
			return false
		}
		// The directory already knows its git root. Schedule that corpus
		// again without walking or rereading gitignore. The memo expires
		// with the index cache, and the next search discovers from scratch.
		if !beginFileWarm(dir, includeHidden) {
			return false
		}
		go func() {
			defer endFileWarm(dir, includeHidden)
			scheduleSearchIndexes(memo.root, "", memo.exclude, "", includeHidden)
		}()
		return true
	}
	if !beginFileWarm(dir, includeHidden) {
		return false
	}
	// Git walk and gitignore reads stay off the file-search path.
	go func() {
		defer endFileWarm(dir, includeHidden)
		warmSearchIndex(path, includeHidden)
	}()
	return true
}

func warmSearchIndex(path string, includeHidden bool) {
	root := searchWarmRoot(path)
	if root == "" {
		return
	}
	dir := fileWarmDirectory(path)
	// A parent directory's memo is not an answer. searchWarmRoot already
	// stopped at this file's own git boundary, including a submodule gitfile.
	if memo, ok := loadRootWarm(root, includeHidden); ok {
		storeFileWarm(dir, includeHidden, memo.root, memo.exclude)
		if !searchCorpusWarmSettled(memo.root, memo.exclude, includeHidden) {
			scheduleSearchIndexes(memo.root, "", memo.exclude, "", includeHidden)
		}
		return
	}
	// Same exclude string as a default directory search of this root, including
	// the leading NUL that marks gitignore lines. Without it the warmed corpus
	// is a different cache key and the next directory search builds again.
	exclude := resolveSearchExcludePattern(nil, root)
	storeRootWarm(root, includeHidden, exclude)
	storeFileWarm(dir, includeHidden, root, exclude)
	scheduleSearchIndexes(root, "", exclude, "", includeHidden)
}

func searchWarmRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	abs = filepath.Clean(abs)
	dir := abs
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		dir = filepath.Dir(abs)
	}
	root := dir
	current := dir
	for i := 0; i < 32; i++ {
		if searchDirHasGit(current) {
			root = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if root == "" || filepath.Dir(root) == root {
		return ""
	}
	return root
}

func contentTrigrams(s string) map[string]bool {
	return contentTrigramsBytes([]byte(s))
}

func contentTrigramsBytes(data []byte) map[string]bool {
	seen := make(map[[3]byte]bool)
	var window [3]byte
	windowLen := 0
	for _, b := range data {
		if b >= utf8.RuneSelf {
			return contentTrigramsUnicodeBytes(data)
		}
		switch {
		case b >= 'A' && b <= 'Z':
			b += 'a' - 'A'
		case (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_':
		default:
			b = ' '
		}
		if windowLen < 3 {
			window[windowLen] = b
			windowLen++
		} else {
			window[0], window[1], window[2] = window[1], window[2], b
		}
		if windowLen < 3 || window[0] == ' ' || window[1] == ' ' || window[2] == ' ' {
			continue
		}
		seen[window] = true
	}
	out := make(map[string]bool, len(seen))
	for trigram := range seen {
		out[string(trigram[:])] = true
	}
	return out
}

func contentTrigramsUnicodeBytes(data []byte) map[string]bool {
	seen := make(map[[3]rune]bool)
	var window [3]rune
	windowLen := 0
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			r = ' '
		}
		data = data[size:]
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			if unicode.IsLetter(r) {
				r = searchFoldRune(r)
			}
		} else {
			r = ' '
		}
		if windowLen < 3 {
			window[windowLen] = r
			windowLen++
		} else {
			window[0], window[1], window[2] = window[1], window[2], r
		}
		if windowLen < 3 {
			continue
		}
		if window[0] == ' ' || window[1] == ' ' || window[2] == ' ' {
			continue
		}
		seen[window] = true
	}
	out := make(map[string]bool, len(seen))
	for trigram := range seen {
		out[string(trigram[:])] = true
	}
	return out
}

// searchFoldRune maps a letter to the representative shared with every rune
// in its Unicode simple-fold cycle, which is the cycle regexp (?i) matches.
// An ASCII member wins so U+017F folds to s. unicode.ToLower leaves U+017F
// unchanged and would drop a case-insensitive hit in an ASCII file.
func searchFoldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	best := r
	start := r
	for i := 0; i < 16; i++ {
		r = unicode.SimpleFold(r)
		if r == start {
			break
		}
		if r < utf8.RuneSelf {
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			return r
		}
		if r < best {
			best = r
		}
	}
	return best
}

func dedupeSortedStrings(items []string) []string {
	if len(items) < 2 {
		return items
	}
	out := items[:0]
	var last string
	for i, item := range items {
		if i == 0 || item != last {
			out = append(out, item)
			last = item
		}
	}
	return out
}
