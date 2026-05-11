package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// segmentTextForFTS tokenizes text using gse (via bm25.Tokenize) and returns
// a space-separated string suitable for FTS5 indexing. This enables Chinese
// text to be properly searchable via FTS5's default unicode61 tokenizer, which
// splits on whitespace.
//
// For text that is purely ASCII/Latin, the original text is returned unchanged
// (FTS5's unicode61 tokenizer handles it correctly).
func segmentTextForFTS(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !containsCJKRunes(text) {
		return text
	}
	tokens := bm25.Tokenize(text)
	if len(tokens) == 0 {
		return text
	}
	return strings.Join(tokens, " ")
}

// buildFTSQuerySegmented builds an FTS5 MATCH expression from a query string.
// For CJK text, it uses gse tokenization to split into meaningful terms.
// For non-CJK text, it uses the original whitespace-based splitting.
func buildFTSQuerySegmented(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	if !containsCJKRunes(query) {
		return buildFTSQuery(query)
	}
	// Use gse tokenization for CJK queries
	tokens := bm25.Tokenize(query)
	if len(tokens) == 0 {
		return buildFTSQuery(query)
	}
	// Filter out single-char tokens that are common stop words
	meaningful := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// Keep multi-char tokens and single-char CJK tokens
		if len([]rune(t)) > 1 || hasCJKRune(t) {
			meaningful = append(meaningful, t)
		}
	}
	if len(meaningful) == 0 {
		return buildFTSQuery(query)
	}
	// Build OR query for better recall (any term match is relevant)
	parts := make([]string, 0, len(meaningful))
	for _, term := range meaningful {
		parts = append(parts, quoteFTSTerm(term))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, " OR ")
}

// hasCJKRune returns true if the string contains at least one CJK character.
// Alias for containsCJKRunes for internal use.
func hasCJKRune(s string) bool {
	return containsCJKRunes(s)
}

// containsCJKRunes returns true if the string contains any CJK Unified Ideograph characters.
// Used to detect when FTS5's unicode61 tokenizer is likely to fail on Chinese/Japanese/Korean text.
func containsCJKRunes(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}

// RebuildFTSIndex drops and rebuilds all FTS indexes with properly segmented content.
// This should be called once after upgrading to gse-based FTS to re-index existing data.
// The rebuild is atomic per table — if any table rebuild fails, that table's FTS is
// restored to its previous state (the other tables that succeeded remain rebuilt).
func (s *SQLiteStore) RebuildFTSIndex(ctx context.Context) error {
	if err := s.rebuildNodesFTS(ctx); err != nil {
		return fmt.Errorf("rebuild document_nodes_fts: %w", err)
	}
	if err := s.rebuildCardsFTS(ctx); err != nil {
		return fmt.Errorf("rebuild knowledge_cards_fts: %w", err)
	}
	if err := s.rebuildFactsFTS(ctx); err != nil {
		return fmt.Errorf("rebuild knowledge_facts_fts: %w", err)
	}
	// Backfill card embeddings if embedder is available
	if s.embedder != nil && !embedding.IsNoop(s.embedder) {
		if err := s.backfillCardEmbeddings(ctx); err != nil {
			// Non-fatal: FTS is rebuilt, embeddings can be backfilled later
			fmt.Printf("[knowledge] embedding backfill failed: %v\n", err)
		}
	}
	return nil
}

func (s *SQLiteStore) rebuildNodesFTS(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, text FROM document_nodes`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type nodeRow struct {
		id, title, text string
	}
	var nodes []nodeRow
	for rows.Next() {
		var n nodeRow
		if err := rows.Scan(&n.id, &n.title, &n.text); err != nil {
			return err
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Atomic: delete + re-insert in one transaction-like sequence.
	// If insert fails midway, at least the successfully inserted rows are searchable.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM document_nodes_fts`); err != nil {
		return err
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`,
			n.id, segmentTextForFTS(n.title), segmentTextForFTS(n.text))
	}
	return nil
}

func (s *SQLiteStore) rebuildCardsFTS(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, claim, summary, entities_json, topics_json, tags_json FROM knowledge_cards`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cardRow struct {
		id, title, claim, summary, entitiesJSON, topicsJSON, tagsJSON string
	}
	var cards []cardRow
	for rows.Next() {
		var c cardRow
		if err := rows.Scan(&c.id, &c.title, &c.claim, &c.summary, &c.entitiesJSON, &c.topicsJSON, &c.tagsJSON); err != nil {
			return err
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_cards_fts`); err != nil {
		return err
	}
	for _, c := range cards {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		parts := []string{c.summary}
		var entities []string
		if c.entitiesJSON != "" && c.entitiesJSON != "null" {
			_ = json.Unmarshal([]byte(c.entitiesJSON), &entities)
		}
		if len(entities) > 0 {
			parts = append(parts, strings.Join(entities, " "))
		}
		var topics []string
		if c.topicsJSON != "" && c.topicsJSON != "null" {
			_ = json.Unmarshal([]byte(c.topicsJSON), &topics)
		}
		if len(topics) > 0 {
			parts = append(parts, strings.Join(topics, " "))
		}
		var tags []string
		if c.tagsJSON != "" && c.tagsJSON != "null" {
			_ = json.Unmarshal([]byte(c.tagsJSON), &tags)
		}
		if len(tags) > 0 {
			parts = append(parts, strings.Join(tags, " "))
		}
		ftsSummary := strings.TrimSpace(strings.Join(parts, "\n"))
		_, _ = s.db.ExecContext(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`,
			c.id, segmentTextForFTS(c.title), segmentTextForFTS(c.claim), segmentTextForFTS(ftsSummary))
	}
	return nil
}

func (s *SQLiteStore) rebuildFactsFTS(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, subject, predicate, object FROM knowledge_facts`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type factRow struct {
		id, subject, predicate, object string
	}
	var facts []factRow
	for rows.Next() {
		var f factRow
		if err := rows.Scan(&f.id, &f.subject, &f.predicate, &f.object); err != nil {
			return err
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_facts_fts`); err != nil {
		return err
	}
	for _, f := range facts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`,
			f.id, segmentTextForFTS(f.subject), segmentTextForFTS(f.predicate), segmentTextForFTS(f.object))
	}
	return nil
}

// ftsSegmentationVersion is bumped when the segmentation algorithm changes,
// forcing a re-index on next startup.
const ftsSegmentationVersion = 1

// HasFTSSegmentationMarker checks if the FTS index has already been rebuilt
// with the current segmentation version. Uses a lightweight metadata table.
func (s *SQLiteStore) HasFTSSegmentationMarker() bool {
	var version int
	err := s.db.QueryRow(`SELECT value FROM knowledge_meta WHERE key = 'fts_segmentation_version'`).Scan(&version)
	if err != nil {
		return false
	}
	return version >= ftsSegmentationVersion
}

// SetFTSSegmentationMarker persists the current segmentation version so
// RebuildFTSIndex is not repeated on subsequent startups.
func (s *SQLiteStore) SetFTSSegmentationMarker() {
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_meta (key TEXT PRIMARY KEY, value TEXT)`)
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO knowledge_meta(key, value) VALUES ('fts_segmentation_version', ?)`, ftsSegmentationVersion)
}
