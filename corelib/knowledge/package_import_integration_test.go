package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// 7.1: Test full flow: ImportPackageSources with real SQLiteStore then verify
//      batch appears in ListImportBatchesPage
// ---------------------------------------------------------------------------

func TestIntegration_ImportPackageSources_BatchAppearsInList(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	sources := []PackageSource{
		{ID: "s1", Kind: "text", Title: "Architecture Guide", Content: "Microservices architecture with event sourcing and CQRS."},
		{ID: "s2", Kind: "url", URI: "https://docs.example.com/api", Title: "API Docs", Content: "REST API documentation for the service."},
		{ID: "s3", Kind: "text", Title: "Setup Notes", Content: "Development environment setup instructions."},
	}
	opts := PackageImportOptions{
		OwnerID:   "user-integration",
		TenantID:  "tenant-integration",
		TopicHint: "Project Knowledge Share",
	}

	result := ImportPackageSources(ctx, store, sources, opts)

	if result.Imported != 3 {
		t.Fatalf("Imported=%d, want 3", result.Imported)
	}
	if result.Total != 3 {
		t.Fatalf("Total=%d, want 3", result.Total)
	}

	// Verify batch appears in list
	page, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
		OwnerID:  "user-integration",
		TenantID: "tenant-integration",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListImportBatchesPage: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 batch in page, got Total=%d", page.Total)
	}
	if len(page.Batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(page.Batches))
	}

	batch := page.Batches[0]
	if batch.TopicHint != "Project Knowledge Share" {
		t.Fatalf("batch TopicHint=%q, want 'Project Knowledge Share'", batch.TopicHint)
	}
	if batch.Status != "completed" {
		t.Fatalf("batch Status=%q, want 'completed'", batch.Status)
	}
	if batch.TotalFiles != 3 {
		t.Fatalf("batch TotalFiles=%d, want 3", batch.TotalFiles)
	}
	if batch.Imported != 3 {
		t.Fatalf("batch Imported=%d, want 3", batch.Imported)
	}
	if batch.OwnerID != "user-integration" {
		t.Fatalf("batch OwnerID=%q, want 'user-integration'", batch.OwnerID)
	}

	// Verify items exist
	items, err := store.ListImportItems(ctx, batch.ID, 10)
	if err != nil {
		t.Fatalf("ListImportItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 import items, got %d", len(items))
	}
	for _, item := range items {
		if item.Status != "imported" {
			t.Fatalf("item %s status=%q, want 'imported'", item.ID, item.Status)
		}
		if item.BatchID != batch.ID {
			t.Fatalf("item %s batch_id=%q, want %q", item.ID, item.BatchID, batch.ID)
		}
	}

	// Verify sources are linked
	srcs, err := store.ListSources(ctx, ListSourcesOptions{
		OwnerID:  "user-integration",
		TenantID: "tenant-integration",
		BatchID:  batch.ID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListSources by batch: %v", err)
	}
	if len(srcs) != 3 {
		t.Fatalf("expected 3 sources linked to batch, got %d", len(srcs))
	}
	for _, src := range srcs {
		if src.BatchID != batch.ID {
			t.Fatalf("source %s batch_id=%q, want %q", src.ID, src.BatchID, batch.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// 7.2: Test full flow: share import then DeleteImportBatch removes batch,
//      items, and linked sources
// ---------------------------------------------------------------------------

func TestIntegration_ImportThenDelete_RemovesAll(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	sources := []PackageSource{
		{ID: "d1", Kind: "text", Title: "Delete Me 1", Content: "Content to be deleted in cascade test."},
		{ID: "d2", Kind: "text", Title: "Delete Me 2", Content: "More content for deletion."},
	}
	opts := PackageImportOptions{
		OwnerID:   "user-del",
		TenantID:  "tenant-del",
		TopicHint: "Will Be Deleted",
	}

	result := ImportPackageSources(ctx, store, sources, opts)
	if result.Imported != 2 {
		t.Fatalf("Imported=%d, want 2", result.Imported)
	}

	// Get batch ID
	page, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
		OwnerID:  "user-del",
		TenantID: "tenant-del",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListImportBatchesPage: %v", err)
	}
	if len(page.Batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(page.Batches))
	}
	batchID := page.Batches[0].ID

	// Verify sources exist before delete
	srcsBefore, err := store.ListSources(ctx, ListSourcesOptions{
		OwnerID:  "user-del",
		TenantID: "tenant-del",
		BatchID:  batchID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListSources before delete: %v", err)
	}
	if len(srcsBefore) != 2 {
		t.Fatalf("expected 2 sources before delete, got %d", len(srcsBefore))
	}

	// Delete batch
	deleteResult, err := store.DeleteImportBatch(ctx, ImportBatchDeleteRequest{
		BatchID:  batchID,
		OwnerID:  "user-del",
		TenantID: "tenant-del",
	})
	if err != nil {
		t.Fatalf("DeleteImportBatch: %v", err)
	}
	if deleteResult.DeletedBatches != 1 {
		t.Fatalf("DeletedBatches=%d, want 1", deleteResult.DeletedBatches)
	}
	if deleteResult.DeletedSources != 2 {
		t.Fatalf("DeletedSources=%d, want 2", deleteResult.DeletedSources)
	}

	// Verify batch is gone
	pageAfter, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
		OwnerID:  "user-del",
		TenantID: "tenant-del",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListImportBatchesPage after: %v", err)
	}
	if len(pageAfter.Batches) != 0 {
		t.Fatalf("expected 0 batches after delete, got %d", len(pageAfter.Batches))
	}

	// Verify items are gone
	itemsAfter, err := store.ListImportItems(ctx, batchID, 10)
	if err != nil {
		t.Fatalf("ListImportItems after: %v", err)
	}
	if len(itemsAfter) != 0 {
		t.Fatalf("expected 0 items after delete, got %d", len(itemsAfter))
	}

	// Verify sources are gone
	srcsAfter, err := store.ListSources(ctx, ListSourcesOptions{
		OwnerID:  "user-del",
		TenantID: "tenant-del",
		BatchID:  batchID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListSources after: %v", err)
	}
	if len(srcsAfter) != 0 {
		t.Fatalf("expected 0 sources after delete, got %d", len(srcsAfter))
	}
}

// ---------------------------------------------------------------------------
// 7.3: Test regression: existing directory import via ImportDirectory
//      continues unchanged
// ---------------------------------------------------------------------------

func TestIntegration_DirectoryImport_Unchanged(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "readme.md"), []byte("# README\n\nProject documentation for regression test."))
	mustWrite(t, filepath.Join(root, "notes.txt"), []byte("Development notes for the project."))

	res, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		OwnerID:      "user-dir",
		TenantID:     "tenant-dir",
		ProjectPath:  "D:/regression-test",
		Recursive:    true,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if res.Status != ImportStatusCompleted {
		t.Fatalf("directory import status=%q, want 'completed'", res.Status)
	}
	if res.ImportedFiles != 2 {
		t.Fatalf("ImportedFiles=%d, want 2", res.ImportedFiles)
	}
	if res.BatchID == "" {
		t.Fatalf("expected non-empty BatchID from directory import")
	}

	// Verify batch and items exist via the same list page
	page, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
		OwnerID:  "user-dir",
		TenantID: "tenant-dir",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListImportBatchesPage: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("expected 1 directory batch, got %d", page.Total)
	}
	dirBatch := page.Batches[0]
	if dirBatch.ID != res.BatchID {
		t.Fatalf("batch ID mismatch: %q vs %q", dirBatch.ID, res.BatchID)
	}
	if dirBatch.RootPath != root {
		t.Fatalf("batch RootPath=%q, want %q", dirBatch.RootPath, root)
	}
	if dirBatch.Imported != 2 {
		t.Fatalf("batch Imported=%d, want 2", dirBatch.Imported)
	}

	// Search should work
	results, err := store.Search(ctx, SearchOptions{Query: "regression", ProjectPath: "D:/regression-test", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results from directory import")
	}
}
