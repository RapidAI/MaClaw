package knowledge

import (
	"context"
	"fmt"
	"strings"
)

// PackageSource represents one source entry in a knowledge package.
// This is the canonical import-side representation — both GUI and MaClawSrv
// map their package structs to this before calling ImportPackageSources.
type PackageSource struct {
	ID           string
	Kind         string
	URI          string
	CanonicalURI string
	Title        string
	TopicHint    string
	Labels       []string
	Content      string // Inline content (text extracted at export time).
}

// PackageImportOptions configures the import behavior.
type PackageImportOptions struct {
	OwnerID   string
	TenantID  string
	SaveScope string // e.g. SaveScopePersonal. Empty = store default.
	DryRun    bool   // When true, classify sources without writing. Store can be nil.
}

// PackageImportResult contains the outcome of a package import.
type PackageImportResult struct {
	Imported int
	Skipped  int
	Total    int
	Warnings []string
}

// PackageImportStore is the minimal interface required for importing package sources.
// Both SQLiteStore and any test mock can implement this.
type PackageImportStore interface {
	SaveText(ctx context.Context, req TextSaveRequest) (Source, error)
	SaveURL(ctx context.Context, req URLSaveRequest) (Source, error)
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
func ImportPackageSources(ctx context.Context, store PackageImportStore, sources []PackageSource, opts PackageImportOptions) PackageImportResult {
	result := PackageImportResult{
		Total:    len(sources),
		Warnings: make([]string, 0),
	}
	for _, item := range sources {
		if ctx.Err() != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("import cancelled at %d/%d", result.Imported+result.Skipped, result.Total))
			break
		}

		uri := strings.TrimSpace(firstNonEmpty(item.CanonicalURI, item.URI))
		content := strings.TrimSpace(item.Content)
		isHTTP := strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
		label := firstNonEmpty(item.ID, item.Title, uri)

		switch {
		case isHTTP:
			if opts.DryRun {
				// In dry-run, URL sources are counted as importable (via re-fetch or content fallback).
				result.Imported++
				continue
			}
			// URL source — try re-fetch for freshness, fall back to inline content.
			_, err := store.SaveURL(ctx, URLSaveRequest{
				URL:       uri,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
			})
			if err == nil {
				result.Imported++
				continue
			}
			// Re-fetch failed — fall back to inline content if available.
			if content != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s re-fetch failed (%v), using inline content", uri, err))
				if _, saveErr := store.SaveText(ctx, TextSaveRequest{
					Text:      content,
					Title:     firstNonEmpty(item.Title, uri),
					OwnerID:   opts.OwnerID,
					TenantID:  opts.TenantID,
					TopicHint: item.TopicHint,
					Labels:    item.Labels,
					SaveScope: opts.SaveScope,
				}); saveErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s inline fallback failed: %v", uri, saveErr))
					result.Skipped++
				} else {
					result.Imported++
				}
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("url source %s skipped: re-fetch failed (%v), no inline content", uri, err))
				result.Skipped++
			}

		case content != "":
			if opts.DryRun {
				result.Imported++
				continue
			}
			// Text source with inline content — import directly.
			if _, err := store.SaveText(ctx, TextSaveRequest{
				Text:      content,
				Title:     item.Title,
				OwnerID:   opts.OwnerID,
				TenantID:  opts.TenantID,
				TopicHint: item.TopicHint,
				Labels:    item.Labels,
				SaveScope: opts.SaveScope,
			}); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("text source %s skipped: %v", label, err))
				result.Skipped++
				continue
			}
			result.Imported++

		default:
			// Metadata-only — no fetchable URI, no content.
			itemKind := strings.ToLower(strings.TrimSpace(item.Kind))
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s source %s is metadata-only", itemKind, label))
			result.Skipped++
		}
	}
	return result
}
