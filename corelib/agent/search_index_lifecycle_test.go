package agent

import (
	"context"
	"encoding/gob"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	searchIndexAutoReconcile = false
	searchIndexPersistEnabled = false
	os.Exit(m.Run())
}

func TestSearchIndexFirstQueryDoesNotWaitForBuild(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc FirstQuerySymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "FirstQuerySymbol",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) {
		t.Fatalf("first search missed the file:\n%s", out)
	}
	if !strings.Contains(out, "mode=full_scan") || !strings.Contains(out, "fallback=index_unavailable") {
		t.Fatalf("first search should scan while the index builds:\n%s", out)
	}
	indexed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "FirstQuerySymbol",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(indexed, "mode=indexed") || !strings.Contains(indexed, path) {
		t.Fatalf("later search should use the published index:\n%s", indexed)
	}
}

func TestSearchIndexAlternationStillSchedulesCorpus(t *testing.T) {
	dir := t.TempDir()
	todoFile := filepath.Join(dir, "todo.go")
	fixmeFile := filepath.Join(dir, "fixme.go")
	otherFile := filepath.Join(dir, "other.go")
	if err := os.WriteFile(todoFile, []byte("package main\n// TODO: AlternationCorpusNeedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixmeFile, []byte("package main\n// FIXME: AlternationCorpusNeedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherFile, []byte("package main\n// nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TODO|FIXME",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, todoFile) || !strings.Contains(out, fixmeFile) || strings.Contains(out, otherFile) {
		t.Fatalf("alternation search missed a branch:\n%s", out)
	}
	if !strings.Contains(out, "mode=full_scan") || !strings.Contains(out, "fallback=index_unavailable") {
		t.Fatalf("alternation search should scan while the index builds:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	exclude := resolveSearchExcludePattern(map[string]interface{}{}, dir)
	deadline := time.Now().Add(8 * time.Second)
	for publishedSearchIndex(root, "", exclude, "", false) == nil {
		if time.Now().After(deadline) {
			t.Fatal("alternation search did not schedule a corpus build")
		}
		time.Sleep(5 * time.Millisecond)
	}
	indexed := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TODO|FIXME",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(indexed, "mode=indexed") || !strings.Contains(indexed, todoFile) || !strings.Contains(indexed, fixmeFile) || strings.Contains(indexed, otherFile) {
		t.Fatalf("alternation should use the corpus once it is published:\n%s", indexed)
	}
}

func TestSearchProcessIsTestBinary(t *testing.T) {
	if !searchProcessIsTestBinary() {
		t.Fatalf("go test binary %q was not recognized", os.Args[0])
	}
}

func TestSearchIndexCorpusServesEveryGlob(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	tsFile := filepath.Join(dir, "main.ts")
	body := "export const SharedCorpusNeedle = 1\n"
	if err := os.WriteFile(goFile, []byte("package main\nconst SharedCorpusNeedle = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	goOut := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SharedCorpusNeedle",
		"glob":        "**/*.go",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(goOut, goFile) || strings.Contains(goOut, tsFile) || !strings.Contains(goOut, "mode=indexed") {
		t.Fatalf("go glob should use the corpus index:\n%s", goOut)
	}
	tsOut := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "SharedCorpusNeedle",
		"glob":        "**/*.ts",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(tsOut, tsFile) || strings.Contains(tsOut, goFile) || !strings.Contains(tsOut, "mode=indexed") {
		t.Fatalf("ts glob should reuse the corpus index:\n%s", tsOut)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	searchIndexCache.Lock()
	defer searchIndexCache.Unlock()
	found := 0
	for key, idx := range searchIndexCache.byRoot {
		if idx == nil || idx.root != filepath.Clean(root) {
			continue
		}
		found++
		if strings.Contains(key, "\x00scoped") || idx.kind == searchIndexScopedKind {
			t.Fatalf("corpus hit still built a scoped index: %s", key)
		}
	}
	if found != 1 {
		t.Fatalf("corpus indexes for %s = %d, want 1", root, found)
	}
}

func TestSearchIndexTruncationDoesNotPublishPartialCoverage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc PartialOne() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\nfunc PartialTwo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := maxIndexedSearchFiles
	maxIndexedSearchFiles = 1
	defer func() { maxIndexedSearchFiles = original }()

	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	candidates, ok, stats := indexedSearchCandidates(root, "PartialTwo", "", "", "", false, false)
	if ok || len(candidates) != 0 || stats.fallbackReason != "index_unavailable" {
		t.Fatalf("incomplete coverage returned ok=%v candidates=%v stats=%+v", ok, candidates, stats)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if corpusBuildFailed(searchIndexCorpusKey(root, "", false)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	candidates, ok, stats = indexedSearchCandidates(root, "PartialTwo", "", "", "", false, false)
	if ok || stats.fallbackReason != "index_unavailable" {
		t.Fatalf("truncated index was published: ok=%v stats=%+v", ok, stats)
	}
}

func TestNoteSearchIndexDirtyIsVisibleBeforeReconcile(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "main.go")
	if err := os.WriteFile(original, []byte("package main\nfunc BeforeNote() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "BeforeNote",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, original) {
		t.Fatalf("warmup missed original file:\n%s", out)
	}
	added := filepath.Join(dir, "added.go")
	if err := os.WriteFile(added, []byte("package main\nfunc NotedBeforeReconcile() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	miss := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "NotedBeforeReconcile",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(miss, added) {
		t.Fatalf("unnoted external file should wait for reconcile:\n%s", miss)
	}
	NoteSearchIndexDirty(added)
	hit := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "NotedBeforeReconcile",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(hit, added) || !strings.Contains(hit, "mode=indexed") {
		t.Fatalf("noted file should be read before reconcile:\n%s", hit)
	}
}

func TestSearchIndexReconcileOverdueFallsBackToFullScan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc OverdueSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, ok := buildLocalSearchIndex(dir, "", "", "", false)
	if !ok {
		t.Fatal("buildLocalSearchIndex failed")
	}
	idx.complete = true
	stale := time.Now().Add(-searchIndexReconcileOverdue - time.Minute)
	idx.builtAt = stale
	idx.reconciledAt = stale
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	idx.root = root
	idx.key = searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	searchIndexCache.byRoot[idx.key] = idx
	searchIndexCache.Unlock()

	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "OverdueSymbol",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, path) || !strings.Contains(out, "mode=full_scan") || !strings.Contains(out, "fallback=reconcile_overdue") {
		t.Fatalf("overdue index should fall back to a full scan:\n%s", out)
	}
}

func TestPersistedSearchIndexRejectsMismatchAndTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc DiskSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	idx, ok := buildLocalSearchIndex(root, "", "", "", false)
	if !ok {
		t.Fatal("buildLocalSearchIndex failed")
	}
	idx.complete = true
	idx.kind = searchIndexCorpusKind
	idx.key = searchIndexCorpusKey(root, "", false)
	disk := t.TempDir()
	if err := writeSearchIndexFile(disk, idx); err != nil {
		t.Fatal(err)
	}
	incomplete := *idx
	incomplete.complete = false
	if err := writeSearchIndexFile(disk, &incomplete); err == nil {
		t.Fatal("incomplete index was persisted")
	}
	loaded, ok := loadSearchIndexFile(disk, idx.key, root, "", false)
	if !ok || loaded == nil || !loaded.fileSet[path] {
		t.Fatalf("load complete index ok=%v idx=%v", ok, loaded)
	}
	if _, ok := loadSearchIndexFile(disk, idx.key, filepath.Join(root, "other"), "", false); ok {
		t.Fatal("mismatched root was accepted")
	}
	if _, err := os.Stat(searchIndexDiskPath(disk, idx.key)); !os.IsNotExist(err) {
		t.Fatalf("mismatched index file still exists: %v", err)
	}
	if err := writeSearchIndexFile(disk, idx); err != nil {
		t.Fatal(err)
	}
	storedPath := searchIndexDiskPath(disk, idx.key)
	if err := os.WriteFile(storedPath, []byte("not-a-gob"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadSearchIndexFile(disk, idx.key, root, "", false); ok {
		t.Fatal("truncated index was accepted")
	}
	if _, err := os.Stat(storedPath); !os.IsNotExist(err) {
		t.Fatalf("truncated index file still exists: %v", err)
	}
}

func TestPersistedCorpusLoadsWithoutRereadingContent(t *testing.T) {
	dir := t.TempDir()
	disk := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc DiskReuseSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousEnabled := searchIndexPersistEnabled
	previousDir := searchIndexDiskDir
	searchIndexPersistEnabled = true
	searchIndexDiskDir = disk
	defer func() {
		searchIndexPersistEnabled = previousEnabled
		searchIndexDiskDir = previousDir
	}()

	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DiskReuseSymbol",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(out, "mode=indexed") {
		t.Fatalf("warmup did not publish an index:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, resolveSearchExcludePattern(map[string]interface{}{}, dir), false)
	stored := searchIndexDiskPath(disk, key)
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("published index was not persisted: %v", err)
	}
	loaded, ok := loadSearchIndexFile(disk, key, root, "", false)
	if !ok {
		t.Fatal("persisted index did not load")
	}
	loaded.postings = map[string][]int{"xxx": {0}}
	if err := writeSearchIndexFile(disk, loaded); err != nil {
		t.Fatal(err)
	}
	searchIndexCache.Lock()
	delete(searchIndexCache.byRoot, key)
	searchIndexCache.Unlock()
	searchBuildMu.Lock()
	delete(searchBuildSlots, key)
	searchBuildMu.Unlock()

	out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DiskReuseSymbol",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, path) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("reload should use the stored postings before a content rebuild:\n%s", out)
	}
}

func TestToolWriteFileNotesSearchIndexDirty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc ExistingWriteHook() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "ExistingWriteHook",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, "main.go") {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	added := filepath.Join(dir, "hooked.go")
	wrote := ToolWriteFile(map[string]interface{}{
		"path":    added,
		"content": "package main\nfunc WrittenThroughTool() {}\n",
	})
	if !strings.Contains(wrote, added) {
		t.Fatalf("write_file failed: %s", wrote)
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "WrittenThroughTool",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, added) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("write_file should be visible to the next indexed search:\n%s", out)
	}
}

func notedSearchHas(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	if _, ok := notedSearchDirty.paths[abs]; ok {
		return true
	}
	for existing := range notedSearchDirty.paths {
		if searchPathEqual(existing, abs) {
			return true
		}
	}
	return false
}

func backdateSearchNote(t *testing.T, path string) {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	abs = filepath.Clean(abs)
	notedSearchDirty.Lock()
	defer notedSearchDirty.Unlock()
	if _, ok := notedSearchDirty.paths[abs]; !ok {
		t.Fatalf("missing note for %s", abs)
	}
	notedSearchDirty.paths[abs] = time.Now().Add(-time.Second)
}

func writeIndexedGoFile(t *testing.T, dir, name, fn string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("package main\nfunc "+fn+"() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSearchPathUnder(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !searchPathUnder(`C:\Repo`, `c:\repo\file.go`) {
			t.Fatal("windows path case should stay inside the root")
		}
		if searchPathUnder(`C:\Repo`, `C:\Repository\file.go`) {
			t.Fatal("neighbor directory matched the root")
		}
		if !searchPathUnder(`C:\`, `C:\file.go`) {
			t.Fatal("volume root should contain its files")
		}
		if searchPathUnder(`C:\`, `D:\file.go`) {
			t.Fatal("volume root matched another drive")
		}
		rel, ok := searchIndexRelativePath(`C:\`, `C:\main.go`)
		if !ok || rel != "main.go" {
			t.Fatalf("volume relative path = %q, %v", rel, ok)
		}
		rel, ok = searchIndexRelativePath(`C:\Repo`, `C:\Repo\main.go`)
		if !ok || rel != "main.go" {
			t.Fatalf("repo relative path = %q, %v", rel, ok)
		}
		if _, ok := searchIndexRelativePath(`C:\Repo`, `C:\Repository\file.go`); ok {
			t.Fatal("neighbor directory produced a relative path")
		}
		return
	}
	if !searchPathUnder("/repo", "/repo/file.go") {
		t.Fatal("path should stay inside the root")
	}
	if searchPathUnder("/repo", "/Repo/file.go") {
		t.Fatal("path case should stay distinct")
	}
	rel, ok := searchIndexRelativePath("/", "/main.go")
	if !ok || rel != "main.go" {
		t.Fatalf("root relative path = %q, %v", rel, ok)
	}
}

func TestNotedSearchPathsUseVolumeCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	root := `C:\RepoCase`
	path := `c:\repocase\added.go`
	neighbor := `C:\RepoCaseExtra\added.go`
	notedSearchDirty.Lock()
	notedSearchDirty.paths[path] = time.Now()
	notedSearchDirty.paths[neighbor] = time.Now()
	notedSearchDirty.Unlock()
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, path)
		delete(notedSearchDirty.paths, neighbor)
		notedSearchDirty.Unlock()
	})
	got := notedSearchPaths(root, "", "", "", false)
	if len(got) != 1 || !strings.EqualFold(got[0], path) {
		t.Fatalf("noted paths = %#v, want only the folded file", got)
	}
}

func TestSearchIndexForgetReconciledNotesStatsOutsideLock(t *testing.T) {
	dir := t.TempDir()
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	clean := func(path string) string {
		t.Helper()
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			t.Fatal(absErr)
		}
		return filepath.Clean(abs)
	}
	kept := clean(writeIndexedGoFile(t, dir, "kept.go", "KeptNote"))
	settledPath := clean(writeIndexedGoFile(t, dir, "settled.go", "SettledNote"))
	missing := clean(filepath.Join(root, "missing.go"))
	refreshed := clean(filepath.Join(root, "refreshed.go"))
	skipped := clean(filepath.Join(root, "node_modules", "skip.go"))
	for _, path := range []string{kept, settledPath, missing, refreshed, skipped} {
		NoteSearchIndexDirty(path)
		backdateSearchNote(t, path)
	}
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		defer notedSearchDirty.Unlock()
		for _, path := range []string{kept, settledPath, missing, refreshed, skipped} {
			delete(notedSearchDirty.paths, path)
		}
	})
	stated := map[string]int{}
	before := time.Now()
	prevStat := statSearchNote
	t.Cleanup(func() { statSearchNote = prevStat })
	statSearchNote = func(path string) (os.FileInfo, error) {
		held := !notedSearchDirty.TryLock()
		if held {
			t.Errorf("Stat ran while the note lock was held: %s", path)
		} else {
			notedSearchDirty.Unlock()
		}
		stated[path]++
		// Same instant as this pass started: not After(before), but not the
		// timestamp that was classified either.
		if !held && searchPathEqual(path, refreshed) {
			notedSearchDirty.Lock()
			notedSearchDirty.paths[path] = before
			notedSearchDirty.Unlock()
		}
		return os.Stat(path)
	}
	forgetReconciledNotes(&localSearchIndex{root: root}, root, before, map[string]struct{}{
		searchPathKey(settledPath): {},
	}, nil)
	if !notedSearchHas(kept) {
		t.Fatal("note for a file that still exists should stay")
	}
	if notedSearchHas(missing) {
		t.Fatal("note for a missing file should drop")
	}
	if !notedSearchHas(refreshed) {
		t.Fatal("note refreshed during Stat should stay")
	}
	if notedSearchHas(settledPath) {
		t.Fatal("settled note should drop")
	}
	if notedSearchHas(skipped) {
		t.Fatal("skipped-directory note should drop")
	}
	if stated[kept] != 1 || stated[missing] != 1 || stated[refreshed] != 1 {
		t.Fatalf("stat calls = %#v", stated)
	}
	if stated[settledPath] != 0 || stated[skipped] != 0 {
		t.Fatalf("settled and skipped notes were stated: %#v", stated)
	}
}

func TestSearchIndexForgetKeepsSettledNoteChangedSinceSnapshot(t *testing.T) {
	dir := t.TempDir()
	root := mustCleanAbs(t, dir)
	sameTick := mustCleanAbs(t, filepath.Join(root, "same.go"))
	fresh := mustCleanAbs(t, filepath.Join(root, "fresh.go"))
	unchanged := mustCleanAbs(t, filepath.Join(root, "old.go"))
	NoteSearchIndexDirty(sameTick)
	NoteSearchIndexDirty(unchanged)
	backdateSearchNote(t, sameTick)
	backdateSearchNote(t, unchanged)
	snap := snapshotNotedSearchStamps(root)
	before := time.Now()
	notedSearchDirty.Lock()
	notedSearchDirty.paths[sameTick] = before
	notedSearchDirty.paths[fresh] = before
	notedSearchDirty.Unlock()
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		defer notedSearchDirty.Unlock()
		delete(notedSearchDirty.paths, sameTick)
		delete(notedSearchDirty.paths, fresh)
		delete(notedSearchDirty.paths, unchanged)
	})
	settled := map[string]struct{}{
		searchPathKey(sameTick):  {},
		searchPathKey(fresh):     {},
		searchPathKey(unchanged): {},
	}
	forgetReconciledNotes(&localSearchIndex{root: root}, root, before, settled, snap)
	if !notedSearchHas(sameTick) {
		t.Fatal("settled note rewritten at the pass start should stay")
	}
	if !notedSearchHas(fresh) {
		t.Fatal("settled note created during the pass should stay")
	}
	if notedSearchHas(unchanged) {
		t.Fatal("settled note with an unchanged stamp should drop")
	}
}

func TestSearchIndexReconcileDropsUnsearchableNotes(t *testing.T) {
	dir := t.TempDir()
	kept := writeIndexedGoFile(t, dir, "kept.go", "KeptSearchableToken")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "KeptSearchableToken",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, kept) {
		t.Fatalf("warmup missed kept file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	binary := filepath.Join(dir, "binary.go")
	if err := os.WriteFile(binary, []byte{0, 'b', 'i', 'n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	oversize := filepath.Join(dir, "oversize.go")
	file, err := os.Create(oversize)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxSearchFileSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(binary)
	NoteSearchIndexDirty(oversize)
	binaryKey := mustCleanAbs(t, binary)
	oversizeKey := mustCleanAbs(t, oversize)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, binaryKey)
		delete(notedSearchDirty.paths, oversizeKey)
		notedSearchDirty.Unlock()
	})
	backdateSearchNote(t, binary)
	backdateSearchNote(t, oversize)
	deadline := time.Now().Add(2 * time.Second)
	reconciled := false
	for time.Now().Before(deadline) {
		if reconcilePublishedCorpus(root, "", false) {
			reconciled = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reconciled {
		t.Fatal("reconcile did not finish")
	}
	if notedSearchHas(binary) {
		t.Fatal("binary note should drop; search never reads that file")
	}
	if notedSearchHas(oversize) {
		t.Fatal("oversize note should drop; search never reads that file")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "KeptSearchableToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, kept) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("indexed search lost the text file:\n%s", out)
	}
}

func TestSearchIndexSymlinkWriteNotesTarget(t *testing.T) {
	dir := t.TempDir()
	target := writeIndexedGoFile(t, dir, "target.go", "SymlinkTargetOld")
	link := filepath.Join(dir, "link.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable on this platform/configuration: %v", err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SymlinkTargetOld",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(out, target) || strings.Contains(out, link) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("warmup missed target:\n%s", out)
	}
	if err := os.WriteFile(link, []byte("package main\nfunc SymlinkTargetNew() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(link)
	targetKey := mustCleanAbs(t, target)
	linkKey := mustCleanAbs(t, link)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, targetKey)
		delete(notedSearchDirty.paths, linkKey)
		notedSearchDirty.Unlock()
	})
	if !notedSearchHas(target) {
		t.Fatal("write through a symlink should note the target")
	}
	if notedSearchHas(link) {
		t.Fatal("symlink path should not keep the note")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "SymlinkTargetNew",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, target) || strings.Contains(out, link) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("indexed search missed the write through a symlink:\n%s", out)
	}
	notedSearchDirty.Lock()
	notedSearchDirty.paths[linkKey] = time.Now().Add(-time.Second)
	notedSearchDirty.Unlock()
	root := mustCleanAbs(t, dir)
	deadline := time.Now().Add(2 * time.Second)
	reconciled := false
	for time.Now().Before(deadline) {
		if reconcilePublishedCorpus(root, "", false) {
			reconciled = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reconciled {
		t.Fatal("reconcile did not finish")
	}
	if notedSearchHas(link) {
		t.Fatal("symlink note should drop; search never reads that path")
	}
}

func TestSearchIndexDirSymlinkNotesRealFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeIndexedGoFile(t, real, "file.go", "DirLinkOld")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable on this platform/configuration: %v", err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "DirLinkOld",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(out, file) || strings.Contains(out, link) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("warmup missed real file:\n%s", out)
	}
	via := filepath.Join(link, "file.go")
	if err := os.WriteFile(via, []byte("package main\nfunc DirLinkNew() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(via)
	fileKey := mustCleanAbs(t, file)
	viaKey := mustCleanAbs(t, via)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, fileKey)
		delete(notedSearchDirty.paths, viaKey)
		notedSearchDirty.Unlock()
	})
	if !notedSearchHas(file) {
		t.Fatal("write through a directory symlink should note the real file")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "DirLinkNew",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, file) || strings.Contains(out, link) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("indexed search followed a directory symlink:\n%s", out)
	}
	notedSearchDirty.Lock()
	notedSearchDirty.paths[viaKey] = time.Now().Add(-time.Second)
	notedSearchDirty.Unlock()
	root := mustCleanAbs(t, dir)
	deadline := time.Now().Add(2 * time.Second)
	reconciled := false
	for time.Now().Before(deadline) {
		if reconcilePublishedCorpus(root, "", false) {
			reconciled = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reconciled {
		t.Fatal("reconcile did not finish")
	}
	if notedSearchHas(via) {
		t.Fatal("note through a directory symlink should drop")
	}
}

func TestSearchIndexJunctionWriteNotesRealFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows reparse point")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeIndexedGoFile(t, real, "file.go", "JunctionRealToken")
	link := filepath.Join(dir, "link")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput()
	if err != nil {
		t.Skipf("junction not available: %v (%s)", err, out)
	}
	if warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "JunctionRealToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(warmed, file) || strings.Contains(warmed, link) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("warmup missed real file:\n%s", warmed)
	}
	fresh := filepath.Join(real, "fresh.go")
	via := filepath.Join(link, "fresh.go")
	if err := os.WriteFile(via, []byte("package main\nfunc JunctionFreshToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(via)
	freshKey := mustCleanAbs(t, fresh)
	viaKey := mustCleanAbs(t, via)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, freshKey)
		delete(notedSearchDirty.paths, viaKey)
		notedSearchDirty.Unlock()
	})
	if !notedSearchHas(fresh) {
		t.Fatal("write through a directory junction should note the real file")
	}
	found := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "JunctionFreshToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(found, fresh) || strings.Contains(found, link) || !strings.Contains(found, "mode=indexed") {
		t.Fatalf("indexed search followed a directory junction:\n%s", found)
	}
	notedSearchDirty.Lock()
	notedSearchDirty.paths[viaKey] = time.Now().Add(-time.Second)
	notedSearchDirty.Unlock()
	root := mustCleanAbs(t, dir)
	deadline := time.Now().Add(2 * time.Second)
	reconciled := false
	for time.Now().Before(deadline) {
		if reconcilePublishedCorpus(root, "", false) {
			reconciled = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reconciled {
		t.Fatal("reconcile did not finish")
	}
	if notedSearchHas(via) {
		t.Fatal("note through a directory junction should drop")
	}
}

func TestSearchIndexNestedJunctionWriteNotesRealFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows reparse point")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeIndexedGoFile(t, real, "file.go", "NestedJunctionOld")
	hop := filepath.Join(dir, "hop")
	gate := filepath.Join(dir, "gate")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", hop, real).CombinedOutput(); err != nil {
		t.Skipf("junction not available: %v (%s)", err, out)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", gate, hop).CombinedOutput(); err != nil {
		t.Skipf("nested junction not available: %v (%s)", err, out)
	}
	if warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        real,
		"pattern":     "NestedJunctionOld",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(warmed, file) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("warmup missed real file:\n%s", warmed)
	}
	if warmed := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        hop,
		"pattern":     "NestedJunctionOld",
		"output_mode": "files_with_matches",
		"stats":       true,
	}); !strings.Contains(warmed, filepath.Join(hop, "file.go")) || !strings.Contains(warmed, "mode=indexed") {
		t.Fatalf("warmup missed junction file:\n%s", warmed)
	}
	fresh := filepath.Join(real, "fresh.go")
	via := filepath.Join(gate, "fresh.go")
	mid := filepath.Join(hop, "fresh.go")
	if err := os.WriteFile(via, []byte("package main\nfunc NestedJunctionFresh() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(via)
	freshKey := mustCleanAbs(t, fresh)
	viaKey := mustCleanAbs(t, via)
	midKey := mustCleanAbs(t, mid)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, freshKey)
		delete(notedSearchDirty.paths, viaKey)
		delete(notedSearchDirty.paths, midKey)
		notedSearchDirty.Unlock()
	})
	if !notedSearchHas(fresh) {
		t.Fatal("write through a nested directory junction should note the real file")
	}
	if !notedSearchHas(mid) {
		t.Fatal("write through a nested directory junction should note the intermediate path")
	}
	found := ToolRipgrep(map[string]interface{}{
		"path":        real,
		"pattern":     "NestedJunctionFresh",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(found, fresh) || !strings.Contains(found, "mode=indexed") {
		t.Fatalf("indexed search missed a write through a nested directory junction:\n%s", found)
	}
	foundHop := ToolRipgrep(map[string]interface{}{
		"path":        hop,
		"pattern":     "NestedJunctionFresh",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(foundHop, mid) || strings.Contains(foundHop, gate) || !strings.Contains(foundHop, "mode=indexed") {
		t.Fatalf("indexed junction search missed a write through the next junction:\n%s", foundHop)
	}
}

func mustCleanAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(abs)
}

func TestSearchIndexReconcilePatchesSmallChange(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		writeIndexedGoFile(t, dir, name, "Keep"+strings.TrimSuffix(name, ".go"))
	}
	stable := writeIndexedGoFile(t, dir, "stable.go", "QqqStableToken")
	changed := writeIndexedGoFile(t, dir, "changed.go", "BeforePatchToken")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "QqqStableToken",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, stable) {
		t.Fatalf("warmup missed stable file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	idx := searchIndexCache.byRoot[key]
	before := []int(nil)
	if idx != nil {
		before = idx.postings["qqq"]
	}
	searchIndexCache.Unlock()
	if idx == nil || len(before) == 0 {
		t.Fatal("stable trigram was not indexed")
	}
	if err := os.WriteFile(changed, []byte("package main\nfunc AfterPatchToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("reconcile did not publish the patched file")
	}
	searchIndexCache.Lock()
	next := searchIndexCache.byRoot[key]
	after := []int(nil)
	if next != nil {
		after = next.postings["qqq"]
	}
	searchIndexCache.Unlock()
	if next == nil || next == idx {
		t.Fatal("small reconcile rebuilt from scratch or dropped the index")
	}
	if len(after) == 0 || &after[0] != &before[0] {
		t.Fatal("unchanged postings were copied")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AfterPatchToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, changed) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("patched index missed the edited file:\n%s", out)
	}
}

func TestSearchIndexReconcileDropsDeletedFile(t *testing.T) {
	dir := t.TempDir()
	kept := writeIndexedGoFile(t, dir, "kept.go", "KeepDeletedIndex")
	deleted := writeIndexedGoFile(t, dir, "deleted.go", "GoneDeletedIndex")
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeIndexedGoFile(t, dir, name, "Pad"+strings.TrimSuffix(name, ".go"))
	}
	writeIndexedGoFile(t, dir, "stable.go", "QqqStableToken")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "KeepDeletedIndex",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, kept) {
		t.Fatalf("warmup missed kept file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	idx := searchIndexCache.byRoot[key]
	before := []int(nil)
	if idx != nil {
		before = idx.postings["qqq"]
	}
	searchIndexCache.Unlock()
	if idx == nil || len(before) == 0 || !idx.fileSet[deleted] {
		t.Fatal("deleted file was not indexed")
	}
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("reconcile did not publish the deletion")
	}
	searchIndexCache.Lock()
	next := searchIndexCache.byRoot[key]
	after := []int(nil)
	if next != nil {
		after = next.postings["qqq"]
	}
	inSet := next != nil && next.fileSet[deleted]
	searchIndexCache.Unlock()
	if inSet {
		t.Fatal("deleted file stayed in the index")
	}
	if len(after) == 0 || &after[0] != &before[0] {
		t.Fatal("deletion rebuilt every posting list")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "GoneDeletedIndex",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, deleted) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("deleted file still searchable:\n%s", out)
	}
}

func TestSearchIndexReconcileIgnoresSkippedNotes(t *testing.T) {
	dir := t.TempDir()
	kept := writeIndexedGoFile(t, dir, "main.go", "SkipNoteKeep")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SkipNoteKeep",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, kept) {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	skippedDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(skippedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skipped := filepath.Join(skippedDir, "skip.go")
	if err := os.WriteFile(skipped, []byte("package skip\nfunc SkipNoteHidden() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(skipped)
	backdateSearchNote(t, skipped)
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	prev := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if prev == nil {
		t.Fatal("missing published index")
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("skipped note failed reconcile")
	}
	searchIndexCache.Lock()
	next := searchIndexCache.byRoot[key]
	indexedSkip := next != nil && next.fileSet[skipped]
	searchIndexCache.Unlock()
	if next != prev || indexedSkip {
		t.Fatal("note inside a skipped directory rebuilt the corpus")
	}
	if notedSearchHas(skipped) {
		t.Fatal("note inside a skipped directory was kept")
	}
}

func TestSearchIndexNoteOnSkipNamedFileStaysVisible(t *testing.T) {
	dir := t.TempDir()
	kept := writeIndexedGoFile(t, dir, "main.go", "SkipNamedKeep")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "SkipNamedKeep",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, kept) {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	envFile := filepath.Join(dir, "env")
	if err := os.WriteFile(envFile, []byte("package main\nfunc EnvFileToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vendorFile := filepath.Join(dir, "vendor", "hidden.go")
	if err := os.MkdirAll(filepath.Dir(vendorFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendorFile, []byte("package hidden\nfunc VendorDirToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(envFile)
	NoteSearchIndexDirty(vendorFile)
	envKey := mustCleanAbs(t, envFile)
	vendorKey := mustCleanAbs(t, vendorFile)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		delete(notedSearchDirty.paths, envKey)
		delete(notedSearchDirty.paths, vendorKey)
		notedSearchDirty.Unlock()
	})
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "EnvFileToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, envFile) || !strings.Contains(out, "mode=indexed") {
		t.Fatalf("file named env should be visible before reconcile:\n%s", out)
	}
	hidden := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "VendorDirToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(hidden, vendorFile) || !strings.Contains(hidden, "mode=indexed") {
		t.Fatalf("file inside vendor should stay out of the index:\n%s", hidden)
	}
	backdateSearchNote(t, envFile)
	backdateSearchNote(t, vendorFile)
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	forgetReconciledNotes(&localSearchIndex{root: filepath.Clean(root)}, root, time.Now(), nil, nil)
	if !notedSearchHas(envFile) {
		t.Fatal("unread note for a file named env should stay")
	}
	if notedSearchHas(vendorFile) {
		t.Fatal("note inside vendor should drop")
	}
}

func TestSearchIndexReconcileKeepsFilesInUnlistedDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeIndexedGoFile(t, dir, name, "Keep"+strings.TrimSuffix(name, ".go"))
	}
	stable := writeIndexedGoFile(t, dir, "stable.go", "QqqStableToken")
	changed := writeIndexedGoFile(t, dir, "changed.go", "BeforeUnlistedToken")
	lockedDir := filepath.Join(dir, "locked")
	if err := os.MkdirAll(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(lockedDir, "kept.go")
	if err := os.WriteFile(kept, []byte("package main\nfunc KeptLockedSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "QqqStableToken",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, stable) {
		t.Fatalf("warmup missed stable file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	idx := searchIndexCache.byRoot[key]
	before := []int(nil)
	keptIndexed := false
	if idx != nil {
		before = idx.postings["qqq"]
		keptIndexed = idx.fileSet[kept]
	}
	searchIndexCache.Unlock()
	if idx == nil || len(before) == 0 || !keptIndexed {
		t.Fatal("locked file was not indexed")
	}
	if err := os.WriteFile(changed, []byte("package main\nfunc AfterUnlistedToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("package main\nfunc KeptLockedUpdated() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(changed)
	NoteSearchIndexDirty(kept)
	backdateSearchNote(t, changed)
	backdateSearchNote(t, kept)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		defer notedSearchDirty.Unlock()
		for _, path := range []string{changed, kept} {
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				continue
			}
			delete(notedSearchDirty.paths, filepath.Clean(abs))
		}
	})

	lockedAbs, err := filepath.Abs(lockedDir)
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("unlistable directory failed reconcile")
	}
	searchIndexCache.Lock()
	next := searchIndexCache.byRoot[key]
	after := []int(nil)
	stillKept := false
	if next != nil {
		after = next.postings["qqq"]
		stillKept = next.fileSet[kept]
	}
	searchIndexCache.Unlock()
	if next == nil || !stillKept {
		t.Fatal("unlistable directory deleted an indexed file")
	}
	if len(after) == 0 || &after[0] != &before[0] {
		t.Fatal("unlistable directory rebuilt every posting list")
	}
	if !notedSearchHas(kept) {
		t.Fatal("note for a file that was not re-read was dropped")
	}
	if notedSearchHas(changed) {
		t.Fatal("note for a re-read file was kept")
	}
	updated := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "KeptLockedUpdated",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(updated, kept) || !strings.Contains(updated, "mode=indexed") {
		t.Fatalf("noted file inside an unlistable directory was missed:\n%s", updated)
	}
	edited := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AfterUnlistedToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(edited, changed) || !strings.Contains(edited, "mode=indexed") {
		t.Fatalf("patched index missed the edited file:\n%s", edited)
	}
}

func TestSearchIndexReconcileUnpublishesEmptyTree(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"} {
		files = append(files, writeIndexedGoFile(t, dir, name, "EmptyTree"+strings.TrimSuffix(name, ".go")))
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "EmptyTreea",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, files[0]) {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	for _, path := range files {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if reconcilePublishedCorpus(root, "", false) {
		t.Fatal("empty tree stayed indexed")
	}
	searchIndexCache.Lock()
	_, still := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if still {
		t.Fatal("empty corpus stayed published")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "EmptyTreea",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if strings.Contains(out, files[0]) || !strings.Contains(out, "mode=full_scan") {
		t.Fatalf("deleted tree should scan:\n%s", out)
	}
}

func TestSearchIndexRebuildKeepsUnlistedFiles(t *testing.T) {
	dir := t.TempDir()
	changed := writeIndexedGoFile(t, dir, "changed.go", "BeforeRebuildToken")
	writeIndexedGoFile(t, dir, "other.go", "OtherRebuildToken")
	lockedDir := filepath.Join(dir, "locked")
	if err := os.MkdirAll(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(lockedDir, "kept.go")
	if err := os.WriteFile(kept, []byte("package main\nfunc KeptAcrossRebuild() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "KeptAcrossRebuild",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, kept) {
		t.Fatalf("warmup missed locked file:\n%s", out)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	searchIndexCache.Lock()
	before := searchIndexCache.byRoot[key]
	beforeOther := []int(nil)
	if before != nil {
		beforeOther = before.postings["oth"]
	}
	searchIndexCache.Unlock()
	if before == nil || len(beforeOther) == 0 {
		t.Fatal("unchanged file was not indexed")
	}
	if err := os.WriteFile(changed, []byte("package main\nfunc AfterRebuildToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	NoteSearchIndexDirty(changed)
	NoteSearchIndexDirty(kept)
	backdateSearchNote(t, changed)
	backdateSearchNote(t, kept)
	t.Cleanup(func() {
		notedSearchDirty.Lock()
		defer notedSearchDirty.Unlock()
		for _, path := range []string{changed, kept} {
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				continue
			}
			delete(notedSearchDirty.paths, filepath.Clean(abs))
		}
	})
	lockedAbs, err := filepath.Abs(lockedDir)
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	if !reconcilePublishedCorpus(root, "", false) {
		t.Fatal("rebuild dropped the corpus instead of keeping the unlistable directory")
	}
	searchIndexCache.Lock()
	next := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if next == nil || next == before {
		t.Fatal("large change did not rebuild the corpus")
	}
	if len(next.postings["oth"]) == 0 || &next.postings["oth"][0] == &beforeOther[0] {
		t.Fatal("large change patched the corpus in place")
	}
	if !searchFileSetHas(next, kept) {
		t.Fatal("rebuild deleted a file in an unlistable directory")
	}
	if !searchPostingHasFile(next, "kep", kept) {
		t.Fatal("rebuild dropped the postings for an unlistable file")
	}
	if !notedSearchHas(kept) {
		t.Fatal("rebuild cleared a note for a file it did not read")
	}
	if notedSearchHas(changed) {
		t.Fatal("rebuild kept a note for a file it reindexed")
	}
	updated := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "AfterRebuildToken",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(updated, changed) || !strings.Contains(updated, "mode=indexed") {
		t.Fatalf("rebuild missed the edited file:\n%s", updated)
	}
	keptHit := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "KeptAcrossRebuild",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(keptHit, kept) || !strings.Contains(keptHit, "mode=indexed") {
		t.Fatalf("rebuild missed the unlistable file:\n%s", keptHit)
	}
}

func TestSearchIndexBuildBlockedSubtreeIsNotEmpty(t *testing.T) {
	dir := t.TempDir()
	lockedDir := filepath.Join(dir, "locked")
	if err := os.MkdirAll(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockedDir, "kept.go"), []byte("package main\nfunc BlockedBuildSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockedAbs, err := filepath.Abs(lockedDir)
	if err != nil {
		t.Fatal(err)
	}
	lockedAbs = filepath.Clean(lockedAbs)
	previousWalk := searchIndexWalk
	t.Cleanup(func() { searchIndexWalk = previousWalk })
	searchIndexWalk = func(ctx context.Context, walkRoot string, fn fs.WalkDirFunc) error {
		return previousWalk(ctx, walkRoot, func(path string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.IsDir() && searchPathEqual(path, lockedAbs) {
				if callErr := fn(path, d, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, d, os.ErrPermission); callErr != nil && callErr != filepath.SkipDir {
					return callErr
				}
				return filepath.SkipDir
			}
			return fn(path, d, err)
		})
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	result := buildLocalSearchIndexResult(context.Background(), root, "", "", "", false)
	if result.empty || result.idx != nil || result.truncated || result.cancelled {
		t.Fatal("a blocked subtree was treated as an empty or complete corpus")
	}
	if len(result.blocked) == 0 {
		t.Fatal("blocked directory was not recorded")
	}
}

func TestSearchIndexWalkKeepsPartialBatch(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previous := readSearchDirBatch
	t.Cleanup(func() { readSearchDirBatch = previous })
	readSearchDirBatch = func(f *os.File, n int) ([]os.DirEntry, error) {
		entries, err := f.ReadDir(n)
		if err != nil || len(entries) == 0 {
			return entries, err
		}
		return entries, os.ErrPermission
	}
	seen := map[string]bool{}
	if err := walkDirCtx(context.Background(), dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		seen[d.Name()] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !seen["a.go"] || !seen["b.go"] {
		t.Fatalf("partial directory batch was dropped: %#v", seen)
	}
}

func TestSearchIndexLoadedTombstoneIsNotAFile(t *testing.T) {
	disk := t.TempDir()
	idx := &localSearchIndex{
		key:      "tomb-key",
		root:     "R",
		kind:     searchIndexCorpusKind,
		complete: true,
		files:    []string{"a.go", "", "b.go"},
		fileSet:  map[string]bool{"a.go": true, "b.go": true},
		fileMeta: map[string]localSearchFileMeta{
			"a.go": {size: 1, signature: 1},
			"b.go": {size: 1, signature: 2},
		},
		postings: map[string][]int{"abc": {0, 2}},
	}
	if err := writeSearchIndexFile(disk, idx); err != nil {
		t.Fatal(err)
	}
	loaded, ok := loadSearchIndexFile(disk, idx.key, "R", "", false)
	if !ok {
		t.Fatal("tombstone index did not load")
	}
	if loaded.fileSet[""] {
		t.Fatal("tombstone became a live file")
	}
	if !loaded.fileSet["a.go"] || !loaded.fileSet["b.go"] {
		t.Fatalf("live files = %#v", loaded.fileSet)
	}
	if len(loaded.files) != 3 || loaded.files[1] != "" {
		t.Fatalf("files = %#v", loaded.files)
	}
}

func searchFileSetHas(idx *localSearchIndex, path string) bool {
	if idx == nil || idx.fileSet == nil {
		return false
	}
	if idx.fileSet[path] {
		return true
	}
	for existing := range idx.fileSet {
		if searchPathEqual(existing, path) {
			return true
		}
	}
	return false
}

func searchPostingHasFile(idx *localSearchIndex, trigram, path string) bool {
	if idx == nil {
		return false
	}
	for _, id := range idx.postings[trigram] {
		if id >= 0 && id < len(idx.files) && searchPathEqual(idx.files[id], path) {
			return true
		}
	}
	return false
}

func TestPublishSearchIndexIfCurrentDoesNotClobberReplacement(t *testing.T) {
	key := "clobber-" + t.Name()
	prev := &localSearchIndex{key: key, complete: true, builtAt: time.Now(), files: []string{"a"}, fileSet: map[string]bool{"a": true}}
	newer := &localSearchIndex{key: key, complete: true, builtAt: time.Now(), files: []string{"b"}, fileSet: map[string]bool{"b": true}}
	searchIndexCache.Lock()
	searchIndexCache.byRoot[key] = newer
	searchIndexCache.Unlock()
	t.Cleanup(func() {
		searchIndexCache.Lock()
		if searchIndexCache.byRoot[key] == newer {
			delete(searchIndexCache.byRoot, key)
		}
		searchIndexCache.Unlock()
		stopSearchIndexReconciler(key)
	})
	rebuilt := &localSearchIndex{files: []string{"stale"}, fileSet: map[string]bool{"stale": true}, postings: map[string][]int{"abc": {0}}}
	if publishSearchIndexIfCurrent(key, prev, rebuilt, searchIndexCorpusKind) {
		t.Fatal("stale reconcile replaced a newer index")
	}
	searchIndexCache.Lock()
	got := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if got != newer {
		t.Fatal("newer index was overwritten")
	}
}

func TestSearchIndexReconcileUnpublishesWhenCoverageWouldTruncate(t *testing.T) {
	dir := t.TempDir()
	writeIndexedGoFile(t, dir, "a.go", "TruncateKeepA")
	writeIndexedGoFile(t, dir, "b.go", "TruncateKeepB")
	if out := ripgrepWhenIndexed(t, map[string]interface{}{
		"path":        dir,
		"pattern":     "TruncateKeepA",
		"output_mode": "files_with_matches",
	}); !strings.Contains(out, "a.go") {
		t.Fatalf("warmup missed file:\n%s", out)
	}
	original := maxIndexedSearchFiles
	maxIndexedSearchFiles = 2
	defer func() { maxIndexedSearchFiles = original }()
	added := writeIndexedGoFile(t, dir, "c.go", "TruncateAdded")
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(root)
	key := searchIndexCorpusKey(root, "", false)
	if reconcilePublishedCorpus(root, "", false) {
		t.Fatal("reconcile published an index that cannot cover the tree")
	}
	searchIndexCache.Lock()
	_, still := searchIndexCache.byRoot[key]
	searchIndexCache.Unlock()
	if still {
		t.Fatal("truncated corpus stayed published")
	}
	out := ToolRipgrep(map[string]interface{}{
		"path":        dir,
		"pattern":     "TruncateAdded",
		"output_mode": "files_with_matches",
		"stats":       true,
	})
	if !strings.Contains(out, added) || !strings.Contains(out, "mode=full_scan") {
		t.Fatalf("search should scan once coverage no longer fits:\n%s", out)
	}
}

func TestPersistedSearchIndexKeepsForeignKey(t *testing.T) {
	disk := t.TempDir()
	key := "corpus-key-a"
	foreign := storedSearchIndex{
		Version:     searchIndexDiskVersion,
		Key:         "corpus-key-b",
		Root:        "R",
		MaxFileSize: MaxSearchFileSize,
		Kind:        searchIndexCorpusKind,
		Files:       []string{"a.go"},
		Sizes:       []int64{1},
		ModUnixNano: []int64{1},
		Signatures:  []uint64{1},
		Postings:    map[string][]int{"abc": {0}},
	}
	path := searchIndexDiskPath(disk, key)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gob.NewEncoder(file).Encode(&foreign); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadSearchIndexFile(disk, key, "R", "", false); ok {
		t.Fatal("accepted a different cache key")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("foreign cache file was deleted: %v", err)
	}
}
