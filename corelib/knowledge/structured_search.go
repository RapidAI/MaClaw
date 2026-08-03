package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func (s *SQLiteStore) searchTableRowsByEmbedding(ctx context.Context, opts StructuredSearchOptions, limit int) ([]SearchResult, error) {
	emb, generation := s.currentEmbedderSnapshot()
	if emb == nil || embedding.IsNoop(emb) || strings.TrimSpace(opts.Query) == "" {
		return nil, nil
	}
	queryVector, err := emb.Embed(opts.Query)
	if err != nil || !validEmbeddingVector(queryVector, emb.Dim()) {
		return nil, nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil, nil
	}
	where := []string{
		"r.embedding IS NOT NULL",
		"LENGTH(r.embedding) > 0",
		`EXISTS (SELECT 1 FROM knowledge_embedding_metadata em WHERE em.entity_type = 'table_row' AND em.entity_id = r.id AND em.model_id = ? AND em.dimension = ?)`,
	}
	args := []interface{}{embeddingModelIdentifier(emb), len(queryVector)}
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, nil, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.table_id, r.row_index, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, ''), r.embedding,
		t.sheet_name,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM kb_rows r JOIN kb_tables t ON t.id = r.table_id JOIN kb_sources s ON s.id = r.source_id
		WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type candidate struct {
		result SearchResult
		vector []float32
		sim    float64
	}
	var candidates []candidate
	var vectors []vectorANNVector
	for rows.Next() {
		var result SearchResult
		var source Source
		var blob []byte
		var publishedAt, fetchedAt, createdAt, updatedAt string
		var primaryKey string
		if err := rows.Scan(&result.RowID, &result.TableID, &result.RowIndex, &primaryKey, &result.Summary, &blob, &result.SheetName,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt, &source.ContentHash,
			&source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath, &source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		vector := bytesToFloat32Slice(blob)
		if !validEmbeddingVector(vector, len(queryVector)) {
			continue
		}
		source.PublishedAt, source.FetchedAt, source.CreatedAt, source.UpdatedAt = parseTime(publishedAt), parseTime(fetchedAt), parseTime(createdAt), parseTime(updatedAt)
		result.Source, result.ResultType = source, "table_row"
		result.Claim = primaryKey
		result.RowRange = fmt.Sprintf("%d:%d", result.RowIndex, result.RowIndex)
		result.Snippet = result.Summary
		result.Citation = formatResultCitation(result)
		candidates = append(candidates, candidate{result: result, vector: vector})
		vectors = append(vectors, vectorANNVector{key: "row:" + result.RowID, vector: vector})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	indexes := s.vectorANNCandidates("table_row:"+embeddingModelIdentifier(emb), generation, vectors, queryVector, limit*4)
	for _, index := range indexes {
		candidate := &candidates[index]
		candidate.sim = cosineSimilarity(queryVector, candidate.vector)
		if candidate.sim < .25 {
			continue
		}
		candidate.result.Score = 1 + (candidate.sim-.25)*4
	}
	filtered := make([]candidate, 0, len(indexes))
	for _, index := range indexes {
		if candidates[index].sim >= .25 {
			filtered = append(filtered, candidates[index])
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].sim == filtered[j].sim {
			return filtered[i].result.RowID < filtered[j].result.RowID
		}
		return filtered[i].sim > filtered[j].sim
	})
	if limit > len(filtered) {
		limit = len(filtered)
	}
	results := make([]SearchResult, limit)
	for i := range results {
		results[i] = filtered[i].result
	}
	// The row scan and exact cosine pass may run after a model switch. Return no
	// semantic candidates in that case so SearchStructured retains only its
	// lexical/filter results rather than mixing an old embedding space into a
	// newly configured retriever.
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil, nil
	}
	return results, nil
}

func (s *SQLiteStore) SearchStructured(ctx context.Context, opts StructuredSearchOptions) ([]SearchResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is nil")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	hasRowFilters := hasStructuredRowFilters(opts)
	results, err := s.searchTableRowsByCells(ctx, opts, limit)
	if err != nil {
		return nil, err
	}
	if !hasRowFilters && strings.TrimSpace(opts.Query) != "" {
		ftsOpts := SearchOptions{
			Query:           opts.Query,
			OwnerID:         opts.OwnerID,
			TenantID:        opts.TenantID,
			ProjectPath:     opts.ProjectPath,
			SearchScope:     opts.SearchScope,
			SourceIDs:       opts.SourceIDs,
			SourceID:        opts.SourceID,
			Limit:           limit,
			IncludeDisabled: opts.IncludeDisabled,
		}
		ftsResults, err := s.searchTableRowsFTS(ctx, ftsOpts)
		if err != nil {
			return nil, err
		}
		// Spreadsheet rows use the same segmented FTS representation as documents,
		// but legacy imports and dictionary gaps can still miss no-space-script
		// substrings. Run a scoped literal LIKE fallback for those scripts before
		// fusing semantic results; this keeps structured retrieval consistent with
		// the general knowledge search path.
		if containsNoSpaceScriptRunes(opts.Query) {
			likeResults, err := s.searchTableRowsLikeFallback(ctx, opts, limit)
			if err != nil {
				return nil, err
			}
			ftsResults = mergeTableRowResults(ftsResults, likeResults, limit)
		}
		results = mergeTableRowResults(results, ftsResults, limit)
		if emb, _ := s.currentEmbedderSnapshot(); emb != nil && !embedding.IsNoop(emb) {
			semantic, err := s.searchTableRowsByEmbedding(ctx, opts, limit)
			if err != nil {
				return nil, err
			}
			results = rrfFuse(results, semantic, limit)
		}
	}
	sortSearchResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	if err := s.hydrateSearchResultSourceLabels(ctx, results); err != nil {
		return nil, err
	}
	if err := s.hydrateSearchResultNodeMetadata(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) searchTableRowsLikeFallback(ctx context.Context, opts StructuredSearchOptions, limit int) ([]SearchResult, error) {
	terms := structuredLikeTerms(opts.Query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	likeWhere, likeArgs := structuredRowLikeWhere(terms)
	if likeWhere == "" {
		return nil, nil
	}
	where := []string{likeWhere}
	args := append([]interface{}{}, likeArgs...)
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, nil, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	// Rank in SQLite before applying LIMIT. A one-character CJK match is useful
	// for recall but much weaker than a row matching several query characters;
	// ordering by row index first could otherwise discard the precise row before
	// Go gets a chance to score it.
	matchScore, matchArgs := structuredRowLikeMatchScore(terms)
	args = append(args, matchArgs...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.table_id, r.row_index, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, ''),
		t.sheet_name,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		COALESCE(r.row_text, ''), 0.0
		FROM kb_rows r
		JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = r.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY `+matchScore+` DESC, r.row_index ASC, r.id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		result, err := scanTableRowSearchResult(rows, false)
		if err != nil {
			return nil, err
		}
		text := strings.ToLower(result.Claim + "\n" + result.Summary)
		matches := 0
		for _, term := range terms {
			if strings.Contains(text, strings.ToLower(term)) {
				matches++
			}
		}
		result.Score = 1.5 + float64(matches)*.3
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func structuredRowLikeWhere(terms []string) (string, []interface{}) {
	conditions := make([]string, 0, len(terms))
	args := make([]interface{}, 0, len(terms)*2)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		conditions = append(conditions, "(r.primary_key_text LIKE ? ESCAPE '\\' OR r.row_text LIKE ? ESCAPE '\\')")
		pattern := "%" + escapeSQLiteLikePattern(term) + "%"
		args = append(args, pattern, pattern)
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return "(" + strings.Join(conditions, " OR ") + ")", args
}

func structuredRowLikeMatchScore(terms []string) (string, []interface{}) {
	parts := make([]string, 0, len(terms))
	args := make([]interface{}, 0, len(terms)*2)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		parts = append(parts, "CASE WHEN (r.primary_key_text LIKE ? ESCAPE '\\' OR r.row_text LIKE ? ESCAPE '\\') THEN 1 ELSE 0 END")
		pattern := "%" + escapeSQLiteLikePattern(term) + "%"
		args = append(args, pattern, pattern)
	}
	if len(parts) == 0 {
		return "0", nil
	}
	return strings.Join(parts, " + "), args
}

func structuredLikeTerms(query string) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0, 12)
	for _, r := range normalizeKnowledgeLexicalText(query) {
		if !isNoSpaceScriptRune(r) || isCJKStopChar(r) {
			continue
		}
		term := string(r)
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) >= 12 {
			return terms
		}
	}
	return terms
}

func (s *SQLiteStore) searchTableRowsFTS(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	query := buildFTSQuerySegmented(opts.Query)
	if query == "" {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"kb_rows_fts MATCH ?"}
	args := []interface{}{query}
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, opts.SourceKinds, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.table_id, r.row_index, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, ''),
		t.sheet_name,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(kb_rows_fts, 2, '', '', '...', 32), bm25(kb_rows_fts)
		FROM kb_rows_fts
		JOIN kb_rows r ON r.id = kb_rows_fts.row_id
		JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = r.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY bm25(kb_rows_fts)
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		result, err := scanTableRowSearchResult(rows, true)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) searchKBTableCardsFTS(ctx context.Context, opts SearchOptions, limit int) ([]SearchResult, error) {
	query := buildFTSQuerySegmented(opts.Query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"kb_cards_fts MATCH ?", "c.row_id IS NOT NULL", "c.origin_type = 'table_row'"}
	args := []interface{}{query}
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, opts.SourceKinds, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.row_id, c.title, c.claim, c.summary,
		COALESCE(r.table_id, ''), COALESCE(r.row_index, 0), COALESCE(t.sheet_name, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(kb_cards_fts, 2, '', '', '...', 32), bm25(kb_cards_fts)
		FROM kb_cards_fts
		JOIN kb_cards c ON c.id = kb_cards_fts.card_id
		LEFT JOIN kb_rows r ON r.id = c.row_id
		LEFT JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = c.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY bm25(kb_cards_fts)
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var result SearchResult
		var source Source
		var publishedAt, fetchedAt, createdAt, updatedAt string
		var snippet string
		var rank float64
		if err := rows.Scan(&result.CardID, &result.RowID, &result.CardTitle, &result.Claim, &result.Summary,
			&result.TableID, &result.RowIndex, &result.SheetName,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
			&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
			return nil, err
		}
		source.PublishedAt = parseTime(publishedAt)
		source.FetchedAt = parseTime(fetchedAt)
		source.CreatedAt = parseTime(createdAt)
		source.UpdatedAt = parseTime(updatedAt)
		result.Source = source
		result.ResultType = "card"
		if result.RowIndex > 0 {
			result.RowRange = fmt.Sprintf("%d:%d", result.RowIndex, result.RowIndex)
		}
		result.Snippet = strings.TrimSpace(snippet)
		result.Score = scoreSearchResult(result, opts, -rank)
		result.Citation = formatResultCitation(result)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) searchKBTableFactsFTS(ctx context.Context, opts SearchOptions, limit int) ([]SearchResult, error) {
	query := buildFTSQuerySegmented(opts.Query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"kb_facts_fts MATCH ?", "f.row_id IS NOT NULL", "c.origin_type = 'table_row'"}
	args := []interface{}{query}
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, opts.SourceKinds, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.card_id, f.row_id, f.subject, f.predicate, f.object, c.title, c.claim, c.summary,
		COALESCE(r.table_id, ''), COALESCE(r.row_index, 0), COALESCE(t.sheet_name, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		bm25(kb_facts_fts)
		FROM kb_facts_fts
		JOIN kb_facts f ON f.id = kb_facts_fts.fact_id
		JOIN kb_cards c ON c.id = f.card_id
		LEFT JOIN kb_rows r ON r.id = f.row_id
		LEFT JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = f.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY bm25(kb_facts_fts)
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var result SearchResult
		var source Source
		var publishedAt, fetchedAt, createdAt, updatedAt string
		var rank float64
		if err := rows.Scan(&result.FactID, &result.CardID, &result.RowID, &result.Subject, &result.Predicate, &result.Object, &result.CardTitle, &result.Claim, &result.Summary,
			&result.TableID, &result.RowIndex, &result.SheetName,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
			&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &rank); err != nil {
			return nil, err
		}
		source.PublishedAt = parseTime(publishedAt)
		source.FetchedAt = parseTime(fetchedAt)
		source.CreatedAt = parseTime(createdAt)
		source.UpdatedAt = parseTime(updatedAt)
		result.Source = source
		result.ResultType = "fact"
		if result.RowIndex > 0 {
			result.RowRange = fmt.Sprintf("%d:%d", result.RowIndex, result.RowIndex)
		}
		result.Snippet = strings.TrimSpace(result.Subject + " " + result.Predicate + " " + result.Object)
		result.Score = scoreSearchResult(result, opts, -rank)
		result.Citation = formatResultCitation(result)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) searchTableRowsByCells(ctx context.Context, opts StructuredSearchOptions, limit int) ([]SearchResult, error) {
	if !hasStructuredRowFilters(opts) {
		return nil, nil
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, nil, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	if len(opts.SheetNames) > 0 {
		names := normalizeSearchStrings(opts.SheetNames)
		if len(names) == 1 {
			where = append(where, "LOWER(t.sheet_name) = ?")
			args = append(args, names[0])
		} else if len(names) > 1 {
			where = append(where, "LOWER(t.sheet_name) IN ("+placeholders(len(names))+")")
			for _, name := range names {
				args = append(args, name)
			}
		}
	}
	if query := buildFTSQuerySegmented(opts.Query); query != "" {
		// FTS is the fast path. For no-space scripts, preserve the same literal
		// fallback used by query-only structured search, inside this query so every
		// cell and source filter applies to both branches.
		queryWhere := `r.id IN (SELECT row_id FROM kb_rows_fts WHERE kb_rows_fts MATCH ?)`
		queryArgs := []interface{}{query}
		if containsNoSpaceScriptRunes(opts.Query) {
			if likeWhere, likeArgs := structuredRowLikeWhere(structuredLikeTerms(opts.Query)); likeWhere != "" {
				queryWhere = "(" + queryWhere + " OR " + likeWhere + ")"
				queryArgs = append(queryArgs, likeArgs...)
			}
		}
		where = append(where, queryWhere)
		args = append(args, queryArgs...)
	}
	for column, value := range opts.ColumnEquals {
		col := normalizeSpreadsheetColumnName(column)
		if col == "" || strings.TrimSpace(value) == "" {
			continue
		}
		where = append(where, `r.id IN (SELECT ce.row_id FROM kb_cells ce WHERE ce.normalized_column_name = ? AND (LOWER(ce.normalized_value) = LOWER(?) OR LOWER(ce.raw_value) = LOWER(?)))`)
		args = append(args, col, normalizeStructuredCellLookupValue(value), strings.TrimSpace(value))
	}
	for column, value := range opts.ColumnContains {
		col := normalizeSpreadsheetColumnName(column)
		if col == "" {
			continue
		}
		needles := normalizeStructuredContainsLookupValues(value)
		if len(needles) == 0 {
			continue
		}
		containsWhere := make([]string, 0, len(needles)*2)
		containsArgs := make([]interface{}, 0, len(needles)*2)
		for _, needle := range needles {
			containsWhere = append(containsWhere, "LOWER(cc.raw_value) LIKE ? ESCAPE '\\'", "LOWER(cc.normalized_value) LIKE ? ESCAPE '\\'")
			pattern := "%" + escapeSQLiteLikePattern(needle) + "%"
			containsArgs = append(containsArgs, pattern, pattern)
		}
		where = append(where, `r.id IN (SELECT cc.row_id FROM kb_cells cc WHERE cc.normalized_column_name = ? AND (`+strings.Join(containsWhere, " OR ")+`))`)
		args = append(args, col)
		args = append(args, containsArgs...)
	}
	for column, rng := range opts.NumberRanges {
		col := normalizeSpreadsheetColumnName(column)
		if col == "" || (rng.Min == nil && rng.Max == nil) {
			continue
		}
		numberWhere := []string{"cn.normalized_column_name = ?"}
		numberArgs := []interface{}{col}
		if rng.Min != nil {
			numberWhere = append(numberWhere, "cn.number_value >= ?")
			numberArgs = append(numberArgs, *rng.Min)
		}
		if rng.Max != nil {
			numberWhere = append(numberWhere, "cn.number_value <= ?")
			numberArgs = append(numberArgs, *rng.Max)
		}
		where = append(where, `r.id IN (SELECT cn.row_id FROM kb_cells cn WHERE `+strings.Join(numberWhere, " AND ")+`)`)
		args = append(args, numberArgs...)
	}
	for column, rng := range opts.DateRanges {
		col := normalizeSpreadsheetColumnName(column)
		startRaw := strings.TrimSpace(rng.Start)
		endRaw := strings.TrimSpace(rng.End)
		if col == "" || (startRaw == "" && endRaw == "") {
			continue
		}
		dateWhere := []string{"cd.normalized_column_name = ?"}
		dateArgs := []interface{}{col}
		if startRaw != "" {
			dateWhere = append(dateWhere, "cd.date_value >= ?")
			dateArgs = append(dateArgs, normalizeStructuredDateLookupValue(startRaw))
		}
		if endRaw != "" {
			dateWhere = append(dateWhere, "cd.date_value <= ?")
			dateArgs = append(dateArgs, normalizeStructuredDateLookupValue(endRaw))
		}
		where = append(where, `r.id IN (SELECT cd.row_id FROM kb_cells cd WHERE `+strings.Join(dateWhere, " AND ")+`)`)
		args = append(args, dateArgs...)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.table_id, r.row_index, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, ''),
		t.sheet_name,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		COALESCE(r.row_text, ''), 0.0
		FROM kb_rows r
		JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = r.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY r.row_index ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		result, err := scanTableRowSearchResult(rows, false)
		if err != nil {
			return nil, err
		}
		result.Score = 5.0
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func hasStructuredRowFilters(opts StructuredSearchOptions) bool {
	if len(normalizeSearchStrings(opts.SheetNames)) > 0 {
		return true
	}
	for column := range opts.ColumnEquals {
		if normalizeSpreadsheetColumnName(column) != "" && strings.TrimSpace(opts.ColumnEquals[column]) != "" {
			return true
		}
	}
	for column := range opts.ColumnContains {
		if normalizeSpreadsheetColumnName(column) != "" && strings.TrimSpace(opts.ColumnContains[column]) != "" {
			return true
		}
	}
	for column, rng := range opts.NumberRanges {
		if normalizeSpreadsheetColumnName(column) != "" && (rng.Min != nil || rng.Max != nil) {
			return true
		}
	}
	for column, rng := range opts.DateRanges {
		if normalizeSpreadsheetColumnName(column) != "" && (strings.TrimSpace(rng.Start) != "" || strings.TrimSpace(rng.End) != "") {
			return true
		}
	}
	return false
}

func normalizeStructuredCellLookupValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", ""), 64); err == nil {
		return strconv.FormatFloat(parsed, 'f', -1, 64)
	}
	if parsed, ok := parseSpreadsheetBool(value); ok {
		return strconv.FormatBool(parsed)
	}
	if date := normalizedDate(value); date != "" {
		return date
	}
	return value
}

func normalizeStructuredContainsLookupValues(value string) []string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" {
		return nil
	}
	seen := map[string]struct{}{raw: {}}
	values := []string{raw}
	add := func(candidate string) {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		values = append(values, candidate)
	}
	if date := normalizedDate(value); date != "" {
		add(date)
	}
	if strings.Contains(strings.TrimSpace(value), ",") {
		if parsed, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(value), ",", ""), 64); err == nil {
			add(strconv.FormatFloat(parsed, 'f', -1, 64))
		}
	}
	if parsed, ok := parseSpreadsheetBool(value); ok {
		add(strconv.FormatBool(parsed))
	}
	return values
}

func escapeSQLiteLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func normalizeStructuredDateLookupValue(value string) string {
	value = strings.TrimSpace(value)
	if date := normalizedDate(value); date != "" {
		return date
	}
	return value
}

func scanTableRowSearchResult(rows *sql.Rows, ranked bool) (SearchResult, error) {
	var result SearchResult
	var source Source
	var primaryKeyText, rowText, snippet string
	var publishedAt, fetchedAt, createdAt, updatedAt string
	var rank float64
	if err := rows.Scan(&result.RowID, &result.TableID, &result.RowIndex, &primaryKeyText, &rowText, &result.SheetName,
		&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
		&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
		&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
		return SearchResult{}, err
	}
	source.PublishedAt = parseTime(publishedAt)
	source.FetchedAt = parseTime(fetchedAt)
	source.CreatedAt = parseTime(createdAt)
	source.UpdatedAt = parseTime(updatedAt)
	result.Source = source
	result.ResultType = "table_row"
	result.RowRange = fmt.Sprintf("%d:%d", result.RowIndex, result.RowIndex)
	result.Snippet = strings.TrimSpace(snippet)
	if result.Snippet == "" {
		result.Snippet = strings.TrimSpace(rowText)
	}
	result.Summary = strings.TrimSpace(rowText)
	result.Claim = strings.TrimSpace(primaryKeyText)
	if ranked {
		result.Score = scoreSearchResult(result, SearchOptions{}, -rank) + 1.6
	} else if result.Score == 0 {
		result.Score = 1
	}
	result.Citation = formatResultCitation(result)
	return result, nil
}

func appendKBSourceFilters(where []string, args []interface{}, alias, ownerID, tenantID, projectPath, searchScope string, sourceKinds []string, sourceIDs []string, includeDisabled bool) ([]string, []interface{}) {
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		where = append(where, prefix+"tenant_id = ?")
		args = append(args, tenantID)
	}
	if ownerID = strings.TrimSpace(ownerID); ownerID != "" {
		where = append(where, prefix+"owner_id = ?")
		args = append(args, ownerID)
	}
	switch strings.ToLower(strings.TrimSpace(searchScope)) {
	case SaveScopePersonal, SaveScopeLocalOnly, "local":
		where = append(where, "COALESCE("+prefix+"project_path, '') = ''")
	case SaveScopeProject:
		if projectPath = strings.TrimSpace(projectPath); projectPath != "" {
			where = append(where, prefix+"project_path = ?")
			args = append(args, projectPath)
		}
	default:
		if projectPath = strings.TrimSpace(projectPath); projectPath != "" {
			where = append(where, prefix+"project_path = ?")
			args = append(args, projectPath)
		}
	}
	kinds := normalizeSearchStrings(sourceKinds)
	if len(kinds) == 1 {
		where = append(where, prefix+"kind = ?")
		args = append(args, kinds[0])
	} else if len(kinds) > 1 {
		where = append(where, prefix+"kind IN ("+placeholders(len(kinds))+")")
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	ids := normalizeSearchIDs(sourceIDs)
	if len(ids) == 1 {
		where = append(where, prefix+"id = ?")
		args = append(args, ids[0])
	} else if len(ids) > 1 {
		where = append(where, prefix+"id IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if !includeDisabled {
		where = append(where, prefix+"status <> ?")
		args = append(args, StatusDisabled)
	}
	return where, args
}

func mergeTableRowResults(existing, incoming []SearchResult, limit int) []SearchResult {
	if limit <= 0 {
		limit = 20
	}
	out := make([]SearchResult, 0, minInt(limit, len(existing)+len(incoming)))
	seen := make(map[string]struct{})
	for _, result := range append(existing, incoming...) {
		key := result.RowID
		if key == "" {
			key = result.Citation
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, result)
		if len(out) >= limit {
			break
		}
	}
	return out
}
