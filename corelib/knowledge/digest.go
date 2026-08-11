package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) SourceDigest(ctx context.Context, sourceID string, nodeLimit int, cardLimit int, factLimit int, linkLimit int) (SourceDigestResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceDigestResult{}, fmt.Errorf("source id is required")
	}
	nodeLimit = boundedDigestLimit(nodeLimit, 8, 50)
	cardLimit = boundedDigestLimit(cardLimit, 8, 100)
	factLimit = boundedDigestLimit(factLimit, 12, 200)
	linkLimit = boundedDigestLimit(linkLimit, 8, 100)
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceDigestResult{}, err
	}
	nodes, err := s.ListNodesBySource(ctx, sourceID, nodeLimit)
	if err != nil {
		return SourceDigestResult{}, err
	}
	cards, err := s.ListCardsBySource(ctx, sourceID, cardLimit)
	if err != nil {
		return SourceDigestResult{}, err
	}
	facts, err := s.ListFactsBySource(ctx, sourceID, factLimit)
	if err != nil {
		return SourceDigestResult{}, err
	}
	links, err := s.ListSourceLinks(ctx, sourceID, linkLimit)
	if err != nil {
		return SourceDigestResult{}, err
	}
	timeline, err := s.SourceTimeline(ctx, sourceID, 12)
	if err != nil {
		return SourceDigestResult{}, err
	}
	result := SourceDigestResult{
		SourceID:      sourceID,
		Source:        source,
		Title:         sourceCitationLabel(source),
		Labels:        append([]string(nil), source.Labels...),
		NodeCount:     source.NodeCount,
		CardCount:     source.CardCount,
		FactCount:     source.FactCount,
		LinkCount:     len(links),
		TimelineCount: timeline.Count,
		Nodes:         nodes,
		Cards:         cards,
		Facts:         facts,
		Links:         links,
		Timeline:      timeline.Events,
		Notes:         []string{"local_source_digest_no_llm", "query_does_not_require_llm"},
		GeneratedAt:   time.Now().UTC(),
	}
	if result.NodeCount == 0 {
		result.NodeCount = len(nodes)
	}
	if result.CardCount == 0 {
		result.CardCount = len(cards)
	}
	if result.FactCount == 0 {
		result.FactCount = len(facts)
	}
	result.Topics, result.Entities, result.Tags = sourceDigestTerms(cards)
	return result, nil
}

func boundedDigestLimit(value, fallback, max int) int {
	if value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return value
}

func sourceDigestTerms(cards []Card) ([]string, []string, []string) {
	topics := make([]string, 0)
	entities := make([]string, 0)
	tags := make([]string, 0)
	seenTopics := map[string]struct{}{}
	seenEntities := map[string]struct{}{}
	seenTags := map[string]struct{}{}
	for _, card := range cards {
		for _, topic := range card.Topics {
			appendDigestTerm(&topics, seenTopics, topic, 16)
		}
		for _, entity := range card.Entities {
			appendDigestTerm(&entities, seenEntities, entity, 16)
		}
		for _, tag := range card.Tags {
			appendDigestTerm(&tags, seenTags, tag, 16)
		}
	}
	return topics, entities, tags
}

func appendDigestTerm(out *[]string, seen map[string]struct{}, value string, limit int) {
	value = strings.TrimSpace(value)
	if value == "" || len(*out) >= limit {
		return
	}
	key := strings.ToLower(value)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, value)
}
