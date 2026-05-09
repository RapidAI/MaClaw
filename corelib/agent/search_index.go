package agent

import (
	"bytes"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
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
)

var maxIndexedSearchFiles = 100000

type localSearchIndex struct {
	key      string
	root     string
	glob     string
	exclude  string
	fileType string
	hidden   bool
	builtAt  time.Time
	usedAt   time.Time
	files    []string
	fileSet  map[string]bool
	fileMeta map[string]localSearchFileMeta
	postings map[string][]int
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
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, false, searchCandidateStats{fallbackReason: "base_not_directory"}
	}
	terms := literalSearchTermsForQuery(pattern, fixedString)
	if len(terms) == 0 {
		return nil, false, searchCandidateStats{fallbackReason: "no_required_literal"}
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return nil, false, searchCandidateStats{fallbackReason: "bad_root"}
	}
	root = filepath.Clean(root)

	idx, ok := cachedSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	if !ok {
		return nil, false, searchCandidateStats{fallbackReason: "index_unavailable"}
	}
	stats := searchCandidateStats{indexed: true, indexedFiles: len(idx.files)}

	queryTrigrams := queryTrigrams(terms)
	if len(queryTrigrams) == 0 {
		return nil, false, searchCandidateStats{fallbackReason: "no_query_trigrams"}
	}
	sort.Slice(queryTrigrams, func(i, j int) bool {
		return len(idx.postings[queryTrigrams[i]]) < len(idx.postings[queryTrigrams[j]])
	})

	candidates := indexedCandidatesFromTrigrams(idx, queryTrigrams)
	files := filterIndexedCandidateFiles(idx, root, globPattern, excludePattern, fileType, candidates)
	dirtyFiles := idx.dirtyCandidateFiles(globPattern, excludePattern, fileType, includeHidden)
	if shouldRebuildSearchIndex(idx, len(dirtyFiles)) {
		if rebuilt, ok := rebuildCachedSearchIndexForScope(root, globPattern, excludePattern, fileType, includeHidden); ok {
			idx = rebuilt
			stats.indexedFiles = len(idx.files)
			stats.rebuilt = true
			candidates = indexedCandidatesFromTrigrams(idx, queryTrigrams)
			files = filterIndexedCandidateFiles(idx, root, globPattern, excludePattern, fileType, candidates)
			dirtyFiles = idx.dirtyCandidateFiles(globPattern, excludePattern, fileType, includeHidden)
		}
	}
	stats.candidateFiles = len(files)
	stats.dirtyFiles = len(dirtyFiles)
	files = append(files, dirtyFiles...)
	sort.Strings(files)
	files = dedupeSortedStrings(files)
	return files, true, stats
}

func filterIndexedCandidateFiles(idx *localSearchIndex, root, globPattern, excludePattern, fileType string, candidates map[int]bool) []string {
	files := make([]string, 0, len(candidates))
	for id := range candidates {
		if id < 0 || id >= len(idx.files) {
			continue
		}
		path := idx.files[id]
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
			continue
		}
		files = append(files, path)
	}
	return files
}

func indexedCandidatesFromTrigrams(idx *localSearchIndex, queryTrigrams []string) map[int]bool {
	candidates := make(map[int]bool)
	for _, id := range idx.postings[queryTrigrams[0]] {
		candidates[id] = true
	}
	for _, trigram := range queryTrigrams[1:] {
		next := make(map[int]bool)
		for _, id := range idx.postings[trigram] {
			if candidates[id] {
				next[id] = true
			}
		}
		candidates = next
		if len(candidates) == 0 {
			break
		}
	}
	return candidates
}

func cachedSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	now := time.Now()
	key := searchIndexCacheKey(root, globPattern, excludePattern, fileType, includeHidden)
	searchIndexCache.Lock()
	if idx := searchIndexCache.byRoot[key]; idx != nil {
		if now.Sub(idx.builtAt) <= searchIndexCacheTTL {
			idx.usedAt = now
			searchIndexCache.Unlock()
			return idx, true
		}
		delete(searchIndexCache.byRoot, key)
		searchIndexCache.Unlock()
	} else {
		searchIndexCache.Unlock()
	}

	idx, ok := buildLocalSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	if !ok {
		return nil, false
	}

	searchIndexCache.Lock()
	if existing := searchIndexCache.byRoot[key]; existing != nil {
		existing.usedAt = time.Now()
		searchIndexCache.Unlock()
		return existing, true
	}
	searchIndexCache.byRoot[key] = idx
	pruneSearchIndexCacheLocked(time.Now())
	searchIndexCache.Unlock()
	return idx, true
}

func rebuildCachedSearchIndex(root string) (*localSearchIndex, bool) {
	return rebuildCachedSearchIndexForScope(root, "", "", "", false)
}

func rebuildCachedSearchIndexForScope(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	idx, ok := buildLocalSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	if !ok {
		return nil, false
	}
	searchIndexCache.Lock()
	searchIndexCache.byRoot[idx.key] = idx
	pruneSearchIndexCacheLocked(time.Now())
	searchIndexCache.Unlock()
	return idx, true
}

func buildLocalSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, bool) {
	now := time.Now()
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
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != root && isSearchSymlink(d) {
			return nil
		}
		if path != root && d.IsDir() && shouldSkipSearchDir(d.Name(), includeHidden) {
			return filepath.SkipDir
		}
		if path != root {
			if !includeHidden && isHiddenSearchPath(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
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
		if len(idx.files) >= maxIndexedSearchFiles {
			truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSearchFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.Contains(data[:min(len(data), 8000)], []byte{0}) {
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
	if err != nil || truncated || len(idx.files) == 0 {
		return nil, false
	}
	return idx, true
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
	indexedCount := len(idx.files)
	if indexedCount == 0 {
		return true
	}
	return float64(dirtyCount)/float64(indexedCount) >= maxDirtySearchRatio
}

func pruneSearchIndexCacheLocked(now time.Time) {
	for root, idx := range searchIndexCache.byRoot {
		if now.Sub(idx.builtAt) > searchIndexCacheTTL {
			delete(searchIndexCache.byRoot, root)
		}
	}
	for len(searchIndexCache.byRoot) > maxSearchIndexCacheRoot {
		var oldestRoot string
		var oldestUsedAt time.Time
		for root, idx := range searchIndexCache.byRoot {
			if oldestRoot == "" || idx.usedAt.Before(oldestUsedAt) {
				oldestRoot = root
				oldestUsedAt = idx.usedAt
			}
		}
		if oldestRoot == "" {
			return
		}
		delete(searchIndexCache.byRoot, oldestRoot)
	}
}

func (idx *localSearchIndex) dirtyCandidateFiles(globPattern, excludePattern, fileType string, includeHidden bool) []string {
	var files []string
	_ = filepath.WalkDir(idx.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
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
		rel, err := filepath.Rel(idx.root, path)
		if err != nil {
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
	return files
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
	parts := regexp.MustCompile(`[^[:alnum:]_]+`).Split(strings.Join(requiredLiteralFragments(re), " "), -1)
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) >= 3 {
			terms = append(terms, part)
		}
	}
	sort.Slice(terms, func(i, j int) bool {
		return len([]rune(terms[i])) > len([]rune(terms[j]))
	})
	if len(terms) > 3 {
		terms = terms[:3]
	}
	return terms
}

func literalSearchTermsForQuery(pattern string, fixedString bool) []string {
	if fixedString {
		return literalSearchTermsFromText(pattern)
	}
	return literalSearchTerms(pattern)
}

func literalSearchTermsFromText(text string) []string {
	parts := regexp.MustCompile(`[^[:alnum:]_]+`).Split(text, -1)
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) >= 3 {
			terms = append(terms, part)
		}
	}
	sort.Slice(terms, func(i, j int) bool {
		return len([]rune(terms[i])) > len([]rune(terms[j]))
	})
	if len(terms) > 3 {
		terms = terms[:3]
	}
	return terms
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

func queryTrigrams(terms []string) []string {
	set := make(map[string]bool)
	for _, term := range terms {
		term = strings.ToLower(term)
		for trigram := range contentTrigrams(term) {
			set[trigram] = true
		}
	}
	out := make([]string, 0, len(set))
	for trigram := range set {
		out = append(out, trigram)
	}
	return out
}

func contentTrigrams(s string) map[string]bool {
	return contentTrigramsBytes([]byte(s))
}

func contentTrigramsBytes(data []byte) map[string]bool {
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
			r = unicode.ToLower(r)
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
