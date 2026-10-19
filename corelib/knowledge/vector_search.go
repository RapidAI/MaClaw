package knowledge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type nodeEmbeddingRow struct {
	id, title, text string
	textLen         int
}

type tableRowEmbeddingRow struct {
	id, primaryKey, text string
}

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

// searchByEmbedding performs vector similarity search on card AND node embeddings.
// Returns results sorted by cosine similarity to the query embedding.
// Searches both distilled cards AND original document nodes to ensure
// information lost during distillation is still discoverable.
func (s *SQLiteStore) searchByEmbedding(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	emb, generation := s.currentEmbedderSnapshot()
	if emb == nil || embedding.IsNoop(emb) {
		return nil, nil
	}
	// A library rebuild is embedding documents on this model. The query would
	// wait behind those batches. Once that rebuild has finished, a search that
	// was already in flight still embeds against the vectors now on disk.
	if skipQueryEmbeddingDuringRefresh(ctx) {
		return nil, nil
	}
	// The active model has no stored vectors yet. Embedding the query would only
	// contend with the library refresh and still match nothing.
	modelID := embeddingModelIdentifier(emb)
	if !s.hasCurrentModelEmbeddings(ctx, modelID, emb.Dim()) {
		return nil, nil
	}
	// The query side of asymmetric retrieval carries its own EmbeddingGemma task
	// prompt; documents were indexed with RoleDocument, so the two spaces are a
	// matched pair. Embedders without prompt support fall back to a plain Embed.
	queryVec, err := embedding.EmbedAs(emb, opts.Query, embedding.RoleQuery)
	if err != nil || !validEmbeddingVector(queryVec, emb.Dim()) {
		return nil, nil
	}
	// A model can switch while a remote Embed call is in flight. Do not search
	// its newly selected space with a vector produced by the previous model.
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	// --- Card embedding search ---
	// SQLite has no native vector index. Until an ANN backend is enabled, score
	// every ACL-filtered vector instead of pre-truncating by importance; ranking
	// before cosine evaluation silently loses relevant low-importance content.
	where := []string{"c.embedding IS NOT NULL", "LENGTH(c.embedding) > 0"}
	args := make([]interface{}, 0)
	where, args = appendSearchFilters(where, args, "s", opts)
	if modelID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM knowledge_embedding_metadata em WHERE em.entity_type = 'card' AND em.entity_id = c.id AND em.model_id = ? AND em.dimension = ?)`)
		args = append(args, modelID, len(queryVec))
	}

	sqlQuery := `SELECT c.id, COALESCE(c.node_id, ''), COALESCE(c.title, ''), COALESCE(c.claim, ''), COALESCE(c.summary, ''), c.embedding,
		s.id, s.kind, s.uri, COALESCE(s.canonical_uri, ''), COALESCE(s.title, ''), COALESCE(s.author, ''), COALESCE(s.site_name, ''), COALESCE(s.published_at, ''), s.fetched_at, s.content_hash,
		COALESCE(s.owner_id, ''), COALESCE(s.tenant_id, ''), COALESCE(s.project_path, ''), COALESCE(s.topic_hint, ''), COALESCE(s.source_trust, 0.5), COALESCE(s.batch_id, ''), COALESCE(s.relative_path, ''), s.status, COALESCE(s.error_message, ''), s.created_at, s.updated_at
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE ` + strings.Join(where, " AND ") + `
		AND NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)`

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
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
	var vectorCandidates []vectorANNVector

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
		if !validEmbeddingVector(cardVec, len(queryVec)) {
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
		candidates = append(candidates, candidate{result: result, vector: cardVec})
		vectorCandidates = append(vectorCandidates, vectorANNVector{key: "card:" + result.CardID, vector: cardVec})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		candidateIndexes := s.vectorANNCandidates("card:"+modelID, generation, vectorCandidates, queryVec, limit*4)
		accelerated := make([]candidate, 0, len(candidateIndexes))
		for _, index := range candidateIndexes {
			candidate := candidates[index]
			candidate.sim = cosineSimilarity(queryVec, candidate.vector)
			if candidate.sim < 0.3 { // minimum similarity threshold
				continue
			}
			candidate.result.Score = 1.0 + (candidate.sim-0.3)*4.3
			candidate.result.Citation = formatResultCitation(candidate.result)
			accelerated = append(accelerated, candidate)
		}
		candidates = accelerated
	}

	// --- Node embedding search (original document full text) ---
	nodeResults, nodeErr := s.searchNodesByEmbedding(ctx, queryVec, modelID, generation, opts)
	if nodeErr == nil && len(nodeResults) > 0 {
		for _, nr := range nodeResults {
			// Reverse the score→sim mapping: score = 1.0 + (sim-0.25)*4.0
			sim := (nr.Score-1.0)/4.0 + 0.25
			candidates = append(candidates, candidate{result: nr, sim: sim})
		}
	}

	// Sort by similarity descending; stable tiebreakers keep pagination and
	// evaluations deterministic.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].sim == candidates[j].sim {
			return embeddingResultKey(candidates[i].result) < embeddingResultKey(candidates[j].result)
		}
		return candidates[i].sim > candidates[j].sim
	})

	results := make([]SearchResult, 0, limit)
	for i, c := range candidates {
		if i >= limit {
			break
		}
		results = append(results, c.result)
	}
	// Candidate loading and cosine ranking can outlive the query Embed call.
	// Suppress an otherwise internally-consistent but stale result set when the
	// configured model changed while SQLite was being scanned.
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil, nil
	}
	return results, nil
}

// SearchResultLocator is the stable identity of one hit. Callers that merge
// corpora use it as the displayed locator. The same key is the RRF identity,
// so a card and a fact from that card stay distinct.
func SearchResultLocator(result SearchResult) string {
	return embeddingResultKey(result)
}

func embeddingResultKey(r SearchResult) string {
	// A fact carries its parent CardID and a table row can also carry derived
	// card/fact fields. Use the explicit result type before looking at optional
	// provenance IDs, otherwise hybrid RRF treats distinct facts from one card as
	// the same card and silently drops evidence.
	switch r.ResultType {
	case "table_row":
		if r.RowID != "" {
			return "row:" + r.RowID
		}
	case "fact":
		if r.FactID != "" {
			return "fact:" + r.FactID
		}
	case "card":
		if r.CardID != "" {
			return "card:" + r.CardID
		}
	case "node":
		if r.NodeID != "" {
			return "node:" + r.NodeID
		}
	}
	if r.RowID != "" {
		return "row:" + r.RowID
	}
	if r.FactID != "" {
		return "fact:" + r.FactID
	}
	if r.CardID != "" {
		return "card:" + r.CardID
	}
	return "node:" + r.NodeID
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
	if !validEmbeddingVector(a, 0) || !validEmbeddingVector(b, len(a)) {
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

// validEmbeddingVector rejects malformed model output and legacy corrupt blobs
// before they reach cosine or LSH. A zero/NaN/Inf vector has no meaningful
// direction; accepting it makes ordering non-deterministic and can poison an
// otherwise valid model space.
func validEmbeddingVector(vector []float32, dimension int) bool {
	if len(vector) == 0 || (dimension > 0 && len(vector) != dimension) {
		return false
	}
	var norm float64
	for _, value := range vector {
		asFloat := float64(value)
		if math.IsNaN(asFloat) || math.IsInf(asFloat, 0) {
			return false
		}
		norm += asFloat * asFloat
	}
	return norm > 0
}

// validateEmbeddingBatchOutput keeps every persistence path aligned with the
// embedder contract. A short batch used to silently leave records stale, while
// a long batch could index past the pending table-row slice. Failing the batch
// lets a later retry repair all of its records without partial ambiguity.
func validateEmbeddingBatchOutput(entity string, vectors [][]float32, expected, dimension int) error {
	if len(vectors) != expected {
		return fmt.Errorf("%s embedder returned %d vectors for %d inputs", entity, len(vectors), expected)
	}
	for i, vector := range vectors {
		if !validEmbeddingVector(vector, dimension) {
			return fmt.Errorf("%s embedder returned invalid vector at batch index %d", entity, i)
		}
	}
	return nil
}

type rrfScoredResult struct {
	result SearchResult
	score  float64
}

// rrfFuse merges FTS results and embedding results using Reciprocal Rank Fusion.
// k=60 is the standard RRF constant.
func rrfFuse(ftsResults, embResults []SearchResult, limit int) []SearchResult {
	const k = 60.0

	// Build a score map by the concrete result entity. Provenance fields (for
	// example Fact.CardID) must not collapse different result types.
	scoreMap := make(map[string]*rrfScoredResult)

	for rank, r := range ftsResults {
		key := embeddingResultKey(r)
		if s, ok := scoreMap[key]; ok {
			s.score += 1.0 / (k + float64(rank+1))
		} else {
			scoreMap[key] = &rrfScoredResult{result: r, score: 1.0 / (k + float64(rank+1))}
		}
	}
	for rank, r := range embResults {
		key := embeddingResultKey(r)
		if s, ok := scoreMap[key]; ok {
			s.score += 1.0 / (k + float64(rank+1))
			// If embedding result has higher individual score, use its score for display
			if r.Score > s.result.Score {
				s.result.Score = r.Score
			}
		} else {
			scoreMap[key] = &rrfScoredResult{result: r, score: 1.0 / (k + float64(rank+1))}
		}
	}
	// Different indexes can surface the same evidence through different entity
	// types (for example a distilled card plus its source node). Merge only exact
	// normalized evidence duplicates; distinct facts from one card remain intact.
	scoreMap = mergeExactEvidenceDuplicates(scoreMap)

	// Sort by RRF score. A map iteration order must never leak into retrieval
	// output: tied scores are common for short result lists and pagination needs
	// deterministic ordering.
	all := make([]rrfScoredResult, 0, len(scoreMap))
	for _, s := range scoreMap {
		all = append(all, *s)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score == all[j].score {
			return embeddingResultKey(all[i].result) < embeddingResultKey(all[j].result)
		}
		return all[i].score > all[j].score
	})

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

func mergeExactEvidenceDuplicates(in map[string]*rrfScoredResult) map[string]*rrfScoredResult {
	if len(in) < 2 {
		return in
	}
	out := make(map[string]*rrfScoredResult, len(in))
	evidenceToKey := make(map[string]string, len(in))
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		candidate := in[key]
		evidence := exactSearchEvidenceKey(candidate.result)
		if evidence == "" {
			out[key] = candidate
			continue
		}
		if existingKey, ok := evidenceToKey[evidence]; ok {
			existing := out[existingKey]
			existing.score += candidate.score
			if candidate.result.Score > existing.result.Score {
				existing.result.Score = candidate.result.Score
			}
			continue
		}
		evidenceToKey[evidence] = key
		out[key] = candidate
	}
	return out
}

func exactSearchEvidenceKey(r SearchResult) string {
	text := strings.ToLower(strings.Join(strings.Fields(BestContentText(r)), " "))
	if len([]rune(text)) < 24 {
		return ""
	}
	source := strings.TrimSpace(r.Source.ContentHash)
	if source == "" {
		source = strings.TrimSpace(r.Source.ID)
	}
	if source == "" {
		return ""
	}
	location := fmt.Sprintf("page=%d\x00sheet=%s\x00row=%s\x00col=%s",
		r.Page,
		strings.ToLower(strings.TrimSpace(r.SheetName)),
		strings.ToLower(strings.TrimSpace(r.RowRange)),
		strings.ToLower(strings.TrimSpace(r.ColRange)),
	)
	return source + "\x00" + location + "\x00" + text
}

type skipQueryEmbeddingKey struct{}

// SkipQueryEmbedding marks ctx so vector search returns without embedding the
// query. Full-text results are unaffected. Interactive search uses this while
// a library rebuild is running.
func SkipQueryEmbedding(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipQueryEmbeddingKey{}, true)
}

func skipQueryEmbedding(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skipped, _ := ctx.Value(skipQueryEmbeddingKey{}).(bool)
	return skipped
}

// skipQueryEmbeddingDuringRefresh reports that this search opted out of query
// embedding and a library rebuild currently owns the model.
func skipQueryEmbeddingDuringRefresh(ctx context.Context) bool {
	return skipQueryEmbedding(ctx) && LibraryEmbeddingRefreshActive()
}

// embeddingBackfillBatchSize is how many cards are embedded and persisted
// before the next cancellation check. Tests shrink it to prove a cancelled
// batch leaves earlier batches on disk.
var embeddingBackfillBatchSize = 64

// hasCurrentModelEmbeddings reports whether card or node search has any vector
// in this retrieval space. Table-row vectors do not count: text search never
// reads them, so they must not cause a query embed that cannot match.
func (s *SQLiteStore) hasCurrentModelEmbeddings(ctx context.Context, modelID string, dimension int) bool {
	if s == nil || s.db == nil || strings.TrimSpace(modelID) == "" || dimension <= 0 {
		return false
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM knowledge_embedding_metadata
		WHERE model_id = ? AND dimension = ? AND entity_type IN (?, ?) LIMIT 1`,
		modelID, dimension, embeddingEntityCard, embeddingEntityNode).Scan(&one)
	return err == nil && one == 1
}

func (s *SQLiteStore) hasCurrentModelEmbeddingsFor(ctx context.Context, entityType, modelID string, dimension int) bool {
	if s == nil || s.db == nil || strings.TrimSpace(modelID) == "" || dimension <= 0 {
		return false
	}
	query := `SELECT 1 FROM knowledge_embedding_metadata WHERE model_id = ? AND dimension = ?`
	args := []interface{}{modelID, dimension}
	if entityType = strings.TrimSpace(entityType); entityType != "" {
		query += ` AND entity_type = ?`
		args = append(args, entityType)
	}
	query += ` LIMIT 1`
	var one int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&one)
	return err == nil && one == 1
}

// backfillCardEmbeddings generates embeddings for cards that don't have one yet.
func (s *SQLiteStore) backfillCardEmbeddings(ctx context.Context) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.backfillCardEmbeddingsForGeneration(ctx, emb, generation)
}

func (s *SQLiteStore) backfillCardEmbeddingsForGeneration(ctx context.Context, emb embedding.Embedder, generation uint64) error {
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}
	modelID := embeddingModelIdentifier(emb)
	batchSize := embeddingBackfillBatchSize
	if batchSize <= 0 {
		batchSize = 64
	}
	// Page by id so a library rebuild does not hold every card before the first
	// batch is persisted, and a cancel does not discard that work.
	after := ""
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		rows, err := s.db.QueryContext(ctx, `SELECT c.id, COALESCE(c.title, ''), COALESCE(c.claim, ''), COALESCE(c.summary, '') FROM knowledge_cards c
			WHERE c.id > ?
			AND (c.embedding IS NULL OR LENGTH(c.embedding) = 0
				OR NOT EXISTS (SELECT 1 FROM knowledge_embedding_metadata em
					WHERE em.entity_type = 'card' AND em.entity_id = c.id AND em.model_id = ? AND em.dimension = ?))
			ORDER BY c.id
			LIMIT ?`, after, modelID, emb.Dim(), batchSize)
		if err != nil {
			return err
		}
		type cardInfo struct {
			id, title, claim, summary string
		}
		batch := make([]cardInfo, 0, batchSize)
		for rows.Next() {
			var c cardInfo
			if err := rows.Scan(&c.id, &c.title, &c.claim, &c.summary); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		after = batch[len(batch)-1].id
		texts := make([]string, len(batch))
		for i, c := range batch {
			texts[i] = cardEmbeddingText(Card{Title: c.title, Claim: c.claim, Summary: c.summary})
		}
		vectors, err := embedding.EmbedBatchAs(emb, texts, embedding.RoleDocument)
		if err != nil {
			return err
		}
		// Drop this batch when the refresh was cancelled while the model ran.
		// Batches already persisted stay in the database.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		if err := validateEmbeddingBatchOutput("card", vectors, len(batch), emb.Dim()); err != nil {
			return err
		}
		for i, c := range batch {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !s.isEmbedderGenerationCurrent(generation) {
				return nil
			}
			updated, err := s.persistEmbeddingIfCurrent(ctx, embeddingEntityCard, c.id, embeddingModelIdentifier(emb), vectors[i], generation, `UPDATE knowledge_cards SET embedding = ?
				WHERE id = ? AND COALESCE(title, '') = ? AND COALESCE(claim, '') = ? AND COALESCE(summary, '') = ?`,
				float32SliceToBytes(vectors[i]), c.id, c.title, c.claim, c.summary)
			if err != nil {
				return err
			}
			if !updated {
				continue // card was deleted or rewritten while EmbedBatch was running
			}
		}
	}
}

// BackfillTableRowEmbeddings embeds normalized spreadsheet row text. Structured
// rows have a separate lifecycle from document nodes, so their metadata is
// recorded under table_row and never shares a card/node vector space.
func (s *SQLiteStore) BackfillTableRowEmbeddings(ctx context.Context) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.backfillTableRowEmbeddingsForGeneration(ctx, emb, generation)
}

func (s *SQLiteStore) backfillTableRowEmbeddingsForGeneration(ctx context.Context, emb embedding.Embedder, generation uint64) error {
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}
	pageSize := embeddingBackfillBatchSize
	if pageSize <= 0 {
		pageSize = 64
	}
	modelID := embeddingModelIdentifier(emb)
	after := ""
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		rows, err := s.db.QueryContext(ctx, `SELECT r.id, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, '')
			FROM kb_rows r
			WHERE r.id > ?
			AND (r.embedding IS NULL OR LENGTH(r.embedding) = 0
				OR NOT EXISTS (SELECT 1 FROM knowledge_embedding_metadata em
					WHERE em.entity_type = 'table_row' AND em.entity_id = r.id AND em.model_id = ? AND em.dimension = ?))
			ORDER BY r.id
			LIMIT ?`, after, modelID, emb.Dim(), pageSize)
		if err != nil {
			return err
		}
		page := make([]tableRowEmbeddingRow, 0, pageSize)
		for rows.Next() {
			var row tableRowEmbeddingRow
			if err := rows.Scan(&row.id, &row.primaryKey, &row.text); err != nil {
				rows.Close()
				return err
			}
			page = append(page, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		after = page[len(page)-1].id
		pending := make([]tableRowEmbeddingRow, 0, len(page))
		for _, row := range page {
			if tableRowEmbeddingText(row.primaryKey, row.text) != "" {
				pending = append(pending, row)
			}
		}
		if len(pending) == 0 {
			continue
		}
		if err := s.embedAndStoreTableRowEmbeddings(ctx, pending, emb, generation); err != nil {
			return err
		}
	}
}

func (s *SQLiteStore) embedAndStoreTableRowEmbeddings(ctx context.Context, pending []tableRowEmbeddingRow, emb embedding.Embedder, generation uint64) error {
	if len(pending) == 0 {
		return nil
	}
	batchSize := embeddingBackfillBatchSize
	if batchSize <= 0 {
		batchSize = 64
	}
	for start := 0; start < len(pending); start += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		end := start + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		texts := make([]string, end-start)
		for i, row := range pending[start:end] {
			texts[i] = tableRowEmbeddingText(row.primaryKey, row.text)
		}
		vectors, err := embedding.EmbedBatchAs(emb, texts, embedding.RoleDocument)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := validateEmbeddingBatchOutput("table-row", vectors, len(texts), emb.Dim()); err != nil {
			return err
		}
		for i, vector := range vectors {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !s.isEmbedderGenerationCurrent(generation) {
				return nil
			}
			row := pending[start+i]
			updated, err := s.persistEmbeddingIfCurrent(ctx, embeddingEntityRow, row.id, embeddingModelIdentifier(emb), vector, generation, `UPDATE kb_rows SET embedding = ?
				WHERE id = ? AND COALESCE(primary_key_text, '') = ? AND COALESCE(row_text, '') = ?`, float32SliceToBytes(vector), row.id, row.primaryKey, row.text)
			if err != nil {
				return err
			}
			if !updated {
				continue // row was deleted or rewritten while EmbedBatch was running
			}
		}
	}
	s.invalidateVectorANN()
	return nil
}

func tableRowEmbeddingText(primaryKey, rowText string) string {
	primaryKey = strings.TrimSpace(primaryKey)
	rowText = strings.TrimSpace(rowText)
	if primaryKey == "" {
		return rowText
	}
	if rowText == "" {
		return primaryKey
	}
	return primaryKey + "\n" + rowText
}

// BackfillTableRowEmbeddingsForSources generates missing embeddings only for
// rows belonging to newly imported spreadsheet sources.
func (s *SQLiteStore) BackfillTableRowEmbeddingsForSources(ctx context.Context, sourceIDs []string) error {
	emb, generation := s.currentEmbedderSnapshot()
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}
	ids := uniqueNonEmptyIDs(sourceIDs)
	if len(ids) == 0 {
		return nil
	}
	var pending []tableRowEmbeddingRow
	// Keep each IN list well below SQLite's variable limit. Large directory
	// imports otherwise fail before any of their spreadsheet rows are indexed.
	const sourceBatchSize = 400
	for start := 0; start < len(ids); start += sourceBatchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		end := start + sourceBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]interface{}, 0, len(batch)+2)
		for _, id := range batch {
			args = append(args, id)
		}
		args = append(args, embeddingModelIdentifier(emb), emb.Dim())
		rows, err := s.db.QueryContext(ctx, `SELECT r.id, COALESCE(r.primary_key_text, ''), COALESCE(r.row_text, '')
			FROM kb_rows r
			WHERE r.source_id IN (`+placeholders+`)
			AND (r.embedding IS NULL OR LENGTH(r.embedding) = 0
				OR NOT EXISTS (SELECT 1 FROM knowledge_embedding_metadata em
					WHERE em.entity_type = 'table_row' AND em.entity_id = r.id AND em.model_id = ? AND em.dimension = ?))`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var row tableRowEmbeddingRow
			if err := rows.Scan(&row.id, &row.primaryKey, &row.text); err != nil {
				rows.Close()
				return err
			}
			if tableRowEmbeddingText(row.primaryKey, row.text) != "" {
				pending = append(pending, row)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return s.embedAndStoreTableRowEmbeddings(ctx, pending, emb, generation)
}

// ---------------------------------------------------------------------------
// Document Node Embedding: vector search over original document full text.
//
// Rationale: LLM distillation (cards/facts) necessarily loses information.
// A 1300-character page compressed into a 300-character card claim loses ~77%
// of content. Vector search over the ORIGINAL document text ensures that
// information not captured in cards/facts can still be found via semantic
// similarity. This is the root-cause fix for distillation loss.
// ---------------------------------------------------------------------------

// EnsureNodeEmbeddingColumn adds the embedding column to document_nodes if
// it doesn't already exist. Safe to call multiple times (idempotent).
func (s *SQLiteStore) EnsureNodeEmbeddingColumn() error {
	_, err := s.db.Exec(`ALTER TABLE document_nodes ADD COLUMN embedding BLOB`)
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil // already exists
	}
	return err
}

// BackfillNodeEmbeddings generates embeddings for document_nodes that don't
// have one yet. Uses the full node text for embedding, providing semantic
// coverage of the original document content.
func (s *SQLiteStore) BackfillNodeEmbeddings(ctx context.Context) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.backfillNodeEmbeddingsForGeneration(ctx, emb, generation)
}

func (s *SQLiteStore) backfillNodeEmbeddingsForGeneration(ctx context.Context, emb embedding.Embedder, generation uint64) error {
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}
	// Ensure column exists first
	if err := s.EnsureNodeEmbeddingColumn(); err != nil {
		return err
	}

	// Page by id so a library rebuild does not hold every document body at once.
	pageSize := embeddingBackfillBatchSize
	if pageSize <= 0 {
		pageSize = 64
	}
	modelID := embeddingModelIdentifier(emb)
	after := ""
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		rows, err := s.db.QueryContext(ctx, `SELECT n.id, COALESCE(n.title, ''), substr(COALESCE(n.text, ''), 1, `+nodeEmbeddingReadCharsSQL+`), length(COALESCE(n.text, '')) FROM document_nodes n
			WHERE n.id > ?
			AND (n.embedding IS NULL OR LENGTH(n.embedding) = 0
				OR NOT EXISTS (SELECT 1 FROM knowledge_embedding_metadata em
					WHERE em.entity_type = 'node' AND em.entity_id = n.id AND em.model_id = ? AND em.dimension = ?))
			ORDER BY n.id
			LIMIT ?`, after, modelID, emb.Dim(), pageSize)
		if err != nil {
			// Column may not exist yet on older DBs — not fatal
			if strings.Contains(err.Error(), "no such column") {
				return nil
			}
			return err
		}
		page := make([]nodeEmbeddingRow, 0, pageSize)
		for rows.Next() {
			var n nodeEmbeddingRow
			if err := rows.Scan(&n.id, &n.title, &n.text, &n.textLen); err != nil {
				rows.Close()
				return err
			}
			page = append(page, n)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		after = page[len(page)-1].id
		if err := s.embedAndStoreNodeEmbeddingsForGeneration(ctx, page, emb, generation, nil); err != nil {
			return err
		}
	}
}

// BackfillNodeEmbeddingsForSources generates missing document node embeddings
// for newly saved/imported sources. It keeps vector recall current without
// waiting for a full index rebuild or startup backfill.
func (s *SQLiteStore) BackfillNodeEmbeddingsForSources(ctx context.Context, sourceIDs []string) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.backfillNodeEmbeddingsForSourcesWithGeneration(ctx, sourceIDs, emb, generation, nil)
}

func (s *SQLiteStore) backfillNodeEmbeddingsForSourcesWithGeneration(ctx context.Context, sourceIDs []string, emb embedding.Embedder, generation uint64, onProgress EmbeddingProgressFunc) error {
	if emb == nil || embedding.IsNoop(emb) || !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}
	nodes, err := s.queryMissingNodeEmbeddingsForGeneration(ctx, sourceIDs, emb, generation)
	if err != nil || len(nodes) == 0 {
		return err
	}
	return s.embedAndStoreNodeEmbeddingsForGeneration(ctx, nodes, emb, generation, onProgress)
}

// BackfillNodeEmbeddingsForSourcesWithProgress is like BackfillNodeEmbeddingsForSources but reports progress.
func (s *SQLiteStore) BackfillNodeEmbeddingsForSourcesWithProgress(ctx context.Context, sourceIDs []string, onProgress EmbeddingProgressFunc) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.backfillNodeEmbeddingsForSourcesWithGeneration(ctx, sourceIDs, emb, generation, onProgress)
}

// queryMissingNodeEmbeddings loads document nodes that need embedding for the given source IDs.
// Returns nil, nil when embedder is unavailable or no nodes need embedding.
func (s *SQLiteStore) queryMissingNodeEmbeddings(ctx context.Context, sourceIDs []string) ([]nodeEmbeddingRow, error) {
	emb, generation := s.currentEmbedderSnapshot()
	return s.queryMissingNodeEmbeddingsForGeneration(ctx, sourceIDs, emb, generation)
}

func (s *SQLiteStore) queryMissingNodeEmbeddingsForGeneration(ctx context.Context, sourceIDs []string, emb embedding.Embedder, generation uint64) ([]nodeEmbeddingRow, error) {
	if emb == nil || embedding.IsNoop(emb) {
		return nil, nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil, nil
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	if err := s.EnsureNodeEmbeddingColumn(); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	ids := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// Chunk IN lists: very large imports can exceed SQLite variable limits.
	const idBatch = 400
	var nodes []nodeEmbeddingRow
	for start := 0; start < len(ids); start += idBatch {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil, nil
		}
		end := start + idBatch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]interface{}, 0, len(chunk))
		for i, id := range chunk {
			placeholders[i] = "?"
			args = append(args, id)
		}
		args = append(args, embeddingModelIdentifier(emb), emb.Dim())
		rows, err := s.db.QueryContext(ctx, `SELECT n.id, COALESCE(n.title, ''), substr(COALESCE(n.text, ''), 1, `+nodeEmbeddingReadCharsSQL+`), length(COALESCE(n.text, '')) FROM document_nodes n
			WHERE n.source_id IN (`+strings.Join(placeholders, ",")+`)
			AND (n.embedding IS NULL OR LENGTH(n.embedding) = 0
				OR NOT EXISTS (SELECT 1 FROM knowledge_embedding_metadata em
					WHERE em.entity_type = 'node' AND em.entity_id = n.id AND em.model_id = ? AND em.dimension = ?))`, args...)
		if err != nil {
			if strings.Contains(err.Error(), "no such column") {
				return nil, nil
			}
			return nil, err
		}
		for rows.Next() {
			var n nodeEmbeddingRow
			if err := rows.Scan(&n.id, &n.title, &n.text, &n.textLen); err != nil {
				rows.Close()
				return nil, err
			}
			nodes = append(nodes, n)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

func (s *SQLiteStore) embedAndStoreNodeEmbeddings(ctx context.Context, nodes []nodeEmbeddingRow) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.embedAndStoreNodeEmbeddingsForGeneration(ctx, nodes, emb, generation, nil)
}

// EmbeddingProgressFunc reports progress during embedding generation.
type EmbeddingProgressFunc func(processed, total int)

func (s *SQLiteStore) embedAndStoreNodeEmbeddingsWithProgress(ctx context.Context, nodes []nodeEmbeddingRow, onProgress EmbeddingProgressFunc) error {
	emb, generation := s.currentEmbedderSnapshot()
	return s.embedAndStoreNodeEmbeddingsForGeneration(ctx, nodes, emb, generation, onProgress)
}

func (s *SQLiteStore) embedAndStoreNodeEmbeddingsForGeneration(ctx context.Context, nodes []nodeEmbeddingRow, emb embedding.Embedder, generation uint64, onProgress EmbeddingProgressFunc) error {
	if len(nodes) == 0 {
		return nil
	}
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	if !s.isEmbedderGenerationCurrent(generation) {
		return nil
	}

	// Process in batches so document text is prepared for the batch in hand,
	// not copied for the whole library first.
	batchSize := embeddingBackfillBatchSize
	if batchSize <= 0 {
		batchSize = 64
	}
	totalNodes := len(nodes)
	for batchStart := 0; batchStart < totalNodes; batchStart += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.isEmbedderGenerationCurrent(generation) {
			return nil
		}
		batchEnd := batchStart + batchSize
		if batchEnd > totalNodes {
			batchEnd = totalNodes
		}
		texts := make([]string, batchEnd-batchStart)
		for i := batchStart; i < batchEnd; i++ {
			texts[i-batchStart] = nodeEmbeddingText(nodes[i].title, nodes[i].text)
		}
		vectors, embErr := embedding.EmbedBatchAs(emb, texts, embedding.RoleDocument)
		if embErr != nil {
			return embErr
		}
		// Drop this batch when the refresh was cancelled while the model ran.
		// Batches already persisted stay in the database.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := validateEmbeddingBatchOutput("node", vectors, batchEnd-batchStart, emb.Dim()); err != nil {
			return err
		}
		for i, vec := range vectors {
			if !s.isEmbedderGenerationCurrent(generation) {
				return nil
			}
			nodeIdx := batchStart + i
			if nodeIdx >= totalNodes {
				return fmt.Errorf("node embedding batch index %d exceeds %d nodes", nodeIdx, totalNodes)
			}
			// The read kept only a prefix. Match that prefix and the full character
			// length so a long page still persists, while an edit during the
			// model call does not keep a stale vector.
			updated, err := s.persistEmbeddingIfCurrent(ctx, embeddingEntityNode, nodes[nodeIdx].id, embeddingModelIdentifier(emb), vec, generation, `UPDATE document_nodes SET embedding = ?
				WHERE id = ? AND COALESCE(title, '') = ? AND substr(COALESCE(text, ''), 1, `+nodeEmbeddingReadCharsSQL+`) = ? AND length(COALESCE(text, '')) = ?`,
				float32SliceToBytes(vec), nodes[nodeIdx].id, nodes[nodeIdx].title, nodes[nodeIdx].text, nodes[nodeIdx].textLen)
			if err != nil {
				return err
			}
			if !updated {
				continue // node was deleted or rewritten while EmbedBatch was running
			}
		}
		if onProgress != nil {
			onProgress(batchEnd, totalNodes)
		}
	}
	return nil
}

func nodeEmbeddingText(title, text string) string {
	t := text
	if title != "" && !strings.HasPrefix(t, title) {
		t = title + " " + t
	}
	const maxEmbedChars = 2000
	if len([]rune(t)) > maxEmbedChars {
		t = string([]rune(t)[:maxEmbedChars])
	}
	return t
}

// nodeEmbeddingReadCharsSQL is the document prefix, in characters, read while
// embedding. The model keeps 2000 runes, so a rebuild must not copy the rest
// of a long page into memory.
const nodeEmbeddingReadCharsSQL = "8000"

// extractQueryTermsForSnippet splits a query into meaningful terms for snippet extraction.
// Uses gse tokenization for CJK, keeping multi-char tokens. For non-CJK, splits on whitespace.
func extractQueryTermsForSnippet(query string) []string {
	if query == "" {
		return nil
	}
	if containsNoSpaceScriptRunes(query) {
		tokens := append(bm25.Tokenize(query), scriptNGrams(query, 2)...)
		var terms []string
		for _, t := range tokens {
			t = strings.TrimSpace(t)
			if t != "" && len([]rune(t)) >= 2 {
				terms = append(terms, t)
			}
		}
		if len(terms) > 0 {
			return terms
		}
	}
	// Fallback: split on whitespace
	var terms []string
	for _, w := range strings.Fields(query) {
		w = strings.TrimSpace(w)
		if len(w) >= 2 {
			terms = append(terms, w)
		}
	}
	return terms
}

// searchNodesByEmbedding performs vector similarity search on document_nodes.
// This is the root-cause fix for distillation loss: it searches the ORIGINAL
// document text embeddings, not the distilled card claims.
func (s *SQLiteStore) searchNodesByEmbedding(ctx context.Context, queryVec []float32, modelID string, generation uint64, opts SearchOptions, nodeTypes ...string) ([]SearchResult, error) {
	if !validEmbeddingVector(queryVec, 0) {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	where := []string{"n.embedding IS NOT NULL", "LENGTH(n.embedding) > 0"}
	args := make([]interface{}, 0)
	if len(nodeTypes) > 0 {
		normalized := normalizeSearchStrings(nodeTypes)
		if len(normalized) == 1 {
			where = append(where, "n.type = ?")
			args = append(args, normalized[0])
		} else if len(normalized) > 1 {
			placeholders := strings.TrimRight(strings.Repeat("?,", len(normalized)), ",")
			where = append(where, "n.type IN ("+placeholders+")")
			for _, nodeType := range normalized {
				args = append(args, nodeType)
			}
		}
	}
	where, args = appendSearchFilters(where, args, "s", opts)
	if modelID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM knowledge_embedding_metadata em WHERE em.entity_type = 'node' AND em.entity_id = n.id AND em.model_id = ? AND em.dimension = ?)`)
		args = append(args, modelID, len(queryVec))
	}

	// Score vectors without the document body. Full text is loaded only for the
	// hits that will be returned, so a library of long pages is not copied on
	// every query.
	sqlQuery := `SELECT n.id, COALESCE(n.title, ''), n.type, COALESCE(n.page, 0), n.embedding,
		s.id, s.kind, s.uri, COALESCE(s.canonical_uri, ''), COALESCE(s.title, ''), COALESCE(s.author, ''), COALESCE(s.site_name, ''), COALESCE(s.published_at, ''), s.fetched_at, s.content_hash,
		COALESCE(s.owner_id, ''), COALESCE(s.tenant_id, ''), COALESCE(s.project_path, ''), COALESCE(s.topic_hint, ''), COALESCE(s.source_trust, 0.5), COALESCE(s.batch_id, ''), COALESCE(s.relative_path, ''), s.status, COALESCE(s.error_message, ''), s.created_at, s.updated_at
		FROM document_nodes n
		JOIN knowledge_sources s ON s.id = n.source_id
		WHERE ` + strings.Join(where, " AND ")

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		// Column may not exist yet
		if strings.Contains(err.Error(), "no such column") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		result SearchResult
		vector []float32
		sim    float64
	}
	var candidates []candidate
	var vectorCandidates []vectorANNVector

	for rows.Next() {
		var result SearchResult
		var source Source
		var embBlob []byte
		var publishedAt, fetchedAt, createdAt, updatedAt string

		if err := rows.Scan(&result.NodeID, &result.NodeTitle, &result.NodeType, &result.Page, &embBlob,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
			&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		nodeVec := bytesToFloat32Slice(embBlob)
		if !validEmbeddingVector(nodeVec, len(queryVec)) {
			continue
		}

		source.PublishedAt = parseTime(publishedAt)
		source.FetchedAt = parseTime(fetchedAt)
		source.CreatedAt = parseTime(createdAt)
		source.UpdatedAt = parseTime(updatedAt)
		result.Source = source
		result.ResultType = "node"
		candidates = append(candidates, candidate{result: result, vector: nodeVec})
		vectorCandidates = append(vectorCandidates, vectorANNVector{key: "node:" + result.NodeID, vector: nodeVec})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		candidateIndexes := s.vectorANNCandidates("node:"+modelID, generation, vectorCandidates, queryVec, limit*4)
		accelerated := make([]candidate, 0, len(candidateIndexes))
		for _, index := range candidateIndexes {
			candidate := candidates[index]
			candidate.sim = cosineSimilarity(queryVec, candidate.vector)
			if candidate.sim < 0.25 { // lower threshold for nodes (more content → noisier embedding)
				continue
			}
			candidate.result.Score = 1.0 + (candidate.sim-0.25)*4.0
			candidate.result.Citation = formatResultCitation(candidate.result)
			accelerated = append(accelerated, candidate)
		}
		candidates = accelerated
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].sim == candidates[j].sim {
			return candidates[i].result.NodeID < candidates[j].result.NodeID
		}
		return candidates[i].sim > candidates[j].sim
	})

	results := make([]SearchResult, 0, limit)
	for i, c := range candidates {
		if i >= limit {
			break
		}
		results = append(results, c.result)
	}
	if err := s.fillNodeEmbeddingSnippets(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

// fillNodeEmbeddingSnippets loads the original document text for the nodes
// already chosen by cosine rank.
func (s *SQLiteStore) fillNodeEmbeddingSnippets(ctx context.Context, results []SearchResult) error {
	if s == nil || s.db == nil || len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		id := strings.TrimSpace(result.NodeID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(text, '') FROM document_nodes WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	texts := make(map[string]string, len(ids))
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			return err
		}
		texts[id] = text
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range results {
		if text, ok := texts[results[i].NodeID]; ok {
			// Full text of a hit gives the model the evidence that was embedded.
			results[i].Snippet = text
		}
	}
	return nil
}
