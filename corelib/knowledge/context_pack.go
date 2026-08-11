package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"unicode"
)

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
	if enriched, changed, err := s.enrichContextPackNodes(ctx, results); err != nil {
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

// enrichContextPackNodes adds a parent heading and immediate surrounding child
// text for chunked document hits. The original hit remains the citation anchor.
func (s *SQLiteStore) enrichContextPackNodes(ctx context.Context, results []SearchResult) ([]SearchResult, bool, error) {
	changed := false
	for i := range results {
		if results[i].ResultType != "node" || results[i].NodeID == "" || results[i].ParentNodeID == "" {
			continue
		}
		var parentTitle string
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(title, '') FROM document_nodes WHERE id = ?`, results[i].ParentNodeID).Scan(&parentTitle); err != nil && err != sql.ErrNoRows {
			return nil, false, err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(text, '') FROM document_nodes WHERE parent_id = ? ORDER BY offset ASC, id ASC`, results[i].ParentNodeID)
		if err != nil {
			return nil, false, err
		}
		type child struct{ id, text string }
		var children []child
		for rows.Next() {
			var c child
			if err := rows.Scan(&c.id, &c.text); err != nil {
				_ = rows.Close()
				return nil, false, err
			}
			children = append(children, c)
		}
		if err := rows.Close(); err != nil {
			return nil, false, err
		}
		position := -1
		for n, child := range children {
			if child.id == results[i].NodeID {
				position = n
				break
			}
		}
		if position < 0 {
			continue
		}
		parts := make([]string, 0, 3)
		if parentTitle = strings.TrimSpace(parentTitle); parentTitle != "" {
			parts = append(parts, parentTitle)
		}
		if position > 0 {
			parts = append(parts, "[preceding] "+strings.TrimSpace(children[position-1].text))
		}
		parts = append(parts, strings.TrimSpace(children[position].text))
		if position+1 < len(children) {
			parts = append(parts, "[following] "+strings.TrimSpace(children[position+1].text))
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if text != "" && text != results[i].Snippet {
			results[i].Snippet = text
			changed = true
		}
	}
	return results, changed, nil
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

func contextPackText(result SearchResult) string {
	switch result.ResultType {
	case "table_row":
		return strings.TrimSpace(strings.Join(nonEmptyStrings(result.Summary, result.Snippet, result.Claim), "\n"))
	case "card":
		return strings.TrimSpace(strings.Join(nonEmptyStrings(result.Claim, result.Summary, result.Snippet), "\n"))
	case "fact":
		fact := strings.TrimSpace(strings.Join(nonEmptyStrings(result.Subject, result.Predicate, result.Object), " "))
		return strings.TrimSpace(strings.Join(nonEmptyStrings(fact, result.Snippet), "\n"))
	default:
		return strings.TrimSpace(strings.Join(nonEmptyStrings(result.Snippet, result.Summary, result.Claim), "\n"))
	}
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
