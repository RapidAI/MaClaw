package knowledge

import (
	"context"
	"sort"
	"strings"
)

func (s *SQLiteStore) TopicRelevance(ctx context.Context, opts SearchOptions) (TopicRelevanceReport, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}
	terms := topicRelevanceTerms(opts)
	report := TopicRelevanceReport{
		TopicHint: strings.TrimSpace(opts.TopicHint),
		Query:     strings.TrimSpace(opts.Query),
		Terms:     terms,
		Notes:     []string{"local_topic_relevance_no_llm", "metadata_labels_cards_facts_nodes"},
	}
	if len(terms) == 0 {
		report.Notes = append(report.Notes, "empty_topic_terms")
		return report, nil
	}
	sourceLimit := opts.Limit * 8
	if sourceLimit < 100 {
		sourceLimit = 100
	}
	if sourceLimit > 1000 {
		sourceLimit = 1000
	}
	sources, err := s.suggestionSources(ctx, opts, sourceLimit)
	if err != nil {
		return TopicRelevanceReport{}, err
	}
	items := make([]TopicRelevanceSource, 0, len(sources))
	for _, source := range sources {
		item, err := s.topicRelevanceSource(ctx, source, terms)
		if err != nil {
			return TopicRelevanceReport{}, err
		}
		if item.Score <= 0 {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].Source.UpdatedAt.Equal(items[j].Source.UpdatedAt) {
			return sourceCitationLabel(items[i].Source) < sourceCitationLabel(items[j].Source)
		}
		return items[i].Source.UpdatedAt.After(items[j].Source.UpdatedAt)
	})
	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	report.Count = len(items)
	report.Sources = items
	return report, nil
}

func (s *SQLiteStore) topicRelevanceSource(ctx context.Context, source Source, terms []string) (TopicRelevanceSource, error) {
	item := TopicRelevanceSource{Source: source}
	termHits := make(map[string]struct{})
	sourceText := strings.ToLower(strings.Join([]string{
		source.Title,
		source.SiteName,
		source.URI,
		source.CanonicalURI,
		source.RelativePath,
		source.TopicHint,
	}, "\n"))
	for _, term := range terms {
		if strings.Contains(sourceText, term) {
			item.SourceHits++
			termHits[term] = struct{}{}
		}
		for _, label := range source.Labels {
			if strings.Contains(strings.ToLower(label), term) {
				item.LabelMatches = appendUniqueString(item.LabelMatches, label)
				termHits[term] = struct{}{}
			}
		}
	}
	cardHits, cardTerms, err := s.topicTermHitCount(ctx, "knowledge_cards", source.ID, terms, []string{"title", "claim", "summary", "topics_json", "tags_json"})
	if err != nil {
		return item, err
	}
	factHits, factTerms, err := s.topicTermHitCount(ctx, "knowledge_facts", source.ID, terms, []string{"subject", "predicate", "object"})
	if err != nil {
		return item, err
	}
	nodeHits, nodeTerms, err := s.topicTermHitCount(ctx, "document_nodes", source.ID, terms, []string{"title", "text", "sheet_name", "row_range", "col_range"})
	if err != nil {
		return item, err
	}
	item.CardHits = cardHits
	item.FactHits = factHits
	item.NodeHits = nodeHits
	for _, term := range append(append(cardTerms, factTerms...), nodeTerms...) {
		termHits[term] = struct{}{}
	}
	for term := range termHits {
		item.MatchedTerms = append(item.MatchedTerms, term)
	}
	sort.Strings(item.MatchedTerms)
	sort.Strings(item.LabelMatches)
	item.Score = float64(item.SourceHits)*1.0 + float64(len(item.LabelMatches))*1.2 + float64(item.CardHits)*0.8 + float64(item.FactHits)*0.6 + float64(item.NodeHits)*0.35
	if source.TopicHint != "" {
		for _, term := range terms {
			if strings.Contains(strings.ToLower(source.TopicHint), term) {
				item.Score += 1.5
				break
			}
		}
	}
	if source.SourceTrust > 0 {
		item.Score += source.SourceTrust * 0.2
	}
	return item, nil
}

func (s *SQLiteStore) topicTermHitCount(ctx context.Context, table, sourceID string, terms []string, columns []string) (int, []string, error) {
	if len(terms) == 0 || len(columns) == 0 {
		return 0, nil, nil
	}
	expr := make([]string, 0, len(columns))
	for _, column := range columns {
		expr = append(expr, "COALESCE("+column+", '')")
	}
	textExpr := "LOWER(" + strings.Join(expr, " || ' ' || ") + ")"
	matchedTerms := make([]string, 0)
	total := 0
	for _, term := range terms {
		var count int
		err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE source_id = ? AND "+textExpr+" LIKE ?", sourceID, "%"+term+"%").Scan(&count)
		if err != nil {
			return 0, nil, err
		}
		if count > 0 {
			total += count
			matchedTerms = append(matchedTerms, term)
		}
	}
	return total, matchedTerms, nil
}

func topicRelevanceTerms(opts SearchOptions) []string {
	values := []string{opts.TopicHint}
	values = append(values, opts.ContextTerms...)
	if strings.TrimSpace(opts.TopicHint) == "" {
		values = append(values, opts.Query)
	}
	terms := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, term := range topicSplitter.Split(value, -1) {
			term = strings.ToLower(strings.TrimSpace(term))
			if len([]rune(term)) < 2 {
				continue
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
			if len(terms) >= 16 {
				return terms
			}
		}
	}
	return terms
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}
