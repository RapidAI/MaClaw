package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkImportedSourcesUsesFastPath(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "link-fast.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ids := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		src, err := store.SaveText(ctx, TextSaveRequest{
			Title:       fmt.Sprintf("Windows 防勒索 模块 %d", i),
			Text:        fmt.Sprintf("行为审计与崩溃报告说明 %d。关键词：知识库 导入 加速。", i),
			ProjectPath: "D:/link-fast",
			SaveScope:   SaveScopeProject,
			DistillMode: DistillModeRules,
		})
		if err != nil {
			t.Fatalf("SaveText %d: %v", i, err)
		}
		ids = append(ids, src.ID)
	}

	// Ensure FTS is searchable before linking.
	hits, err := store.Search(ctx, SearchOptions{Query: "防勒索", ProjectPath: "D:/link-fast", Limit: 5})
	if err != nil || len(hits) == 0 {
		t.Fatalf("precondition search failed: err=%v hits=%d", err, len(hits))
	}

	progressCalls := 0
	// Small batch: do not exclude intra-batch so related titles can link.
	store.linkImportedSources(ctx, ids, nil, func(done, total int) {
		progressCalls++
		if total != len(ids) {
			t.Fatalf("total=%d want %d", total, len(ids))
		}
	})
	if progressCalls == 0 {
		t.Fatal("expected progress callbacks")
	}

	// At least one source should have topic-related links after fast linking.
	linkedAny := false
	for _, id := range ids {
		links, err := store.ListSourceLinks(ctx, id, 20)
		if err != nil {
			t.Fatalf("ListSourceLinks: %v", err)
		}
		if len(links) > 0 {
			linkedAny = true
			break
		}
	}
	if !linkedAny {
		t.Fatal("expected at least one fast topic link among related imports")
	}
}

func TestSkipIntraBatchLinking(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "link-batch.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Seed one pre-existing neighbor outside the batch.
	seed, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Windows 安全基线 防勒索",
		Text:        "既有知识：防勒索策略与行为审计。",
		ProjectPath: "D:/link-batch",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed SaveText: %v", err)
	}

	ids := make([]string, 0, skipIntraBatchLinkMin)
	for i := 0; i < skipIntraBatchLinkMin; i++ {
		src, err := store.SaveText(ctx, TextSaveRequest{
			Title:       fmt.Sprintf("Windows 防勒索 批量模块 %d", i),
			Text:        fmt.Sprintf("批量导入模块 %d 行为审计。", i),
			ProjectPath: "D:/link-batch",
			SaveScope:   SaveScopeProject,
			DistillMode: DistillModeRules,
		})
		if err != nil {
			t.Fatalf("SaveText %d: %v", i, err)
		}
		ids = append(ids, src.ID)
	}

	exclude := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		exclude[id] = struct{}{}
	}
	store.linkImportedSources(ctx, ids, exclude, nil)

	// Batch members should not link to each other; may link to seed.
	for _, id := range ids {
		links, err := store.ListSourceLinks(ctx, id, 50)
		if err != nil {
			t.Fatalf("ListSourceLinks: %v", err)
		}
		for _, link := range links {
			other := link.RelatedSourceID
			if other == id {
				continue
			}
			if _, isBatch := exclude[other]; isBatch {
				t.Fatalf("intra-batch link not excluded: %s -> %s", id, other)
			}
		}
	}
	// Seed may receive reverse links from batch members.
	_ = seed
}

func TestScheduleImportPostWorkCompletesOnClose(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "async-post.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	store.SetEmbedder(testKnowledgeEmbedder{})

	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	mustWrite(t, path, []byte("# Async post\n\n内容用于后台 embedding 与 linking。\n"))

	res, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/async-post",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024 * 1024,
		DistillMode:  DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if res.ImportedFiles != 1 {
		t.Fatalf("imported=%d", res.ImportedFiles)
	}
	// Close waits for background post-work.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify node embeddings were written.
	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store2.Close()
	sources, err := store2.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/async-post", Limit: 5})
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources after reopen: %v %#v", err, sources)
	}
	nodes, err := store2.ListNodesBySource(ctx, sources[0].ID, 20)
	if err != nil {
		t.Fatalf("ListNodesBySource: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes")
	}
	// query missing embeddings — should be empty after background backfill.
	missing, err := store2.queryMissingNodeEmbeddings(ctx, []string{sources[0].ID})
	if err != nil {
		t.Fatalf("queryMissingNodeEmbeddings: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected embeddings backfilled in background, missing=%d", len(missing))
	}
}

func TestThrottleImportPostProgress(t *testing.T) {
	var phases []string
	var progresses []int
	cb := throttleImportPostProgress(func(phase string, progress int) {
		phases = append(phases, phase)
		progresses = append(progresses, progress)
	}, time.Hour) // huge interval — only force points should pass

	cb("linking", 0)
	cb("linking", 5)  // drop
	cb("linking", 9)  // drop
	cb("linking", 10) // force: +10
	cb("linking", 100)
	cb("embedding", 0)
	cb("embedding", 50) // force: +10 from 0... wait 50-0>=10
	cb("embedding", 100)

	if len(phases) < 5 {
		t.Fatalf("expected forced emissions, got phases=%v progresses=%v", phases, progresses)
	}
	if phases[0] != "linking" || progresses[0] != 0 {
		t.Fatalf("first = %s %d", phases[0], progresses[0])
	}
	// 5% and 9% must not appear
	for i := range progresses {
		if phases[i] == "linking" && (progresses[i] == 5 || progresses[i] == 9) {
			t.Fatalf("throttled progress leaked: %v %v", phases, progresses)
		}
	}
}

func TestOpenSecondarySQLiteStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secondary.db")
	primary, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	primary.Close()

	sec, err := openSecondarySQLiteStore(path)
	if err != nil {
		t.Fatalf("secondary: %v", err)
	}
	defer sec.Close()
	if sec.dbPath != path {
		t.Fatalf("dbPath=%q", sec.dbPath)
	}
	// Should be able to query schema created by primary.
	var n int
	if err := sec.db.QueryRow(`SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
}

func TestCancelBackgroundBeforeSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel-early.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(testKnowledgeEmbedder{})

	src, err := store.SaveText(context.Background(), TextSaveRequest{
		Title:       "early cancel",
		Text:        "cancel requested before schedule should still finish onDone",
		ProjectPath: "D:/early-cancel",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	// Request cancel before post-work is scheduled.
	store.CancelBackground()
	done := make(chan struct{})
	store.scheduleImportPostWork([]string{src.ID}, nil, func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onDone not called when cancel raced ahead of schedule")
	}
	store.WaitBackground()
}

func TestCancelBackgroundAbortsPostWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel-bg.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(testKnowledgeEmbedder{})

	// Seed many sources so linking has work to cancel.
	ids := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		src, err := store.SaveText(context.Background(), TextSaveRequest{
			Title:       fmt.Sprintf("cancel-bg-%d", i),
			Text:        fmt.Sprintf("cancel background post work content %d 防勒索", i),
			ProjectPath: "D:/cancel-bg",
			SaveScope:   SaveScopeProject,
			DistillMode: DistillModeRules,
		})
		if err != nil {
			t.Fatalf("SaveText: %v", err)
		}
		ids = append(ids, src.ID)
	}

	done := make(chan struct{})
	store.scheduleImportPostWork(ids, nil, func() { close(done) })
	// Cancel quickly — should not hang WaitBackground.
	store.CancelBackground()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onDone not called after CancelBackground")
	}
	store.WaitBackground()
}

func TestScheduleImportPostWorkCallsOnDoneOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ondone.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(testKnowledgeEmbedder{})

	src, err := store.SaveText(context.Background(), TextSaveRequest{
		Title:       "onDone once",
		Text:        "post-work should invoke onDone exactly once.",
		ProjectPath: "D:/ondone",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	var calls int
	store.scheduleImportPostWork([]string{src.ID}, nil, func() { calls++ })
	store.WaitBackground()
	if calls != 1 {
		t.Fatalf("onDone calls=%d want 1", calls)
	}
}

func TestRefreshSourceTopicLinksFastSelfSkip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "link-self.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	src, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "孤立文档 unique-xyz-only",
		Text:        "内容足够形成卡片：unique-xyz-only 无邻居。",
		ProjectPath: "D:/link-self",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	res, err := store.refreshSourceTopicLinksFast(ctx, src.ID, 6, nil)
	if err != nil {
		t.Fatalf("refreshSourceTopicLinksFast: %v", err)
	}
	// Self should never be linked.
	for _, link := range res.Links {
		if link.RelatedSourceID == src.ID || link.SourceID == link.RelatedSourceID {
			t.Fatalf("self link created: %#v", link)
		}
	}
}

// TestWaitBackgroundBeforeCloseCompletesPostWork mirrors the GUI import-job lifecycle:
// Import returns while post-work runs; the caller must WaitBackground before Close.
// Close alone cancels post-work and would leave sources unlinked.
func TestWaitBackgroundBeforeCloseCompletesPostWork(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "wait-close.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	ids := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		src, err := store.SaveText(ctx, TextSaveRequest{
			Title:       fmt.Sprintf("WaitClose 防勒索 文档 %d", i),
			Text:        fmt.Sprintf("共享主题内容 防勒索 行为审计 %d。", i),
			ProjectPath: "D:/wait-close",
			SaveScope:   SaveScopeProject,
			DistillMode: DistillModeRules,
		})
		if err != nil {
			store.Close()
			t.Fatalf("SaveText: %v", err)
		}
		ids = append(ids, src.ID)
	}

	done := make(chan struct{})
	store.scheduleImportPostWork(ids, nil, func() { close(done) })
	// GUI pattern: wait for indexing, then close (do not Close first).
	store.WaitBackground()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		store.Close()
		t.Fatal("onDone not called after WaitBackground")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and verify topic links were written (post-work completed).
	store2, err := NewSQLiteStore(filepath.Join(dir, "wait-close.db"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store2.Close()
	linked := 0
	for _, id := range ids {
		links, err := store2.ListSourceLinks(ctx, id, 20)
		if err != nil {
			t.Fatalf("ListSourceLinks: %v", err)
		}
		linked += len(links)
	}
	if linked == 0 {
		t.Fatal("expected topic links after WaitBackground; Close-first would cancel post-work")
	}
}

// TestCloseCancelsInFlightPostWork documents that Close aborts background work.
// Import jobs must WaitBackground before Close (see GUI KnowledgeStartImport*).
func TestCloseCancelsInFlightPostWork(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "close-cancels.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	ids := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		src, err := store.SaveText(ctx, TextSaveRequest{
			Title:       fmt.Sprintf("close-cancel-%d", i),
			Text:        fmt.Sprintf("content for cancel-on-close %d 防勒索", i),
			ProjectPath: "D:/close-cancel",
			SaveScope:   SaveScopeProject,
			DistillMode: DistillModeRules,
		})
		if err != nil {
			store.Close()
			t.Fatalf("SaveText: %v", err)
		}
		ids = append(ids, src.ID)
	}

	done := make(chan struct{})
	store.scheduleImportPostWork(ids, nil, func() { close(done) })
	// Immediate Close cancels; must not hang.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onDone not called after Close cancel")
	}
}
