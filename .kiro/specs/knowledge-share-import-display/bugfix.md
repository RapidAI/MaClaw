# Bugfix Requirements Document

## Introduction

After importing knowledge via a knowledge base sharing link in MaClawSrv's virtual employee knowledge management interface, the imported entries are not displayed in the "自己的知识库列表" (Own Import Batches) section. The area shows "暂无导入数据" (No import batches) even though the import operation succeeds and data is stored in the backend.

The root cause is that `ImportPackageSources` (used by the share import path) saves individual knowledge sources via `SaveText`/`SaveURL` but does **not** create an `ImportBatch` record in the `knowledge_import_batches` table. The "自己的知识库列表" UI queries `/api/v1/knowledge/import/batches` which calls `ListImportBatchesPage` — this only returns entries from the `knowledge_import_batches` table. Since share imports never write to this table, the UI always shows empty.

In contrast, directory/file imports go through `importScannedItems` which creates a batch record (`insertBatch`) and links individual import items to it. The share import path bypasses this entirely.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a user imports knowledge via a sharing link (POST `/api/v1/knowledge/import/share`) THEN the system saves knowledge sources to the database but does NOT create an `ImportBatch` record in the `knowledge_import_batches` table

1.2 WHEN a user views the "自己的知识库列表" section after importing via sharing link THEN the system returns an empty list from `ListImportBatchesPage` because no batch record exists for the share import

1.3 WHEN a user imports knowledge via a package file (POST `/api/v1/knowledge/import/package`) THEN the system saves knowledge sources but does NOT create an `ImportBatch` record (same missing batch creation as share imports)

### Expected Behavior (Correct)

2.1 WHEN a user imports knowledge via a sharing link (POST `/api/v1/knowledge/import/share`) THEN the system SHALL create an `ImportBatch` record in the `knowledge_import_batches` table with the import metadata (source count, status, topic hint from the share/package title, timestamps)

2.2 WHEN a user views the "自己的知识库列表" section after importing via sharing link THEN the system SHALL display the import batch with its display name (derived from the share/package title), file count, status, and timestamps

2.3 WHEN a user imports knowledge via a package file (POST `/api/v1/knowledge/import/package`) THEN the system SHALL create an `ImportBatch` record with the package metadata (title, source count, status)

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a user imports knowledge via directory/file upload THEN the system SHALL CONTINUE TO create an `ImportBatch` record through the existing `importScannedItems` path

3.2 WHEN a user views the "自己的知识库列表" section with only directory/file imports THEN the system SHALL CONTINUE TO display those batches correctly with file names, status, and sample files

3.3 WHEN a user deletes an import batch THEN the system SHALL CONTINUE TO delete both the batch record and its associated knowledge sources

3.4 WHEN knowledge is imported via sharing link THEN the individual knowledge sources (text/URL) SHALL CONTINUE TO be saved correctly and be searchable/queryable through the knowledge search API

3.5 WHEN knowledge is imported via sharing link THEN the import job async status tracking (`/api/v1/knowledge/import/jobs/{jobId}`) SHALL CONTINUE TO work correctly

---

## Bug Condition (Formal)

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type KnowledgeImportRequest
  OUTPUT: boolean
  
  // Returns true when import is done via share link or package file
  // (paths that use ImportPackageSources without creating a batch record)
  RETURN X.ImportMethod IN {"share_link", "package_file"}
END FUNCTION
```

## Fix Checking Property

```pascal
// Property: Fix Checking — Share/package imports create batch records
FOR ALL X WHERE isBugCondition(X) DO
  result ← ImportPackageSources'(X)
  batchRecord ← ListImportBatchesPage(X.OwnerID, X.TenantID)
  ASSERT EXISTS batch IN batchRecord.Batches WHERE
    batch.OwnerID = X.OwnerID AND
    batch.TenantID = X.TenantID AND
    batch.TotalFiles = result.Total AND
    batch.ImportedFiles = result.Imported AND
    batch.Status IN {"completed", "partial"}
END FOR
```

## Preservation Checking Property

```pascal
// Property: Preservation Checking — Directory/file imports unchanged
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT F(X) = F'(X)
  // importScannedItems path behavior is identical before and after fix
END FOR
```
