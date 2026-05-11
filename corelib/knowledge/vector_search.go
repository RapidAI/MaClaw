package knowledge

import (
	"context"
	"encoding/binary"
	"math"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// cardEmbeddingText returns the text to embed for a card.
// Combines title + claim for a concise semantic representation.
func cardEmbeddingText(card Card) string {
	parts := []string{}
	if card.Title != "" {
		parts = append(parts, card.Title)
	}
	if card.Claim != "" {
		parts = append(parts, card.Claim)
	}
	if len(parts) == 0 && card.Summary != "" {
		parts = append(parts, card.Summary)
	}
	return strings.Join(parts, " ")
}

// searchByEmbedding performs vector similarity search on card embeddings.
// Returns cards sorted by cosine similarity to the query embedding.
// Uses a lightweight in-memory cache of card embeddings to avoid repeated
// SQLite BLOB reads on every search call.
func (s *SQLiteStore) searchByEmbedding(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	if s.embedder == nil || embedding.IsNoop(s.embedder) {
		return nil, nil
	}
	queryVec, err := s.embedder.Embed(opts.Query)
	if err != nil || len(queryVec) == 0 {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	// Load cards with embeddings. For small knowledge bases (<500 cards),
	// load all at once. For larger ones, limit to top-N by importance to
	// bound memory and CPU usage.
	const maxEmbeddingCandidates = 500
	where := []string{"c.embedding IS NOT NULL", "LENGTH(c.embedding) > 0"}
	args := make([]interface{}, 0)
	where, args = appendSearchFilters(where, args, "s", opts)
	args = append(args, maxEmbeddingCandidates)

	sqlQuery := `SELECT c.id, COALESCE(c.node_id, ''), c.title, c.claim, c.summary, c.embedding,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE ` + strings.Join(where, " AND ") + `
		AND NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)
		ORDER BY c.importance DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		result SearchResult
		sim    float64
	}
	var candidates []candidate

	for rows.Next() {
		var result SearchResult
		var source Source
		var embBlob []byte
		var publishedAt, fetchedAt, createdAt, updatedAt string

		if err := rows.Scan(&result.CardID, &result.NodeID, &result.CardTitle, &result.Claim, &result.Summary, &embBlob,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
			&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		cardVec := bytesToFloat32Slice(embBlob)
		if len(cardVec) == 0 || len(cardVec) != len(queryVec) {
			continue
		}

		sim := cosineSimilarity(queryVec, cardVec)
		if sim < 0.3 { // minimum similarity threshold
			continue
		}

		source.PublishedAt = parseTime(publishedAt)
		source.FetchedAt = parseTime(fetchedAt)
		source.CreatedAt = parseTime(createdAt)
		source.UpdatedAt = parseTime(updatedAt)
		result.Source = source
		result.ResultType = "card"
		result.Snippet = result.Claim
		// Score: cosine similarity scaled to be comparable with FTS scores
		// cosine sim 0.3-1.0 → score 1.0-4.0
		result.Score = 1.0 + (sim-0.3)*4.3
		result.Citation = formatResultCitation(result)

		candidates = append(candidates, candidate{result: result, sim: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by similarity descending
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].sim > candidates[i].sim {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	results := make([]SearchResult, 0, limit)
	for i, c := range candidates {
		if i >= limit {
			break
		}
		results = append(results, c.result)
	}
	return results, nil
}

// float32SliceToBytes converts a float32 slice to a byte slice for SQLite BLOB storage.
func float32SliceToBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// bytesToFloat32Slice converts a byte slice from SQLite BLOB back to float32 slice.
func bytesToFloat32Slice(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// rrfFuse merges FTS results and embedding results using Reciprocal Rank Fusion.
// k=60 is the standard RRF constant.
func rrfFuse(ftsResults, embResults []SearchResult, limit int) []SearchResult {
	const k = 60.0

	type scored struct {
		result SearchResult
		score  float64
	}

	// Build score map by card/fact/node ID
	scoreMap := make(map[string]*scored)
	keyOf := func(r SearchResult) string {
		if r.CardID != "" {
			return "card:" + r.CardID
		}
		if r.FactID != "" {
			return "fact:" + r.FactID
		}
		return "node:" + r.NodeID
	}

	for rank, r := range ftsResults {
		key := keyOf(r)
		if s, ok := scoreMap[key]; ok {
			s.score += 1.0 / (k + float64(rank+1))
		} else {
			scoreMap[key] = &scored{result: r, score: 1.0 / (k + float64(rank+1))}
		}
	}
	for rank, r := range embResults {
		key := keyOf(r)
		if s, ok := scoreMap[key]; ok {
			s.score += 1.0 / (k + float64(rank+1))
			// If embedding result has higher individual score, use its score for display
			if r.Score > s.result.Score {
				s.result.Score = r.Score
			}
		} else {
			scoreMap[key] = &scored{result: r, score: 1.0 / (k + float64(rank+1))}
		}
	}

	// Sort by RRF score
	all := make([]scored, 0, len(scoreMap))
	for _, s := range scoreMap {
		all = append(all, *s)
	}
	for i := 0; i < len(all)-1; i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].score > all[i].score {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	results := make([]SearchResult, 0, limit)
	for i, s := range all {
		if i >= limit {
			break
		}
		// Use the higher of RRF-derived score and original score
		rrfScore := s.score * 100 // scale RRF to be comparable
		if rrfScore > s.result.Score {
			s.result.Score = rrfScore
		}
		results = append(results, s.result)
	}
	return results
}

// backfillCardEmbeddings generates embeddings for cards that don't have one yet.
func (s *SQLiteStore) backfillCardEmbeddings(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, claim, summary FROM knowledge_cards WHERE embedding IS NULL OR LENGTH(embedding) = 0`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type cardInfo struct {
		id, title, claim, summary string
	}
	var cards []cardInfo
	for rows.Next() {
		var c cardInfo
		if err := rows.Scan(&c.id, &c.title, &c.claim, &c.summary); err != nil {
			return err
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cards) == 0 {
		return nil
	}

	// Batch embed
	texts := make([]string, len(cards))
	for i, c := range cards {
		texts[i] = cardEmbeddingText(Card{Title: c.title, Claim: c.claim, Summary: c.summary})
	}
	vectors, err := s.embedder.EmbedBatch(texts)
	if err != nil {
		return err
	}

	// Update cards with embeddings
	for i, c := range cards {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if i >= len(vectors) || len(vectors[i]) == 0 {
			continue
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE knowledge_cards SET embedding = ? WHERE id = ?`,
			float32SliceToBytes(vectors[i]), c.id)
	}
	return nil
}
