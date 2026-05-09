package knowledge

import (
	"context"
	"fmt"
	"strings"
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
	pack := ContextPackResult{
		Query:     query,
		Items:     make([]ContextPackItem, 0, maxItems),
		Citations: make([]Citation, 0, maxItems),
		Notes:     []string{"local_context_pack_no_llm", "card_fact_node_ranked", "budgeted_context"},
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
		pack.Items = append(pack.Items, item)
		pack.CharacterCount += len([]rune(text))
		citation := citationFromResult(result)
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

func contextPackTitle(result SearchResult) string {
	for _, candidate := range []string{result.CardTitle, result.NodeTitle, result.Source.Title, result.Source.RelativePath, result.Source.CanonicalURI, result.Source.URI} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return result.ResultType
}

func contextPackText(result SearchResult) string {
	switch result.ResultType {
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
