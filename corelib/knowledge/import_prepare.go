package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Minimum items before paying goroutine overhead for intra-document prep.
const parallelPrepMinItems = 4

// parallelFor runs fn(i) for i in [0,n). Uses multiple workers when n is large
// enough that CJK FTS / card work benefits from multi-core.
func parallelFor(n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if n < parallelPrepMinItems {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	workers := importParallelWorkers(n)
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > n {
			end = n
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(from, to int) {
			defer wg.Done()
			for i := from; i < to; i++ {
				fn(i)
			}
		}(start, end)
	}
	wg.Wait()
}

// prepareNodesForInsert assigns IDs and pre-computes FTS tokens + metadata JSON
// in parallel. Safe for fresh inserts where FTS DELETE is unnecessary.
func prepareNodesForInsert(nodes []DocumentNode) []lightPreparedNode {
	if len(nodes) == 0 {
		return nil
	}
	// Preserve the caller's backing slice: downstream card generation depends
	// on these generated IDs. Annotation only edits fields in-place and never
	// expands this low-level insertion path.
	annotated := annotateMultilingualNodeMetadata(nodes)
	copy(nodes, annotated)
	bm25.PrewarmDict()
	out := make([]lightPreparedNode, len(nodes))
	parallelFor(len(nodes), func(i int) {
		node := nodes[i]
		if node.ID == "" {
			node.ID = NewID("kdn")
		}
		node = sanitizeSnapshotDocumentNode(node)
		meta, _ := json.Marshal(node.Metadata)
		out[i] = lightPreparedNode{
			node:     node,
			ftsTitle: segmentTextForFTS(node.Title),
			ftsText:  segmentTextForFTS(node.Text),
			metaJSON: string(meta),
		}
	})
	// Write IDs back so later card building sees stable node IDs.
	for i := range nodes {
		nodes[i].ID = out[i].node.ID
	}
	return out
}

// prepareCardsAndFacts builds distilled cards/facts and pre-computes FTS + JSON
// fields with multi-core workers. Cards may already include embeddings.
func prepareCardsAndFacts(source Source, cards []Card) (prepCards []lightPreparedCard, prepFacts []lightPreparedFact) {
	if len(cards) == 0 {
		return nil, nil
	}
	bm25.PrewarmDict()
	type cardBundle struct {
		card  lightPreparedCard
		facts []lightPreparedFact
	}
	bundles := make([]cardBundle, len(cards))
	parallelFor(len(cards), func(i int) {
		card := enrichCardStructure(source, cards[i])
		if card.ID == "" {
			card.ID = NewID("kcard")
		}
		now := time.Now().UTC()
		if card.CreatedAt.IsZero() {
			card.CreatedAt = now
		}
		if card.UpdatedAt.IsZero() {
			card.UpdatedAt = now
		}
		if card.Confidence <= 0 {
			card.Confidence = 0.5
		}
		if card.Importance <= 0 {
			card.Importance = 1
		}
		if card.SourceTrust <= 0 {
			card.SourceTrust = 0.5
		}
		entitiesJSON, _ := json.Marshal(card.Entities)
		topicsJSON, _ := json.Marshal(card.Topics)
		tagsJSON, _ := json.Marshal(card.Tags)
		ftsSummary := cardFTSSummary(card)
		pc := lightPreparedCard{
			card:         card,
			entitiesJSON: string(entitiesJSON),
			topicsJSON:   string(topicsJSON),
			tagsJSON:     string(tagsJSON),
			ftsTitle:     segmentTextForFTS(card.Title),
			ftsClaim:     segmentTextForFTS(card.Claim),
			ftsSummary:   segmentTextForFTS(ftsSummary),
		}
		rawFacts := BuildFactsForCard(source, card)
		facts := make([]lightPreparedFact, 0, len(rawFacts))
		for _, fact := range rawFacts {
			if fact.ID == "" {
				fact.ID = NewID("kfact")
			}
			if fact.Confidence <= 0 {
				fact.Confidence = 0.5
			}
			facts = append(facts, lightPreparedFact{
				fact:       fact,
				ftsSubject: segmentTextForFTS(fact.Subject),
				ftsPred:    segmentTextForFTS(fact.Predicate),
				ftsObject:  segmentTextForFTS(fact.Object),
			})
		}
		bundles[i] = cardBundle{card: pc, facts: facts}
	})
	prepCards = make([]lightPreparedCard, 0, len(cards))
	prepFacts = make([]lightPreparedFact, 0, len(cards)*2)
	for _, b := range bundles {
		prepCards = append(prepCards, b.card)
		prepFacts = append(prepFacts, b.facts...)
	}
	return prepCards, prepFacts
}

// buildCardsForNodesFast builds rule cards with optional multi-core fan-out for
// multi-section documents (common for single large markdown/PDF text extracts).
func buildCardsForNodesFast(source Source, nodes []DocumentNode) []Card {
	if len(nodes) < parallelPrepMinItems {
		return BuildCardsForNodes(source, nodes)
	}
	perNode := make([][]Card, len(nodes))
	now := time.Now().UTC()
	parallelFor(len(nodes), func(i int) {
		// Reuse the single-node path of BuildCardsForNodes via a one-element slice
		// to keep list-structure / entity logic identical.
		cards := BuildCardsForNodes(source, nodes[i:i+1])
		// BuildCardsForNodes stamps CreatedAt with its own now; normalize for stability.
		for j := range cards {
			if cards[j].CreatedAt.IsZero() {
				cards[j].CreatedAt = now
			}
			if cards[j].UpdatedAt.IsZero() {
				cards[j].UpdatedAt = now
			}
		}
		perNode[i] = cards
	})
	out := make([]Card, 0, len(nodes))
	for _, cards := range perNode {
		out = append(out, cards...)
	}
	return out
}

// insertDocumentNodesFast writes nodes with prepared statements and precomputed FTS.
func insertDocumentNodesFast(ctx context.Context, tx *sql.Tx, nodes []DocumentNode) error {
	if len(nodes) == 0 {
		return nil
	}
	prepared := prepareNodesForInsert(nodes)
	// Node-only prepared statements (avoid preparing unused card/fact SQL).
	nodeIns, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO document_nodes
		(id, source_id, parent_id, type, title, text, level, page, sheet_name, row_range, col_range, xpath, offset, metadata_json, token_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer nodeIns.Close()
	nodeFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer nodeFTSIns.Close()
	for _, pn := range prepared {
		node := pn.node
		if _, err := nodeIns.ExecContext(ctx,
			node.ID, node.SourceID, node.ParentID, node.Type, node.Title, node.Text, node.Level, node.Page, node.SheetName,
			node.RowRange, node.ColRange, node.XPath, node.Offset, pn.metaJSON, node.TokenCount,
		); err != nil {
			return err
		}
		if _, err := nodeFTSIns.ExecContext(ctx, node.ID, pn.ftsTitle, pn.ftsText); err != nil {
			return err
		}
	}
	return nil
}

// distillAndSaveCardsFast is the multi-core-aware path used by single-file and
// multi-node imports: parallel FTS/fact prep + prepared SQL inserts.
func (s *SQLiteStore) distillAndSaveCardsFast(ctx context.Context, tx *sql.Tx, source Source, nodes []DocumentNode, mode string) (Source, error) {
	if len(nodes) == 0 {
		return source, nil
	}
	// Ensure node IDs exist (insertDocumentNodesFast may have assigned them already).
	for i := range nodes {
		if nodes[i].ID == "" {
			nodes[i].ID = NewID("kdn")
		}
	}

	cards := buildCardsForNodesFast(source, nodes)
	if s != nil && s.distiller != nil && shouldUseLLMForMode(mode, source, nodes) {
		if llmCards, err := s.distiller.DistillCards(ctx, source, nodes); err == nil && len(llmCards) > 0 {
			cards = NormalizeDistilledCards(source, llmCards)
		}
	}
	if len(cards) == 0 {
		return source, nil
	}

	embeddingModelID := ""
	if emb, _ := s.currentEmbedderSnapshot(); emb != nil && !embedding.IsNoop(emb) {
		embeddingModelID = embeddingModelIdentifier(emb)
		texts := make([]string, len(cards))
		for i, card := range cards {
			texts[i] = cardEmbeddingText(card)
		}
		if vectors, err := emb.EmbedBatch(texts); err == nil && len(vectors) == len(cards) {
			for i := range cards {
				cards[i].Embedding = vectors[i]
			}
		}
	}

	prepCards, prepFacts := prepareCardsAndFacts(source, cards)
	stmts, err := prepareLightImportStmts(ctx, tx)
	if err != nil {
		return source, err
	}
	defer stmts.Close()
	if err := insertPreparedCardsFacts(ctx, tx, stmts, prepCards, prepFacts, embeddingModelID); err != nil {
		return source, err
	}
	source.Status = StatusDistilled
	source.UpdatedAt = time.Now().UTC()
	return source, insertSource(ctx, tx, source)
}

// insertPreparedCardsFacts writes precomputed cards and facts (shared by multi-file
// and single-file fast paths).
func insertPreparedCardsFacts(ctx context.Context, tx *sql.Tx, stmts *lightImportStmts, cards []lightPreparedCard, facts []lightPreparedFact, modelID string) error {
	for _, pc := range cards {
		card := pc.card
		var embBlob interface{}
		if len(card.Embedding) > 0 {
			embBlob = float32SliceToBytes(card.Embedding)
		}
		if _, err := stmts.cardIns.ExecContext(ctx,
			card.ID, card.SourceID, nullableString(card.NodeID), card.Title, card.Claim, card.Summary,
			pc.entitiesJSON, pc.topicsJSON, pc.tagsJSON,
			card.ProjectPath, card.OwnerID, card.TenantID, formatTime(card.ValidAt), formatTime(card.InvalidAt),
			card.Confidence, card.Importance, card.SourceTrust, embBlob, formatTime(card.CreatedAt), formatTime(card.UpdatedAt),
		); err != nil {
			return err
		}
		if _, err := stmts.cardFTSIns.ExecContext(ctx, card.ID, pc.ftsTitle, pc.ftsClaim, pc.ftsSummary); err != nil {
			return err
		}
		if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityCard, card.ID, modelID, len(card.Embedding)); err != nil {
			return err
		}
	}
	for _, pf := range facts {
		fact := pf.fact
		if _, err := stmts.factIns.ExecContext(ctx,
			fact.ID, fact.CardID, fact.SourceID, fact.Subject, fact.Predicate, fact.Object, boolInt(fact.Negated),
			formatTime(fact.ValidAt), formatTime(fact.InvalidAt), fact.Confidence,
		); err != nil {
			return err
		}
		if _, err := stmts.factFTSIns.ExecContext(ctx, fact.ID, pf.ftsSubject, pf.ftsPred, pf.ftsObject); err != nil {
			return err
		}
	}
	return nil
}

// lookupContentHashes returns the subset of hashes that already exist for the
// import scope. Used by small-batch ImportFiles to avoid loading every hash in
// the knowledge base.
func (s *SQLiteStore) lookupContentHashes(ctx context.Context, req DirectoryImportRequest, hashes []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(hashes) == 0 {
		return out, nil
	}
	// Deduplicate inputs.
	uniq := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		uniq = append(uniq, h)
	}
	if len(uniq) == 0 {
		return out, nil
	}

	const batch = 80
	for start := 0; start < len(uniq); start += batch {
		end := start + batch
		if end > len(uniq) {
			end = len(uniq)
		}
		chunk := uniq[start:end]
		where := []string{"content_hash IN (" + placeholders(len(chunk)) + ")"}
		args := make([]interface{}, 0, len(chunk)+3)
		for _, h := range chunk {
			args = append(args, h)
		}
		if req.TenantID != "" {
			where = append(where, "tenant_id = ?")
			args = append(args, req.TenantID)
		}
		if req.OwnerID != "" {
			where = append(where, "owner_id = ?")
			args = append(args, req.OwnerID)
		}
		if req.ProjectPath != "" && req.SaveScope == SaveScopeProject {
			where = append(where, "project_path = ?")
			args = append(args, req.ProjectPath)
		}
		q := `SELECT content_hash FROM knowledge_sources WHERE ` + joinAND(where)
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return nil, err
			}
			if h != "" {
				out[h] = struct{}{}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

func joinAND(parts []string) string {
	if len(parts) == 0 {
		return "1=1"
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += " AND " + parts[i]
	}
	return out
}

// applyExistingHashSkips marks queued items whose content hash already exists.
func applyExistingHashSkips(result DirectoryImportResult, items []ImportItem, existing map[string]struct{}) (DirectoryImportResult, []ImportItem) {
	if len(existing) == 0 {
		return result, items
	}
	for i := range items {
		item := &items[i]
		if item.Status != ItemStatusQueued || item.FileHash == "" {
			continue
		}
		if _, ok := existing[item.FileHash]; !ok {
			continue
		}
		item.Status = ItemStatusSkippedDuplicate
		item.ErrorMessage = "duplicate content hash"
		item.UpdatedAt = time.Now().UTC()
		result.QueuedFiles--
		result.DuplicateFiles++
		result.SkippedFiles++
		if result.QueuedFiles < 0 {
			result.QueuedFiles = 0
		}
		// Queued items contributed to EstimatedBytes during scan; reverse that.
		if item.FileSize > 0 && result.EstimatedBytes >= item.FileSize {
			result.EstimatedBytes -= item.FileSize
		} else if item.FileSize > 0 {
			result.EstimatedBytes = 0
		}
	}
	result.Items = items
	return result, items
}
