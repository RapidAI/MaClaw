package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Mock store implementing both PackageImportStore and BatchCreator for PBT.
// ---------------------------------------------------------------------------

type mockBatchCreatorStore struct {
	batches []ImportBatch
	items   []ImportItem
	sources []Source
}

func (m *mockBatchCreatorStore) SaveText(_ context.Context, req TextSaveRequest) (Source, error) {
	src := Source{
		ID:      NewID("ksrc"),
		Kind:    SourceKindText,
		Title:   req.Title,
		BatchID: req.BatchID,
		Status:  StatusParsed,
	}
	m.sources = append(m.sources, src)
	return src, nil
}

func (m *mockBatchCreatorStore) SaveURL(_ context.Context, req URLSaveRequest) (Source, error) {
	src := Source{
		ID:      NewID("ksrc"),
		Kind:    SourceKindURL,
		URI:     req.URL,
		BatchID: req.BatchID,
		Status:  StatusParsed,
	}
	m.sources = append(m.sources, src)
	return src, nil
}

func (m *mockBatchCreatorStore) CreateImportBatch(_ context.Context, batch ImportBatch) error {
	m.batches = append(m.batches, batch)
	return nil
}

func (m *mockBatchCreatorStore) UpdateImportBatch(_ context.Context, batch ImportBatch) error {
	for i, b := range m.batches {
		if b.ID == batch.ID {
			m.batches[i].Status = batch.Status
			m.batches[i].Imported = batch.Imported
			m.batches[i].Skipped = batch.Skipped
			m.batches[i].Failed = batch.Failed
			m.batches[i].TotalFiles = batch.TotalFiles
			m.batches[i].UpdatedAt = batch.UpdatedAt
			return nil
		}
	}
	return fmt.Errorf("batch not found: %s", batch.ID)
}

func (m *mockBatchCreatorStore) CreateImportItem(_ context.Context, item ImportItem) error {
	m.items = append(m.items, item)
	return nil
}

// mockNoBatchStore only implements PackageImportStore (not BatchCreator).
type mockNoBatchStore struct {
	sources []Source
}

func (m *mockNoBatchStore) SaveText(_ context.Context, req TextSaveRequest) (Source, error) {
	src := Source{
		ID:      NewID("ksrc"),
		Kind:    SourceKindText,
		Title:   req.Title,
		BatchID: req.BatchID,
		Status:  StatusParsed,
	}
	m.sources = append(m.sources, src)
	return src, nil
}

func (m *mockNoBatchStore) SaveURL(_ context.Context, req URLSaveRequest) (Source, error) {
	src := Source{
		ID:      NewID("ksrc"),
		Kind:    SourceKindURL,
		URI:     req.URL,
		BatchID: req.BatchID,
		Status:  StatusParsed,
	}
	m.sources = append(m.sources, src)
	return src, nil
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

func genPackageSourceKind() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"url", "text", "metadata"})
}

func genPackageSource() *rapid.Generator[PackageSource] {
	return rapid.Custom(func(t *rapid.T) PackageSource {
		kind := genPackageSourceKind().Draw(t, "kind")
		ps := PackageSource{
			ID:    rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "id"),
			Kind:  kind,
			Title: "Source " + rapid.StringMatching(`[A-Za-z]{3,10}`).Draw(t, "title"),
		}
		switch kind {
		case "url":
			ps.URI = "https://example.com/" + rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "path")
			ps.Content = "Inline content for " + ps.Title
		case "text":
			ps.Content = "Text content for " + ps.Title + " " + rapid.StringMatching(`[a-z ]{10,50}`).Draw(t, "body")
		case "metadata":
			// No URI, no content — metadata-only
		}
		return ps
	})
}

func genPackageSources(minCount, maxCount int) *rapid.Generator[[]PackageSource] {
	return rapid.Custom(func(t *rapid.T) []PackageSource {
		n := rapid.IntRange(minCount, maxCount).Draw(t, "sourceCount")
		sources := make([]PackageSource, n)
		for i := range sources {
			sources[i] = genPackageSource().Draw(t, fmt.Sprintf("source_%d", i))
		}
		return sources
	})
}

// ---------------------------------------------------------------------------
// Property 1: Batch Creation
// Feature: knowledge-share-import-display, Property 1: Batch Creation
//
// For any valid PackageSource set with BatchCreator store,
// ImportPackageSources creates exactly one ImportBatch with correct TopicHint
// and TotalFiles.
//
// **Validates: Requirements 1.1, 2.1**
// ---------------------------------------------------------------------------

func TestProperty1_BatchCreation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sources := genPackageSources(1, 20).Draw(t, "sources")
		topicHint := rapid.StringMatching(`[A-Za-z ]{3,20}`).Draw(t, "topicHint")

		store := &mockBatchCreatorStore{}
		opts := PackageImportOptions{
			OwnerID:   "owner1",
			TenantID:  "tenant1",
			TopicHint: topicHint,
		}

		ImportPackageSources(context.Background(), store, sources, opts)

		// Exactly one batch must be created.
		if len(store.batches) != 1 {
			t.Fatalf("expected exactly 1 batch, got %d", len(store.batches))
		}

		batch := store.batches[0]
		if batch.TopicHint != topicHint {
			t.Fatalf("batch TopicHint=%q, want %q", batch.TopicHint, topicHint)
		}
		if batch.TotalFiles != len(sources) {
			t.Fatalf("batch TotalFiles=%d, want %d", batch.TotalFiles, len(sources))
		}
	})
}

// ---------------------------------------------------------------------------
// Property 2: Status Derivation
// Feature: knowledge-share-import-display, Property 2: Status Derivation
//
// For any counts tuple where imported+skipped+failed==total,
// deriveImportBatchStatus returns correct status and invariant holds.
//
// **Validates: Requirements 1.2, 1.3, 1.4, 2.2**
// ---------------------------------------------------------------------------

func TestProperty2_StatusDerivation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 100).Draw(t, "total")
		imported := rapid.IntRange(0, total).Draw(t, "imported")
		remaining := total - imported
		skipped := rapid.IntRange(0, remaining).Draw(t, "skipped")
		failed := remaining - skipped

		// Invariant: imported + skipped + failed == total
		if imported+skipped+failed != total {
			t.Fatalf("invariant broken: %d+%d+%d != %d", imported, skipped, failed, total)
		}

		status := deriveImportBatchStatus(imported, skipped, failed, total)

		switch {
		case imported == 0 && total > 0:
			if status != "failed" {
				t.Fatalf("expected 'failed' when imported=0 total=%d, got %q", total, status)
			}
		case imported == total:
			if status != "completed" {
				t.Fatalf("expected 'completed' when imported==total=%d, got %q", total, status)
			}
		default:
			if status != "partial" {
				t.Fatalf("expected 'partial' for imported=%d total=%d, got %q", imported, total, status)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 3: Source-to-Batch Linking
// Feature: knowledge-share-import-display, Property 3: Source-to-Batch Linking
//
// For any successfully imported source with BatchCreator store, source BatchID
// equals batch ID and ImportItem with status=imported exists.
//
// **Validates: Requirements 1.5, 2.3**
// ---------------------------------------------------------------------------

func TestProperty3_SourceToBatchLinking(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate sources that will be importable (url or text, not metadata-only)
		n := rapid.IntRange(1, 10).Draw(t, "sourceCount")
		sources := make([]PackageSource, n)
		for i := range sources {
			kind := rapid.SampledFrom([]string{"url", "text"}).Draw(t, fmt.Sprintf("kind_%d", i))
			sources[i] = PackageSource{
				ID:    fmt.Sprintf("src_%d", i),
				Kind:  kind,
				Title: fmt.Sprintf("Title %d", i),
			}
			if kind == "url" {
				sources[i].URI = fmt.Sprintf("https://example.com/%d", i)
				sources[i].Content = fmt.Sprintf("Content %d", i)
			} else {
				sources[i].Content = fmt.Sprintf("Text content %d", i)
			}
		}

		store := &mockBatchCreatorStore{}
		opts := PackageImportOptions{
			OwnerID:   "owner1",
			TenantID:  "tenant1",
			TopicHint: "Test Topic",
		}

		result := ImportPackageSources(context.Background(), store, sources, opts)

		if len(store.batches) != 1 {
			t.Fatalf("expected 1 batch, got %d", len(store.batches))
		}
		batchID := store.batches[0].ID

		// All sources should have BatchID set
		for _, src := range store.sources {
			if src.BatchID != batchID {
				t.Fatalf("source %s has BatchID=%q, want %q", src.ID, src.BatchID, batchID)
			}
		}

		// Number of "imported" items should equal result.Imported
		importedItems := 0
		for _, item := range store.items {
			if item.BatchID != batchID {
				t.Fatalf("item %s has BatchID=%q, want %q", item.ID, item.BatchID, batchID)
			}
			if item.Status == "imported" {
				importedItems++
			}
		}
		if importedItems != result.Imported {
			t.Fatalf("imported items=%d, result.Imported=%d", importedItems, result.Imported)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 4: Graceful Degradation
// Feature: knowledge-share-import-display, Property 4: Graceful Degradation
//
// For any store implementing only PackageImportStore (no BatchCreator),
// ImportPackageSources imports correctly without panic and returns accurate counts.
//
// **Validates: Requirements 5.1, 5.3, 6.3**
// ---------------------------------------------------------------------------

func TestProperty4_GracefulDegradation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sources := genPackageSources(0, 15).Draw(t, "sources")

		store := &mockNoBatchStore{}
		opts := PackageImportOptions{
			OwnerID:  "owner1",
			TenantID: "tenant1",
		}

		// Must not panic
		result := ImportPackageSources(context.Background(), store, sources, opts)

		if result.Total != len(sources) {
			t.Fatalf("result.Total=%d, want %d", result.Total, len(sources))
		}

		// Count expected importable sources
		expectedImported := 0
		expectedSkipped := 0
		for _, s := range sources {
			uri := firstNonEmpty(s.CanonicalURI, s.URI)
			hasHTTP := len(uri) > 0 && (len(uri) >= 7 && uri[:7] == "http://" || len(uri) >= 8 && uri[:8] == "https://")
			hasContent := len(s.Content) > 0
			switch {
			case hasHTTP:
				expectedImported++
			case hasContent:
				expectedImported++
			default:
				expectedSkipped++
			}
		}

		if result.Imported != expectedImported {
			t.Fatalf("result.Imported=%d, expected %d", result.Imported, expectedImported)
		}
		if result.Skipped != expectedSkipped {
			t.Fatalf("result.Skipped=%d, expected %d", result.Skipped, expectedSkipped)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 5: Cascade Delete
// Feature: knowledge-share-import-display, Property 5: Cascade Delete
//
// For any ImportBatch created by share/package import, DeleteImportBatch
// removes batch record, all linked ImportItems, and all linked Sources.
//
// **Validates: Requirements 4.1, 4.2**
// ---------------------------------------------------------------------------

func TestProperty5_CascadeDelete(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		sources := genPackageSources(1, 10).Draw(rt, "sources")
		// Only use importable sources for this test (url or text)
		importable := make([]PackageSource, 0)
		for _, s := range sources {
			if s.Content != "" || (len(s.URI) >= 8 && s.URI[:8] == "https://") {
				importable = append(importable, s)
			}
		}
		if len(importable) == 0 {
			importable = []PackageSource{{
				Kind:    "text",
				Title:   "Fallback",
				Content: "Some text content",
			}}
		}

		ctx := context.Background()
		dbName := fmt.Sprintf("knowledge_%d.db", time.Now().UnixNano())
		store, err := NewSQLiteStore(filepath.Join(dir, dbName))
		if err != nil {
			rt.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		opts := PackageImportOptions{
			OwnerID:   "owner1",
			TenantID:  "tenant1",
			TopicHint: "Delete Test",
		}

		ImportPackageSources(ctx, store, importable, opts)

		// Verify batch was created
		page, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
			OwnerID:  "owner1",
			TenantID: "tenant1",
			Limit:    10,
		})
		if err != nil {
			rt.Fatalf("ListImportBatchesPage: %v", err)
		}
		if len(page.Batches) != 1 {
			rt.Fatalf("expected 1 batch, got %d", len(page.Batches))
		}
		batchID := page.Batches[0].ID

		// Delete the batch
		result, err := store.DeleteImportBatch(ctx, ImportBatchDeleteRequest{
			BatchID:  batchID,
			OwnerID:  "owner1",
			TenantID: "tenant1",
		})
		if err != nil {
			rt.Fatalf("DeleteImportBatch: %v", err)
		}
		if result.DeletedBatches != 1 {
			rt.Fatalf("DeletedBatches=%d, want 1", result.DeletedBatches)
		}

		// Verify batch is gone
		pageAfter, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
			OwnerID:  "owner1",
			TenantID: "tenant1",
			Limit:    10,
		})
		if err != nil {
			rt.Fatalf("ListImportBatchesPage after delete: %v", err)
		}
		if len(pageAfter.Batches) != 0 {
			rt.Fatalf("expected 0 batches after delete, got %d", len(pageAfter.Batches))
		}

		// Verify items are gone
		items, err := store.ListImportItems(ctx, batchID, 100)
		if err != nil {
			rt.Fatalf("ListImportItems: %v", err)
		}
		if len(items) != 0 {
			rt.Fatalf("expected 0 items after delete, got %d", len(items))
		}

		// Verify sources are gone
		srcs, err := store.ListSources(ctx, ListSourcesOptions{
			OwnerID:  "owner1",
			TenantID: "tenant1",
			BatchID:  batchID,
			Limit:    100,
		})
		if err != nil {
			rt.Fatalf("ListSources: %v", err)
		}
		if len(srcs) != 0 {
			rt.Fatalf("expected 0 sources after delete, got %d", len(srcs))
		}
	})
}

// ---------------------------------------------------------------------------
// Property 6: Batch Visibility
// Feature: knowledge-share-import-display, Property 6: Batch Visibility
//
// For any mix of ImportBatch records from different origins,
// ListImportBatchesPage returns all sorted by updated_at DESC.
//
// **Validates: Requirements 3.1, 3.2**
// ---------------------------------------------------------------------------

func TestProperty6_BatchVisibility(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		nBatches := rapid.IntRange(2, 8).Draw(rt, "nBatches")

		ctx := context.Background()
		dbName := fmt.Sprintf("knowledge_%d.db", time.Now().UnixNano())
		store, err := NewSQLiteStore(filepath.Join(dir, dbName))
		if err != nil {
			rt.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		baseTime := time.Now().UTC().Add(-time.Hour)
		origins := []string{"share://share1", "package://pkg1", "/home/user/docs", "share://share2"}

		for i := 0; i < nBatches; i++ {
			origin := origins[rapid.IntRange(0, len(origins)-1).Draw(rt, fmt.Sprintf("origin_%d", i))]
			// Use unique timestamps by adding i*10 seconds to avoid collisions
			batchTime := baseTime.Add(time.Duration(i*10) * time.Second)
			batch := ImportBatch{
				ID:         NewID("kbatch"),
				RootPath:   origin,
				OwnerID:    "owner1",
				TenantID:   "tenant1",
				TopicHint:  fmt.Sprintf("Batch %d", i),
				Status:     "completed",
				TotalFiles: rapid.IntRange(1, 10).Draw(rt, fmt.Sprintf("total_%d", i)),
				Imported:   1,
				CreatedAt:  batchTime,
				UpdatedAt:  batchTime,
			}
			if err := store.CreateImportBatch(ctx, batch); err != nil {
				rt.Fatalf("CreateImportBatch %d: %v", i, err)
			}
		}

		page, err := store.ListImportBatchesPage(ctx, ListImportBatchesOptions{
			OwnerID:  "owner1",
			TenantID: "tenant1",
			Limit:    100,
		})
		if err != nil {
			rt.Fatalf("ListImportBatchesPage: %v", err)
		}

		if len(page.Batches) != nBatches {
			rt.Fatalf("expected %d batches, got %d", nBatches, len(page.Batches))
		}

		// Verify sorted by updated_at DESC (non-ascending order: each batch's
		// updated_at must be >= the next batch's updated_at)
		for i := 0; i < len(page.Batches)-1; i++ {
			if page.Batches[i].UpdatedAt.Before(page.Batches[i+1].UpdatedAt) {
				rt.Fatalf("batches not sorted by updated_at DESC: batch[%d].UpdatedAt=%v < batch[%d].UpdatedAt=%v",
					i, page.Batches[i].UpdatedAt, i+1, page.Batches[i+1].UpdatedAt)
			}
		}
	})
}
