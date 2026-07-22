package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

type packageCaptureURLStore struct {
	mockBatchCreatorStore
	savedURL string
}

func (s *packageCaptureURLStore) SaveURL(_ context.Context, req URLSaveRequest) (Source, error) {
	s.savedURL = req.URL
	return Source{ID: "saved-url"}, nil
}

func TestImportPackageSources_UsesFetchableURIWhenCanonicalURIIsNotHTTP(t *testing.T) {
	store := &packageCaptureURLStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID:           "mixed-uri",
		CanonicalURI: "knowledge://archived/mixed-uri",
		URI:          "HTTPS://example.test/article",
	}}, PackageImportOptions{})

	if result.Imported != 1 || result.Status != "completed" {
		t.Fatalf("fetchable fallback URI was not imported: %#v", result)
	}
	if store.savedURL != "HTTPS://example.test/article" {
		t.Fatalf("SaveURL URL=%q, want original HTTP URI", store.savedURL)
	}
}

func TestImportPackageSources_SkipsMalformedCanonicalHTTPURIForValidOriginal(t *testing.T) {
	store := &packageCaptureURLStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID:           "malformed-canonical",
		CanonicalURI: "https:///missing-host",
		URI:          "https://example.test/original",
	}}, PackageImportOptions{})

	if result.Imported != 1 || result.Status != "completed" {
		t.Fatalf("valid original URI was not imported: %#v", result)
	}
	if store.savedURL != "https://example.test/original" {
		t.Fatalf("SaveURL URL=%q, want valid original URI", store.savedURL)
	}
}

func TestImportPackageSources_SkipsBlockedCanonicalHTTPURIForValidOriginal(t *testing.T) {
	store := &packageCaptureURLStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID:           "blocked-canonical",
		CanonicalURI: "http://localhost/private",
		URI:          "https://example.test/original",
	}}, PackageImportOptions{})

	if result.Imported != 1 || result.Status != "completed" {
		t.Fatalf("valid original URI was not imported: %#v", result)
	}
	if store.savedURL != "https://example.test/original" {
		t.Fatalf("SaveURL URL=%q, want valid original URI", store.savedURL)
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

type packageAmbiguousBatchCreateStore struct {
	mockBatchCreatorStore
	createCalls int
	updateCalls int
}

func (s *packageAmbiguousBatchCreateStore) CreateImportBatch(_ context.Context, batch ImportBatch) error {
	s.createCalls++
	s.batches = append(s.batches, batch) // Simulate commit succeeding before timeout is observed.
	return context.DeadlineExceeded
}

func (s *packageAmbiguousBatchCreateStore) UpdateImportBatch(ctx context.Context, batch ImportBatch) error {
	s.updateCalls++
	return s.mockBatchCreatorStore.UpdateImportBatch(ctx, batch)
}

func TestImportPackageSources_AmbiguousBatchCreationStillFinalizesCommittedBatch(t *testing.T) {
	store := &packageAmbiguousBatchCreateStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID: "source-one", Content: "content",
	}}, PackageImportOptions{})

	if result.Imported != 1 || result.Status != "completed" {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if store.createCalls != 1 || store.updateCalls != 1 {
		t.Fatalf("batch calls create=%d update=%d, want 1/1", store.createCalls, store.updateCalls)
	}
	if len(store.batches) != 1 || store.batches[0].Status != "completed" || store.batches[0].Imported != 1 {
		t.Fatalf("possibly committed batch was left running: %#v", store.batches)
	}
	if len(store.items) != 0 {
		t.Fatalf("item tracking should remain disabled after ambiguous creation: %#v", store.items)
	}
	if len(store.sources) != 1 || store.sources[0].BatchID == "" {
		t.Fatalf("source lost recovery batch ID: %#v", store.sources)
	}
	if !containsWarning(result.Warnings, "batch creation failed") {
		t.Fatalf("missing ambiguous creation warning: %#v", result.Warnings)
	}
}

type packageDefiniteBatchCreateFailureStore struct {
	mockBatchCreatorStore
	updateCalls int
}

func (s *packageDefiniteBatchCreateFailureStore) CreateImportBatch(context.Context, ImportBatch) error {
	return errors.New("batch table unavailable")
}

func (s *packageDefiniteBatchCreateFailureStore) UpdateImportBatch(ctx context.Context, batch ImportBatch) error {
	s.updateCalls++
	return s.mockBatchCreatorStore.UpdateImportBatch(ctx, batch)
}

func TestImportPackageSources_DefiniteBatchCreationFailureDoesNotLeakBatchID(t *testing.T) {
	store := &packageDefiniteBatchCreateFailureStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID: "source-one", Content: "content",
	}}, PackageImportOptions{})

	if result.Imported != 1 || result.Status != "completed" {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if store.updateCalls != 0 {
		t.Fatalf("final update called %d times after definite creation failure", store.updateCalls)
	}
	if len(store.sources) != 1 || store.sources[0].BatchID != "" {
		t.Fatalf("nonexistent batch ID leaked into source: %#v", store.sources)
	}
	if !containsWarning(result.Warnings, "batch creation failed") {
		t.Fatalf("missing creation failure warning: %#v", result.Warnings)
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

type packageTimeoutStore struct {
	mockBatchCreatorStore
	finalContextErr error
}

func (s *packageTimeoutStore) SaveText(ctx context.Context, req TextSaveRequest) (Source, error) {
	if req.Title == "slow" {
		<-ctx.Done()
		return Source{}, ctx.Err()
	}
	return s.mockBatchCreatorStore.SaveText(ctx, req)
}

func (s *packageTimeoutStore) UpdateImportBatch(ctx context.Context, batch ImportBatch) error {
	s.finalContextErr = ctx.Err()
	return s.mockBatchCreatorStore.UpdateImportBatch(ctx, batch)
}

func TestImportPackageSources_SourceTimeoutDoesNotCancelBatch(t *testing.T) {
	store := &packageTimeoutStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{
		{ID: "source-slow", Title: "slow", Content: "slow content"},
		{ID: "source-fast", Title: "fast", Content: "fast content"},
	}, PackageImportOptions{PerSourceTimeout: 20 * time.Millisecond})

	if result.Imported != 1 || result.Failed != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Status != "partial" {
		t.Fatalf("status=%q, want partial", result.Status)
	}
	if len(store.sources) != 1 || store.sources[0].Title != "fast" {
		t.Fatalf("next source was not imported after timeout: %#v", store.sources)
	}
	if !containsWarning(result.Warnings, "source-slow") || !containsWarning(result.Warnings, "deadline exceeded") {
		t.Fatalf("missing source timeout warning: %#v", result.Warnings)
	}
	if len(result.RetrySourceIDs) != 1 || result.RetrySourceIDs[0] != "source-slow" {
		t.Fatalf("RetrySourceIDs=%#v, want source-slow", result.RetrySourceIDs)
	}
	if len(result.ImportedSourceIDs) != 1 || result.ImportedSourceIDs[0] != store.sources[0].ID {
		t.Fatalf("ImportedSourceIDs=%#v, want persisted source ID %q", result.ImportedSourceIDs, store.sources[0].ID)
	}
	if store.finalContextErr != nil {
		t.Fatalf("final batch update inherited a cancelled source context: %v", store.finalContextErr)
	}
	if len(store.batches) != 1 || store.batches[0].Status != "partial" {
		t.Fatalf("batch should be partial after one source timeout: %#v", store.batches)
	}
}

type packageSlowItemStore struct {
	mockBatchCreatorStore
	itemCalls int
}

func (s *packageSlowItemStore) CreateImportItem(ctx context.Context, item ImportItem) error {
	s.itemCalls++
	if s.itemCalls == 1 {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.mockBatchCreatorStore.CreateImportItem(ctx, item)
}

func TestImportPackageSources_SlowItemBookkeepingUsesShortIndependentTimeout(t *testing.T) {
	store := &packageSlowItemStore{}
	started := time.Now()
	result := ImportPackageSources(context.Background(), store, []PackageSource{
		{ID: "one", Content: "first"},
		{ID: "two", Content: "second"},
	}, PackageImportOptions{ItemWriteTimeout: 20 * time.Millisecond, BatchWriteTimeout: time.Second})

	if result.Imported != 2 || result.Failed != 0 {
		t.Fatalf("bookkeeping timeout affected source imports: %#v", result)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("item bookkeeping used the long batch timeout: %v", elapsed)
	}
	if !containsWarning(result.Warnings, "link item failed") || !containsWarning(result.Warnings, "deadline exceeded") {
		t.Fatalf("missing bookkeeping timeout warning: %#v", result.Warnings)
	}
}

type packageParentCancelStore struct {
	mockBatchCreatorStore
	finalContextErr error
}

func (s *packageParentCancelStore) SaveText(ctx context.Context, _ TextSaveRequest) (Source, error) {
	<-ctx.Done()
	return Source{}, ctx.Err()
}

func (s *packageParentCancelStore) UpdateImportBatch(ctx context.Context, batch ImportBatch) error {
	s.finalContextErr = ctx.Err()
	return s.mockBatchCreatorStore.UpdateImportBatch(ctx, batch)
}

func TestImportPackageSources_ParentCancellationStopsAndFinalizesWithFreshContext(t *testing.T) {
	store := &packageParentCancelStore{}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	result := ImportPackageSources(ctx, store, []PackageSource{
		{ID: "source-current", Content: "one"},
		{ID: "source-pending", Content: "two"},
	}, PackageImportOptions{PerSourceTimeout: time.Second})

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("parent context was not cancelled: %v", ctx.Err())
	}
	if result.Imported != 0 || result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("parent cancellation must not classify sources as failed: %#v", result)
	}
	if len(result.RetrySourceIDs) != 2 || result.RetrySourceIDs[0] != "source-current" || result.RetrySourceIDs[1] != "source-pending" {
		t.Fatalf("RetrySourceIDs=%#v, want current and pending", result.RetrySourceIDs)
	}
	if store.finalContextErr != nil {
		t.Fatalf("final batch update inherited parent cancellation: %v", store.finalContextErr)
	}
	if len(store.batches) != 1 || store.batches[0].Status != "partial" {
		t.Fatalf("cancelled batch should be finalized as partial: %#v", store.batches)
	}
}

type packageCancelAfterCommitStore struct {
	mockBatchCreatorStore
	cancel context.CancelFunc
	calls  int
}

func (s *packageCancelAfterCommitStore) SaveText(ctx context.Context, req TextSaveRequest) (Source, error) {
	s.calls++
	saved, err := s.mockBatchCreatorStore.SaveText(ctx, req)
	if err == nil && s.calls == 1 {
		s.cancel()
	}
	return saved, err
}

func TestImportPackageSources_CancellationAfterSuccessfulSaveRetriesOnlyPendingSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &packageCancelAfterCommitStore{cancel: cancel}
	result := ImportPackageSources(ctx, store, []PackageSource{
		{ID: "source-committed", Content: "one"},
		{ID: "source-pending", Content: "two"},
	}, PackageImportOptions{})

	if result.Imported != 1 || result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("successful save raced with cancellation: %#v", result)
	}
	if result.Status != "partial" {
		t.Fatalf("status=%q, want partial after cancellation", result.Status)
	}
	if len(result.ImportedSourceIDs) != 1 || result.ImportedSourceIDs[0] != store.sources[0].ID {
		t.Fatalf("ImportedSourceIDs=%#v, want committed source ID %q", result.ImportedSourceIDs, store.sources[0].ID)
	}
	if len(result.RetrySourceIDs) != 1 || result.RetrySourceIDs[0] != "source-pending" {
		t.Fatalf("RetrySourceIDs=%#v, want only source-pending", result.RetrySourceIDs)
	}
	if store.calls != 1 {
		t.Fatalf("SaveText calls=%d, want 1", store.calls)
	}
	if len(store.batches) != 1 || store.batches[0].Status != "partial" || store.batches[0].Imported != 1 {
		t.Fatalf("cancelled batch finalization is incoherent: %#v", store.batches)
	}
}

type packageURLInternalTimeoutStore struct {
	mockBatchCreatorStore
}

type packageURLSharedDeadlineStore struct {
	mockBatchCreatorStore
	urlDeadline      time.Time
	fallbackDeadline time.Time
}

func (s *packageURLSharedDeadlineStore) SaveURL(ctx context.Context, _ URLSaveRequest) (Source, error) {
	s.urlDeadline, _ = ctx.Deadline()
	return Source{}, errors.New("fetch failed")
}

func (s *packageURLSharedDeadlineStore) SaveText(ctx context.Context, req TextSaveRequest) (Source, error) {
	s.fallbackDeadline, _ = ctx.Deadline()
	return s.mockBatchCreatorStore.SaveText(ctx, req)
}

func TestImportPackageSources_URLFetchAndFallbackShareSourceDeadline(t *testing.T) {
	store := &packageURLSharedDeadlineStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID: "shared-budget", URI: "https://example.test/article", Content: "portable content",
	}}, PackageImportOptions{PerSourceTimeout: time.Second})

	if result.Imported != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if store.urlDeadline.IsZero() || !store.urlDeadline.Equal(store.fallbackDeadline) {
		t.Fatalf("URL and fallback deadlines differ: url=%v fallback=%v", store.urlDeadline, store.fallbackDeadline)
	}
}

func (s *packageURLInternalTimeoutStore) SaveURL(context.Context, URLSaveRequest) (Source, error) {
	return Source{}, context.DeadlineExceeded
}

func TestImportPackageSources_URLInternalTimeoutStillUsesInlineFallback(t *testing.T) {
	store := &packageURLInternalTimeoutStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID: "url-timeout", URI: "https://example.test/article", Title: "fallback", Content: "portable content",
	}}, PackageImportOptions{PerSourceTimeout: time.Second})

	if result.Imported != 1 || result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("internal URL timeout should use inline fallback: %#v", result)
	}
	if len(store.sources) != 1 || store.sources[0].Title != "fallback" {
		t.Fatalf("inline fallback was not saved: %#v", store.sources)
	}
	if len(result.ImportedSourceIDs) != 1 || result.ImportedSourceIDs[0] != store.sources[0].ID {
		t.Fatalf("ImportedSourceIDs=%#v, want persisted fallback source ID %q", result.ImportedSourceIDs, store.sources[0].ID)
	}
	if len(result.RetrySourceIDs) != 0 {
		t.Fatalf("successful fallback must not be retried: %#v", result.RetrySourceIDs)
	}
}

type packageURLBudgetExpiredWrappedStore struct {
	mockBatchCreatorStore
	fallbackCalls int
}

func (s *packageURLBudgetExpiredWrappedStore) SaveURL(ctx context.Context, _ URLSaveRequest) (Source, error) {
	<-ctx.Done()
	// Some fetch layers replace ctx.Err() with a transport-specific error. The
	// importer's own source context remains authoritative for budget expiry.
	return Source{}, errors.New("fetch aborted")
}

func (s *packageURLBudgetExpiredWrappedStore) SaveText(ctx context.Context, req TextSaveRequest) (Source, error) {
	s.fallbackCalls++
	return s.mockBatchCreatorStore.SaveText(ctx, req)
}

func TestImportPackageSources_URLSourceBudgetExpiryDoesNotStartFallback(t *testing.T) {
	store := &packageURLBudgetExpiredWrappedStore{}
	result := ImportPackageSources(context.Background(), store, []PackageSource{{
		ID: "url-budget", URI: "https://example.test/article", Content: "portable content",
	}}, PackageImportOptions{PerSourceTimeout: 20 * time.Millisecond})

	if result.Imported != 0 || result.Failed != 1 || result.Skipped != 1 {
		t.Fatalf("expired URL source budget was misclassified: %#v", result)
	}
	if store.fallbackCalls != 0 {
		t.Fatalf("fallback calls=%d, want 0 after source budget expired", store.fallbackCalls)
	}
	if len(result.RetrySourceIDs) != 1 || result.RetrySourceIDs[0] != "url-budget" {
		t.Fatalf("RetrySourceIDs=%#v, want url-budget", result.RetrySourceIDs)
	}
	if !containsWarning(result.Warnings, "timed out and can be retried") {
		t.Fatalf("missing source-timeout warning: %#v", result.Warnings)
	}
}

type packageURLFallbackCancelStore struct {
	mockBatchCreatorStore
}

func (s *packageURLFallbackCancelStore) SaveURL(context.Context, URLSaveRequest) (Source, error) {
	return Source{}, errors.New("fetch failed")
}

func (s *packageURLFallbackCancelStore) SaveText(ctx context.Context, _ TextSaveRequest) (Source, error) {
	<-ctx.Done()
	return Source{}, ctx.Err()
}

func TestImportPackageSources_URLFallbackParentCancellationStopsBatch(t *testing.T) {
	store := &packageURLFallbackCancelStore{}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	result := ImportPackageSources(ctx, store, []PackageSource{
		{ID: "url-current", URI: "https://example.test/current", Content: "fallback"},
		{ID: "source-pending", Content: "pending"},
	}, PackageImportOptions{PerSourceTimeout: time.Second})

	if result.Imported != 0 || result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("parent cancellation during fallback must not classify failure: %#v", result)
	}
	if len(result.RetrySourceIDs) != 2 || result.RetrySourceIDs[0] != "url-current" || result.RetrySourceIDs[1] != "source-pending" {
		t.Fatalf("RetrySourceIDs=%#v, want current and pending", result.RetrySourceIDs)
	}
	if len(store.batches) != 1 || store.batches[0].Status != "partial" {
		t.Fatalf("cancelled fallback batch should be partial: %#v", store.batches)
	}
}
