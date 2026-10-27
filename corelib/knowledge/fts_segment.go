package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Recall contract for no-space scripts (Han, kana, Hangul, Thai)
//
// The lexical index holds gse words plus deterministic overlapping n-grams
// (2 and 3) per contiguous script run. Query building must not require a gse
// token on the indexed side: gse is context sensitive, so the same surface
// string can be one word in the query and two words in the document, or two
// words in a library indexed by an older dictionary. Short spans are therefore
// matched through their n-grams. Precision for names and multi-span terms is
// enforced afterwards by retainEntityMentionEvidence, which requires every
// span to appear in the result.
//
// Keep the indexer (segmentTextForFTS) and the query builder
// (buildFTSQuerySegmented) on the same n-gram contract.
//
// segmentTextForFTS tokenizes text and returns a space-separated string
// suitable for FTS5 indexing. This enables Chinese text to be properly
// searchable via FTS5's default unicode61 tokenizer, which splits on whitespace.
//
// Indexed text has two independent parts for no-space scripts:
//   - gse dictionary/HMM words, which give BM25 rare-term weight;
//   - overlapping character n-grams (2 and 3) per contiguous script run.
func segmentTextForFTS(text string) string {
	text = normalizeKnowledgeLexicalText(text)
	if text == "" {
		return ""
	}
	if !containsNoSpaceScriptRunes(text) {
		return text
	}
	tokens := bm25.Segment(text)
	tokens = append(tokens, scriptNGramTokens(text, 2, 3)...)
	if len(tokens) == 0 {
		return text
	}
	return strings.Join(uniqueFTSTerms(tokens), " ")
}

// buildFTSQuerySegmented builds an FTS5 MATCH expression from a query string.
// Plain Latin queries keep the whitespace split. No-space-script queries are
// expressed over the deterministic n-gram contract instead of gse tokens:
// a short span becomes the intersection of its overlapping bigrams, so it is
// found whenever the characters occur adjacently in the text, no matter how
// the dictionary cut either side.
func buildFTSQuerySegmented(query string) string {
	query = normalizeKnowledgeLexicalText(query)
	if query == "" {
		return ""
	}
	if !containsNoSpaceScriptRunes(query) {
		return buildFTSQuery(query)
	}
	// A short Han query is an entity span or a term. Each span is rendered on
	// its own n-grams; composite names AND their spans. Evidence filtering
	// later enforces the whole span.
	if anchors, strict := bm25.QueryAnchors(query); len(anchors) > 0 && (strict || len(anchors) == 1) {
		parts := make([]string, 0, len(anchors))
		for _, anchor := range anchors {
			if expr := buildNoSpaceSpanFTSExpr(anchor); expr != "" {
				parts = append(parts, expr)
			}
		}
		if len(parts) > 0 {
			if !strict || len(parts) == 1 {
				return parts[0]
			}
			return strings.Join(parts, " AND ")
		}
	}
	// Longer sentences and questions keep recall-first OR over gse words and
	// n-grams: every word of a question is not expected to co-occur.
	tokens := bm25.Segment(query)
	tokens = append(tokens, scriptNGramTokens(query, 2, 3)...)
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
	meaningful = uniqueFTSTerms(meaningful)
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

// buildNoSpaceSpanFTSExpr renders one span of a no-space script as an FTS5
// expression over the indexed n-grams.
//
// A span of at most three characters is itself an indexed n-gram, so the
// quoted span is exact and selective. A longer span cannot be represented by
// one n-gram; it is the intersection of all of its overlapping bigrams. Every
// bigram of the span is indexed whenever the span occurs in a contiguous run,
// which makes the expression equivalent to "this span occurs here" without
// depending on a dictionary token that gse may or may not have produced.
//
// Bigrams, not trigrams, are the common denominator: every index generation
// carries per-run bigrams, including libraries written before the current
// n-gram contract was installed. A 4+ character span is always classified as
// strict, so the evidence filter still requires the whole span afterwards;
// a broad candidate set therefore cannot leak fragment matches into results.
func buildNoSpaceSpanFTSExpr(span string) string {
	span = strings.TrimSpace(span)
	if span == "" {
		return ""
	}
	// Mixed spans (Latin letters, digits, spaces, punctuation) are not covered
	// by the n-gram contract. Keep them as one literal term.
	for _, r := range span {
		if !isNoSpaceScriptRune(r) {
			return quoteFTSTerm(span)
		}
	}
	if len([]rune(span)) <= 3 {
		return quoteFTSTerm(span)
	}
	grams := uniqueFTSTerms(scriptNGrams(span, 2))
	parts := make([]string, 0, len(grams))
	for _, gram := range grams {
		parts = append(parts, quoteFTSTerm(gram))
	}
	if len(parts) == 0 {
		return quoteFTSTerm(span)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, " AND ")
}

// retainEntityMentionEvidence drops hits that do not contain every content
// anchor of a composite or unknown name. A single dictionary word is left
// unchanged so semantic neighbors of that word can still be used. Fragment
// overlaps stay out of the evidence the model is told to trust.
func retainEntityMentionEvidence(query string, results []SearchResult) []SearchResult {
	anchors, strict := bm25.QueryAnchors(normalizeKnowledgeLexicalText(query))
	if !strict || len(anchors) == 0 || len(results) == 0 {
		return results
	}
	kept := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if evidenceHasAnchors(result, anchors) {
			kept = append(kept, result)
		}
	}
	return kept
}

// strictAnchorsAlreadyCovered reports that the lexical candidate pool is
// already saturated with node hits containing every content anchor, so the
// full-table LIKE scan cannot add recall.
//
// One covering hit is not saturation. A 4+ character span is matched through
// its bigram intersection, so a node can satisfy the FTS expression while
// other documents that contain the same span never enter the pool. The LIKE
// scan is the correctness net for no-space scripts; it is skipped only when
// the pool this query would keep is already full.
func strictAnchorsAlreadyCovered(query string, results []SearchResult, want int) bool {
	anchors, strict := bm25.QueryAnchors(normalizeKnowledgeLexicalText(query))
	if !strict || len(anchors) == 0 || len(results) == 0 || want <= 0 {
		return false
	}
	covered := 0
	for _, result := range results {
		// A card can name the entity and still omit the original passage.
		// Only a node hit is enough to skip the full-text LIKE scan.
		if result.ResultType != "node" || !evidenceHasAnchors(result, anchors) {
			continue
		}
		covered++
		if covered >= want {
			return true
		}
	}
	return false
}

func evidenceHasAnchors(result SearchResult, anchors []string) bool {
	fields := []string{
		result.CardTitle,
		result.Claim,
		result.Summary,
		result.Snippet,
		result.NodeTitle,
		result.Subject,
		result.Predicate,
		result.Object,
		result.ColumnName,
		result.SheetName,
		result.Source.Title,
	}
	for _, anchor := range anchors {
		if !fieldContainsAnchor(fields, strings.ToLower(anchor)) {
			return false
		}
	}
	return true
}

func fieldContainsAnchor(fields []string, anchor string) bool {
	for _, field := range fields {
		field = strings.ToLower(field)
		if field == "" {
			continue
		}
		if strings.Contains(field, anchor) {
			return true
		}
		// Only FTS token spaces need joining. A line break in the original
		// text must not turn the end of one line and the start of the next
		// into a name.
		if strings.Contains(field, " ") && strings.Contains(compactHanTokenGaps(field), anchor) {
			return true
		}
	}
	return false
}

// compactHanTokenGaps joins Han characters that FTS segmentation separated
// with spaces. "中国人 奇强" matches the name again. "奇 安信" stays 奇安信 and
// does not become 奇强.
func compactHanTokenGaps(text string) string {
	runes := []rune(text)
	if len(runes) < 3 {
		return text
	}
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) && isCJK(runes[i-1]) && isCJK(runes[i+1]) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// normalizeKnowledgeLexicalText applies search-only folding. Source text stays
// untouched so citations retain their original language and spelling.
func normalizeKnowledgeLexicalText(text string) string {
	text = normalizeKnowledgeText(text)
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if isArabicRune(r) {
			switch r {
			case '\u0622', '\u0623', '\u0625': // alef variants
				r = '\u0627'
			case '\u0649': // alef maqsura
				r = '\u064a'
			case '\u0629': // teh marbuta
				r = '\u0647'
			}
		}
		// Arabic harakat and other combining marks are optional in queries.
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
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

func containsNoSpaceScriptRunes(s string) bool {
	for _, r := range s {
		if isNoSpaceScriptRune(r) {
			return true
		}
	}
	return false
}

// scriptNGrams returns overlapping character n-grams of one size for every
// contiguous no-space-script run.
func scriptNGrams(text string, n int) []string {
	if n < 1 {
		return nil
	}
	return scriptNGramTokens(text, n)
}

// scriptNGramTokens returns overlapping n-grams for several sizes in one pass
// over the text. Runs shorter than a size are kept whole so a single character
// or a short word still contributes a token.
//
// n-grams are derived per run, never across a run boundary: joining the
// remaining characters of two sentences would index bigrams such as 件系 from
// "文件。系统", which never occurs in the source text and only adds false
// candidates and noise to BM25 document length.
func scriptNGramTokens(text string, sizes ...int) []string {
	if len(sizes) == 0 {
		return nil
	}
	runs := noSpaceScriptRuneRuns(text)
	if len(runs) == 0 {
		return nil
	}
	capacity := 0
	for _, run := range runs {
		capacity += len(run)
	}
	tokens := make([]string, 0, capacity*len(sizes))
	for _, runes := range runs {
		for _, n := range sizes {
			if n < 1 {
				continue
			}
			if len(runes) < n {
				tokens = append(tokens, string(runes))
				continue
			}
			for i := 0; i+n <= len(runes); i++ {
				tokens = append(tokens, string(runes[i:i+n]))
			}
		}
	}
	return tokens
}

// noSpaceScriptRuneRuns splits text into maximal runs of no-space-script runes.
func noSpaceScriptRuneRuns(text string) [][]rune {
	var runs [][]rune
	var current []rune
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, current)
			current = nil
		}
	}
	for _, r := range text {
		if isNoSpaceScriptRune(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func uniqueFTSTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}

// isCJKStopChar returns true for high-frequency CJK characters that are too
// common to be useful as LIKE search terms. These are function words (particles,
// conjunctions, pronouns) that appear in nearly every Chinese text and would
// match too many irrelevant documents.
func isCJKStopChar(r rune) bool {
	switch r {
	case '的', '了', '是', '在', '有', '和', '与', '或', '不', '也',
		'都', '就', '而', '及', '等', '这', '那', '你', '我', '他',
		'她', '它', '们', '个', '为', '到', '把', '被', '让', '从',
		'对', '中', '上', '下', '里', '吗', '呢', '吧', '啊', '哦':
		return true
	}
	return false
}

// RebuildFTSSegmentation rebuilds lexical indexes from stored text.
// Body indexes are document nodes and table rows. Card and fact indexes stay
// so claim annotations can still collapse onto those bodies.
// It does not embed. Callers that also need vectors use RebuildFTSIndex or
// SetEmbedder. A second embedding pass would not raise
// LibraryEmbeddingRefreshActive, so search would wait behind it.
func (s *SQLiteStore) RebuildFTSSegmentation(ctx context.Context) error {
	if err := s.rebuildNodesFTS(ctx); err != nil {
		return fmt.Errorf("rebuild document_nodes_fts: %w", err)
	}
	if err := s.rebuildRowsFTS(ctx); err != nil {
		return fmt.Errorf("rebuild kb_rows_fts: %w", err)
	}
	if err := s.rebuildCardsFTS(ctx); err != nil {
		return fmt.Errorf("rebuild knowledge_cards_fts: %w", err)
	}
	if err := s.rebuildFactsFTS(ctx); err != nil {
		return fmt.Errorf("rebuild knowledge_facts_fts: %w", err)
	}
	return nil
}

// RebuildFTSIndex rebuilds lexical indexes, then fills missing node and row
// vectors from stored text. Card vectors are not rebuilt here.
func (s *SQLiteStore) RebuildFTSIndex(ctx context.Context) error {
	if err := s.RebuildFTSSegmentation(ctx); err != nil {
		return err
	}
	if emb := s.currentEmbedder(); emb != nil && !embedding.IsNoop(emb) {
		if err := s.BackfillNodeEmbeddings(ctx); err != nil {
			fmt.Printf("[knowledge] node embedding backfill failed: %v\n", err)
		}
		if err := s.BackfillTableRowEmbeddings(ctx); err != nil {
			fmt.Printf("[knowledge] table row embedding backfill failed: %v\n", err)
		}
	}
	return nil
}

const ftsRebuildPageSize = 128

// Pages stay small because the store keeps a single SQLite connection.
// A prepared statement or an open query would occupy that connection and
// block the following write. Each page is closed before its rows are inserted.
func (s *SQLiteStore) rebuildNodesFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM document_nodes_fts`); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(title, ''), COALESCE(text, '') FROM document_nodes WHERE id > ? ORDER BY id LIMIT ?`, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type nodeRow struct{ id, title, text string }
		page := make([]nodeRow, 0, ftsRebuildPageSize)
		for rows.Next() {
			var n nodeRow
			if err := rows.Scan(&n.id, &n.title, &n.text); err != nil {
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
		args := make([][]any, len(page))
		for i, n := range page {
			args[i] = []any{n.id, segmentTextForFTS(n.title), segmentTextForFTS(n.text)}
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

func (s *SQLiteStore) rebuildRowsFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kb_rows_fts`); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(primary_key_text, ''), COALESCE(row_text, '') FROM kb_rows WHERE id > ? ORDER BY id LIMIT ?`, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type rowFTS struct{ id, primaryKey, text string }
		page := make([]rowFTS, 0, ftsRebuildPageSize)
		for rows.Next() {
			var row rowFTS
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
		args := make([][]any, len(page))
		for i, row := range page {
			args[i] = []any{row.id, segmentTextForFTS(row.primaryKey), segmentTextForFTS(row.text)}
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES (?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

// rebuildSourceBodyFTS rewrites one source's node and row lexical indexes from
// the text already stored on those bodies. Segmentation runs between short
// writes so a page query is closed before the insert.
func (s *SQLiteStore) rebuildSourceBodyFTS(ctx context.Context, sourceID string) error {
	if err := s.rebuildSourceNodeFTS(ctx, sourceID); err != nil {
		return err
	}
	return s.rebuildSourceRowFTS(ctx, sourceID)
}

func (s *SQLiteStore) rebuildSourceNodeFTS(ctx context.Context, sourceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM document_nodes_fts WHERE node_id IN (SELECT id FROM document_nodes WHERE source_id = ?)`, sourceID); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(title, ''), COALESCE(text, '') FROM document_nodes WHERE source_id = ? AND id > ? ORDER BY id LIMIT ?`, sourceID, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type nodeRow struct{ id, title, text string }
		page := make([]nodeRow, 0, ftsRebuildPageSize)
		for rows.Next() {
			var n nodeRow
			if err := rows.Scan(&n.id, &n.title, &n.text); err != nil {
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
		args := make([][]any, len(page))
		for i, n := range page {
			args[i] = []any{n.id, segmentTextForFTS(n.title), segmentTextForFTS(n.text)}
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

func (s *SQLiteStore) rebuildSourceRowFTS(ctx context.Context, sourceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kb_rows_fts WHERE row_id IN (SELECT id FROM kb_rows WHERE source_id = ?)`, sourceID); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(primary_key_text, ''), COALESCE(row_text, '') FROM kb_rows WHERE source_id = ? AND id > ? ORDER BY id LIMIT ?`, sourceID, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type rowFTS struct{ id, primaryKey, text string }
		page := make([]rowFTS, 0, ftsRebuildPageSize)
		for rows.Next() {
			var row rowFTS
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
		args := make([][]any, len(page))
		for i, row := range page {
			args[i] = []any{row.id, segmentTextForFTS(row.primaryKey), segmentTextForFTS(row.text)}
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES (?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

func (s *SQLiteStore) rebuildCardsFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_cards_fts`); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(title, ''), COALESCE(claim, ''), COALESCE(summary, ''), COALESCE(entities_json, ''), COALESCE(topics_json, ''), COALESCE(tags_json, '') FROM knowledge_cards WHERE id > ? ORDER BY id LIMIT ?`, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type cardRow struct {
			id, title, claim, summary, entitiesJSON, topicsJSON, tagsJSON string
		}
		page := make([]cardRow, 0, ftsRebuildPageSize)
		for rows.Next() {
			var c cardRow
			if err := rows.Scan(&c.id, &c.title, &c.claim, &c.summary, &c.entitiesJSON, &c.topicsJSON, &c.tagsJSON); err != nil {
				rows.Close()
				return err
			}
			page = append(page, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		args := make([][]any, 0, len(page))
		for _, c := range page {
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
			args = append(args, []any{c.id, segmentTextForFTS(c.title), segmentTextForFTS(c.claim), segmentTextForFTS(ftsSummary)})
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

func (s *SQLiteStore) rebuildFactsFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_facts_fts`); err != nil {
		return err
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(subject, ''), COALESCE(predicate, ''), COALESCE(object, '') FROM knowledge_facts WHERE id > ? ORDER BY id LIMIT ?`, after, ftsRebuildPageSize)
		if err != nil {
			return err
		}
		type factRow struct{ id, subject, predicate, object string }
		page := make([]factRow, 0, ftsRebuildPageSize)
		for rows.Next() {
			var f factRow
			if err := rows.Scan(&f.id, &f.subject, &f.predicate, &f.object); err != nil {
				rows.Close()
				return err
			}
			page = append(page, f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		args := make([][]any, len(page))
		for i, f := range page {
			args[i] = []any{f.id, segmentTextForFTS(f.subject), segmentTextForFTS(f.predicate), segmentTextForFTS(f.object)}
		}
		if err := s.insertFTSPage(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`, args); err != nil {
			return err
		}
		if len(page) < ftsRebuildPageSize {
			return nil
		}
		after = page[len(page)-1].id
	}
}

// insertFTSPage writes one page in its own transaction. A rebuild runs while
// imports and searches keep the database busy, and the index table has already
// been cleared when this is called: a single SQLITE_BUSY here would leave the
// lexical index partial until the next successful rebuild. Retry the page on
// lock errors instead of abandoning it.
func (s *SQLiteStore) insertFTSPage(ctx context.Context, query string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	const attempts = 4
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
		err = s.insertFTSPageOnce(ctx, query, rows)
		if err == nil || !IsSQLiteLockedError(err) {
			return err
		}
	}
	return err
}

func (s *SQLiteStore) insertFTSPageOnce(ctx context.Context, query string, rows [][]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, query, row...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ftsSegmentationVersion is bumped when the segmentation algorithm changes,
// forcing the next version-gated rebuild to rewrite the lexical indexes.
// Version 2 indexes overlapping 2- and 3-grams per contiguous script run
// (previously one document-wide bigram stream), which is the contract
// short-span queries are built on.
const ftsSegmentationVersion = 2

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
