package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultPackageSourceTimeout     = 2 * time.Minute
	defaultPackageItemWriteTimeout  = 2 * time.Second
	defaultPackageBatchWriteTimeout = 15 * time.Second
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
	OwnerID           string
	TenantID          string
	SaveScope         string        // e.g. SaveScopePersonal. Empty = store default.
	DryRun            bool          // When true, classify sources without writing. Store can be nil.
	TopicHint         string        // Display name for the batch (from package/share title).
	RootPath          string        // Origin identifier for the batch, e.g. "share://{id}" or "package://{id}".
	PerSourceTimeout  time.Duration // Zero uses a conservative local-import default.
	ItemWriteTimeout  time.Duration // Zero uses a short timeout for each non-critical item record.
	BatchWriteTimeout time.Duration // Zero uses a bounded timeout for batch creation/finalization.
}

// PackageImportResult contains the outcome of a package import.
type PackageImportResult struct {
	Status            string
	Imported          int
	Skipped           int
	Failed            int
	Total             int
	ImportedSourceIDs []string // Persisted store IDs (or package labels during dry-run).
	SkippedSourceIDs  []string // Package IDs/labels for metadata-only entries.
	FailedSourceIDs   []string // Package IDs/labels for entries whose save failed.
	RetrySourceIDs    []string // Package IDs/labels; one value per package entry, preserving order.
	Warnings          []string
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
func detachedTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func linkBatchItem(parent context.Context, bc BatchCreator, batchID string, sourceID string, filePath string, kind string, status string, errMsg string, fileSize int64, timeout time.Duration, warnings *[]string) {
	if bc == nil {
		return
	}
	ctx, cancel := detachedTimeoutContext(parent, timeout)
	defer cancel()
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

func packageSourceLabel(item PackageSource) string {
	label := strings.TrimSpace(firstNonEmpty(item.ID, item.Title, item.CanonicalURI, item.URI))
	if label == "" {
		return "unknown source"
	}
	return label
}

func packageImportTimeout(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func packageHTTPURI(item PackageSource) string {
	for _, candidate := range []string{item.CanonicalURI, item.URI} {
		candidate = strings.TrimSpace(candidate)
		// Reuse the save path's structural/public-host validation so an invalid
		// canonical URI cannot hide a usable original URI. DNS resolution and
		// configured domain policy are still enforced later by SaveURL.
		if _, err := ValidatePublicHTTPURL(candidate); err == nil {
			return candidate
		}
	}
	return ""
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
	if ctx.Err() != nil {
		result.Status = "partial"
		result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled at 0/%d", result.Total))
		for _, pending := range sources {
			result.RetrySourceIDs = append(result.RetrySourceIDs, packageSourceLabel(pending))
		}
		return result
	}
	perSourceTimeout := packageImportTimeout(opts.PerSourceTimeout, defaultPackageSourceTimeout)
	itemWriteTimeout := packageImportTimeout(opts.ItemWriteTimeout, defaultPackageItemWriteTimeout)
	batchWriteTimeout := packageImportTimeout(opts.BatchWriteTimeout, defaultPackageBatchWriteTimeout)

	// --- Before loop: type-assert store to BatchCreator ---
	var batchCreator BatchCreator
	var batchFinalizer BatchCreator
	var batchID string
	if !opts.DryRun && store != nil {
		if bc, ok := store.(BatchCreator); ok {
			batchFinalizer = bc
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
			// Creation starts new work and must respect an already-cancelled caller.
			// Only recovery/finalization bookkeeping detaches from cancellation.
			batchCtx, cancel := context.WithTimeout(ctx, batchWriteTimeout)
			err := bc.CreateImportBatch(batchCtx, batch)
			cancel()
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("batch creation failed: %v", err))
				batchCreator = nil
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					// A cancellation can race a successful commit. Keep batchFinalizer
					// and batchID so the final bounded UPDATE can recover that batch.
				} else {
					// A definite creation failure must not leak a nonexistent batch ID
					// into successfully persisted sources.
					batchFinalizer = nil
					batchID = ""
				}
			} else {
				batchCreator = bc
			}
		}
	}

	failed := 0
	cancelled := false
	for index, item := range sources {
		if ctx.Err() != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled at %d/%d", result.Imported+result.Skipped, result.Total))
			cancelled = true
			for _, pending := range sources[index:] {
				result.RetrySourceIDs = append(result.RetrySourceIDs, packageSourceLabel(pending))
			}
			break
		}

		uri := packageHTTPURI(item)
		content := strings.TrimSpace(item.Content)
		isHTTP := uri != ""
		label := packageSourceLabel(item)
		if item.ContentTruncated {
			result.Warnings = append(result.Warnings, fmt.Sprintf("source %s content is truncated", label))
		}

		switch {
		case isHTTP:
			if opts.DryRun {
				// In dry-run, URL sources are counted as importable (via re-fetch or content fallback).
				result.Imported++
				result.ImportedSourceIDs = append(result.ImportedSourceIDs, label)
				continue
			}
			if store == nil {
				errMsg := "knowledge store is unavailable"
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s skipped: %s", uri, errMsg))
				result.Skipped++
				failed++
				result.FailedSourceIDs = append(result.FailedSourceIDs, label)
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "failed", errMsg, 0, itemWriteTimeout, &result.Warnings)
				continue
			}
			// URL source — try re-fetch for freshness, fall back to inline content.
			sourceCtx, cancelSource := context.WithTimeout(ctx, perSourceTimeout)
			savedSource, err := store.SaveURL(sourceCtx, URLSaveRequest{
				URL:       uri,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
				BatchID:   batchID,
			})
			if err == nil {
				cancelSource()
				result.Imported++
				result.ImportedSourceIDs = append(result.ImportedSourceIDs, savedSource.ID)
				linkBatchItem(ctx, batchCreator, batchID, savedSource.ID, uri, "url", "imported", "", 0, itemWriteTimeout, &result.Warnings)
				continue
			}
			if ctx.Err() != nil {
				cancelSource()
				cancelled = true
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				for _, pending := range sources[index+1:] {
					result.RetrySourceIDs = append(result.RetrySourceIDs, packageSourceLabel(pending))
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled while processing source %s: %v", label, ctx.Err()))
				break
			}
			// Only classify expiry of the per-source budget as a source timeout.
			// SaveURL can return an internal HTTP deadline first; inline package
			// content should still be attempted in that case.
			if errors.Is(sourceCtx.Err(), context.DeadlineExceeded) {
				cancelSource()
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s timed out and can be retried: %v", uri, err))
				result.Skipped++
				failed++
				result.FailedSourceIDs = append(result.FailedSourceIDs, label)
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "failed", err.Error(), 0, itemWriteTimeout, &result.Warnings)
				continue
			}
			// Re-fetch failed — fall back to inline content if available.
			if content != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s re-fetch failed (%v), using inline content", uri, err))
				// Re-fetch and inline fallback share one per-source budget. Giving each
				// stage a fresh timeout would let one source consume up to 2x the limit.
				fallbackSource, saveErr := store.SaveText(sourceCtx, TextSaveRequest{
					Text:      content,
					Title:     firstNonEmpty(item.Title, uri),
					OwnerID:   opts.OwnerID,
					TenantID:  opts.TenantID,
					TopicHint: item.TopicHint,
					Labels:    item.Labels,
					SaveScope: opts.SaveScope,
					BatchID:   batchID,
				})
				cancelSource()
				if saveErr != nil {
					if ctx.Err() != nil {
						cancelled = true
						result.RetrySourceIDs = append(result.RetrySourceIDs, label)
						for _, pending := range sources[index+1:] {
							result.RetrySourceIDs = append(result.RetrySourceIDs, packageSourceLabel(pending))
						}
						result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled while processing source %s: %v", label, ctx.Err()))
						break
					}
					result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s inline fallback failed: %v", uri, saveErr))
					result.Skipped++
					failed++
					result.FailedSourceIDs = append(result.FailedSourceIDs, label)
					result.RetrySourceIDs = append(result.RetrySourceIDs, label)
					linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "failed", saveErr.Error(), 0, itemWriteTimeout, &result.Warnings)
				} else {
					result.Imported++
					result.ImportedSourceIDs = append(result.ImportedSourceIDs, fallbackSource.ID)
					linkBatchItem(ctx, batchCreator, batchID, fallbackSource.ID, uri, "text", "imported", "", int64(len(content)), itemWriteTimeout, &result.Warnings)
				}
			} else {
				cancelSource()
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s skipped: re-fetch failed (%v), no inline content", uri, err))
				result.Skipped++
				failed++
				result.FailedSourceIDs = append(result.FailedSourceIDs, label)
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				linkBatchItem(ctx, batchCreator, batchID, "", uri, "url", "skipped", err.Error(), 0, itemWriteTimeout, &result.Warnings)
			}

		case content != "":
			if opts.DryRun {
				result.Imported++
				result.ImportedSourceIDs = append(result.ImportedSourceIDs, label)
				continue
			}
			if store == nil {
				errMsg := "knowledge store is unavailable"
				result.Warnings = append(result.Warnings, fmt.Sprintf("text source %s skipped: %s", label, errMsg))
				result.Skipped++
				failed++
				result.FailedSourceIDs = append(result.FailedSourceIDs, label)
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				linkBatchItem(ctx, batchCreator, batchID, "", label, "text", "failed", errMsg, int64(len(content)), itemWriteTimeout, &result.Warnings)
				continue
			}
			// Text source with inline content — import directly.
			sourceCtx, cancelSource := context.WithTimeout(ctx, perSourceTimeout)
			savedSource, err := store.SaveText(sourceCtx, TextSaveRequest{
				Text:      content,
				Title:     item.Title,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
				BatchID:   batchID,
			})
			cancelSource()
			if err != nil {
				if ctx.Err() != nil {
					cancelled = true
					result.RetrySourceIDs = append(result.RetrySourceIDs, label)
					for _, pending := range sources[index+1:] {
						result.RetrySourceIDs = append(result.RetrySourceIDs, packageSourceLabel(pending))
					}
					result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled while processing source %s: %v", label, ctx.Err()))
					break
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("text source %s skipped: %v", label, err))
				result.Skipped++
				failed++
				result.FailedSourceIDs = append(result.FailedSourceIDs, label)
				result.RetrySourceIDs = append(result.RetrySourceIDs, label)
				linkBatchItem(ctx, batchCreator, batchID, "", label, "text", "failed", err.Error(), int64(len(content)), itemWriteTimeout, &result.Warnings)
				continue
			}
			result.Imported++
			result.ImportedSourceIDs = append(result.ImportedSourceIDs, savedSource.ID)
			linkBatchItem(ctx, batchCreator, batchID, savedSource.ID, firstNonEmpty(item.Title, label), "text", "imported", "", int64(len(content)), itemWriteTimeout, &result.Warnings)

		default:
			// Metadata-only — no fetchable URI, no content.
			itemKind := strings.ToLower(strings.TrimSpace(item.Kind))
			if itemKind == "" {
				itemKind = "metadata"
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s source %s is metadata-only", itemKind, label))
			result.Skipped++
			result.SkippedSourceIDs = append(result.SkippedSourceIDs, label)
			linkBatchItem(ctx, batchCreator, batchID, "", label, itemKind, "skipped", "metadata-only, no content", 0, itemWriteTimeout, &result.Warnings)
		}
		if cancelled {
			break
		}
	}

	// --- After loop: update batch with final counts and derived status ---
	if batchFinalizer != nil && batchID != "" {
		now := time.Now().UTC()
		// result.Skipped includes both metadata-only skips and save failures.
		// Separate them for the batch: true skips vs actual failures.
		trueSkipped := result.Skipped - failed
		finalStatus := deriveImportBatchStatus(result.Imported, trueSkipped, failed, result.Total)
		if cancelled {
			finalStatus = "partial"
		}
		updateBatch := ImportBatch{
			ID:         batchID,
			Status:     finalStatus,
			TotalFiles: result.Total,
			Imported:   result.Imported,
			Skipped:    trueSkipped,
			Failed:     failed,
			UpdatedAt:  now,
		}
		updateCtx, cancel := detachedTimeoutContext(ctx, batchWriteTimeout)
		err := batchFinalizer.UpdateImportBatch(updateCtx, updateBatch)
		cancel()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("batch final update failed: %v", err))
		}
	}

	result.Failed = failed
	trueSkipped := result.Skipped - result.Failed
	result.Status = deriveImportBatchStatus(result.Imported, trueSkipped, result.Failed, result.Total)
	if cancelled {
		result.Status = "partial"
	}
	return result
}
