package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestImportDirectoryParallelLightMarkdown(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	const n = 24
	dir := filepath.Join(root, "fc_windows")
	for i := 0; i < n; i++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("Module%02d_知识库.md", i)), []byte(fmt.Sprintf(
			"# 模块 %d 知识库\n\n该模块负责处理崩溃报告与行为审计。\n\n## 要点\n\n- 支持 Windows 平台\n- 文件编号 %d\n- 关键词：防勒索 行为分析\n",
			i, i,
		)))
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge-parallel.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(testKnowledgeEmbedder{})

	var progressCount atomic.Int64
	store.SetImportProgressCallback(func(progress DirectoryImportResult) {
		progressCount.Add(1)
	})

	res, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		ProjectPath:  "D:/project-parallel",
		SaveScope:    SaveScopeProject,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024 * 1024,
		DistillMode:  DistillModeRules,
	})
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	// With progress callback, return status is "indexing" until background post-work finishes.
	if res.Status != ImportStatusCompleted && res.Status != ImportStatusIndexing {
		t.Fatalf("status=%s failed=%d items=%v", res.Status, res.FailedFiles, res.FailedItems)
	}
	if res.ImportedFiles != n {
		t.Fatalf("imported=%d want %d (total=%d skipped=%d failed=%d)", res.ImportedFiles, n, res.TotalFiles, res.SkippedFiles, res.FailedFiles)
	}
	if res.ProcessedFiles != n {
		t.Fatalf("processed=%d want %d", res.ProcessedFiles, n)
	}
	if progressCount.Load() == 0 {
		t.Fatal("expected progress callbacks")
	}
	store.WaitBackground()

	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project-parallel", Limit: 100})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != n {
		t.Fatalf("sources=%d want %d", len(sources), n)
	}
	for _, src := range sources {
		if src.Status != StatusDistilled {
			t.Fatalf("source not distilled: %#v", src)
		}
		if src.CardCount == 0 {
			t.Fatalf("expected cards for source: %#v", src)
		}
	}

	// Chinese FTS should work after parallel segmented insert.
	results, err := store.Search(ctx, SearchOptions{Query: "防勒索", ProjectPath: "D:/project-parallel", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected Chinese FTS hits for 防勒索")
	}

	// Unchanged files are skipped as duplicates (content-hash dedupe).
	resDup, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		ProjectPath:  "D:/project-parallel",
		SaveScope:    SaveScopeProject,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024 * 1024,
		DistillMode:  DistillModeRules,
	})
	if err != nil {
		t.Fatalf("duplicate re-import: %v", err)
	}
	if resDup.FailedFiles != 0 || resDup.DuplicateFiles != n {
		t.Fatalf("expected all duplicates on re-import: %#v", resDup)
	}

	// Changed content should re-import via parallel path and replace derived rows.
	for i := 0; i < n; i++ {
		mustWrite(t, filepath.Join(root, "fc_windows", fmt.Sprintf("Module%02d_知识库.md", i)), []byte(fmt.Sprintf(
			"# 模块 %d 知识库（修订）\n\n更新后的崩溃报告说明。\n\n关键词：行为审计 知识卡片\n", i,
		)))
	}
	res2, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		ProjectPath:  "D:/project-parallel",
		SaveScope:    SaveScopeProject,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024 * 1024,
		DistillMode:  DistillModeRules,
	})
	if err != nil {
		t.Fatalf("changed re-import: %v", err)
	}
	if res2.ImportedFiles != n || res2.FailedFiles != 0 {
		t.Fatalf("changed re-import unexpected: imported=%d failed=%d total=%d", res2.ImportedFiles, res2.FailedFiles, res2.TotalFiles)
	}
	sources2, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project-parallel", Limit: 100})
	if err != nil {
		t.Fatalf("ListSources after reimport: %v", err)
	}
	if len(sources2) != n {
		t.Fatalf("after reimport sources=%d want %d", len(sources2), n)
	}
	results2, err := store.Search(ctx, SearchOptions{Query: "知识卡片", ProjectPath: "D:/project-parallel", Limit: 10})
	if err != nil {
		t.Fatalf("Search after reimport: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("expected search hits after content update")
	}
}

func TestCanParallelLightImport(t *testing.T) {
	if !canParallelLightImport(SourceKindMarkdown, DirectoryImportRequest{DistillMode: DistillModeRules}) {
		t.Fatal("markdown rules_only should parallelize")
	}
	if !canParallelLightImport(SourceKindText, DirectoryImportRequest{}) {
		t.Fatal("text auto without topic should parallelize")
	}
	if canParallelLightImport(SourceKindPDF, DirectoryImportRequest{}) {
		t.Fatal("pdf should stay sequential")
	}
	if canParallelLightImport(SourceKindMarkdown, DirectoryImportRequest{DistillMode: DistillModeLLMIfAny}) {
		t.Fatal("llm_if_available should stay sequential")
	}
	if canParallelLightImport(SourceKindMarkdown, DirectoryImportRequest{TopicHint: "security"}) {
		t.Fatal("topic hint auto mode should stay sequential")
	}
	if !canParallelLightImport(SourceKindMarkdown, DirectoryImportRequest{TopicHint: "security", DistillMode: DistillModeRules}) {
		t.Fatal("topic hint with rules_only may parallelize")
	}
}
