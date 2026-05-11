package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
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
func (s *SQLiteStore) RebuildFTSIndex(ctx context.Context) error {
	// Rebuild document_nodes_fts
	if _, err := s.db.ExecContext(ctx, `DELETE FROM document_nodes_fts`); err != nil {
		return fmt.Errorf("clear document_nodes_fts: %w", err)
	}
	nodeRows, err := s.db.QueryContext(ctx, `SELECT id, title, text FROM document_nodes`)
	if err != nil {
		return fmt.Errorf("query document_nodes: %w", err)
	}
	for nodeRows.Next() {
		var id, title, text string
		if err := nodeRows.Scan(&id, &title, &text); err != nil {
			_ = nodeRows.Close()
			return err
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`,
			id, segmentTextForFTS(title), segmentTextForFTS(text))
	}
	if err := nodeRows.Close(); err != nil {
		return err
	}
	if err := nodeRows.Err(); err != nil {
		return err
	}

	// Rebuild knowledge_cards_fts
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_cards_fts`); err != nil {
		return fmt.Errorf("clear knowledge_cards_fts: %w", err)
	}
	cardRows, err := s.db.QueryContext(ctx, `SELECT id, title, claim, summary, entities_json, topics_json, tags_json FROM knowledge_cards`)
	if err != nil {
		return fmt.Errorf("query knowledge_cards: %w", err)
	}
	for cardRows.Next() {
		var id, title, claim, summary, entitiesJSON, topicsJSON, tagsJSON string
		if err := cardRows.Scan(&id, &title, &claim, &summary, &entitiesJSON, &topicsJSON, &tagsJSON); err != nil {
			_ = cardRows.Close()
			return err
		}
		// Reconstruct ftsSummary from stored fields
		parts := []string{summary}
		var entities []string
		if entitiesJSON != "" && entitiesJSON != "null" {
			_ = json.Unmarshal([]byte(entitiesJSON), &entities)
		}
		if len(entities) > 0 {
			parts = append(parts, strings.Join(entities, " "))
		}
		var topics []string
		if topicsJSON != "" && topicsJSON != "null" {
			_ = json.Unmarshal([]byte(topicsJSON), &topics)
		}
		if len(topics) > 0 {
			parts = append(parts, strings.Join(topics, " "))
		}
		var tags []string
		if tagsJSON != "" && tagsJSON != "null" {
			_ = json.Unmarshal([]byte(tagsJSON), &tags)
		}
		if len(tags) > 0 {
			parts = append(parts, strings.Join(tags, " "))
		}
		ftsSummary := strings.TrimSpace(strings.Join(parts, "\n"))
		_, _ = s.db.ExecContext(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`,
			id, segmentTextForFTS(title), segmentTextForFTS(claim), segmentTextForFTS(ftsSummary))
	}
	if err := cardRows.Close(); err != nil {
		return err
	}
	if err := cardRows.Err(); err != nil {
		return err
	}

	// Rebuild knowledge_facts_fts
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_facts_fts`); err != nil {
		return fmt.Errorf("clear knowledge_facts_fts: %w", err)
	}
	factRows, err := s.db.QueryContext(ctx, `SELECT id, subject, predicate, object FROM knowledge_facts`)
	if err != nil {
		return fmt.Errorf("query knowledge_facts: %w", err)
	}
	for factRows.Next() {
		var id, subject, predicate, object string
		if err := factRows.Scan(&id, &subject, &predicate, &object); err != nil {
			_ = factRows.Close()
			return err
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`,
			id, segmentTextForFTS(subject), segmentTextForFTS(predicate), segmentTextForFTS(object))
	}
	if err := factRows.Close(); err != nil {
		return err
	}
	return factRows.Err()
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
