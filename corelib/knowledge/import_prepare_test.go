package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSingleFileImportLargeMarkdownFastPath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// One file, many sections — exercises intra-document parallel prep.
	var b strings.Builder
	b.WriteString("# 单文件知识库\n\n总览：崩溃报告与行为审计模块说明。\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "## 章节 %d 防勒索\n\n该章节描述 Windows 平台行为分析要点 %d。关键词：知识卡片 导入加速。\n\n", i, i)
	}
	path := filepath.Join(root, "single_large_知识库.md")
	mustWrite(t, path, []byte(b.String()))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "single-file.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(testKnowledgeEmbedder{})

	start := time.Now()
	res, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/project-single",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 8 * 1024 * 1024,
		DistillMode:  DistillModeRules,
	}, []string{path})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if res.ImportedFiles != 1 || res.FailedFiles != 0 {
		t.Fatalf("unexpected import result: %#v", res)
	}
	t.Logf("single large md import took %s", elapsed)

	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project-single", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Status != StatusDistilled {
		t.Fatalf("unexpected sources: %#v", sources)
	}
	if sources[0].CardCount < 10 {
		t.Fatalf("expected many cards from multi-section file, got %d", sources[0].CardCount)
	}
	hits, err := store.Search(ctx, SearchOptions{Query: "防勒索", ProjectPath: "D:/project-single", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected Chinese FTS hits after single-file import")
	}
}

func TestMarkExistingContentDuplicatesTargeted(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "targeted-dup.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	root := t.TempDir()
	path := filepath.Join(root, "dup.md")
	mustWrite(t, path, []byte("unique seeded body for targeted duplicate detection"))
	// Import once so content_hash exists in DB.
	imp, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/targeted-dup",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
		DistillMode:  DistillModeRules,
	}, []string{path})
	if err != nil || imp.ImportedFiles != 1 {
		t.Fatalf("seed import: err=%v result=%#v", err, imp)
	}
	store.WaitBackground()

	// Same content, new path + a brand-new file.
	pathDup := filepath.Join(root, "dup-copy.md")
	mustWrite(t, pathDup, []byte("unique seeded body for targeted duplicate detection"))
	path2 := filepath.Join(root, "new.md")
	mustWrite(t, path2, []byte("brand new content not in db"))

	res, err := store.ScanFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/targeted-dup",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{pathDup, path2})
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if res.QueuedFiles != 1 {
		t.Fatalf("queued=%d want 1 (one dup, one new)", res.QueuedFiles)
	}
	if res.DuplicateFiles != 1 {
		t.Fatalf("duplicates=%d want 1", res.DuplicateFiles)
	}
	// EstimatedBytes should only include still-queued files after DB dedupe.
	if res.EstimatedBytes <= 0 {
		t.Fatalf("estimated_bytes=%d want >0 for remaining queued file", res.EstimatedBytes)
	}
	// Queued file is "new.md"; dup-copy should not inflate estimate beyond one file size.
	var queuedSize int64
	for _, it := range res.Items {
		if it.Status == ItemStatusQueued {
			queuedSize += it.FileSize
		}
	}
	if res.EstimatedBytes != queuedSize {
		t.Fatalf("estimated_bytes=%d want queued sizes sum %d", res.EstimatedBytes, queuedSize)
	}
}

func TestApplyExistingHashSkipsAdjustsEstimatedBytes(t *testing.T) {
	result := DirectoryImportResult{
		QueuedFiles:    2,
		EstimatedBytes: 300,
	}
	items := []ImportItem{
		{Status: ItemStatusQueued, FileHash: "aaa", FileSize: 100, FilePath: "a.md", RelativePath: "a.md"},
		{Status: ItemStatusQueued, FileHash: "bbb", FileSize: 200, FilePath: "b.md", RelativePath: "b.md"},
		{Status: ItemStatusSkippedDuplicate, FileHash: "aaa", FileSize: 100, FilePath: "a-copy.md", RelativePath: "a-copy.md"},
	}
	result, items = applyExistingHashSkips(result, items, map[string]string{"aaa": "src-aaa"}, nil, nil)
	if result.QueuedFiles != 1 || result.DuplicateFiles != 1 || result.SkippedFiles != 1 {
		t.Fatalf("counters: %#v", result)
	}
	if result.EstimatedBytes != 200 {
		t.Fatalf("estimated_bytes=%d want 200", result.EstimatedBytes)
	}
	if items[0].Status != ItemStatusSkippedDuplicate || items[0].SourceID != "src-aaa" || items[1].Status != ItemStatusQueued {
		t.Fatalf("items statuses: %s %s source %s", items[0].Status, items[1].Status, items[0].SourceID)
	}
	if items[2].SourceID != "src-aaa" {
		t.Fatalf("within-batch duplicate source = %q", items[2].SourceID)
	}
}

func TestApplyExistingHashSkipsUsesLocalUpdate(t *testing.T) {
	result := DirectoryImportResult{QueuedFiles: 1, DuplicateFiles: 1, SkippedFiles: 1, EstimatedBytes: 100}
	items := []ImportItem{
		{Status: ItemStatusQueued, FileHash: "aaa", FileSize: 100, FilePath: "note.md"},
		{Status: ItemStatusSkippedDuplicate, FileHash: "aaa", FileSize: 100, FilePath: "copy.md"},
	}
	blocked := map[string]struct{}{"note.md": {}}
	local := map[string]string{"aaa": "local-src"}
	result, items = applyExistingHashSkips(result, items, map[string]string{"aaa": "other-src"}, blocked, local)
	if items[0].Status != ItemStatusQueued || items[0].SourceID != "" {
		t.Fatalf("blocked item changed: %#v", items[0])
	}
	if items[1].Status != ItemStatusSkippedDuplicate || items[1].SourceID != "local-src" {
		t.Fatalf("copy = %#v", items[1])
	}
	if result.QueuedFiles != 1 || result.DuplicateFiles != 1 || result.SkippedFiles != 1 || result.EstimatedBytes != 100 {
		t.Fatalf("counters %#v", result)
	}
}

func TestPreferContentHashSourceKeepsProjectMembership(t *testing.T) {
	const owner = "desktop-user"
	// A membership in this project beats a newer row stored in another project.
	if !preferContentHashSource(owner, owner, "t1", "D:/Alpha", true, owner, "t9", "D:/Gamma", false, "D:/Beta") {
		t.Fatal("membered source lost to a newer other project")
	}
	if preferContentHashSource(owner, owner, "t9", "D:/Gamma", false, owner, "t1", "D:/Alpha", true, "D:/Beta") {
		t.Fatal("newer other project replaced the membered source")
	}
	// The row stored in this project still beats a newer membership-only row.
	if !preferContentHashSource(owner, owner, "t1", "D:/Beta", false, owner, "t9", "D:/Alpha", true, "D:/Beta") {
		t.Fatal("native project lost to a newer membership")
	}
	if preferContentHashSource(owner, owner, "t9", "D:/Alpha", true, owner, "t1", "D:/Beta", false, "D:/Beta") {
		t.Fatal("newer membership replaced the native project")
	}
}

func TestHashesToLinkSkipsLocalUpdate(t *testing.T) {
	items := []ImportItem{
		{Status: ItemStatusQueued, FileHash: "aaa", FilePath: "copy.md"},
		{Status: ItemStatusSkippedDuplicate, FileHash: "aaa", FilePath: "note.md"},
		{Status: ItemStatusQueued, FileHash: "bbb", FilePath: "other.md"},
	}
	link := hashesToLink(items, map[string]string{"aaa": "other-project", "bbb": "beta"}, map[string]struct{}{"note.md": {}}, map[string]string{"aaa": "local"})
	if _, ok := link["aaa"]; ok {
		t.Fatalf("linked a hash this batch will write locally: %#v", link)
	}
	if link["bbb"] != "beta" {
		t.Fatalf("link = %#v", link)
	}
}

func TestLookupContentHashesSmallBatch(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "hash-lookup.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	src, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "seed",
		Text:        "seed content for hash lookup " + strings.Repeat("x", 32),
		ProjectPath: "D:/hash-proj",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	if src.ContentHash == "" {
		t.Fatal("expected content hash on saved source")
	}

	found, err := store.lookupContentHashes(ctx, DirectoryImportRequest{
		ProjectPath: "D:/hash-proj",
		SaveScope:   SaveScopeProject,
	}, []string{src.ContentHash, "deadbeef", src.ContentHash})
	if err != nil {
		t.Fatalf("lookupContentHashes: %v", err)
	}
	if _, ok := found[src.ContentHash]; !ok {
		t.Fatalf("expected existing hash, got %#v", found)
	}
	if len(found) != 1 {
		t.Fatalf("expected only known hash, got %#v", found)
	}
}

func TestPrepareNodesForInsertAssignsIDs(t *testing.T) {
	nodes := []DocumentNode{
		{Title: "a", Text: "中文内容一"},
		{Title: "b", Text: "中文内容二"},
		{Title: "c", Text: "中文内容三"},
		{Title: "d", Text: "中文内容四"},
	}
	prepared := prepareNodesForInsert(nodes)
	if len(prepared) != 4 {
		t.Fatalf("prepared=%d", len(prepared))
	}
	for i, pn := range prepared {
		if pn.node.ID == "" || nodes[i].ID == "" {
			t.Fatalf("missing id at %d", i)
		}
		if pn.ftsText == "" {
			t.Fatalf("expected FTS text at %d", i)
		}
	}
}
