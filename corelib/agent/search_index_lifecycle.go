package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codesearch"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	searchIndexCorpusKind = "corpus"
	searchIndexScopedKind = "scoped"
	// Version 2 folds letters on the regexp simple-fold cycle. Version 1 used
	// unicode.ToLower, which keeps U+017F distinct from s, so those corpora
	// cannot answer a case-insensitive query and must be rebuilt.
	searchIndexDiskVersion         = 2
	searchIndexBackgroundBuildTime = 5 * time.Minute
	searchIndexBuildRetry          = 10 * time.Minute
	searchIndexReconcileInterval   = 30 * time.Second
	searchIndexReconcileOverdue    = 5 * time.Minute
	maxNotedSearchPaths            = 8192
)

var (
	searchIndexAutoReconcile  = true
	searchIndexPersistEnabled = true
	searchIndexDiskDir        string
)

func init() {
	tool.TextFileMutated = NoteSearchIndexDirty
	// Test binaries share these process-global switches. A guiapp test that
	// scans a temp project would otherwise publish into the user's live
	// ~/.maclaw/cache/search-index and start a reconciler for a directory
	// that disappears with the test. Tests that cover persistence opt back in
	// and point searchIndexDiskDir at a temp directory.
	if searchProcessIsTestBinary() {
		searchIndexPersistEnabled = false
		searchIndexAutoReconcile = false
	}
}

func searchProcessIsTestBinary() bool {
	if len(os.Args) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe")
}

type searchBuildStatusKey struct{}

type localSearchBuildResult struct {
	idx       *localSearchIndex
	truncated bool
	cancelled bool
	empty     bool
	blocked   []string
	// ignored lists files this build saw and will not index. Notes for them
	// cannot change a hit: search skips the same files.
	ignored []string
}

func markSearchBuild(ctx context.Context, truncated, cancelled bool) {
	status, _ := ctx.Value(searchBuildStatusKey{}).(*localSearchBuildResult)
	if status == nil {
		return
	}
	status.truncated = truncated
	status.cancelled = cancelled
}

func markSearchBuildEmpty(ctx context.Context) {
	status, _ := ctx.Value(searchBuildStatusKey{}).(*localSearchBuildResult)
	if status == nil {
		return
	}
	status.empty = true
}

func noteSearchBuildBlocked(ctx context.Context, blocked []string) {
	if len(blocked) == 0 {
		return
	}
	status, _ := ctx.Value(searchBuildStatusKey{}).(*localSearchBuildResult)
	if status == nil {
		return
	}
	status.blocked = blocked
}

func noteSearchBuildIgnored(ctx context.Context, path string) {
	if path == "" {
		return
	}
	status, _ := ctx.Value(searchBuildStatusKey{}).(*localSearchBuildResult)
	if status == nil {
		return
	}
	status.ignored = append(status.ignored, path)
}

func buildLocalSearchIndexResult(ctx context.Context, root, globPattern, excludePattern, fileType string, includeHidden bool) localSearchBuildResult {
	if ctx == nil {
		ctx = context.Background()
	}
	status := &localSearchBuildResult{}
	ctx = context.WithValue(ctx, searchBuildStatusKey{}, status)
	idx, ok := buildLocalSearchIndexCtx(ctx, root, globPattern, excludePattern, fileType, includeHidden)
	if ok {
		status.idx = idx
	}
	return *status
}

func searchIndexCorpusKey(root, exclude string, hidden bool) string {
	return searchIndexCacheKey(root, "", exclude, "", hidden)
}

func searchIndexScopedKey(root, globPattern, exclude, fileType string, hidden bool) string {
	return searchIndexCacheKey(root, globPattern, exclude, fileType, hidden) + "\x00scoped"
}

func lookupSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) (*localSearchIndex, string) {
	now := time.Now()
	searchIndexCache.Lock()
	defer searchIndexCache.Unlock()
	if idx := searchIndexCache.byRoot[searchIndexCorpusKey(root, excludePattern, includeHidden)]; idx != nil && idx.complete {
		if reason, usable := searchIndexUsable(idx, now); usable {
			idx.usedAt = now
			return idx, ""
		} else if reason != "" {
			return nil, reason
		}
	}
	if globPattern != "" || fileType != "" {
		key := searchIndexScopedKey(root, globPattern, excludePattern, fileType, includeHidden)
		if idx := searchIndexCache.byRoot[key]; idx != nil && idx.complete {
			if reason, usable := searchIndexUsable(idx, now); usable {
				idx.usedAt = now
				return idx, ""
			} else if reason != "" {
				return nil, reason
			}
		}
	}
	return nil, ""
}

func searchIndexUsable(idx *localSearchIndex, now time.Time) (string, bool) {
	if idx == nil || !idx.complete {
		return "", false
	}
	if now.Sub(idx.builtAt) > searchIndexCacheTTL {
		return "", false
	}
	reconciled := idx.reconciledAt
	if reconciled.IsZero() {
		reconciled = idx.builtAt
	}
	if now.Sub(reconciled) > searchIndexReconcileOverdue {
		return "reconcile_overdue", false
	}
	return "", true
}

func publishedSearchIndex(root, globPattern, excludePattern, fileType string, includeHidden bool) *localSearchIndex {
	idx, _ := lookupSearchIndex(root, globPattern, excludePattern, fileType, includeHidden)
	return idx
}

type searchBuildSlot struct {
	running   bool
	failedAt  time.Time
	truncated bool
}

var searchBuildMu sync.Mutex
var searchBuildSlots = map[string]*searchBuildSlot{}

func scheduleSearchIndexes(root, globPattern, excludePattern, fileType string, includeHidden bool) {
	corpusKey := searchIndexCorpusKey(root, excludePattern, includeHidden)
	scheduleSearchIndexBuild(corpusKey, root, "", excludePattern, "", includeHidden, searchIndexCorpusKind)
	if (globPattern != "" || fileType != "") && corpusBuildFailed(corpusKey) {
		scopedKey := searchIndexScopedKey(root, globPattern, excludePattern, fileType, includeHidden)
		scheduleSearchIndexBuild(scopedKey, root, globPattern, excludePattern, fileType, includeHidden, searchIndexScopedKind)
	}
}

func corpusBuildFailed(key string) bool {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	return slot != nil && !slot.running && !slot.failedAt.IsZero()
}

func publishedComplete(key string) bool {
	searchIndexCache.Lock()
	defer searchIndexCache.Unlock()
	idx := searchIndexCache.byRoot[key]
	if idx == nil || !idx.complete {
		return false
	}
	return time.Since(idx.builtAt) <= searchIndexCacheTTL
}

func scheduleSearchIndexBuild(key, root, globPattern, excludePattern, fileType string, includeHidden bool, kind string) {
	if publishedComplete(key) {
		return
	}
	if !tryStartSearchBuild(key) {
		return
	}
	go runSearchIndexBuild(key, root, globPattern, excludePattern, fileType, includeHidden, kind)
}

func tryStartSearchBuild(key string) bool {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil {
		slot = &searchBuildSlot{}
		searchBuildSlots[key] = slot
	}
	if slot.running {
		return false
	}
	if !slot.failedAt.IsZero() && time.Since(slot.failedAt) < searchIndexBuildRetry {
		return false
	}
	slot.running = true
	slot.failedAt = time.Time{}
	slot.truncated = false
	return true
}

func finishSearchBuild(key string, truncated, failed bool) {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil {
		return
	}
	slot.running = false
	if failed {
		slot.failedAt = time.Now()
		slot.truncated = truncated
	}
}

func runSearchIndexBuild(key, root, globPattern, excludePattern, fileType string, includeHidden bool, kind string) {
	failed := true
	truncated := false
	defer func() { finishSearchBuild(key, truncated, failed) }()
	if publishedComplete(key) {
		failed = false
		return
	}
	started := time.Now()
	noteSnap := snapshotNotedSearchStamps(root)
	var previous *localSearchIndex
	if kind == searchIndexCorpusKind && searchIndexPersistEnabled {
		if loaded, ok := loadSearchIndexFile(searchIndexDiskRoot(), key, root, excludePattern, includeHidden); ok {
			previous = loaded
			if patched, settled, applied := patchLoadedSearchIndex(loaded); applied {
				publishSearchIndex(key, patched, kind)
				failed = false
				forgetReconciledNotes(patched, root, started, settled, noteSnap)
				return
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), searchIndexBackgroundBuildTime)
	defer cancel()
	result := buildLocalSearchIndexResult(ctx, root, globPattern, excludePattern, fileType, includeHidden)
	truncated = result.truncated
	if result.cancelled || result.truncated || result.idx == nil {
		return
	}
	merged, settled, ok := mergeBuiltSearchIndex(previous, result)
	if !ok {
		truncated = true
		return
	}
	publishSearchIndex(key, merged, kind)
	failed = false
	forgetReconciledNotes(merged, root, started, settleSearchPaths(settled, result.ignored), noteSnap)
}

func patchLoadedSearchIndex(loaded *localSearchIndex) (*localSearchIndex, map[string]struct{}, bool) {
	delta, ok := collectSearchIndexDelta(loaded)
	if !ok || delta.overCapacity || delta.tooBig {
		return nil, nil, false
	}
	if delta.empty() {
		setSearchIndexUnlisted(loaded, delta.blocked)
		assignSearchUnread(loaded, delta.unreadable)
		return loaded, settleSearchPaths(nil, delta.ignored), true
	}
	next, settled, applied := applySearchIndexDelta(loaded, delta, true)
	if applied && next != nil {
		setSearchIndexUnlisted(next, delta.blocked)
		assignSearchUnread(next, delta.unreadable)
	}
	return next, settleSearchPaths(settled, delta.ignored), applied
}

func publishSearchIndex(key string, idx *localSearchIndex, kind string) {
	publishSearchIndexMaybe(key, nil, idx, kind, false)
}

func publishSearchIndexIfCurrent(key string, prev, idx *localSearchIndex, kind string) bool {
	return publishSearchIndexMaybe(key, prev, idx, kind, true)
}

func publishSearchIndexMaybe(key string, prev, idx *localSearchIndex, kind string, requireCurrent bool) bool {
	if idx == nil || key == "" {
		return false
	}
	now := time.Now()
	idx.complete = true
	idx.kind = kind
	idx.key = key
	idx.builtAt = now
	idx.usedAt = now
	idx.reconciledAt = now
	searchIndexCache.Lock()
	if requireCurrent && searchIndexCache.byRoot[key] != prev {
		searchIndexCache.Unlock()
		return false
	}
	searchIndexCache.byRoot[key] = idx
	pruneSearchIndexCacheLocked(now)
	searchIndexCache.Unlock()
	clearSearchBuildFailure(key)
	if kind == searchIndexCorpusKind && searchIndexPersistEnabled {
		if dir := searchIndexDiskRoot(); dir != "" {
			_ = writeSearchIndexFile(dir, idx)
		}
	}
	if searchIndexAutoReconcile {
		ensureSearchIndexReconciler(key)
	}
	return true
}

func clearSearchBuildFailure(key string) {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil || slot.running {
		return
	}
	slot.failedAt = time.Time{}
	slot.truncated = false
}

func markSearchBuildFailed(key string, truncated bool) {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil {
		slot = &searchBuildSlot{}
		searchBuildSlots[key] = slot
	}
	if slot.running {
		return
	}
	slot.failedAt = time.Now()
	slot.truncated = truncated
}

func searchBuildInFlight(key string) bool {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	return slot != nil && slot.running
}

func reconcileRebuildCoolingDown(key string) bool {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil || slot.running || slot.failedAt.IsZero() {
		return false
	}
	return time.Since(slot.failedAt) < searchIndexBuildRetry
}

func forgetSearchBuildSlot(key string) {
	searchBuildMu.Lock()
	defer searchBuildMu.Unlock()
	slot := searchBuildSlots[key]
	if slot == nil || slot.running {
		return
	}
	delete(searchBuildSlots, key)
}

var notedSearchDirty = struct {
	sync.Mutex
	paths map[string]time.Time
}{paths: map[string]time.Time{}}

// NoteSearchIndexDirty marks a file this process wrote so the next ripgrep
// reads it before the background reconciler republishes the index. The same
// path is noted for the code_search chunk index.
func NoteSearchIndexDirty(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	abs = filepath.Clean(abs)
	// Write and edit follow symlinks, so the bytes land on the real file.
	// Search does not follow links. A final-component symlink is recorded
	// as its target. A parent junction or directory symlink is recorded as
	// the written path and every hop out to the real file. A search rooted
	// at any of those directories can see the write. The query still skips
	// a path that walks through a link.
	var extras []string
	if resolved, ok := resolveNotedSearchSymlink(abs); ok {
		abs = resolved
	} else {
		extras = resolveParentSymlinkNotes(abs)
	}
	notedSearchDirty.Lock()
	now := time.Now()
	notedSearchDirty.paths[abs] = now
	for _, extra := range extras {
		if extra == "" || searchPathEqual(extra, abs) {
			continue
		}
		notedSearchDirty.paths[extra] = now
	}
	over := len(notedSearchDirty.paths) > maxNotedSearchPaths
	notedSearchDirty.Unlock()
	if over {
		pruneStaleNotedSearchPaths()
	}
	codesearch.NoteDirty(abs)
	for _, extra := range extras {
		codesearch.NoteDirty(extra)
	}
}

// resolveNotedSearchSymlink returns the file a symlink write actually changed.
// A normal file is left as given so its note still matches the indexed path.
func resolveNotedSearchSymlink(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)
	if resolved == "" || searchPathEqual(resolved, path) {
		return "", false
	}
	return resolved, true
}

// resolveParentSymlinkNotes lists the files a write reaches through parent
// directory symlinks or Windows junctions. Each hop is one reparse point.
// The original path is not included. A cycle stops the walk.
func resolveParentSymlinkNotes(path string) []string {
	const maxReparseHops = 32
	seen := map[string]struct{}{searchPathKey(path): {}}
	notes := make([]string, 0, 2)
	current := path
	for len(notes) < maxReparseHops {
		linkDir := nearestSearchReparseDir(current)
		if linkDir == "" {
			break
		}
		next, ok := resolveNextReparseNote(current, linkDir)
		if !ok {
			break
		}
		key := searchPathKey(next)
		if _, dup := seen[key]; dup {
			break
		}
		seen[key] = struct{}{}
		notes = append(notes, next)
		current = next
	}
	return notes
}

func nearestSearchReparseDir(path string) string {
	dir := filepath.Dir(path)
	for {
		info, err := os.Lstat(dir)
		if err == nil && searchReparseUnwalked(info) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

// resolveNextReparseNote resolves one parent reparse. A Windows junction is
// ModeIrregular. EvalSymlinks fails on that path or skips the rest of the
// chain, so the hop uses Readlink and the next junction stays a separate note.
func resolveNextReparseNote(path, linkDir string) (string, bool) {
	info, err := os.Lstat(linkDir)
	if err == nil && info.Mode()&os.ModeIrregular != 0 {
		if resolved, ok := resolveReparseReadlink(path, linkDir); ok {
			return resolved, true
		}
	}
	return resolveReparseTarget(path, linkDir)
}

// resolveReparseTarget resolves path through linkDir. EvalSymlinks follows a
// directory symlink. A Windows junction makes that call fail or return the
// link path; Readlink returns the junction target.
func resolveReparseTarget(path, linkDir string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		resolved = filepath.Clean(resolved)
		if resolved != "" && !searchPathEqual(resolved, path) && !searchPathUnder(linkDir, resolved) {
			return resolved, true
		}
	}
	return resolveReparseReadlink(path, linkDir)
}

func resolveReparseReadlink(path, linkDir string) (string, bool) {
	target, readErr := os.Readlink(linkDir)
	if readErr != nil || target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkDir), target)
	}
	rel, relErr := filepath.Rel(linkDir, path)
	if relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	resolved := filepath.Clean(filepath.Join(target, rel))
	if resolved == "" || searchPathEqual(resolved, path) {
		return "", false
	}
	return resolved, true
}

// searchReparseUnwalked reports a directory the walk does not enter.
// Lstat leaves ModeDir unset for a directory symlink and, on Windows, for a
// directory junction (ModeIrregular). The walk uses that IsDir bit, so those
// names are not searched. A cloud placeholder file is not a parent directory.
func searchReparseUnwalked(info os.FileInfo) bool {
	if info == nil || info.IsDir() {
		return false
	}
	mode := info.Mode()
	return mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0
}

// searchUnreadableCandidate reports a path whose content read failed and that
// is still a file. A Windows directory junction is ModeIrregular and IsDir is
// false, so ReadFile fails, but Stat follows it to a directory. That path is
// not an unread file: opening it later returns ERROR_INVALID_FUNCTION and
// would fail the search.
func searchUnreadableCandidate(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || searchDirReparse(path, info) {
		return false
	}
	return true
}

// searchDirReparse reports that Lstat did not call path a directory, but
// following it lands on one. A Windows junction is ModeIrregular and is not a
// file the corpus failed to read. A cloud placeholder stays a file.
func searchDirReparse(path string, info os.FileInfo) bool {
	if info == nil || info.IsDir() || path == "" {
		return false
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		return false
	}
	followed, err := os.Stat(path)
	return err == nil && followed.IsDir()
}

// searchPathCrossesSymlink reports whether a directory between root and path
// is a symlink or a Windows directory junction. The walk does not descend into
// those directories. cache stores directory keys already classified in this
// query; a nil cache does not remember.
func searchPathCrossesSymlink(root, path string, cache map[string]bool) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == "" || !searchPathUnder(root, path) || searchPathEqual(root, path) {
		return false
	}
	dir := filepath.Dir(path)
	var chain []string
	for !searchPathEqual(dir, root) {
		key := searchPathKey(dir)
		if cache != nil {
			if real, ok := cache[key]; ok {
				if !real {
					return true
				}
				break
			}
		}
		chain = append(chain, dir)
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	for i := len(chain) - 1; i >= 0; i-- {
		info, err := os.Lstat(chain[i])
		if err != nil {
			return false
		}
		crosses := searchReparseUnwalked(info)
		if cache != nil {
			cache[searchPathKey(chain[i])] = !crosses
			if crosses {
				for j := i - 1; j >= 0; j-- {
					cache[searchPathKey(chain[j])] = false
				}
			}
		}
		if crosses {
			return true
		}
	}
	return false
}

func notedSearchPaths(root, globPattern, excludePattern, fileType string, includeHidden bool) []string {
	root = filepath.Clean(root)
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	var out []string
	for path := range notedSearchDirty.paths {
		if !searchPathUnder(root, path) {
			continue
		}
		rel, ok := searchIndexRelativePath(root, path)
		if !ok || rel == "." {
			continue
		}
		if searchRelSkipped(rel, includeHidden) {
			continue
		}
		if !matchesSearchFilters(rel, false, globPattern, excludePattern, fileType) {
			continue
		}
		out = append(out, path)
	}
	return out
}

// statSearchNote is os.Stat except in tests. Confirming a missing note must
// not run it while notedSearchDirty is held: indexed search takes that lock
// to read the dirty overlay.
var statSearchNote = os.Stat

// searchNoteSnap is one note eligible to drop at the start of this pass.
// A later write can replace that timestamp while Stat runs. Windows monotonic
// time often stays on the same interrupt tick across that window, so the new
// stamp is not After(before). Identity of the stamp is the check that keeps it.
type searchNoteSnap struct {
	path string
	ts   time.Time
}

// snapshotNotedSearchStamps copies note timestamps before a walk. The map is
// non-nil even when empty, so a note created during the pass is distinguishable
// from a caller that did not take a snapshot.
func snapshotNotedSearchStamps(root string) map[string]time.Time {
	root = filepath.Clean(root)
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	snap := make(map[string]time.Time)
	for path, ts := range notedSearchDirty.paths {
		if root != "" && !searchPathUnder(root, path) {
			continue
		}
		snap[path] = ts
	}
	return snap
}

// noteStampUnchanged reports whether a settled note still has the stamp captured
// before the walk. A nil snap keeps the older rule and drops the note.
func noteStampUnchanged(snap map[string]time.Time, path string, ts time.Time) bool {
	if snap == nil {
		return true
	}
	old, ok := snap[path]
	return ok && ts.Equal(old)
}

// forgetReconciledNotes drops notes this pass has classified. settled holds
// searchPathKey values for files whose current contents are in the index, or
// that were confirmed deleted or out of policy. A path left over from an older
// index was not necessarily re-read, so membership alone does not drop its note.
// Stat runs outside the note lock. A note whose timestamp changed during Stat stays.
// snap is the stamp map from before the walk. A settled note is dropped only when
// that stamp is unchanged. A rewrite in the same interrupt tick as before is not
// After(before), but it is a different stamp and the new contents were not read.
func forgetReconciledNotes(idx *localSearchIndex, root string, before time.Time, settled map[string]struct{}, snap map[string]time.Time) {
	if idx != nil && idx.root != "" {
		root = idx.root
	}
	root = filepath.Clean(root)
	var pending []searchNoteSnap
	notedSearchDirty.Lock()
	for path, ts := range notedSearchDirty.paths {
		if !searchPathUnder(root, path) || ts.After(before) {
			continue
		}
		rel, ok := searchIndexRelativePath(root, path)
		if ok && rel != "." && searchRelSkipped(rel, true) {
			delete(notedSearchDirty.paths, path)
			continue
		}
		if noteSettled(settled, path) {
			if noteStampUnchanged(snap, path, ts) {
				delete(notedSearchDirty.paths, path)
			}
			continue
		}
		pending = append(pending, searchNoteSnap{path: path, ts: ts})
	}
	notedSearchDirty.Unlock()
	if len(pending) == 0 {
		return
	}
	missing := make([]searchNoteSnap, 0, len(pending))
	for _, note := range pending {
		info, err := statSearchNote(note.path)
		if err == nil && !info.IsDir() && !searchPathCrossesSymlink(root, note.path, nil) {
			continue
		}
		missing = append(missing, note)
	}
	if len(missing) == 0 {
		return
	}
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	for _, note := range missing {
		ts, ok := notedSearchDirty.paths[note.path]
		if !ok || !ts.Equal(note.ts) {
			continue
		}
		delete(notedSearchDirty.paths, note.path)
	}
}

func settleSearchPaths(settled map[string]struct{}, paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return settled
	}
	if settled == nil {
		settled = make(map[string]struct{}, len(paths))
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		settled[searchPathKey(path)] = struct{}{}
	}
	return settled
}

func noteSettled(settled map[string]struct{}, path string) bool {
	if len(settled) == 0 {
		return false
	}
	_, ok := settled[searchPathKey(path)]
	return ok
}

func settledSearchIndexPaths(idx *localSearchIndex) map[string]struct{} {
	if idx == nil || len(idx.fileSet) == 0 {
		return nil
	}
	settled := make(map[string]struct{}, len(idx.fileSet))
	for path := range idx.fileSet {
		if path == "" {
			continue
		}
		settled[searchPathKey(path)] = struct{}{}
	}
	return settled
}

// mergeBuiltSearchIndex keeps files from prev that sit in directories this
// build could not list. settled covers only files the build actually read.
// The bool is false when the merged corpus no longer fits the file cap.
func mergeBuiltSearchIndex(prev *localSearchIndex, result localSearchBuildResult) (*localSearchIndex, map[string]struct{}, bool) {
	if result.idx == nil {
		return nil, nil, false
	}
	settled := settledSearchIndexPaths(result.idx)
	merged := mergeUnlistedSearchFiles(result.idx, prev, result.blocked)
	if indexedSearchFileCount(merged) > maxIndexedSearchFiles {
		return nil, nil, false
	}
	return merged, settled, true
}

func mergeUnlistedSearchFiles(next, prev *localSearchIndex, blocked []string) *localSearchIndex {
	if next == nil || prev == nil || len(blocked) == 0 || len(prev.fileSet) == 0 || next.fileSet == nil {
		return next
	}
	present := make(map[string]struct{}, len(next.fileSet))
	for path := range next.fileSet {
		if path == "" {
			continue
		}
		present[searchPathKey(path)] = struct{}{}
	}
	idOf := make(map[string]int, len(prev.files))
	for i, path := range prev.files {
		if path != "" {
			idOf[path] = i
		}
	}
	if next.fileMeta == nil {
		next.fileMeta = map[string]localSearchFileMeta{}
	}
	if next.postings == nil {
		next.postings = map[string][]int{}
	}
	appended := false
	for path := range prev.fileSet {
		if path == "" {
			continue
		}
		if _, ok := present[searchPathKey(path)]; ok {
			continue
		}
		if !searchPathUnderAny(blocked, path) {
			continue
		}
		oldID, ok := idOf[path]
		if !ok {
			continue
		}
		id := len(next.files)
		next.files = append(next.files, path)
		next.fileSet[path] = true
		if prev.fileMeta != nil {
			next.fileMeta[path] = prev.fileMeta[path]
		}
		present[searchPathKey(path)] = struct{}{}
		appended = true
		for trigram, ids := range prev.postings {
			i := sort.SearchInts(ids, oldID)
			if i >= len(ids) || ids[i] != oldID {
				continue
			}
			next.postings[trigram] = append(next.postings[trigram], id)
		}
	}
	if appended {
		next.fileIDs = searchFileIDsFromSlice(next.files)
	}
	return next
}

func searchRelSkipped(rel string, includeHidden bool) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		// The last component is the file. Skip names apply to directories,
		// matching the walk, which SkipDir's those names and still reads a
		// file that happens to use one.
		if i == len(parts)-1 {
			if !includeHidden && isHiddenSearchPath(part) {
				return true
			}
			continue
		}
		if shouldSkipSearchDir(part, includeHidden) {
			return true
		}
	}
	return false
}

func searchPathEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func searchPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func searchPathPrefix(root string) string {
	if root == "" {
		return ""
	}
	if os.IsPathSeparator(root[len(root)-1]) {
		return root
	}
	return root + string(os.PathSeparator)
}

func searchPathUnder(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == "" || root == "." {
		return false
	}
	if searchPathEqual(root, path) {
		return true
	}
	prefix := searchPathPrefix(root)
	return len(path) >= len(prefix) && searchPathEqual(path[:len(prefix)], prefix)
}

func notedSearchPathSet(idx *localSearchIndex) map[string]struct{} {
	if idx == nil {
		return nil
	}
	return notedSearchKeySet(idx.root, idx.glob, idx.exclude, idx.fileType, idx.hidden)
}

func notedSearchKeySet(root, globPattern, excludePattern, fileType string, includeHidden bool) map[string]struct{} {
	paths := notedSearchPaths(root, globPattern, excludePattern, fileType, includeHidden)
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		set[searchPathKey(path)] = struct{}{}
	}
	return set
}

func notedPath(set map[string]struct{}, path string) bool {
	_, ok := set[searchPathKey(path)]
	return ok
}

func dedupeSearchPaths(items []string) []string {
	if len(items) < 2 {
		return items
	}
	sort.Strings(items)
	seen := make(map[string]struct{}, len(items))
	out := items[:0]
	for _, item := range items {
		key := searchPathKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func pruneStaleNotedSearchPaths() {
	roots := publishedSearchRoots()
	cutoff := time.Now().Add(-searchIndexBackgroundBuildTime)
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	if len(notedSearchDirty.paths) <= maxNotedSearchPaths {
		return
	}
	for path, ts := range notedSearchDirty.paths {
		if !ts.Before(cutoff) || searchPathUnderAny(roots, path) {
			continue
		}
		delete(notedSearchDirty.paths, path)
	}
}

func publishedSearchRoots() []string {
	searchIndexCache.Lock()
	defer searchIndexCache.Unlock()
	roots := make([]string, 0, len(searchIndexCache.byRoot))
	seen := make(map[string]struct{}, len(searchIndexCache.byRoot))
	for _, idx := range searchIndexCache.byRoot {
		if idx == nil || idx.root == "" {
			continue
		}
		if _, ok := seen[idx.root]; ok {
			continue
		}
		seen[idx.root] = struct{}{}
		roots = append(roots, idx.root)
	}
	return roots
}

func searchPathUnderAny(roots []string, path string) bool {
	for _, root := range roots {
		if searchPathUnder(root, path) {
			return true
		}
	}
	return false
}

// cleanSearchUnlisted keeps one cleaned path for each directory the walk
// could not list. An empty result is nil so a complete corpus stores nothing.
func cleanSearchUnlisted(blocked []string) []string {
	if len(blocked) == 0 {
		return nil
	}
	out := make([]string, 0, len(blocked))
	seen := make(map[string]struct{}, len(blocked))
	for _, path := range blocked {
		path = filepath.Clean(path)
		if path == "" || path == "." {
			continue
		}
		key := searchPathKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func searchUnlistedSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	seen := make(map[string]int, len(a))
	for _, path := range a {
		seen[searchPathKey(path)]++
	}
	for _, path := range b {
		key := searchPathKey(path)
		n := seen[key]
		if n == 0 {
			return false
		}
		if n == 1 {
			delete(seen, key)
			continue
		}
		seen[key] = n - 1
	}
	return len(seen) == 0
}

func setSearchIndexUnlisted(idx *localSearchIndex, blocked []string) {
	if idx == nil {
		return
	}
	idx.unlisted = cleanSearchUnlisted(blocked)
}

func cleanSearchUnread(paths []string) []string {
	return cleanSearchUnlisted(paths)
}

func searchUnreadSame(a, b []string) bool {
	return searchUnlistedSame(a, b)
}

// takeSearchUnread merges paths the walk could not read with the corpus list.
// A path stays when its trigrams are missing or stale. A path that is gone,
// a directory, a symlink, a junction, or over the size cap leaves. The bool
// is whether the stored list would change.
func takeSearchUnread(idx *localSearchIndex, extra []string) ([]string, bool) {
	if idx == nil {
		return nil, false
	}
	merged := filterSearchUnread(idx, append(append([]string{}, idx.unread...), extra...))
	return merged, !searchUnreadSame(idx.unread, merged)
}

func filterSearchUnread(idx *localSearchIndex, paths []string) []string {
	paths = cleanSearchUnread(paths)
	if len(paths) == 0 {
		return nil
	}
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
		} else if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Size() > MaxSearchFileSize || searchDirReparse(path, info) {
			continue
		}
		kept = append(kept, path)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func forgetSearchUnread(idx *localSearchIndex, paths ...string) {
	if idx == nil || len(idx.unread) == 0 {
		return
	}
	drop := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		drop[searchPathKey(path)] = struct{}{}
	}
	if len(drop) == 0 {
		return
	}
	kept := make([]string, 0, len(idx.unread))
	for _, path := range idx.unread {
		if _, ok := drop[searchPathKey(path)]; ok {
			continue
		}
		kept = append(kept, path)
	}
	if len(kept) == 0 {
		idx.unread = nil
		return
	}
	idx.unread = kept
}

func assignSearchUnread(idx *localSearchIndex, extra []string) bool {
	next, changed := takeSearchUnread(idx, extra)
	if idx == nil || !changed {
		return false
	}
	idx.unread = next
	return true
}

// searchDirListable reports that dir can be listed through to EOF.
// A partial batch followed by an error is still unknown. The walk reads
// through the same batch function, so this answer is the walk's answer.
// Cancellation or the shared search walk deadline means the listing did
// not finish. A finished batch is listable even when that same call is
// also the moment the context ends.
func searchDirListable(ctx context.Context, dir string, deadline time.Time) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return false
	}
	file, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer file.Close()
	seen := 0
	for {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		entries, err := readSearchDirBatch(file, 128)
		seen += len(entries)
		if err == io.EOF || (err == nil && len(entries) == 0) {
			return true
		}
		if err != nil || ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		if seen > 0 && seen&511 == 0 && !time.Now().Before(deadline) {
			return false
		}
	}
}

var searchReconcileMu sync.Mutex
var searchReconcileStop = map[string]chan struct{}{}

func ensureSearchIndexReconciler(key string) {
	if !searchIndexAutoReconcile {
		return
	}
	searchReconcileMu.Lock()
	if _, ok := searchReconcileStop[key]; ok {
		searchReconcileMu.Unlock()
		return
	}
	stop := make(chan struct{})
	searchReconcileStop[key] = stop
	searchReconcileMu.Unlock()
	go searchIndexReconcileLoop(key, stop)
}

func stopSearchIndexReconciler(key string) {
	searchReconcileMu.Lock()
	stop := searchReconcileStop[key]
	delete(searchReconcileStop, key)
	searchReconcileMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func searchIndexReconcileLoop(key string, stop <-chan struct{}) {
	ticker := time.NewTicker(searchIndexReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			reconcilePublishedSearchIndex(key)
		}
	}
}

func reconcilePublishedCorpus(root, exclude string, hidden bool) bool {
	root = filepath.Clean(root)
	return reconcilePublishedSearchIndex(searchIndexCorpusKey(root, exclude, hidden))
}

func reconcilePublishedSearchIndex(key string) bool {
	if searchBuildInFlight(key) {
		return false
	}
	searchIndexCache.Lock()
	idx := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if idx == nil || !idx.complete {
		return false
	}
	started := time.Now()
	noteSnap := snapshotNotedSearchStamps(idx.root)
	delta, ok := collectSearchIndexDelta(idx)
	if !ok {
		return false
	}
	if delta.empty() && !delta.overCapacity && !delta.tooBig {
		_, unreadChanged := takeSearchUnread(idx, delta.unreadable)
		if !searchUnlistedSame(idx.unlisted, delta.blocked) || unreadChanged {
			next := cloneSearchIndexShell(idx)
			next.fileIDs = searchFileIDsFromSlice(next.files)
			setSearchIndexUnlisted(next, delta.blocked)
			assignSearchUnread(next, delta.unreadable)
			if !publishSearchIndexIfCurrent(key, idx, next, idx.kind) {
				return false
			}
			forgetReconciledNotes(next, next.root, started, settleSearchPaths(nil, delta.ignored), noteSnap)
			return true
		}
		touchSearchIndexReconciled(key, idx)
		forgetReconciledNotes(idx, idx.root, started, settleSearchPaths(nil, delta.ignored), noteSnap)
		return true
	}
	if delta.overCapacity {
		unpublishSearchIndexIfCurrent(key, idx)
		markSearchBuildFailed(key, true)
		return false
	}
	if delta.tooBig {
		if reconcileRebuildCoolingDown(key) {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), searchIndexBackgroundBuildTime)
		defer cancel()
		result := buildLocalSearchIndexResult(ctx, idx.root, idx.glob, idx.exclude, idx.fileType, idx.hidden)
		if result.cancelled || result.truncated || result.idx == nil {
			if result.truncated || result.empty {
				unpublishSearchIndexIfCurrent(key, idx)
				forgetReconciledNotes(nil, idx.root, started, settleSearchPaths(settleSearchPaths(nil, delta.ignored), result.ignored), noteSnap)
			}
			markSearchBuildFailed(key, result.truncated)
			return false
		}
		merged, settled, ok := mergeBuiltSearchIndex(idx, result)
		if !ok {
			unpublishSearchIndexIfCurrent(key, idx)
			markSearchBuildFailed(key, true)
			return false
		}
		if !publishSearchIndexIfCurrent(key, idx, merged, idx.kind) {
			return false
		}
		forgetReconciledNotes(merged, idx.root, started, settleSearchPaths(settleSearchPaths(settled, delta.ignored), result.ignored), noteSnap)
		return true
	}
	patched, settled, applied := applySearchIndexDelta(idx, delta, false)
	settled = settleSearchPaths(settled, delta.ignored)
	if !applied {
		unpublishSearchIndexIfCurrent(key, idx)
		forgetReconciledNotes(nil, idx.root, started, settled, noteSnap)
		return false
	}
	if patched == idx {
		_, unreadChanged := takeSearchUnread(idx, delta.unreadable)
		if !searchUnlistedSame(idx.unlisted, delta.blocked) || unreadChanged {
			next := cloneSearchIndexShell(idx)
			next.fileIDs = searchFileIDsFromSlice(next.files)
			setSearchIndexUnlisted(next, delta.blocked)
			assignSearchUnread(next, delta.unreadable)
			if !publishSearchIndexIfCurrent(key, idx, next, idx.kind) {
				return false
			}
			forgetReconciledNotes(next, next.root, started, settled, noteSnap)
			return true
		}
		touchSearchIndexReconciled(key, idx)
		forgetReconciledNotes(idx, idx.root, started, settled, noteSnap)
		return true
	}
	setSearchIndexUnlisted(patched, delta.blocked)
	assignSearchUnread(patched, delta.unreadable)
	if !publishSearchIndexIfCurrent(key, idx, patched, idx.kind) {
		return false
	}
	forgetReconciledNotes(patched, idx.root, started, settled, noteSnap)
	return true
}

func unpublishSearchIndexIfCurrent(key string, prev *localSearchIndex) {
	searchIndexCache.Lock()
	current := searchIndexCache.byRoot[key]
	if current != prev {
		searchIndexCache.Unlock()
		return
	}
	delete(searchIndexCache.byRoot, key)
	searchIndexCache.Unlock()
	stopSearchIndexReconciler(key)
}

func touchSearchIndexReconciled(key string, idx *localSearchIndex) {
	now := time.Now()
	searchIndexCache.Lock()
	defer searchIndexCache.Unlock()
	current := searchIndexCache.byRoot[key]
	if current != idx {
		return
	}
	current.reconciledAt = now
	current.builtAt = now
}

type searchIndexDelta struct {
	added        []string
	modified     []string
	deleted      []string
	ignored      []string
	blocked      []string
	unreadable   []string
	overCapacity bool
	tooBig       bool
}

func (d searchIndexDelta) empty() bool {
	return len(d.added) == 0 && len(d.modified) == 0 && len(d.deleted) == 0
}

func (d searchIndexDelta) changeCount() int {
	return len(d.added) + len(d.modified) + len(d.deleted)
}

var searchIndexWalk = walkDirCtx

func collectSearchIndexDelta(idx *localSearchIndex) (searchIndexDelta, bool) {
	if idx == nil || idx.root == "" {
		return searchIndexDelta{}, false
	}
	if idx.fileSet == nil || idx.fileMeta == nil {
		return searchIndexDelta{tooBig: true}, true
	}
	fileLimit := maxIndexedSearchFiles
	noted := notedSearchPathSet(idx)
	known := make(map[string]string, len(idx.fileSet))
	for path := range idx.fileSet {
		known[searchPathKey(path)] = path
	}
	seen := make(map[string]bool, len(idx.fileSet))
	var blocked []string
	var delta searchIndexDelta
	tooBig := false
	// Unread trigrams stay stale when size and mtime still match. The query
	// opens those files; reconcile has to read them too, or the list never
	// clears and the index never catches up.
	unreadKnown := make(map[string]struct{}, len(idx.unread))
	for _, path := range idx.unread {
		if path != "" {
			unreadKnown[searchPathKey(path)] = struct{}{}
		}
	}
	walkErr := searchIndexWalk(context.Background(), idx.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if searchPathEqual(path, idx.root) {
				return err
			}
			// A directory that could not be listed is unknown, not empty.
			blocked = append(blocked, path)
			return nil
		}
		if path != idx.root && isSearchSymlink(d) {
			if notedPath(noted, path) {
				delta.ignored = append(delta.ignored, path)
			}
			return nil
		}
		if path != idx.root && d.IsDir() && shouldSkipSearchDir(d.Name(), idx.hidden) {
			return filepath.SkipDir
		}
		if path != idx.root && !idx.hidden && isHiddenSearchPath(d.Name()) {
			return nil
		}
		if path != idx.root {
			rel, relOK := searchIndexRelativePath(idx.root, path)
			if !relOK {
				return nil
			}
			if idx.exclude != "" {
				excluded, matchErr := matchGlob(idx.exclude, rel, d.IsDir())
				if matchErr != nil {
					return matchErr
				}
				if excluded && d.IsDir() {
					return filepath.SkipDir
				}
				if excluded {
					return nil
				}
			}
			if !d.IsDir() && !matchesSearchFilters(rel, false, idx.glob, idx.exclude, idx.fileType) {
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			if existing, ok := known[searchPathKey(path)]; ok {
				seen[existing] = true
				if searchUnreadableCandidate(existing) {
					delta.unreadable = append(delta.unreadable, existing)
				}
			} else if searchUnreadableCandidate(path) {
				delta.unreadable = append(delta.unreadable, path)
			}
			return nil
		}
		if info.Size() > MaxSearchFileSize {
			if notedPath(noted, path) {
				delta.ignored = append(delta.ignored, path)
			}
			return nil
		}
		existing, inIndex := known[searchPathKey(path)]
		if !inIndex {
			binary, readErr := searchPrefixBinary(path)
			if readErr != nil || binary {
				if binary && notedPath(noted, path) {
					delta.ignored = append(delta.ignored, path)
				}
				if readErr != nil && searchUnreadableCandidate(path) {
					delta.unreadable = append(delta.unreadable, path)
				}
				return nil
			}
			delta.added = append(delta.added, path)
			if shouldRebuildSearchIndex(idx, delta.changeCount()) {
				tooBig = true
				return filepath.SkipAll
			}
			return nil
		}
		seen[existing] = true
		meta, hasMeta := idx.fileMeta[existing]
		_, unread := unreadKnown[searchPathKey(existing)]
		if !hasMeta || meta.size != info.Size() || !meta.modTime.Equal(info.ModTime()) || notedPath(noted, path) || notedPath(noted, existing) || unread {
			delta.modified = append(delta.modified, existing)
			if shouldRebuildSearchIndex(idx, delta.changeCount()) {
				tooBig = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return searchIndexDelta{}, false
	}
	if tooBig {
		return searchIndexDelta{tooBig: true, ignored: delta.ignored}, true
	}
	for path := range idx.fileSet {
		if seen[path] || searchPathUnderAny(blocked, path) {
			continue
		}
		delta.deleted = append(delta.deleted, path)
	}
	if len(idx.fileSet)-len(delta.deleted)+len(delta.added) > fileLimit {
		delta.overCapacity = true
	}
	if shouldRebuildSearchIndex(idx, delta.changeCount()) {
		delta.tooBig = true
	}
	delta.blocked = blocked
	return delta, true
}

const (
	searchFileOK = iota
	searchFileSkipped
	searchFileUnreadable
)

func searchPrefixBinary(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	var buf [8000]byte
	n, err := file.Read(buf[:])
	if n == 0 && err != nil && err != io.EOF {
		return false, err
	}
	return bytes.Contains(buf[:n], []byte{0}), nil
}

func readIndexableSearchFile(path string) ([]byte, os.FileInfo, int) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, searchFileSkipped
		}
		return nil, nil, searchFileUnreadable
	}
	if info.IsDir() || info.Size() > MaxSearchFileSize {
		return nil, info, searchFileSkipped
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, searchFileUnreadable
	}
	if bytes.Contains(data[:min(len(data), 8000)], []byte{0}) {
		return nil, info, searchFileSkipped
	}
	return data, info, searchFileOK
}

func applySearchIndexDelta(idx *localSearchIndex, delta searchIndexDelta, inplace bool) (*localSearchIndex, map[string]struct{}, bool) {
	if idx == nil || delta.overCapacity {
		return nil, nil, false
	}
	next := idx
	changed := false
	if !inplace {
		next = cloneSearchIndexShell(idx)
	}
	var settled map[string]struct{}
	settle := func(path string) {
		if path == "" {
			return
		}
		if settled == nil {
			settled = map[string]struct{}{}
		}
		settled[searchPathKey(path)] = struct{}{}
	}
	idOf := make(map[string]int, len(next.files))
	for i, path := range next.files {
		if path != "" {
			idOf[path] = i
		}
	}
	fileLimit := maxIndexedSearchFiles
	drop := func(path string) {
		id, dropOK := idOf[path]
		if !dropOK {
			return
		}
		removeSearchPosting(next.postings, id, !inplace)
		delete(next.fileSet, path)
		delete(next.fileMeta, path)
		next.files[id] = ""
		delete(idOf, path)
		changed = true
		settle(path)
	}
	for _, path := range delta.deleted {
		drop(path)
	}
	for _, path := range delta.modified {
		data, info, kind := readIndexableSearchFile(path)
		if kind == searchFileUnreadable {
			next.unread = append(next.unread, path)
			continue
		}
		if kind == searchFileSkipped {
			drop(path)
			forgetSearchUnread(next, path)
			continue
		}
		id, exists := idOf[path]
		if !exists {
			continue
		}
		forgetSearchUnread(next, path)
		sig := searchContentSignature(data)
		meta := next.fileMeta[path]
		if meta.signature == sig && meta.size == int64(len(data)) && meta.size == info.Size() {
			if !meta.modTime.Equal(info.ModTime()) {
				meta.modTime = info.ModTime()
				next.fileMeta[path] = meta
				changed = true
			}
			settle(path)
			continue
		}
		removeSearchPosting(next.postings, id, !inplace)
		for trigram := range contentTrigramsBytes(data) {
			addSearchPosting(next.postings, trigram, id, !inplace)
		}
		next.fileMeta[path] = localSearchFileMeta{size: info.Size(), modTime: info.ModTime(), signature: sig}
		changed = true
		settle(path)
	}
	for _, path := range delta.added {
		if next.fileSet[path] {
			forgetSearchUnread(next, path)
			settle(path)
			continue
		}
		data, info, kind := readIndexableSearchFile(path)
		if kind == searchFileUnreadable {
			next.unread = append(next.unread, path)
			continue
		}
		if kind == searchFileSkipped {
			forgetSearchUnread(next, path)
			settle(path)
			continue
		}
		if len(next.fileSet) >= fileLimit {
			return nil, settled, false
		}
		id := len(next.files)
		next.files = append(next.files, path)
		next.fileSet[path] = true
		next.fileMeta[path] = localSearchFileMeta{
			size:      info.Size(),
			modTime:   info.ModTime(),
			signature: searchContentSignature(data),
		}
		idOf[path] = id
		for trigram := range contentTrigramsBytes(data) {
			addSearchPosting(next.postings, trigram, id, !inplace)
		}
		changed = true
		forgetSearchUnread(next, path)
		settle(path)
	}
	if len(next.fileSet) == 0 {
		return nil, settled, false
	}
	if !changed && searchUnreadSame(idx.unread, next.unread) {
		return idx, settled, true
	}
	if changed || next.fileIDs == nil {
		next.fileIDs = searchFileIDsFromSlice(next.files)
	}
	return next, settled, true
}

func cloneSearchIndexShell(idx *localSearchIndex) *localSearchIndex {
	next := *idx
	next.fileIDs = nil
	next.files = append([]string(nil), idx.files...)
	next.fileSet = make(map[string]bool, len(idx.fileSet))
	for path, ok := range idx.fileSet {
		if ok {
			next.fileSet[path] = true
		}
	}
	next.fileMeta = make(map[string]localSearchFileMeta, len(idx.fileMeta))
	for path, meta := range idx.fileMeta {
		next.fileMeta[path] = meta
	}
	next.postings = make(map[string][]int, len(idx.postings))
	for trigram, ids := range idx.postings {
		next.postings[trigram] = ids
	}
	next.unlisted = append([]string(nil), idx.unlisted...)
	next.unread = append([]string(nil), idx.unread...)
	return &next
}

func removeSearchPosting(postings map[string][]int, id int, cow bool) {
	for trigram, ids := range postings {
		i := sort.SearchInts(ids, id)
		if i >= len(ids) || ids[i] != id {
			continue
		}
		var next []int
		if cow {
			next = make([]int, 0, len(ids)-1)
			next = append(next, ids[:i]...)
			next = append(next, ids[i+1:]...)
		} else {
			next = append(ids[:i], ids[i+1:]...)
		}
		if len(next) == 0 {
			delete(postings, trigram)
			continue
		}
		postings[trigram] = next
	}
}

func addSearchPosting(postings map[string][]int, trigram string, id int, cow bool) {
	ids := postings[trigram]
	if cow {
		ids = append([]int(nil), ids...)
	}
	postings[trigram] = insertSortedSearchID(ids, id)
}

func insertSortedSearchID(ids []int, id int) []int {
	i := sort.SearchInts(ids, id)
	if i < len(ids) && ids[i] == id {
		return ids
	}
	ids = append(ids, 0)
	copy(ids[i+1:], ids[i:])
	ids[i] = id
	return ids
}

func searchIndexDiskRoot() string {
	if strings.TrimSpace(searchIndexDiskDir) != "" {
		return searchIndexDiskDir
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".maclaw", "cache", "search-index")
}

type storedSearchIndex struct {
	Version     int
	Key         string
	Root        string
	Exclude     string
	Hidden      bool
	MaxFileSize int64
	Kind        string
	Files       []string
	Sizes       []int64
	ModUnixNano []int64
	Signatures  []uint64
	Postings    map[string][]int
	Unlisted    []string
	Unread      []string
}

func searchIndexDiskPath(dir, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".gob")
}

func writeSearchIndexFile(dir string, idx *localSearchIndex) error {
	if idx == nil || !idx.complete || dir == "" {
		return fmt.Errorf("search index is not complete")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stored := storedSearchIndex{
		Version:     searchIndexDiskVersion,
		Key:         idx.key,
		Root:        idx.root,
		Exclude:     idx.exclude,
		Hidden:      idx.hidden,
		MaxFileSize: MaxSearchFileSize,
		Kind:        idx.kind,
		Files:       append([]string(nil), idx.files...),
		Sizes:       make([]int64, len(idx.files)),
		ModUnixNano: make([]int64, len(idx.files)),
		Signatures:  make([]uint64, len(idx.files)),
		Postings:    cloneSearchPostings(idx.postings),
		Unlisted:    append([]string(nil), idx.unlisted...),
		Unread:      append([]string(nil), idx.unread...),
	}
	for i, path := range idx.files {
		meta := idx.fileMeta[path]
		stored.Sizes[i] = meta.size
		stored.ModUnixNano[i] = meta.modTime.UnixNano()
		stored.Signatures[i] = meta.signature
	}
	path := searchIndexDiskPath(dir, idx.key)
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(file).Encode(&stored); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func loadSearchIndexFile(dir, key, root, exclude string, hidden bool) (*localSearchIndex, bool) {
	if dir == "" || key == "" {
		return nil, false
	}
	path := searchIndexDiskPath(dir, key)
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	var stored storedSearchIndex
	decodeErr := gob.NewDecoder(file).Decode(&stored)
	file.Close()
	if decodeErr != nil || stored.Key == "" {
		os.Remove(path)
		return nil, false
	}
	if !storedSearchIndexMatches(&stored, key, root, exclude, hidden) {
		if stored.Key == key {
			os.Remove(path)
		}
		return nil, false
	}
	idx := &localSearchIndex{
		key:      key,
		root:     stored.Root,
		exclude:  stored.Exclude,
		hidden:   stored.Hidden,
		kind:     stored.Kind,
		complete: true,
		files:    stored.Files,
		fileSet:  make(map[string]bool, len(stored.Files)),
		fileMeta: make(map[string]localSearchFileMeta, len(stored.Files)),
		postings: stored.Postings,
		unlisted: cleanSearchUnlisted(stored.Unlisted),
		unread:   cleanSearchUnread(stored.Unread),
	}
	for i, filePath := range stored.Files {
		if filePath == "" {
			continue
		}
		idx.fileSet[filePath] = true
		idx.fileMeta[filePath] = localSearchFileMeta{
			size:      stored.Sizes[i],
			modTime:   time.Unix(0, stored.ModUnixNano[i]),
			signature: stored.Signatures[i],
		}
	}
	idx.fileIDs = searchFileIDsFromSlice(idx.files)
	return idx, true
}

func storedSearchIndexMatches(stored *storedSearchIndex, key, root, exclude string, hidden bool) bool {
	if stored == nil || stored.Version != searchIndexDiskVersion {
		return false
	}
	if stored.Key != key || stored.Root != root || stored.Exclude != exclude || stored.Hidden != hidden {
		return false
	}
	if stored.MaxFileSize != MaxSearchFileSize {
		return false
	}
	if len(stored.Files) == 0 || len(stored.Files) != len(stored.Sizes) || len(stored.Files) != len(stored.ModUnixNano) || len(stored.Files) != len(stored.Signatures) {
		return false
	}
	if len(stored.Postings) == 0 {
		return false
	}
	return true
}

func cloneSearchPostings(in map[string][]int) map[string][]int {
	out := make(map[string][]int, len(in))
	for key, ids := range in {
		out[key] = append([]int(nil), ids...)
	}
	return out
}
