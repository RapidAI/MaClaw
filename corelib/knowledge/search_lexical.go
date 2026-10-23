package knowledge

import (
	"context"
	"strings"
)

// collectLexicalResults runs the FTS, table-row, and no-space LIKE scans.
// candidateLimit is the per-index FTS limit. likeLimit is the CJK LIKE limit;
// a non-positive value keeps opts.Limit. Table rows stay on opts.Limit.
func (s *SQLiteStore) collectLexicalResults(ctx context.Context, opts SearchOptions, ftsQuery string, candidateLimit, likeLimit int) ([]SearchResult, error) {
	results := make([]SearchResult, 0, candidateLimit)
	resultTypes := normalizeSearchResultTypes(opts.ResultTypes)
	seenCards := make(map[string]struct{})
	seenFacts := make(map[string]struct{})
	seenNodes := make(map[string]struct{})

	if wantsSearchType(resultTypes, "card") {
		cardWhere := []string{"knowledge_cards_fts MATCH ?", "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)", "COALESCE(c.invalid_at, '') = ''"}
		cardArgs := []interface{}{ftsQuery}
		cardWhere, cardArgs = appendSearchFilters(cardWhere, cardArgs, "s", opts)
		cardArgs = append(cardArgs, candidateLimit)
		cardQuery := `SELECT c.id, COALESCE(c.node_id, ''), c.title, c.claim, c.summary,
		COALESCE(n.title, ''), COALESCE(n.type, ''), COALESCE(n.page, 0), COALESCE(n.sheet_name, ''), COALESCE(n.row_range, ''), COALESCE(n.col_range, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(knowledge_cards_fts, 2, '', '', '...', 32), bm25(knowledge_cards_fts)
		FROM knowledge_cards_fts
		JOIN knowledge_cards c ON c.id = knowledge_cards_fts.card_id
		LEFT JOIN document_nodes n ON n.id = c.node_id
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE ` + strings.Join(cardWhere, " AND ") + ` ORDER BY bm25(knowledge_cards_fts) LIMIT ?`
		cardRows, err := s.db.QueryContext(ctx, cardQuery, cardArgs...)
		if err != nil {
			return nil, err
		}
		for cardRows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			var snippet string
			var rank float64
			if err := cardRows.Scan(&result.CardID, &result.NodeID, &result.CardTitle, &result.Claim, &result.Summary,
				&result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
				_ = cardRows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "card"
			result.Snippet = strings.TrimSpace(snippet)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			seenCards[result.CardID] = struct{}{}
			if result.NodeID != "" {
				seenNodes[source.ID+"\x00"+result.NodeID] = struct{}{}
			}
			results = append(results, result)
		}
		if err := cardRows.Close(); err != nil {
			return nil, err
		}
		if err := cardRows.Err(); err != nil {
			return nil, err
		}
		kbCardResults, err := s.searchKBTableCardsFTS(ctx, opts, candidateLimit)
		if err != nil {
			return nil, err
		}
		for _, result := range kbCardResults {
			if _, ok := seenCards[result.CardID]; ok {
				continue
			}
			seenCards[result.CardID] = struct{}{}
			results = append(results, result)
		}
	}

	if wantsSearchType(resultTypes, "fact") {
		factWhere := []string{"knowledge_facts_fts MATCH ?", "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)", "COALESCE(f.invalid_at, '') = ''"}
		factArgs := []interface{}{ftsQuery}
		factWhere, factArgs = appendSearchFilters(factWhere, factArgs, "s", opts)
		factArgs = append(factArgs, candidateLimit)
		factQuery := `SELECT f.id, f.card_id, f.subject, f.predicate, f.object, c.title, COALESCE(c.node_id, ''), c.claim, c.summary,
		COALESCE(n.title, ''), COALESCE(n.type, ''), COALESCE(n.page, 0), COALESCE(n.sheet_name, ''), COALESCE(n.row_range, ''), COALESCE(n.col_range, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		bm25(knowledge_facts_fts)
		FROM knowledge_facts_fts
		JOIN knowledge_facts f ON f.id = knowledge_facts_fts.fact_id
		JOIN knowledge_cards c ON c.id = f.card_id
		LEFT JOIN document_nodes n ON n.id = c.node_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE ` + strings.Join(factWhere, " AND ") + ` ORDER BY bm25(knowledge_facts_fts) LIMIT ?`
		factRows, err := s.db.QueryContext(ctx, factQuery, factArgs...)
		if err != nil {
			return nil, err
		}
		for factRows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			var rank float64
			if err := factRows.Scan(&result.FactID, &result.CardID, &result.Subject, &result.Predicate, &result.Object, &result.CardTitle, &result.NodeID, &result.Claim, &result.Summary,
				&result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &rank); err != nil {
				_ = factRows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "fact"
			result.Snippet = strings.TrimSpace(result.Subject + " " + result.Predicate + " " + result.Object)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			seenFacts[result.FactID] = struct{}{}
			seenCards[result.CardID] = struct{}{}
			if result.NodeID != "" {
				seenNodes[source.ID+"\x00"+result.NodeID] = struct{}{}
			}
			results = append(results, result)
		}
		if err := factRows.Close(); err != nil {
			return nil, err
		}
		if err := factRows.Err(); err != nil {
			return nil, err
		}
		kbFactResults, err := s.searchKBTableFactsFTS(ctx, opts, candidateLimit)
		if err != nil {
			return nil, err
		}
		for _, result := range kbFactResults {
			if _, ok := seenFacts[result.FactID]; ok {
				continue
			}
			seenFacts[result.FactID] = struct{}{}
			results = append(results, result)
		}
	}

	if wantsSearchType(resultTypes, "node") {
		nodeWhere := []string{"document_nodes_fts MATCH ?"}
		nodeArgs := []interface{}{ftsQuery}
		nodeWhere, nodeArgs = appendSearchFilters(nodeWhere, nodeArgs, "s", opts)
		nodeArgs = append(nodeArgs, candidateLimit)
		nodeQuery := `SELECT n.id, n.title, n.type, n.page, n.sheet_name, n.row_range, n.col_range,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(document_nodes_fts, 2, '', '', '...', 32), bm25(document_nodes_fts)
		FROM document_nodes_fts
		JOIN document_nodes n ON n.id = document_nodes_fts.node_id
		JOIN knowledge_sources s ON s.id = n.source_id
		WHERE ` + strings.Join(nodeWhere, " AND ") + ` ORDER BY bm25(document_nodes_fts) LIMIT ?`
		nodeRows, err := s.db.QueryContext(ctx, nodeQuery, nodeArgs...)
		if err != nil {
			return nil, err
		}
		defer nodeRows.Close()
		for nodeRows.Next() {
			var result SearchResult
			var snippet string
			var rank float64
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			if err := nodeRows.Scan(&result.NodeID, &result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			if _, ok := seenNodes[source.ID+"\x00"+result.NodeID]; ok {
				continue
			}
			result.Source = source
			result.ResultType = "node"
			result.Snippet = strings.TrimSpace(snippet)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			results = append(results, result)
		}
		if err := nodeRows.Err(); err != nil {
			return nil, err
		}
	}

	if wantsSearchType(resultTypes, "table_row") {
		rowResults, rowErr := s.searchTableRowsFTS(ctx, opts)
		if rowErr == nil && len(rowResults) > 0 {
			seenRows := make(map[string]struct{})
			for _, result := range results {
				if result.ResultType == "table_row" && result.RowID != "" {
					seenRows[result.RowID] = struct{}{}
				}
			}
			for _, result := range rowResults {
				if _, ok := seenRows[result.RowID]; ok {
					continue
				}
				seenRows[result.RowID] = struct{}{}
				results = append(results, result)
			}
		}
	}

	// FTS fallback for languages whose scripts do not consistently expose word
	// boundaries to SQLite unicode61 (CJK, Japanese, Korean, and Thai).
	// This handles three cases:
	// 1. FTS index not yet rebuilt with segmentation (rebuild pending/failed)
	// 2. FTS tokenization mismatch (new words not in gse dictionary)
	// 3. FTS found card/fact results but missed relevant document_nodes content
	//    (distillation loss: cards/facts may not cover all original document text)
	if containsNoSpaceScriptRunes(opts.Query) && !strictAnchorsAlreadyCovered(opts.Query, results) {
		// For no-space scripts, run LIKE fallback to search document_nodes original
		// text when lexical FTS has not already covered a composite name. FTS
		// tokenization mismatch means FTS may find some nodes (via "马勇"
		// matching page 1) but miss others (page 2 has "书籍" which doesn't match
		// query token "书"). LIKE handles arbitrary substrings correctly.
		// Performance: O(nodes × terms) string matching. For typical knowledge bases
		// (<2000 nodes, <12 terms), this is <50ms — acceptable tradeoff for recall.
		likeOpts := opts
		if likeLimit > 0 {
			likeOpts.Limit = likeLimit
		}
		likeResults, likeErr := s.searchCJKLikeFallback(ctx, likeOpts)
		if likeErr == nil && len(likeResults) > 0 {
			// Merge LIKE results into FTS results. IDs may be supplied by callers and
			// are unique only within their entity table, so namespace them before
			// deduplicating: a card ID must never suppress an unrelated fact ID.
			seen := make(map[string]struct{})
			for _, r := range results {
				markSearchResultIDs(seen, r)
			}
			for _, lr := range likeResults {
				if !searchResultIDsSeen(seen, lr) {
					results = append(results, lr)
					markSearchResultIDs(seen, lr)
				}
			}
		}
	}
	return results, nil
}
