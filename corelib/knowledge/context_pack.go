package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const contextPackNodeWindowRunes = 2000

func (s *SQLiteStore) ContextPack(ctx context.Context, opts ContextPackOptions) (ContextPackResult, error) {
	searchOpts := opts.SearchOptions
	query := strings.TrimSpace(searchOpts.Query)
	if query == "" {
		return ContextPackResult{Query: query, Notes: []string{"local_context_pack_no_llm"}}, nil
	}
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 8
	}
	if maxItems > 30 {
		maxItems = 30
	}
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = 6000
	}
	if maxChars > 20000 {
		maxChars = 20000
	}
	if searchOpts.Limit <= 0 || searchOpts.Limit < maxItems {
		searchOpts.Limit = maxItems * 2
	}
	if searchOpts.Limit > 50 {
		searchOpts.Limit = 50
	}
	results, err := s.Search(ctx, searchOpts)
	if err != nil {
		return ContextPackResult{}, err
	}
	results = selectContextPackResults(results, maxItems)
	contextEnriched := false
	if enriched, changed, err := s.enrichContextPackNodes(ctx, query, results); err != nil {
		return ContextPackResult{}, err
	} else {
		results = enriched
		contextEnriched = changed
	}
	pack := ContextPackResult{
		Query:     query,
		Items:     make([]ContextPackItem, 0, maxItems),
		Citations: make([]Citation, 0, maxItems),
		Notes:     []string{"local_context_pack_no_llm", "card_fact_node_ranked", "budgeted_context"},
	}
	if contextEnriched {
		pack.Notes = append(pack.Notes, "parent_neighbor_context")
	}
	if len(results) > 1 {
		pack.Notes = append(pack.Notes, "mmr_source_chunk_diversified")
	}
	seenCitations := make(map[string]struct{})
	for _, result := range results {
		if len(pack.Items) >= maxItems || pack.CharacterCount >= maxChars {
			break
		}
		text := contextPackText(result)
		if text == "" {
			continue
		}
		remaining := maxChars - pack.CharacterCount
		if remaining <= 0 {
			break
		}
		text, truncated := truncateContextText(text, remaining)
		if text == "" {
			break
		}
		if truncated && !hasContextPackNote(pack.Notes, "truncated_to_budget") {
			pack.Notes = append(pack.Notes, "truncated_to_budget")
		}
		label := fmt.Sprintf("K%d", len(pack.Items)+1)
		item := ContextPackItem{
			Label:      label,
			ResultType: result.ResultType,
			Title:      contextPackTitle(result),
			Text:       text,
			SourceID:   result.Source.ID,
			Citation:   result.Citation,
			Score:      result.Score,
		}
		if result.NodeType == NodeTypeImage || result.Source.Kind == SourceKindImage {
			item.Citation = FormatImageCitationLabel(result)
		}
		pack.Items = append(pack.Items, item)
		pack.CharacterCount += len([]rune(text))
		citation := citationFromResult(result)
		citation = ProjectImageCitationForTool(citation, result)
		key := citationKey(citation)
		if _, ok := seenCitations[key]; !ok {
			seenCitations[key] = struct{}{}
			citation.Label = label + " " + citation.Label
			pack.Citations = append(pack.Citations, citation)
		}
	}
	pack.Count = len(pack.Items)
	return pack, nil
}

// selectContextPackResults applies a deterministic MMR pass after retrieval.
// It avoids wasting the prompt budget on overlapping children of one parent
// while retaining enough relevance to answer focused, single-document queries.
func selectContextPackResults(results []SearchResult, limit int) []SearchResult {
	if len(results) <= 1 || limit <= 0 {
		return results
	}
	if limit > len(results) {
		limit = len(results)
	}
	maxScore := results[0].Score
	for _, r := range results[1:] {
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}
	selected := make([]SearchResult, 0, limit)
	used := make([]bool, len(results))
	for len(selected) < limit {
		best, bestScore := -1, math.Inf(-1)
		for i, candidate := range results {
			if used[i] {
				continue
			}
			relevance := candidate.Score / maxScore
			diversity := 0.0
			for _, previous := range selected {
				if sim := contextResultSimilarity(candidate, previous); sim > diversity {
					diversity = sim
				}
			}
			// MMR's relevance weight deliberately stays high: it diversifies only
			// among otherwise comparable results.
			score := .78*relevance - .22*diversity
			if best < 0 || score > bestScore || (score == bestScore && contextResultKey(candidate) < contextResultKey(results[best])) {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		selected = append(selected, results[best])
	}
	return selected
}

func contextResultKey(r SearchResult) string {
	if r.NodeID != "" {
		return "node:" + r.NodeID
	}
	if r.CardID != "" {
		return "card:" + r.CardID
	}
	return r.ResultType + ":" + r.FactID + ":" + r.RowID
}

func contextResultSimilarity(a, b SearchResult) float64 {
	if a.NodeID != "" && a.NodeID == b.NodeID {
		return 1
	}
	if a.ParentNodeID != "" && a.ParentNodeID == b.ParentNodeID {
		return 1
	}
	if a.Source.ID != "" && a.Source.ID == b.Source.ID {
		return .25 + .75*contextTextJaccard(contextPackText(a), contextPackText(b))
	}
	return contextTextJaccard(contextPackText(a), contextPackText(b))
}

func contextTextJaccard(a, b string) float64 {
	left, right := contextTextTokens(a), contextTextTokens(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for token := range left {
		if _, ok := right[token]; ok {
			intersection++
		}
	}
	return float64(intersection) / float64(len(left)+len(right)-intersection)
}

func contextTextTokens(text string) map[string]struct{} {
	text = strings.ToLower(normalizeKnowledgeText(text))
	tokens := make(map[string]struct{})
	runes := []rune(text)
	for i := 0; i < len(runes); {
		if isNoSpaceScriptRune(runes[i]) {
			if i+1 < len(runes) && isNoSpaceScriptRune(runes[i+1]) {
				tokens[string(runes[i:i+2])] = struct{}{}
			}
			i++
			continue
		}
		if !unicode.IsLetter(runes[i]) && !unicode.IsNumber(runes[i]) {
			i++
			continue
		}
		start := i
		for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsNumber(runes[i])) && !isNoSpaceScriptRune(runes[i]) {
			i++
		}
		if start < i {
			tokens[string(runes[start:i])] = struct{}{}
		}
	}
	return tokens
}

// enrichContextPackNodes expands short FTS snippets from the original node and
// adds ordered neighboring text for chunked hits. Neighbors stay on the short
// context-pack window.
func (s *SQLiteStore) enrichContextPackNodes(ctx context.Context, query string, results []SearchResult) ([]SearchResult, bool, error) {
	return s.expandNodeContext(ctx, query, results, false)
}

type chunkSibling struct {
	id   string
	text string
}

type chunkFamily struct {
	title    string
	text     string
	children []chunkSibling
}

// expandNodeContext attaches sibling chunks. fullNeighbors keeps each sibling
// that fits in one chunk, so a model read does not see a short window ending
// in "...". The context pack keeps its shorter neighbor window.
func (s *SQLiteStore) expandNodeContext(ctx context.Context, query string, results []SearchResult, fullNeighbors bool) ([]SearchResult, bool, error) {
	changed := false
	siblings := make(map[string]chunkFamily)
	for i := range results {
		if results[i].ResultType != "node" || results[i].NodeID == "" {
			continue
		}
		if fullNeighbors && (results[i].NodeType == NodeTypeImage || results[i].Source.Kind == SourceKindImage) {
			continue
		}
		anchorID := results[i].ParentNodeID
		if anchorID == "" {
			if !fullNeighbors {
				var text string
				if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(text, '') FROM document_nodes WHERE id = ?`, results[i].NodeID).Scan(&text); err != nil {
					if err == sql.ErrNoRows {
						continue
					}
					return nil, false, err
				}
				// A chunk parent stores no text. Blanking the snippet would drop
				// the highlight the search page kept.
				if strings.TrimSpace(text) == "" {
					continue
				}
				results[i].Snippet = searchBodyTextWindowLimit(text, query, contextPackNodeWindowRunes)
				continue
			}
			// The hit is a section. Search indexed its title and left the empty
			// body as a highlight. The stored record is the child chunks.
			anchorID = results[i].NodeID
		}
		loaded, ok := siblings[anchorID]
		if !ok {
			var err error
			loaded, err = s.loadChunkSiblings(ctx, anchorID)
			if err != nil {
				return nil, false, err
			}
			siblings[anchorID] = loaded
		}
		position := -1
		if results[i].ParentNodeID == "" {
			if strings.TrimSpace(loaded.text) != "" || !chunkChildrenHaveText(loaded.children) {
				continue
			}
			position = matchingChunkChild(loaded.children, query)
		} else {
			for n, child := range loaded.children {
				if child.id == results[i].NodeID {
					position = n
					break
				}
			}
		}
		if position < 0 {
			continue
		}
		// An empty sibling, such as an image node stored beside a paragraph,
		// is not evidence. Stopping on it would show a bare marker and hide
		// the next stored chunk.
		adjacent := func(step int) string {
			for n := position + step; n >= 0 && n < len(loaded.children); n += step {
				text := loaded.children[n].text
				if fullNeighbors {
					text = storedNodeExcerpt(text, query)
				} else {
					text = searchBodyTextWindow(text, query)
				}
				if text != "" {
					return text
				}
			}
			return ""
		}
		current := searchBodyTextWindowLimit(loaded.children[position].text, query, contextPackNodeWindowRunes)
		if fullNeighbors {
			current = storedNodeExcerpt(loaded.children[position].text, query)
		}
		if strings.TrimSpace(current) == "" && !fullNeighbors {
			// The stored child has no text. Keep the highlight Search left.
			// A model read does not copy that highlight: its FTS form ends in
			// "...", which is the signal that the record was cut.
			current = strings.TrimSpace(results[i].Snippet)
		}
		preceding := adjacent(-1)
		following := adjacent(1)
		if current == "" && preceding == "" && following == "" {
			continue
		}
		parts := make([]string, 0, 4)
		if parentTitle := strings.TrimSpace(loaded.title); parentTitle != "" {
			var title string
			if fullNeighbors {
				title = storedNodeExcerpt(parentTitle, query)
			} else {
				title, _ = truncateContextText(parentTitle, 160)
			}
			if title != "" {
				parts = append(parts, title)
			}
		}
		// The context pack keeps the hit first. A model read follows stored
		// sibling order so the record is not rearranged around the hit.
		if fullNeighbors && preceding != "" {
			parts = append(parts, "[preceding] "+preceding)
		}
		if current != "" {
			parts = append(parts, current)
		}
		if !fullNeighbors && preceding != "" {
			parts = append(parts, "[preceding] "+preceding)
		}
		if following != "" {
			parts = append(parts, "[following] "+following)
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if text != "" && text != results[i].Snippet {
			results[i].Snippet = text
			changed = true
		}
	}
	return results, changed, nil
}

func (s *SQLiteStore) loadChunkSiblings(ctx context.Context, parentID string) (chunkFamily, error) {
	var family chunkFamily
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(title, ''), COALESCE(text, '') FROM document_nodes WHERE id = ?`, parentID).Scan(&family.title, &family.text); err != nil && err != sql.ErrNoRows {
		return chunkFamily{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(text, '') FROM document_nodes WHERE parent_id = ?
		ORDER BY CAST(CASE WHEN json_valid(metadata_json) THEN COALESCE(json_extract(metadata_json, '$.chunk_index'), '0') ELSE '0' END AS INTEGER) ASC, offset ASC, id ASC`, parentID)
	if err != nil {
		return chunkFamily{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var child chunkSibling
		if err := rows.Scan(&child.id, &child.text); err != nil {
			return chunkFamily{}, err
		}
		family.children = append(family.children, child)
	}
	if err := rows.Err(); err != nil {
		return chunkFamily{}, err
	}
	return family, nil
}

func chunkChildrenHaveText(children []chunkSibling) bool {
	for _, child := range children {
		if strings.TrimSpace(child.text) != "" {
			return true
		}
	}
	return false
}

// matchingChunkChild picks the stored child a section hit stands for. A child
// that contains the query is the record the search found. Otherwise the first
// stored child is the start of the section.
func matchingChunkChild(children []chunkSibling, query string) int {
	terms := extractQueryTermsForSnippet(query)
	first := -1
	for i, child := range children {
		text := strings.TrimSpace(child.text)
		if text == "" {
			continue
		}
		if first < 0 {
			first = i
		}
		lower := strings.ToLower(text)
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				return i
			}
		}
	}
	return first
}

func contextPackTitle(result SearchResult) string {
	if result.NodeType == NodeTypeImage || result.Source.Kind == SourceKindImage {
		for _, candidate := range []string{result.CardTitle, result.NodeTitle, result.Source.Title, result.Source.RelativePath} {
			if candidate = SafeImageDisplayText(candidate); candidate != "" {
				return candidate
			}
		}
		if sourceID := strings.TrimSpace(result.Source.ID); sourceID != "" {
			return sourceID
		}
		return "knowledge image"
	}
	if result.ResultType == "table_row" {
		parts := nonEmptyStrings(result.Source.Title, result.SheetName)
		if result.RowIndex > 0 {
			parts = append(parts, fmt.Sprintf("row %d", result.RowIndex))
		}
		if len(parts) > 0 {
			return strings.Join(parts, " / ")
		}
	}
	for _, candidate := range []string{result.CardTitle, result.NodeTitle, result.Source.Title, result.Source.RelativePath, result.Source.CanonicalURI, result.Source.URI} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return result.ResultType
}

// SearchEvidenceText is the stored body followed by one claim. Callers that
// show a hit to a model use this so a highlight snippet is not the only text.
func SearchEvidenceText(result SearchResult) string {
	return strings.TrimSpace(strings.Join(nonEmptyStrings(searchBodyText(result), searchClaimLine(result)), "\n"))
}

func contextPackText(result SearchResult) string {
	return SearchEvidenceText(result)
}

// searchBodyText is the node window or the table row. Claims stay out of it.
func searchBodyText(result SearchResult) string {
	if result.ResultType == "table_row" {
		if summary := strings.TrimSpace(result.Summary); summary != "" {
			return summary
		}
		return strings.TrimSpace(result.Snippet)
	}
	if result.ResultType == "fact" {
		fact := strings.TrimSpace(strings.Join(nonEmptyStrings(result.Subject, result.Predicate, result.Object), " "))
		if fact != "" {
			return fact
		}
	}
	if snippet := strings.TrimSpace(result.Snippet); snippet != "" {
		return snippet
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		return summary
	}
	if result.ResultType == "card" {
		return strings.TrimSpace(result.Claim)
	}
	return ""
}

// searchClaimLine is the one annotation that follows the body. A fact triple
// wins over a card claim so the same sentence is not repeated when the body
// already contains it.
func searchClaimLine(result SearchResult) string {
	body := strings.TrimSpace(searchBodyText(result))
	candidates := make([]string, 0, 3)
	if fact := strings.TrimSpace(strings.Join(nonEmptyStrings(result.Subject, result.Predicate, result.Object), " ")); fact != "" {
		candidates = append(candidates, fact)
	}
	if claim := strings.TrimSpace(result.Claim); claim != "" {
		candidates = append(candidates, claim)
	}
	title := strings.TrimSpace(result.CardTitle)
	if title != "" && !strings.EqualFold(title, strings.TrimSpace(result.NodeTitle)) && !strings.EqualFold(title, strings.TrimSpace(result.Source.Title)) {
		candidates = append(candidates, title)
	}
	for _, candidate := range candidates {
		if shown, ok := claimAnnotation(body, candidate); ok {
			return shown
		}
	}
	return ""
}

// claimAnnotation is the card line that still adds information after the
// stored body. Distill keeps a cut claim as a prefix plus "...". Exact
// equality misses that prefix, and the marker then reads as a truncated
// record. A cut whose text is already in the body is omitted. A cut that
// adds other words keeps those words without the marker.
func claimAnnotation(body, candidate string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}
	if body != "" && strings.EqualFold(candidate, body) {
		return "", false
	}
	cut := strings.TrimSuffix(candidate, "...")
	if cut == candidate {
		return candidate, true
	}
	cut = strings.TrimSpace(cut)
	if cut == "" {
		return "", false
	}
	if body != "" && strings.Contains(strings.ToLower(normalizeWhitespace(body)), strings.ToLower(normalizeWhitespace(cut))) {
		return "", false
	}
	return cut, true
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func truncateContextText(text string, maxChars int) (string, bool) {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || text == "" {
		return "", false
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	if maxChars <= 3 {
		return string(runes[:maxChars]), true
	}
	return strings.TrimSpace(string(runes[:maxChars-3])) + "...", true
}

func hasContextPackNote(notes []string, note string) bool {
	for _, item := range notes {
		if item == note {
			return true
		}
	}
	return false
}
