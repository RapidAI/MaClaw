package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BatchCreator is an optional interface that PackageImportStore implementations
// can satisfy to enable automatic ImportBatch record creation during package imports.
// Detected via type assertion in ImportPackageSources.
type BatchCreator interface {
	CreateImportBatch(ctx context.Context, batch ImportBatch) error
	UpdateImportBatch(ctx context.Context, batch ImportBatch) error
	CreateImportItem(ctx context.Context, item ImportItem) error
}

// deriveImportBatchStatus determines the final batch status from import counts.
func deriveImportBatchStatus(imported, skipped, failed, total int) string {
	switch {
	case imported == 0 && total > 0:
		return "failed"
	case imported == total:
		return "completed"
	default:
		return "partial"
	}
}

// PackageSource represents one source entry in a knowledge package.
// This is the canonical import-side representation — both GUI and MaClawSrv
// map their package structs to this before calling ImportPackageSources.
type PackageSource struct {
	ID               string
	Kind             string
	URI              string
	CanonicalURI     string
	Title            string
	TopicHint        string
	Labels           []string
	Content          string // Inline content (text extracted at export time).
	ContentTruncated bool   // True when the exported inline content was already cut down.
}

// PackageImportOptions configures the import behavior.
type PackageImportOptions struct {
	OwnerID   string
	TenantID  string
	SaveScope string // e.g. SaveScopePersonal. Empty = store default.
	DryRun    bool   // When true, classify sources without writing. Store can be nil.
	TopicHint string // Display name for the batch (from package/share title).
	RootPath  string // Origin identifier for the batch, e.g. "share://{id}" or "package://{id}".
}

// PackageImportResult contains the outcome of a package import.
type PackageImportResult struct {
	Imported int
	Skipped  int
	Failed   int
	Total    int
	Warnings []string
}

// PackageImportStore is the minimal interface required for importing package sources.
// Both SQLiteStore and any test mock can implement this.
type PackageImportStore interface {
	SaveText(ctx context.Context, req TextSaveRequest) (Source, error)
	SaveURL(ctx context.Context, req URLSaveRequest) (Source, error)
}

// linkBatchItem creates an ImportItem record linking a source to the batch.
// It is a no-op when batchCreator is nil. Errors are non-fatal and appended
// to warnings.
func linkBatchItem(ctx context.Context, bc BatchCreator, batchID string, sourceID string, filePath string, kind string, status string, errMsg string, fileSize int64, warnings *[]string) {
	if bc == nil {
		return
	}
	now := time.Now().UTC()
	item := ImportItem{
		ID:           NewID("kitem"),
		BatchID:      batchID,
		SourceID:     sourceID,
		FilePath:     filePath,
		Kind:         kind,
		Status:       status,
		ErrorMessage: errMsg,
		FileSize:     fileSize,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if linkErr := bc.CreateImportItem(ctx, item); linkErr != nil {
		*warnings = append(*warnings, fmt.Sprintf("link item failed for %s: %v", filePath, linkErr))
	}
}

// ImportPackageSources imports knowledge sources from a package into the store.
//
// Strategy (single source of truth for both GUI and MaClawSrv):
//   - URL sources (has HTTP URI): try re-fetch first for data freshness.
//     If re-fetch fails AND inline content is available, fall back to SaveText.
//   - Text sources (has content, no HTTP URI): save inline content directly.
//   - Metadata-only sources (no content, no fetchable URI): skip with warning.
//
// When opts.DryRun is true, the function classifies each source using the same
// switch logic but does not call SaveText/SaveURL. The store parameter may be nil
// in dry-run mode. This guarantees dry-run predictions are always consistent with
// actual import behavior.
//
// BatchCreator integration: If the store also implements BatchCreator and DryRun
// is false, ImportPackageSources creates an ImportBatch record before the loop,
// links each imported source via CreateImportItem, and updates the batch with
// final counts after the loop. BatchCreator errors are non-fatal.
func ImportPackageSources(ctx context.Context, store PackageImportStore, sources []PackageSource, opts PackageImportOptions) PackageImportResult {
	result := PackageImportResult{
		Total:    len(sources),
		Warnings: make([]string, 0),
	}

	// --- Before loop: type-assert store to BatchCreator ---
	var batchCreator BatchCreator
	var batchID string
	if !opts.DryRun && store != nil {
		if bc, ok := store.(BatchCreator); ok {
			now := time.Now().UTC()
			batchID = NewID("kbatch")
			batch := ImportBatch{
				ID:         batchID,
				RootPath:   opts.RootPath,
				OwnerID:    opts.OwnerID,
				TenantID:   opts.TenantID,
				TopicHint:  opts.TopicHint,
				Status:     "running",
				TotalFiles: len(sources),
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := bc.CreateImportBatch(ctx, batch); err != nil {
				// Non-fatal: log warning and continue without batch tracking.
				result.Warnings = append(result.Warnings, fmt.Sprintf("batch creation failed: %v", err))
				batchCreator = nil
				batchID = ""
			} else {
				batchCreator = bc
			}
		}
	}

	failed := 0
	for _, item := range sources {
		if ctx.Err() != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled at %d/%d", result.Imported+result.Skipped, result.Total))
			// Update batch with partial counts and "partial" status before returning.
			// Use context.Background() because ctx is already cancelled.
			if batchCreator != nil {
				now := time.Now().UTC()
				updateBatch := ImportBatch{
					ID:         batchID,
					Status:     "partial",
					TotalFiles: result.Total,
					Imported:   result.Imported,
					Skipped:    result.Skipped - failed,
					Failed:     failed,
					UpdatedAt:  now,
				}
				if err := batchCreator.UpdateImportBatch(context.Background(), updateBatch); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("batch update on cancellation failed: %v", err))
				}
			}
			break
		}

		uri := strings.TrimSpace(firstNonEmpty(item.CanonicalURI, item.URI))
		content := strings.TrimSpace(item.Content)
		isHTTP := strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
		label := firstNonEmpty(item.ID, item.Title, uri)
		if label == "" {
			label = "unknown source"
		}
		if item.ContentTruncated {
			result.Warnings = append(result.Warnings, fmt.Sprintf("source %s content is truncated", label))
		}

		switch {
		case isHTTP:
			if opts.DryRun {
				// In dry-run, URL sources are counted as importable (via re-fetch or content fallback).
				result.Imported++
				continue
			}
			if store == nil {
				errMsg := "knowledge store is unavailable"
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s skipped: %s", uri, errMsg))
				result.Skipped++
				failed++
				linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "failed", errMsg, 0, &result.Warnings)
				continue
			}
			// URL source — try re-fetch for freshness, fall back to inline content.
			savedSource, err := store.SaveURL(ctx, URLSaveRequest{
				URL:       uri,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
				BatchID:   batchID,
			})
			if err == nil {
				result.Imported++
				linkBatchItem(ctx, batchCreator, batchID, savedSource.ID, uri, "url", "imported", "", 0, &result.Warnings)
				continue
			}
			// Re-fetch failed — fall back to inline content if available.
			if content != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s re-fetch failed (%v), using inline content", uri, err))
				if fallbackSource, saveErr := store.SaveText(ctx, TextSaveRequest{
					Text:      content,
					Title:     firstNonEmpty(item.Title, uri),
					OwnerID:   opts.OwnerID,
					TenantID:  opts.TenantID,
					TopicHint: item.TopicHint,
					Labels:    item.Labels,
					SaveScope: opts.SaveScope,
					BatchID:   batchID,
				}); saveErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s inline fallback failed: %v", uri, saveErr))
					result.Skipped++
					failed++
					linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "failed", saveErr.Error(), 0, &result.Warnings)
				} else {
					result.Imported++
					linkBatchItem(ctx, batchCreator, batchID, fallbackSource.ID, uri, "text", "imported", "", int64(len(content)), &result.Warnings)
				}
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s skipped: re-fetch failed (%v), no inline content", uri, err))
				result.Skipped++
				failed++
				linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "skipped", err.Error(), 0, &result.Warnings)
			}

		case content != "":
			if opts.DryRun {
				result.Imported++
				continue
			}
			if store == nil {
				errMsg := "knowledge store is unavailable"
				result.Warnings = append(result.Warnings, fmt.Sprintf("text source %s skipped: %s", label, errMsg))
				result.Skipped++
				failed++
				linkBatchItem(ctx, batchCreator, batchID, "", label, "text", "failed", errMsg, int64(len(content)), &result.Warnings)
				continue
			}
			// Text source with inline content — import directly.
			savedSource, err := store.SaveText(ctx, TextSaveRequest{
				Text:      content,
				Title:     item.Title,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
				BatchID:   batchID,
			})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("text source %s skipped: %v", label, err))
				result.Skipped++
				failed++
				linkBatchItem(ctx, batchCreator, batchID, "", label, "text", "failed", err.Error(), int64(len(content)), &result.Warnings)
				continue
			}
			result.Imported++
			linkBatchItem(ctx, batchCreator, batchID, savedSource.ID, firstNonEmpty(item.Title, label), "text", "imported", "", int64(len(content)), &result.Warnings)

		default:
			// Metadata-only — no fetchable URI, no content.
			itemKind := strings.ToLower(strings.TrimSpace(item.Kind))
			if itemKind == "" {
				itemKind = "metadata"
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s source %s is metadata-only", itemKind, label))
			result.Skipped++
			linkBatchItem(ctx, batchCreator, batchID, "", label, itemKind, "skipped", "metadata-only, no content", 0, &result.Warnings)
		}
	}

	// --- After loop: update batch with final counts and derived status ---
	if batchCreator != nil {
		now := time.Now().UTC()
		// result.Skipped includes both metadata-only skips and save failures.
		// Separate them for the batch: true skips vs actual failures.
		trueSkipped := result.Skipped - failed
		finalStatus := deriveImportBatchStatus(result.Imported, trueSkipped, failed, result.Total)
		updateBatch := ImportBatch{
			ID:         batchID,
			Status:     finalStatus,
			TotalFiles: result.Total,
			Imported:   result.Imported,
			Skipped:    trueSkipped,
			Failed:     failed,
			UpdatedAt:  now,
		}
		if err := batchCreator.UpdateImportBatch(ctx, updateBatch); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("batch final update failed: %v", err))
		}
	}

	result.Failed = failed
	return result
}
