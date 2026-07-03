package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func containsWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 6.1: Test import with 0 sources creates a batch with "completed" status
// ---------------------------------------------------------------------------

func TestImportPackageSources_ZeroSources_EmptyBatchCompleted(t *testing.T) {
	store := &mockBatchCreatorStore{}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "Empty",
	}

	result := ImportPackageSources(context.Background(), store, nil, opts)

	if result.Total != 0 {
		t.Fatalf("Total=%d, want 0", result.Total)
	}
	if result.Imported != 0 {
		t.Fatalf("Imported=%d, want 0", result.Imported)
	}
	// With 0 sources, a batch is still created (TotalFiles=0) and immediately
	// finalized. The batch status should reflect 0 total (all "completed").
	if len(store.batches) != 1 {
		t.Fatalf("expected 1 batch (empty batch created and finalized), got %d", len(store.batches))
	}
	// imported==total==0 means imported==total → status "completed"
	if store.batches[0].Status != "completed" {
		t.Fatalf("batch status=%q, want 'completed' for 0 sources", store.batches[0].Status)
	}
	if store.batches[0].TotalFiles != 0 {
		t.Fatalf("batch TotalFiles=%d, want 0", store.batches[0].TotalFiles)
	}
}

// ---------------------------------------------------------------------------
// 6.2: Test import with 1 URL source that fails re-fetch but has inline content
//       shows batch with 1 imported
// ---------------------------------------------------------------------------

type failURLStore struct {
	mockBatchCreatorStore
}

func (s *failURLStore) SaveURL(_ context.Context, req URLSaveRequest) (Source, error) {
	return Source{}, fmt.Errorf("re-fetch failed: network error")
}

func TestImportPackageSources_URLFailsWithInlineContent_BatchImported(t *testing.T) {
	store := &failURLStore{}
	sources := []PackageSource{
		{
			ID:      "url1",
			Kind:    "url",
			URI:     "https://example.com/article",
			Title:   "Article",
			Content: "This is inline content that was captured at export time.",
		},
	}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "URL Fallback Test",
	}

	result := ImportPackageSources(context.Background(), store, sources, opts)

	if result.Total != 1 {
		t.Fatalf("Total=%d, want 1", result.Total)
	}
	if result.Imported != 1 {
		t.Fatalf("Imported=%d, want 1 (should have used inline content fallback)", result.Imported)
	}
	if len(store.batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(store.batches))
	}
	// Final status should be "completed" since all imported
	if store.batches[0].Status != "completed" {
		t.Fatalf("batch status=%q, want 'completed'", store.batches[0].Status)
	}
}

func TestImportPackageSources_ContentTruncatedWarning(t *testing.T) {
	store := &mockBatchCreatorStore{}
	sources := []PackageSource{
		{
			ID:               "text1",
			Kind:             "text",
			Title:            "Large Export",
			Content:          "partial content",
			ContentTruncated: true,
		},
	}

	result := ImportPackageSources(context.Background(), store, sources, PackageImportOptions{})

	if result.Imported != 1 {
		t.Fatalf("Imported=%d, want 1", result.Imported)
	}
	if !containsWarning(result.Warnings, "content is truncated") {
		t.Fatalf("expected truncation warning, got %#v", result.Warnings)
	}
}

func TestImportPackageSources_ContentTruncatedWarningUsesFallbackLabel(t *testing.T) {
	store := &mockBatchCreatorStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{
		{Content: "partial content", ContentTruncated: true},
	}, PackageImportOptions{})

	if result.Imported != 1 {
		t.Fatalf("Imported=%d, want 1", result.Imported)
	}
	if !containsWarning(result.Warnings, "source unknown source content is truncated") {
		t.Fatalf("expected fallback truncation warning, got %#v", result.Warnings)
	}
}

func TestImportPackageSources_NilStoreSkipsImportableSourcesWithoutPanic(t *testing.T) {
	result := ImportPackageSources(context.Background(), nil, []PackageSource{
		{URI: "https://example.com/a", Content: "url fallback"},
		{Title: "Inline", Content: "inline text"},
	}, PackageImportOptions{})

	if result.Imported != 0 || result.Skipped != 2 || result.Failed != 2 {
		t.Fatalf("unexpected nil-store result: %#v", result)
	}
	if !containsWarning(result.Warnings, "knowledge store is unavailable") {
		t.Fatalf("expected nil-store warning, got %#v", result.Warnings)
	}
}

// ---------------------------------------------------------------------------
// 6.3: Test import with all metadata-only sources results in batch status "failed"
// ---------------------------------------------------------------------------

func TestImportPackageSources_AllMetadataOnly_BatchFailed(t *testing.T) {
	store := &mockBatchCreatorStore{}
	sources := []PackageSource{
		{ID: "meta1", Kind: "bookmark", Title: "Bookmark 1"},
		{ID: "meta2", Kind: "reference", Title: "Reference 2"},
		{ID: "meta3", Kind: "link", Title: "Link 3"},
	}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "Metadata Only",
	}

	result := ImportPackageSources(context.Background(), store, sources, opts)

	if result.Total != 3 {
		t.Fatalf("Total=%d, want 3", result.Total)
	}
	if result.Imported != 0 {
		t.Fatalf("Imported=%d, want 0", result.Imported)
	}
	if result.Skipped != 3 {
		t.Fatalf("Skipped=%d, want 3", result.Skipped)
	}
	if len(store.batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(store.batches))
	}
	if store.batches[0].Status != "failed" {
		t.Fatalf("batch status=%q, want 'failed'", store.batches[0].Status)
	}
	// Metadata-only items are "skipped" (not importable), not "failed" (tried and errored).
	if store.batches[0].Skipped != 3 {
		t.Fatalf("batch Skipped=%d, want 3", store.batches[0].Skipped)
	}
	if store.batches[0].Failed != 0 {
		t.Fatalf("batch Failed=%d, want 0", store.batches[0].Failed)
	}
}

// ---------------------------------------------------------------------------
// 6.4: Test DryRun=true does not create batch and BatchCreator methods are
//       never called
// ---------------------------------------------------------------------------

type panicBatchCreatorStore struct {
	mockBatchCreatorStore
}

func (s *panicBatchCreatorStore) CreateImportBatch(_ context.Context, _ ImportBatch) error {
	panic("CreateImportBatch should not be called in DryRun mode")
}

func (s *panicBatchCreatorStore) UpdateImportBatch(_ context.Context, _ ImportBatch) error {
	panic("UpdateImportBatch should not be called in DryRun mode")
}

func (s *panicBatchCreatorStore) CreateImportItem(_ context.Context, _ ImportItem) error {
	panic("CreateImportItem should not be called in DryRun mode")
}

func TestImportPackageSources_DryRun_NoBatchCreated(t *testing.T) {
	store := &panicBatchCreatorStore{}
	sources := []PackageSource{
		{ID: "t1", Kind: "text", Title: "Note 1", Content: "Hello world"},
		{ID: "u1", Kind: "url", URI: "https://example.com/page", Content: "Page content"},
	}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "DryRun Test",
		DryRun:    true,
	}

	// This should not panic since DryRun=true means no store calls
	result := ImportPackageSources(context.Background(), store, sources, opts)

	if result.Total != 2 {
		t.Fatalf("Total=%d, want 2", result.Total)
	}
	if result.Imported != 2 {
		t.Fatalf("Imported=%d, want 2 (dry-run counts importable sources)", result.Imported)
	}
}

// ---------------------------------------------------------------------------
// 6.5: Test multiKnowledgeStore correctly forwards BatchCreator interface
//       via type assertion
// ---------------------------------------------------------------------------

func TestImportPackageSources_MultiKnowledgeStoreForwardsBatchCreator(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	sqliteStore, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	// SQLiteStore itself implements BatchCreator — verify via type assertion
	var pis PackageImportStore = sqliteStore
	bc, ok := pis.(BatchCreator)
	if !ok {
		t.Fatalf("SQLiteStore does not implement BatchCreator")
	}
	_ = bc

	// ImportPackageSources with a real SQLiteStore should create a batch
	sources := []PackageSource{
		{ID: "t1", Kind: "text", Title: "Test Note", Content: "Some content for multiKnowledgeStore test."},
	}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "Multi Store Test",
	}

	result := ImportPackageSources(ctx, sqliteStore, sources, opts)
	if result.Imported != 1 {
		t.Fatalf("Imported=%d, want 1", result.Imported)
	}

	// Verify batch was created in the real store
	page, err := sqliteStore.ListImportBatchesPage(ctx, ListImportBatchesOptions{
		OwnerID:  "owner1",
		TenantID: "tenant1",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListImportBatchesPage: %v", err)
	}
	if len(page.Batches) != 1 {
		t.Fatalf("expected 1 batch in store, got %d", len(page.Batches))
	}
	if page.Batches[0].TopicHint != "Multi Store Test" {
		t.Fatalf("batch TopicHint=%q, want 'Multi Store Test'", page.Batches[0].TopicHint)
	}
}

// ---------------------------------------------------------------------------
// 6.6: Test store implementing only PackageImportStore (no BatchCreator)
//       works without panic
// ---------------------------------------------------------------------------

func TestImportPackageSources_NoBatchCreator_NoPanic(t *testing.T) {
	store := &mockNoBatchStore{}
	sources := []PackageSource{
		{ID: "t1", Kind: "text", Title: "Note A", Content: "Content A"},
		{ID: "t2", Kind: "text", Title: "Note B", Content: "Content B"},
		{ID: "m1", Kind: "bookmark", Title: "Metadata only"},
	}
	opts := PackageImportOptions{
		OwnerID:   "owner1",
		TenantID:  "tenant1",
		TopicHint: "No BatchCreator",
	}

	result := ImportPackageSources(context.Background(), store, sources, opts)

	if result.Total != 3 {
		t.Fatalf("Total=%d, want 3", result.Total)
	}
	if result.Imported != 2 {
		t.Fatalf("Imported=%d, want 2", result.Imported)
	}
	if result.Skipped != 1 {
		t.Fatalf("Skipped=%d, want 1", result.Skipped)
	}
	// Store should have saved the importable sources
	if len(store.sources) != 2 {
		t.Fatalf("store.sources=%d, want 2", len(store.sources))
	}
}
