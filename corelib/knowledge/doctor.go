package knowledge

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func (s *SQLiteStore) Doctor(ctx context.Context) (DoctorResult, error) {
	stats, err := s.Stats(ctx)
	if err != nil {
		return DoctorResult{}, err
	}
	result := DoctorResult{
		Status:      "ok",
		Score:       100,
		Stats:       stats,
		GeneratedAt: time.Now().UTC(),
	}
	addFinding := func(finding DoctorFinding) {
		result.Findings = append(result.Findings, finding)
		switch finding.Severity {
		case "error":
			result.Score -= 30
			result.Status = "error"
		case "warning":
			result.Score -= 15
			if result.Status != "error" {
				result.Status = "warning"
			}
		default:
			result.Score -= 3
		}
	}
	add := func(severity, code, title, detail string, count int, action string) {
		addFinding(DoctorFinding{
			Severity: severity,
			Code:     code,
			Title:    title,
			Detail:   detail,
			Count:    count,
			Action:   action,
		})
	}
	addSourceFinding := func(severity, code, title, detail string, count int, action, where string, args ...interface{}) {
		finding := DoctorFinding{
			Severity: severity,
			Code:     code,
			Title:    title,
			Detail:   detail,
			Count:    count,
			Action:   action,
		}
		ids, examples, err := s.doctorSourceRefs(ctx, where, args...)
		if err == nil {
			finding.SourceIDs = ids
			finding.Examples = examples
		}
		finding.Filter = doctorFindingFilter(finding.Code, finding.SourceIDs)
		addFinding(finding)
	}

	if stats.Sources == 0 {
		add("info", "empty_knowledge_base", "No saved knowledge sources", "The knowledge base is initialized but has no imported documents or saved URLs yet.", 0, "Save a public URL or import local documents from the Knowledge Base settings tab.")
	}
	if policies, err := s.ListURLDomainPolicies(ctx); err != nil {
		add("warning", "url_domain_policy_check_failed", "URL domain policy check failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if len(policies) > 0 {
		allowCount, blockCount := 0, 0
		examples := make([]string, 0, 5)
		for _, policy := range policies {
			switch policy.Action {
			case URLDomainActionAllow:
				allowCount++
			case URLDomainActionBlock:
				blockCount++
			}
			examples = appendLimited(examples, policy.Action+":"+policy.Domain, 5)
		}
		addFinding(DoctorFinding{
			Severity: "info",
			Code:     "url_domain_policies_active",
			Title:    "URL domain policies are active",
			Detail:   fmt.Sprintf("%d allow policies and %d block policies govern public URL saves. Block rules take priority; if allow policies exist, unmatched domains are denied.", allowCount, blockCount),
			Count:    len(policies),
			Action:   "Review the Knowledge Base URL domain policy settings before bulk saving web pages from new domains.",
			Examples: examples,
		})
	}
	if count := stats.SourcesByStatus[StatusFailed]; count > 0 {
		addSourceFinding("error", "failed_sources", "Some sources failed to parse or distill", "Failed sources will not be fully searchable until refreshed or re-imported.", count, "Open the source list filtered by failed status and inspect the error message.", "s.status = ?", StatusFailed)
	}
	if count := stats.SourcesByStatus[StatusPending]; count > 0 {
		addSourceFinding("warning", "pending_sources", "Some sources are still pending", "Pending sources may have metadata but no searchable cards or facts yet.", count, "Refresh URL sources or re-run document import if they stay pending.", "s.status = ?", StatusPending)
	}
	if count := stats.SourcesByStatus[StatusStale]; count > 0 {
		addSourceFinding("warning", "stale_sources", "Some sources are stale", "Stale sources may reflect an older URL/document version.", count, "Refresh URL sources or re-import changed documents when the user asks for current knowledge.", "s.status = ?", StatusStale)
	}
	if count := stats.SourcesByStatus[StatusDisabled]; count > 0 {
		addSourceFinding("info", "disabled_sources", "Some sources are disabled", "Disabled sources are skipped by default search.", count, "Keep them disabled for archival use or delete them if they are no longer useful.", "s.status = ?", StatusDisabled)
	}
	unlabeledWhere := "s.status <> ? AND NOT EXISTS (SELECT 1 FROM knowledge_source_labels sl WHERE sl.source_id = s.id)"
	if count, err := s.doctorSourceCount(ctx, unlabeledWhere, StatusDisabled); err != nil {
		add("warning", "source_label_check_failed", "Source label diagnostic failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if count > 0 {
		addSourceFinding("info", "unlabeled_sources", "Some active sources have no labels", "Unlabeled sources are still searchable, but labels make large knowledge bases easier to browse, filter, and govern.", count, "Add labels manually, use bulk label updates, or enable auto labels during future imports.", unlabeledWhere, StatusDisabled)
	}
	drift, err := s.localFileDrift(ctx)
	if err != nil {
		add("warning", "local_file_drift_check_failed", "Local file drift check failed", err.Error(), 1, "Retry diagnostics after checking file permissions.")
	} else {
		if drift.Missing > 0 {
			addFinding(DoctorFinding{
				Severity:  "warning",
				Code:      "missing_local_files",
				Title:     "Some imported local files are missing",
				Detail:    "The knowledge base still contains these sources, but refresh cannot rebuild them until the files are restored.",
				Count:     drift.Missing,
				Action:    "Restore the files, disable the sources, or delete sources that are no longer useful.",
				SourceIDs: drift.MissingSourceIDs,
				Examples:  drift.MissingExamples,
				Filter:    doctorFindingFilter("missing_local_files", drift.MissingSourceIDs),
			})
		}
		if drift.Changed > 0 {
			addFinding(DoctorFinding{
				Severity:  "info",
				Code:      "changed_local_files",
				Title:     "Some imported local files changed on disk",
				Detail:    "The stored knowledge may be behind the current file contents until refreshed or re-imported.",
				Count:     drift.Changed,
				Action:    "Refresh affected sources or re-import the directory to rebuild local nodes/cards/facts.",
				SourceIDs: drift.ChangedSourceIDs,
				Examples:  drift.ChangedExamples,
				Filter:    doctorFindingFilter("changed_local_files", drift.ChangedSourceIDs),
			})
		}
		if drift.Inaccessible > 0 {
			addFinding(DoctorFinding{
				Severity:  "warning",
				Code:      "inaccessible_local_files",
				Title:     "Some imported local files are inaccessible",
				Detail:    "Permissions, symlinks, or non-regular files prevented local freshness checks.",
				Count:     drift.Inaccessible,
				Action:    "Check file permissions and import regular files directly when needed.",
				SourceIDs: drift.InaccessibleSourceIDs,
				Examples:  drift.InaccessibleExamples,
				Filter:    doctorFindingFilter("inaccessible_local_files", drift.InaccessibleSourceIDs),
			})
		}
	}
	legacyCount := stats.SourcesByKind[SourceKindDOC] + stats.SourcesByKind[SourceKindXLS]
	if legacyCount > 0 {
		addSourceFinding("info", "legacy_office_native", "Legacy Office files parsed natively", ".doc/.xls files are parsed using the built-in pure-Go reader.", legacyCount, "No external tools required.", "s.kind IN (?, ?)", SourceKindDOC, SourceKindXLS)
	}
	pdfOCRWhere := "s.kind = ? AND s.status IN (?, ?, ?) AND NOT EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id AND length(trim(COALESCE(n.text, ''))) >= 40)"
	if count, err := s.doctorSourceCount(ctx, pdfOCRWhere, SourceKindPDF, StatusParsed, StatusDistilled, StatusStale); err != nil {
		add("warning", "pdf_ocr_check_failed", "PDF OCR diagnostic failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if count > 0 {
		addSourceFinding("warning", "pdf_ocr_needed", "Some PDFs appear to need OCR", "These PDF sources have no meaningful extracted text nodes, which usually means they are scanned/image PDFs or the embedded text is not readable.", count, "Run OCR outside MaClaw or import an OCR/text PDF version, then refresh or re-import the affected sources.", pdfOCRWhere, SourceKindPDF, StatusParsed, StatusDistilled, StatusStale)
	}
	if stats.Sources > 0 && stats.Cards == 0 {
		add("warning", "no_cards", "No knowledge cards were distilled", "Search can fall back to source nodes, but card-first recall will be weak.", stats.Sources, "Check parser failures and write-time distillation settings.")
	}
	if stats.Cards > 0 && stats.Facts == 0 {
		add("info", "no_facts", "No structured facts extracted yet", "Card recall works, but entity-relation recall has no facts to use.", stats.Cards, "Rebuild cards and facts from existing parsed nodes to backfill local relation recall.")
	}
	if stats.SourcesWithoutNodes > 0 {
		addSourceFinding("warning", "sources_without_nodes", "Some active sources have no parsed nodes", "These sources are active but have no source nodes, so node fallback search and citation detail will be weak.", stats.SourcesWithoutNodes, "Open source details, inspect parser errors, then refresh URL sources or re-import documents.", "s.status IN (?, ?, ?) AND NOT EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id)", StatusParsed, StatusDistilled, StatusStale)
	}
	missingCardsWithNodesWhere := "s.status IN (?, ?, ?) AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id) AND NOT EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = s.id)"
	if count, err := s.doctorSourceCount(ctx, missingCardsWithNodesWhere, StatusParsed, StatusDistilled, StatusStale); err != nil {
		add("warning", "sources_without_cards_check_failed", "Card coverage diagnostic failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if count > 0 {
		addSourceFinding("warning", "sources_without_cards", "Some active sources have no knowledge cards", "These sources can only be recalled through raw source nodes, not card-first local search.", count, "Rebuild cards and facts from existing parsed nodes to backfill card-first local recall.", missingCardsWithNodesWhere, StatusParsed, StatusDistilled, StatusStale)
	}
	missingFactsWithNodesWhere := "s.status IN (?, ?) AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id) AND NOT EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = s.id)"
	if count, err := s.doctorSourceCount(ctx, missingFactsWithNodesWhere, StatusDistilled, StatusStale); err != nil {
		add("warning", "sources_without_facts_check_failed", "Fact coverage diagnostic failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if count > 0 {
		addSourceFinding("info", "sources_without_facts", "Some distilled sources have no structured facts", "Card recall works, but relation-style recall has no facts for these sources.", count, "Rebuild cards and facts from existing parsed nodes to backfill local relation recall.", missingFactsWithNodesWhere, StatusDistilled, StatusStale)
	}
	if stats.Sources > 1 && stats.SourcesWithoutLinks > 0 {
		addSourceFinding("info", "sources_without_links", "Some active sources have no topic links", "These sources are searchable, but the external-brain source graph has not connected them to related material yet.", stats.SourcesWithoutLinks, "Refresh topic links for affected sources or run a filtered topic-link rebuild after bulk imports.", "s.status IN (?, ?, ?) AND NOT EXISTS (SELECT 1 FROM knowledge_source_links l WHERE l.source_id = s.id)", StatusParsed, StatusDistilled, StatusStale)
	}
	if stats.Sources > 2 && stats.SourceLinks > 0 {
		graph, err := s.SourceGraph(ctx, ListSourcesOptions{Limit: 500}, 2000)
		if err != nil {
			add("warning", "source_graph_check_failed", "Source graph diagnostic failed", err.Error(), 1, "Retry diagnostics after refreshing topic links or checking the local knowledge database.")
		} else {
			if graph.ComponentCount > 1 && graph.LargestComponentSize < graph.Count {
				sourceIDs, examples := doctorSourceGraphFragmentRefs(graph, false)
				severity := "info"
				if graph.LargestComponentSize*2 < graph.Count {
					severity = "warning"
				}
				addFinding(DoctorFinding{
					Severity:  severity,
					Code:      "source_graph_fragmented",
					Title:     "Source graph is fragmented",
					Detail:    fmt.Sprintf("The local source graph has %d connected components. The largest component contains %d of %d sources, so some saved knowledge may not be connected to nearby material yet.", graph.ComponentCount, graph.LargestComponentSize, graph.Count),
					Count:     graph.ComponentCount,
					Action:    "Refresh topic links for smaller components or run filtered source graph inspection to decide which knowledge clusters should be connected.",
					SourceIDs: sourceIDs,
					Examples:  examples,
					Filter:    doctorFindingFilter("source_graph_fragmented", sourceIDs),
				})
			}
			if len(graph.Isolates) > 0 {
				sourceIDs, examples := doctorSourceGraphFragmentRefs(graph, true)
				severity := "info"
				if len(graph.Isolates)*5 >= graph.Count {
					severity = "warning"
				}
				addFinding(DoctorFinding{
					Severity:  severity,
					Code:      "source_graph_isolates",
					Title:     "Some sources are isolated in the source graph",
					Detail:    "Isolated sources are searchable, but they have no persisted topic-related edges to other sources, which weakens graph navigation and cross-source recall diagnostics.",
					Count:     len(graph.Isolates),
					Action:    "Refresh topic links for isolated sources after bulk imports, metadata edits, or label backfills.",
					SourceIDs: sourceIDs,
					Examples:  examples,
					Filter:    doctorFindingFilter("source_graph_isolates", sourceIDs),
				})
			}
		}
	}
	sensitive, err := s.ScanSensitiveContent(ctx, 20)
	if err != nil {
		add("warning", "sensitive_scan_failed", "Sensitive-content scan failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if sensitive.Count > 0 {
		examples := make([]string, 0, len(sensitive.Findings))
		sourceIDs := make([]string, 0, len(sensitive.Findings))
		for _, finding := range sensitive.Findings {
			examples = appendUniqueLimited(examples, firstNonEmpty(finding.RelativePath, finding.SourceTitle, finding.URI, finding.Kind), 5)
			sourceIDs = appendUniqueLimited(sourceIDs, finding.SourceID, 10)
		}
		severity := "warning"
		if sensitive.MaxSeverity == "error" {
			severity = "error"
		}
		addFinding(DoctorFinding{
			Severity:  severity,
			Code:      "possible_sensitive_content",
			Title:     "Possible sensitive content was indexed",
			Detail:    "Local rules found possible secrets, tokens, passwords, or private keys in stored knowledge. Matches are redacted in diagnostics.",
			Count:     sensitive.Count,
			Action:    "Use knowledge_scan_sensitive to inspect redacted findings, then disable or delete affected sources if needed.",
			SourceIDs: sourceIDs,
			Examples:  examples,
			Filter:    doctorFindingFilter("possible_sensitive_content", sourceIDs),
		})
	}
	duplicateGroups, err := s.ListDuplicateCards(ctx, 10)
	if err != nil {
		add("warning", "duplicate_card_check_failed", "Duplicate card check failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else if len(duplicateGroups) > 0 {
		examples := make([]string, 0, len(duplicateGroups))
		sourceIDs := make([]string, 0)
		for _, group := range duplicateGroups {
			examples = appendLimited(examples, group.Claim, 5)
			for _, sourceID := range group.SourceIDs {
				sourceIDs = appendLimited(sourceIDs, sourceID, 10)
			}
		}
		addFinding(DoctorFinding{
			Severity:  "info",
			Code:      "duplicate_card_claims",
			Title:     "Some knowledge cards repeat the same claim",
			Detail:    "Repeated claims are searchable, but large imports may benefit from review or later merge.",
			Count:     len(duplicateGroups),
			Action:    "Use knowledge_list_duplicate_cards to inspect repeated claims before deciding whether to merge or remove sources.",
			SourceIDs: sourceIDs,
			Examples:  examples,
			Filter:    doctorFindingFilter("duplicate_card_claims", sourceIDs),
		})
	}
	quality, err := s.SourceQualityReport(ctx, ListSourcesOptions{Limit: 100})
	if err != nil {
		add("warning", "source_quality_check_failed", "Source quality check failed", err.Error(), 1, "Retry diagnostics after checking the local knowledge database.")
	} else {
		examples := make([]string, 0)
		sourceIDs := make([]string, 0)
		for _, item := range quality.Items {
			if item.Score >= 55 {
				continue
			}
			sourceIDs = appendUniqueLimited(sourceIDs, item.Source.ID, 20)
			example := firstNonEmpty(item.Source.Title, item.Source.RelativePath, item.Source.CanonicalURI, item.Source.URI, item.Source.ID)
			if example != "" {
				examples = appendUniqueLimited(examples, fmt.Sprintf("%s:%d", example, item.Score), 5)
			}
		}
		if len(sourceIDs) > 0 {
			addFinding(DoctorFinding{
				Severity:  "warning",
				Code:      "low_quality_sources",
				Title:     "Some sources have low local quality scores",
				Detail:    "Local scoring found sources with weak coverage, missing labels, low trust, duplicate claims, sensitive signals, or unhealthy statuses.",
				Count:     len(sourceIDs),
				Action:    "Use knowledge_source_quality to inspect source-level signals, then refresh, rebuild, label, suppress duplicates, disable, or delete as appropriate.",
				SourceIDs: sourceIDs,
				Examples:  examples,
				Filter:    doctorFindingFilter("low_quality_sources", sourceIDs),
			})
		}
	}
	if count := stats.BatchesByStatus[ImportStatusFailed]; count > 0 {
		add("error", "failed_import_batches", "Some import batches failed", "A failed batch may leave documents unprocessed.", count, "Open recent imports and inspect batch items.")
	}
	if count := stats.BatchesByStatus[ImportStatusRunning]; count > 0 {
		add("info", "running_import_batches", "Import batches are still running", "Search results may change as more files finish processing.", count, "Wait for import status to complete before evaluating coverage.")
	}
	if count := stats.ImportItemsByStatus[ItemStatusFailed]; count > 0 {
		add("error", "failed_import_items", "Some files failed during import", "Failed files are not searchable until fixed and re-imported.", count, "Open the import batch items and inspect file-level errors.")
	}
	if count := stats.ImportItemsByStatus[ItemStatusSkippedType]; count > 0 {
		add("warning", "unsupported_file_types", "Some files were skipped due to unsupported type", "Skipped unsupported files are not stored as searchable knowledge.", count, "Convert them to docx, pdf, xlsx, csv, markdown, or txt before importing.")
	}
	if count := stats.ImportItemsByStatus[ItemStatusSkippedTooLarge]; count > 0 {
		add("warning", "too_large_files", "Some files exceeded the import size limit", "Large files were skipped to protect local resources.", count, "Increase max file size intentionally or split the files before import.")
	}
	if count := stats.ImportItemsByStatus[ItemStatusSkippedSymlink]; count > 0 {
		add("info", "symlink_skipped", "Some symlinks were skipped", "Directory import avoids following symlinks by default for safer local scans.", count, "Import target files directly if the user explicitly approves them.")
	}
	if count := stats.ImportItemsByStatus[ItemStatusSkippedDuplicate]; count > 0 {
		add("info", "duplicate_files", "Duplicate files were skipped", "Hash de-duplication avoided storing the same content multiple times.", count, "No action is needed unless the duplicate should belong to a different project scope.")
	}

	// --- Image asset health checks ---
	if s.imageAssets != nil {
		imageSourceWhere := "s.kind = ? AND s.status IN (?, ?, ?)"
		imageSourceCount, _ := s.doctorSourceCount(ctx, imageSourceWhere, SourceKindImage, StatusParsed, StatusDistilled, StatusStale)
		if imageSourceCount > 0 {
			// Check for missing original image files
			missingAssets := 0
			missingThumbCount := 0
			rows, err := s.db.QueryContext(ctx, `SELECT n.source_id, n.metadata FROM document_nodes n WHERE n.type = ?`, NodeTypeImage)
			if err == nil {
				for rows.Next() {
					var sourceID, metadataJSON string
					if err := rows.Scan(&sourceID, &metadataJSON); err != nil {
						continue
					}
					// Check asset path exists
					assetPath := extractMetadataValue(metadataJSON, MetaImageAssetPath)
					if assetPath != "" {
						if _, err := os.Stat(assetPath); os.IsNotExist(err) {
							missingAssets++
						}
					}
					// Check thumbnail exists
					thumbPath := s.imageAssets.ThumbPath(sourceID)
					if _, err := os.Stat(thumbPath); os.IsNotExist(err) {
						missingThumbCount++
					}
				}
				rows.Close()
			}
			if missingAssets > 0 {
				add("warning", "missing_image_assets", "Some image assets are missing from disk",
					"Image source entries exist in the database but the original image files were deleted or moved externally.",
					missingAssets, "Re-import the images or delete the affected sources.")
			}
			if missingThumbCount > 0 {
				add("info", "missing_image_thumbnails", "Some image thumbnails are missing",
					"Thumbnails can be regenerated from original images.",
					missingThumbCount, "Run knowledge maintenance or re-import to regenerate thumbnails.")
			}

			// Check for image nodes without description (OCR/Vision not run)
			emptyDescWhere := "n.type = ? AND (n.text IS NULL OR length(trim(n.text)) < 10)"
			var emptyDescCount int
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes n WHERE `+emptyDescWhere, NodeTypeImage).Scan(&emptyDescCount)
			if emptyDescCount > 0 {
				add("info", "images_without_description", "Some images have no description",
					"These images were imported but OCR/Vision description was not generated. They are searchable only by filename and context.",
					emptyDescCount, "Configure Vision LLM or ensure the built-in OCR engine is available, then re-process affected images.")
			}
		}
	}

	if result.Score < 0 {
		result.Score = 0
	}
	if len(result.Findings) == 0 {
		result.Findings = []DoctorFinding{{
			Severity: "info",
			Code:     "healthy",
			Title:    "Knowledge base looks healthy",
			Detail:   "No failed, pending, stale, or unsupported import signals were found in local diagnostics.",
		}}
	}
	return result, nil
}

type localFileDrift struct {
	Missing               int
	Changed               int
	Inaccessible          int
	MissingSourceIDs      []string
	ChangedSourceIDs      []string
	InaccessibleSourceIDs []string
	MissingExamples       []string
	ChangedExamples       []string
	InaccessibleExamples  []string
}

func (s *SQLiteStore) localFileDrift(ctx context.Context) (localFileDrift, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, uri, content_hash FROM knowledge_sources WHERE status <> ?`, StatusDisabled)
	if err != nil {
		return localFileDrift{}, err
	}
	defer rows.Close()
	var drift localFileDrift
	for rows.Next() {
		var id, kind, uri, contentHash string
		if err := rows.Scan(&id, &kind, &uri, &contentHash); err != nil {
			return drift, err
		}
		if !isRefreshableFileSource(kind) || uri == "" {
			continue
		}
		info, err := os.Stat(uri)
		if err != nil {
			if os.IsNotExist(err) {
				drift.Missing++
				drift.MissingSourceIDs = appendLimited(drift.MissingSourceIDs, id, 10)
				drift.MissingExamples = appendLimited(drift.MissingExamples, uri, 5)
			} else {
				drift.Inaccessible++
				drift.InaccessibleSourceIDs = appendLimited(drift.InaccessibleSourceIDs, id, 10)
				drift.InaccessibleExamples = appendLimited(drift.InaccessibleExamples, uri, 5)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			drift.Inaccessible++
			drift.InaccessibleSourceIDs = appendLimited(drift.InaccessibleSourceIDs, id, 10)
			drift.InaccessibleExamples = appendLimited(drift.InaccessibleExamples, uri, 5)
			continue
		}
		hash, err := fileSHA256(uri)
		if err != nil {
			drift.Inaccessible++
			drift.InaccessibleSourceIDs = appendLimited(drift.InaccessibleSourceIDs, id, 10)
			drift.InaccessibleExamples = appendLimited(drift.InaccessibleExamples, uri, 5)
			continue
		}
		if contentHash != "" && hash != contentHash {
			drift.Changed++
			drift.ChangedSourceIDs = appendLimited(drift.ChangedSourceIDs, id, 10)
			drift.ChangedExamples = appendLimited(drift.ChangedExamples, uri, 5)
		}
	}
	return drift, rows.Err()
}

func (s *SQLiteStore) doctorSourceRefs(ctx context.Context, where string, args ...interface{}) ([]string, []string, error) {
	if where == "" {
		where = "1=1"
	}
	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, 10)
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, COALESCE(s.title, ''), COALESCE(s.relative_path, ''), COALESCE(s.canonical_uri, ''), COALESCE(s.uri, '')
		FROM knowledge_sources s WHERE `+where+` ORDER BY s.updated_at DESC LIMIT ?`, queryArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 10)
	examples := make([]string, 0, 5)
	for rows.Next() {
		var id, title, relativePath, canonicalURI, uri string
		if err := rows.Scan(&id, &title, &relativePath, &canonicalURI, &uri); err != nil {
			return ids, examples, err
		}
		ids = appendLimited(ids, id, 10)
		example := firstNonEmpty(title, relativePath, canonicalURI, uri, id)
		examples = appendLimited(examples, example, 5)
	}
	return ids, examples, rows.Err()
}

func (s *SQLiteStore) doctorSourceCount(ctx context.Context, where string, args ...interface{}) (int, error) {
	if where == "" {
		where = "1=1"
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources s WHERE `+where, args...).Scan(&count)
	return count, err
}

func doctorSourceGraphFragmentRefs(graph SourceGraphResult, isolatesOnly bool) ([]string, []string) {
	sourceIDs := make([]string, 0)
	examples := make([]string, 0)
	if isolatesOnly {
		for _, node := range graph.Isolates {
			sourceIDs = appendUniqueLimited(sourceIDs, node.ID, 20)
			examples = appendUniqueLimited(examples, firstNonEmpty(node.Label, node.RelativePath, node.URI, node.ID), 5)
		}
		return sourceIDs, examples
	}
	largestComponentID := 0
	if len(graph.Components) > 0 {
		largestComponentID = graph.Components[0].ID
	}
	for _, node := range graph.Nodes {
		if node.ComponentID == 0 || node.ComponentID == largestComponentID {
			continue
		}
		sourceIDs = appendUniqueLimited(sourceIDs, node.ID, 20)
		examples = appendUniqueLimited(examples, fmt.Sprintf("component:%d %s", node.ComponentID, firstNonEmpty(node.Label, node.RelativePath, node.URI, node.ID)), 5)
	}
	return sourceIDs, examples
}

func doctorFindingFilter(code string, sourceIDs []string) *ListSourcesOptions {
	filter := ListSourcesOptions{Limit: 5000, IncludeDisabled: true}
	switch code {
	case "failed_sources":
		filter.Status = StatusFailed
	case "pending_sources":
		filter.Status = StatusPending
	case "stale_sources":
		filter.Status = StatusStale
	case "disabled_sources":
		filter.Status = StatusDisabled
	case "legacy_office_converter_available", "legacy_office_sources":
		filter.SourceKinds = []string{SourceKindDOC, SourceKindXLS}
	case "pdf_ocr_needed":
		filter.CoverageFilter = "pdf_ocr_needed"
	case "sources_without_nodes":
		filter.CoverageFilter = "missing_nodes"
	case "sources_without_cards":
		filter.CoverageFilter = "missing_cards"
	case "sources_without_facts":
		filter.CoverageFilter = "missing_facts"
	case "sources_without_links":
		filter.CoverageFilter = "missing_links"
	case "unlabeled_sources":
		filter.CoverageFilter = "missing_labels"
	case "missing_local_files", "changed_local_files", "inaccessible_local_files", "possible_sensitive_content", "duplicate_card_claims", "low_quality_sources", "source_graph_fragmented", "source_graph_isolates":
		filter.SourceIDs = append([]string(nil), sourceIDs...)
	default:
		if len(sourceIDs) == 0 {
			return nil
		}
		filter.SourceIDs = append([]string(nil), sourceIDs...)
	}
	if filter.Status == "" && filter.CoverageFilter == "" && len(filter.SourceKinds) == 0 && len(filter.SourceIDs) == 0 {
		return nil
	}
	return &filter
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func appendLimited(values []string, value string, limit int) []string {
	if value == "" || len(values) >= limit {
		return values
	}
	return append(values, value)
}

// extractMetadataValue extracts a key's value from a JSON metadata string.
// Used for quick field extraction without full JSON unmarshal.
func extractMetadataValue(metadataJSON, key string) string {
	// Simple string search for "key":"value" pattern.
	searchKey := `"` + key + `":"`
	idx := strings.Index(metadataJSON, searchKey)
	if idx < 0 {
		return ""
	}
	start := idx + len(searchKey)
	end := strings.Index(metadataJSON[start:], `"`)
	if end < 0 {
		return ""
	}
	return metadataJSON[start : start+end]
}
