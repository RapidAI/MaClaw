package knowledge

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// SearchImages retrieves image nodes using their indexed OCR/caption/context
// text. It is the canonical text-to-image route for local knowledge stores.
//
// It queries image nodes at the database boundary rather than calling Search
// and filtering afterwards: otherwise normal document nodes can consume the
// bounded candidate window before a relevant image is considered.
func (s *SQLiteStore) SearchImages(ctx context.Context, opts ImageSearchOptions) ([]SearchResult, error) {
	searchOpts := opts.SearchOptions
	if strings.TrimSpace(searchOpts.Query) == "" {
		return []SearchResult{}, nil
	}
	if searchOpts.Limit <= 0 {
		searchOpts.Limit = 8
	}
	if searchOpts.Limit > 50 {
		searchOpts.Limit = 50
	}

	results, err := s.searchImageNodesFTS(ctx, searchOpts)
	if err != nil {
		return nil, err
	}
	if containsNoSpaceScriptRunes(searchOpts.Query) {
		likeResults, err := s.searchImageNodesLike(ctx, searchOpts)
		if err != nil {
			return nil, err
		}
		results = mergeImageSearchResults(results, likeResults)
	}

	// Image nodes already receive text embeddings from the normal node-indexing
	// lifecycle. Reuse them only for text-to-image caption/OCR paraphrase recall;
	// this is deliberately not an image-to-image embedding implementation.
	if emb, generation := s.currentEmbedderSnapshot(); emb != nil && !embedding.IsNoop(emb) {
		queryVec, embedErr := emb.Embed(searchOpts.Query)
		if embedErr == nil && validEmbeddingVector(queryVec, emb.Dim()) && s.isEmbedderGenerationCurrent(generation) {
			embResults, vectorErr := s.searchNodesByEmbedding(ctx, queryVec, embeddingModelIdentifier(emb), generation, searchOpts, NodeTypeImage)
			if vectorErr != nil {
				return nil, vectorErr
			}
			if len(embResults) > 0 {
				results = rrfFuse(results, embResults, searchOpts.Limit)
			}
		}
	}

	sortSearchResults(results)
	if len(results) > searchOpts.Limit {
		results = results[:searchOpts.Limit]
	}
	if err := s.hydrateSearchResultNodeMetadata(ctx, results); err != nil {
		return nil, err
	}
	if err := s.hydrateSearchResultSourceLabels(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) searchImageNodesFTS(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	query := buildFTSQuerySegmented(opts.Query)
	if query == "" {
		return nil, nil
	}
	candidateLimit := opts.Limit * 4
	if candidateLimit > 200 {
		candidateLimit = 200
	}
	where := []string{"document_nodes_fts MATCH ?", "n.type = ?"}
	args := []interface{}{query, NodeTypeImage}
	where, args = appendSearchFilters(where, args, "s", opts)
	args = append(args, candidateLimit)
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.title, n.type, n.page, n.sheet_name, n.row_range, n.col_range,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(document_nodes_fts, 2, '', '', '...', 32), bm25(document_nodes_fts)
		FROM document_nodes_fts JOIN document_nodes n ON n.id = document_nodes_fts.node_id
		JOIN knowledge_sources s ON s.id = n.source_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY bm25(document_nodes_fts) LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanImageSearchRows(rows, opts, false)
}

func (s *SQLiteStore) searchImageNodesLike(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	terms := imageSearchTerms(opts.Query)
	if len(terms) == 0 {
		return nil, nil
	}
	conditions := make([]string, 0, len(terms)*2)
	patternArgs := make([]interface{}, 0, len(terms)*2)
	for _, term := range terms {
		pattern := "%" + escapeLikePattern(term) + "%"
		conditions = append(conditions, "n.text LIKE ? ESCAPE '\\'", "n.title LIKE ? ESCAPE '\\'")
		patternArgs = append(patternArgs, pattern, pattern)
	}
	where := []string{"n.type = ?", "(" + strings.Join(conditions, " OR ") + ")"}
	args := append([]interface{}{NodeTypeImage}, patternArgs...)
	where, args = appendSearchFilters(where, args, "s", opts)
	args = append(args, opts.Limit*4)
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.title, n.type, n.page, n.sheet_name, n.row_range, n.col_range,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		n.text, 0.0
		FROM document_nodes n JOIN knowledge_sources s ON s.id = n.source_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY s.fetched_at DESC, n.id ASC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanImageSearchRows(rows, opts, true)
}

func scanImageSearchRows(rows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}, opts SearchOptions, isLike bool) ([]SearchResult, error) {
	results := make([]SearchResult, 0, opts.Limit)
	for rows.Next() {
		var result SearchResult
		var source Source
		var publishedAt, fetchedAt, createdAt, updatedAt, snippet string
		var rank float64
		if err := rows.Scan(&result.NodeID, &result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
			&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
			return nil, err
		}
		source.PublishedAt, source.FetchedAt = parseTime(publishedAt), parseTime(fetchedAt)
		source.CreatedAt, source.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		result.Source, result.ResultType, result.Snippet = source, "node", strings.TrimSpace(snippet)
		base := -rank
		if isLike {
			matches := 0
			text := strings.ToLower(result.NodeTitle + "\n" + result.Snippet)
			for _, term := range imageSearchTerms(opts.Query) {
				if strings.Contains(text, strings.ToLower(term)) {
					matches++
				}
			}
			base = 1.5 + 0.3*float64(matches)
		}
		result.Score = scoreSearchResult(result, opts, base)
		result.Citation = formatResultCitation(result)
		results = append(results, result)
	}
	return results, rows.Err()
}

func imageSearchTerms(query string) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0, 12)
	for _, r := range query {
		if isNoSpaceScriptRune(r) && !isCJKStopChar(r) {
			term := string(r)
			if _, exists := seen[term]; !exists {
				seen[term] = struct{}{}
				terms = append(terms, term)
			}
		}
	}
	for _, term := range append(bm25.Tokenize(query), scriptNGrams(query, 2)...) {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 || !strings.Contains(strings.ToLower(query), strings.ToLower(term)) {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) >= 12 {
			break
		}
	}
	return terms
}

func mergeImageSearchResults(left, right []SearchResult) []SearchResult {
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]SearchResult, 0, len(left)+len(right))
	for _, result := range append(left, right...) {
		if result.NodeID == "" {
			continue
		}
		if _, exists := seen[result.NodeID]; exists {
			continue
		}
		seen[result.NodeID] = struct{}{}
		merged = append(merged, result)
	}
	return merged
}
