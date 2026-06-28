# Requirements Document

## Introduction

This feature fixes the knowledge share import display gap in MaClawSrv. After importing knowledge via a sharing link or package file, the imported entries do not appear in the "自己的知识库列表" (Own Import Batches) section of the virtual employee knowledge management interface. The root cause is that the share/package import path (`ImportPackageSources`) saves individual knowledge sources but does not create an `ImportBatch` record in the `knowledge_import_batches` table. The UI queries this table exclusively, resulting in an empty list despite successful imports.

## Glossary

- **Knowledge_Import_System**: The subsystem in MaClawSrv responsible for importing, storing, and displaying knowledge sources from various import methods (directory, file upload, sharing link, package file).
- **ImportBatch**: A database record in the `knowledge_import_batches` table that groups a set of imported knowledge sources into a single logical batch with metadata (display name, source count, status, timestamps).
- **Share_Import_Path**: The code path triggered by `POST /api/v1/knowledge/import/share` that resolves a Hub sharing link, fetches the knowledge package, and calls `ImportPackageSources` to save individual sources.
- **Package_Import_Path**: The code path triggered by `POST /api/v1/knowledge/import/package` that imports a knowledge JSON package via `ImportPackageSources`.
- **Directory_Import_Path**: The code path triggered by `POST /api/v1/knowledge/import/directory` that scans a directory and calls `importScannedItems`, which creates an `ImportBatch` record and links items to it.
- **ImportPackageSources**: The shared function in `corelib/knowledge/package_import.go` that iterates over `PackageSource` entries and calls `SaveText`/`SaveURL` for each one. Currently does not create batch records.
- **ListImportBatchesPage**: The store method that queries the `knowledge_import_batches` table with pagination, used by the batches list API.
- **TopicHint**: A human-readable label (typically derived from the share title or package title) used as the display name for an import batch.
- **PackageImportStore**: The minimal interface (`SaveText`/`SaveURL`) required by `ImportPackageSources`. Must be extended to support batch creation.

## Requirements

### Requirement 1: Create ImportBatch Record for Share Imports

**User Story:** As a virtual employee user, I want my knowledge imports via sharing links to appear in the "自己的知识库列表" section, so that I can see and manage all my imported knowledge in one place.

#### Acceptance Criteria

1. WHEN a user imports knowledge via a sharing link (POST `/api/v1/knowledge/import/share`), THE Knowledge_Import_System SHALL create an ImportBatch record in the `knowledge_import_batches` table with the share metadata (package title as TopicHint, source count, import status, timestamps).
2. WHEN the share import completes successfully, THE Knowledge_Import_System SHALL set the ImportBatch status to "completed" and record the count of imported, skipped, and failed sources.
3. WHEN the share import partially succeeds (some sources imported, some skipped/failed), THE Knowledge_Import_System SHALL set the ImportBatch status to "partial" and record accurate counts for imported, skipped, and failed sources.
4. IF the share import fails entirely (zero sources imported), THEN THE Knowledge_Import_System SHALL set the ImportBatch status to "failed" and record the failure reason in the batch metadata.
5. WHEN creating the ImportBatch record for a share import, THE Knowledge_Import_System SHALL link each successfully imported source to the batch via the `batch_id` foreign key in the import items table.

### Requirement 2: Create ImportBatch Record for Package Imports

**User Story:** As a virtual employee user, I want my knowledge imports via package files to appear in the "自己的知识库列表" section, so that I have a consistent view of all import operations regardless of the import method.

#### Acceptance Criteria

1. WHEN a user imports knowledge via a package file (POST `/api/v1/knowledge/import/package`), THE Knowledge_Import_System SHALL create an ImportBatch record in the `knowledge_import_batches` table with the package metadata (package title as TopicHint, source count, import status, timestamps).
2. WHEN the package import completes, THE Knowledge_Import_System SHALL update the ImportBatch record with final counts (imported, skipped, failed) and set the appropriate status ("completed" or "partial").
3. WHEN creating the ImportBatch record for a package import, THE Knowledge_Import_System SHALL link each successfully imported source to the batch via the `batch_id` foreign key.

### Requirement 3: Display Share/Package Import Batches in UI

**User Story:** As a virtual employee user, I want to see all my import batches (including share and package imports) listed with their display names, file counts, and status in the knowledge management interface.

#### Acceptance Criteria

1. WHEN a user views the "自己的知识库列表" section after importing via sharing link, THE Knowledge_Import_System SHALL display the import batch with its display name (derived from the share/package title), total source count, imported count, status, and timestamps.
2. WHEN a user views the "自己的知识库列表" section, THE Knowledge_Import_System SHALL return share/package import batches alongside directory/file import batches in the same paginated list, sorted by creation time descending.
3. WHEN a user expands or views details of a share/package import batch, THE Knowledge_Import_System SHALL display sample files (up to 4 items) from the linked import items.

### Requirement 4: Delete Share/Package Import Batches

**User Story:** As a virtual employee user, I want to delete import batches created from sharing links or packages, so that I can clean up unwanted imported knowledge.

#### Acceptance Criteria

1. WHEN a user deletes an import batch that was created from a share or package import, THE Knowledge_Import_System SHALL delete both the batch record and all associated knowledge sources that were imported in that batch.
2. WHEN a user deletes a share/package import batch, THE Knowledge_Import_System SHALL remove the linked import items from the import items table.

### Requirement 5: Preserve Existing Directory/File Import Behavior

**User Story:** As a virtual employee user, I want my existing directory/file import batches to continue working correctly after this change.

#### Acceptance Criteria

1. WHILE the Directory_Import_Path is used, THE Knowledge_Import_System SHALL continue to create ImportBatch records through the existing `importScannedItems` path without modification.
2. THE Knowledge_Import_System SHALL maintain backward compatibility with existing ImportBatch records created by directory/file imports (no schema migration that breaks existing data).
3. WHEN knowledge is imported via sharing link, THE Knowledge_Import_System SHALL continue to save individual knowledge sources (text/URL) correctly and ensure they are searchable/queryable through the knowledge search API.
4. WHEN knowledge is imported via sharing link, THE Knowledge_Import_System SHALL continue to support the async import job status tracking (`/api/v1/knowledge/import/jobs/{jobId}`).

### Requirement 6: Extend PackageImportStore Interface

**User Story:** As a developer, I want a clean interface extension for batch creation in the import path, so that the fix is minimal and testable.

#### Acceptance Criteria

1. THE Knowledge_Import_System SHALL extend the `PackageImportStore` interface (or introduce a new composite interface) to include batch creation and item linking capabilities without breaking existing callers that only need `SaveText`/`SaveURL`.
2. WHEN `ImportPackageSources` is called with a store that supports batch creation, THE Knowledge_Import_System SHALL create a batch record before importing sources and link each imported source to that batch.
3. WHEN `ImportPackageSources` is called with a store that does NOT support batch creation (legacy callers, tests), THE Knowledge_Import_System SHALL continue to import sources without creating a batch record (graceful degradation).
