# Implementation Plan

## Overview
This plan implements the knowledge share/package import display fix for MaClawSrv. The core problem is that `ImportPackageSources` saves individual knowledge sources but never creates an `ImportBatch` record. The solution introduces a `BatchCreator` optional interface detected via Go type assertion, modifies `ImportPackageSources` to use it when available, and implements it on `SQLiteStore` and `multiKnowledgeStore`.

## Tasks

- [x] 1. Define BatchCreator interface and extend data models
  - [x] 1.1 Define `BatchCreator` interface with `CreateImportBatch(ctx, ImportBatch) error`, `UpdateImportBatch(ctx, ImportBatch) error`, and `CreateImportItem(ctx, ImportItem) error` in `corelib/knowledge/package_import.go`
  - [x] 1.2 Add `TopicHint string` field to `PackageImportOptions` struct in `corelib/knowledge/package_import.go`
  - [x] 1.3 Add `BatchID string` field to `TextSaveRequest` struct in `corelib/knowledge/types.go`
  - [x] 1.4 Add `BatchID string` field to `URLSaveRequest` struct in `corelib/knowledge/types.go`
  - [x] 1.5 Add `deriveImportBatchStatus(imported, skipped, failed, total int) string` helper function in `corelib/knowledge/package_import.go`
- [x] 2. Modify ImportPackageSources to create batch records via BatchCreator type assertion
  - [x] 2.1 Before source loop: type-assert store to `BatchCreator`, if successful and not DryRun create ImportBatch with status "running" using opts.TopicHint and len(sources) as TotalFiles
  - [x] 2.2 During loop: after each successful SaveText/SaveURL, if BatchCreator available call CreateImportItem to link source to batch, and set BatchID on TextSaveRequest/URLSaveRequest
  - [x] 2.3 After loop: if BatchCreator available call UpdateImportBatch with final counts and derived status via deriveImportBatchStatus
  - [x] 2.4 Handle context cancellation: update batch with partial counts and "partial" status before returning when context cancelled mid-import
  - [x] 2.5 Handle errors gracefully: CreateImportBatch failure logs warning and continues without batch tracking, CreateImportItem failure logs and continues, UpdateImportBatch failure logs warning
- [x] 3. Implement BatchCreator interface on SQLiteStore
  - [x] 3.1 Implement `CreateImportBatch(ctx, ImportBatch) error` on SQLiteStore wrapping existing insertBatch with transaction
  - [x] 3.2 Implement `UpdateImportBatch(ctx, ImportBatch) error` on SQLiteStore executing UPDATE on knowledge_import_batches
  - [x] 3.3 Implement `CreateImportItem(ctx, ImportItem) error` on SQLiteStore wrapping existing insertImportItem with transaction
  - [x] 3.4 Ensure SaveText/SaveURL pass through BatchID field from request structs to insertSource
- [x] 4. Forward BatchCreator methods on multiKnowledgeStore wrapper
  - [x] 4.1 Add `CreateImportBatch` method on multiKnowledgeStore delegating to s.store.CreateImportBatch
  - [x] 4.2 Add `UpdateImportBatch` method on multiKnowledgeStore delegating to s.store.UpdateImportBatch
  - [x] 4.3 Add `CreateImportItem` method on multiKnowledgeStore delegating to s.store.CreateImportItem
- [x] 5. Write property-based tests using pgregory.net/rapid
  - [x] 5.1 Property 1 (Batch Creation): for any valid PackageSource set with BatchCreator store, ImportPackageSources creates exactly one ImportBatch with correct TopicHint and TotalFiles **Validates: Requirements 1.1, 2.1**
  - [x] 5.2 Property 2 (Status Derivation): for any counts tuple where imported+skipped+failed==total, deriveImportBatchStatus returns correct status and invariant holds **Validates: Requirements 1.2, 1.3, 1.4, 2.2**
  - [x] 5.3 Property 3 (Source-to-Batch Linking): for any successfully imported source with BatchCreator store, source BatchID equals batch ID and ImportItem with status=imported exists **Validates: Requirements 1.5, 2.3**
  - [x] 5.4 Property 4 (Graceful Degradation): for any store implementing only PackageImportStore, ImportPackageSources imports correctly without panic and returns accurate counts **Validates: Requirements 5.1, 5.3, 6.3**
  - [x] 5.5 Property 5 (Cascade Delete): for any ImportBatch created by share/package import, DeleteImportBatch removes batch record, all linked ImportItems, and all linked Sources **Validates: Requirements 4.1, 4.2**
  - [x] 5.6 Property 6 (Batch Visibility): for any mix of ImportBatch records from different origins, ListImportBatchesPage returns all sorted by updated_at DESC **Validates: Requirements 3.1, 3.2**
- [x] 6. Write unit tests for edge cases
  - [x] 6.1 Test import with 0 sources returns immediately without creating batch
  - [x] 6.2 Test import with 1 URL source that fails re-fetch but has inline content shows batch with 1 imported
  - [x] 6.3 Test import with all metadata-only sources results in batch status "failed"
  - [x] 6.4 Test DryRun=true does not create batch and BatchCreator methods are never called
  - [x] 6.5 Test multiKnowledgeStore correctly forwards BatchCreator interface via type assertion
  - [x] 6.6 Test store implementing only PackageImportStore (no BatchCreator) works without panic
- [x] 7. Write integration tests for end-to-end share import flow
  - [x] 7.1 Test full flow: ImportPackageSources with real SQLiteStore then verify batch appears in ListImportBatchesPage
  - [x] 7.2 Test full flow: share import then DeleteImportBatch removes batch, items, and linked sources
  - [x] 7.3 Test regression: existing directory import via ImportDirectory continues unchanged

## Task Dependency Graph
```json
{
  "waves": [
    { "tasks": [1] },
    { "tasks": [2, 3] },
    { "tasks": [4] },
    { "tasks": [5, 6, 7] }
  ]
}
```

## Notes
- No schema migration required — `knowledge_import_batches`, `knowledge_import_items`, and `knowledge_sources.batch_id` tables/columns already exist
- BatchCreator errors are non-fatal: primary import operation must not fail because of batch bookkeeping
- The `BatchCreator` interface uses Go type assertion pattern for backward compatibility: existing callers/mocks implementing only `SaveText`/`SaveURL` continue to work unchanged
- Testing library: `pgregory.net/rapid` (already used in this project)
- Minimum 100 iterations per property test
