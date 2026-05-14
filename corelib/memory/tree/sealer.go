package tree

// Sealer: periodic summarization of tree nodes into higher levels.
// L0 chunks → L1 daily summaries → L2 weekly → L3 monthly.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Sealer manages the periodic summarization of tree nodes.
type Sealer struct {
	store      TreeStore
	summarizer Summarizer
	config     SealConfig
}

// NewSealer creates a sealer with the given store and summarizer.
func NewSealer(store TreeStore, summarizer Summarizer, config SealConfig) *Sealer {
	return &Sealer{
		store:      store,
		summarizer: summarizer,
		config:     config,
	}
}

// SealDaily creates L1 daily summaries from unsealed L0 chunks for a given date.
// Returns the number of L1 nodes created.
func (s *Sealer) SealDaily(date time.Time) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}

	dayStart := startOfDay(date)
	dayEnd := dayStart.Add(24 * time.Hour)

	chunks, err := s.store.ListByLevelAndDate(LevelChunk, dayStart, dayEnd)
	if err != nil {
		return 0, fmt.Errorf("list chunks for %s: %w", dayStart.Format("2006-01-02"), err)
	}

	if len(chunks) < s.config.MinChunksForDaily {
		return 0, nil // not enough chunks to seal
	}

	// Group chunks by source
	groups := groupBySource(chunks)
	created := 0

	for source, sourceChunks := range groups {
		if len(sourceChunks) < 2 {
			continue // need at least 2 chunks to summarize
		}

		summary, err := s.summarizeNodes(sourceChunks)
		if err != nil {
			log.Printf("[tree-sealer] failed to summarize %d chunks for %s/%s: %v",
				len(sourceChunks), dayStart.Format("2006-01-02"), source, err)
			continue
		}

		childIDs := make([]string, len(sourceChunks))
		for i, c := range sourceChunks {
			childIDs[i] = c.ID
		}

		node := &TreeNode{
			ID:         fmt.Sprintf("L1-%s-%s-%d", dayStart.Format("20060102"), source, created),
			Level:      LevelDaily,
			Content:    summary,
			Source:     source,
			Topic:      inferTopic(sourceChunks),
			Children:   childIDs,
			CreatedAt:  time.Now(),
			SealedAt:   time.Now(),
			TokenCount: estimateTokens(summary),
			Tags:       collectTags(sourceChunks),
		}

		if err := s.store.Save(node); err != nil {
			log.Printf("[tree-sealer] failed to save L1 node: %v", err)
			continue
		}

		// Update children's parent
		for _, child := range sourceChunks {
			child.ParentID = node.ID
			_ = s.store.Save(child)
		}

		created++
	}

	if created > 0 {
		log.Printf("[tree-sealer] sealed %d L1 daily nodes for %s", created, dayStart.Format("2006-01-02"))
	}
	return created, nil
}

// SealWeekly creates L2 weekly summaries from L1 daily nodes for a given week.
func (s *Sealer) SealWeekly(weekStart time.Time) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}

	weekEnd := weekStart.Add(7 * 24 * time.Hour)
	dailies, err := s.store.ListByLevelAndDate(LevelDaily, weekStart, weekEnd)
	if err != nil {
		return 0, fmt.Errorf("list dailies for week %s: %w", weekStart.Format("2006-01-02"), err)
	}

	if len(dailies) < s.config.MinDailiesForWeekly {
		return 0, nil
	}

	summary, err := s.summarizeNodes(dailies)
	if err != nil {
		return 0, fmt.Errorf("summarize week %s: %w", weekStart.Format("2006-01-02"), err)
	}

	childIDs := make([]string, len(dailies))
	for i, d := range dailies {
		childIDs[i] = d.ID
	}

	node := &TreeNode{
		ID:         fmt.Sprintf("L2-%s", weekStart.Format("20060102")),
		Level:      LevelWeekly,
		Content:    summary,
		Source:     "mixed",
		Topic:      inferTopic(dailies),
		Children:   childIDs,
		CreatedAt:  time.Now(),
		SealedAt:   time.Now(),
		TokenCount: estimateTokens(summary),
		Tags:       collectTags(dailies),
	}

	if err := s.store.Save(node); err != nil {
		return 0, err
	}

	for _, daily := range dailies {
		daily.ParentID = node.ID
		_ = s.store.Save(daily)
	}

	log.Printf("[tree-sealer] sealed L2 weekly node for week %s (%d dailies)", weekStart.Format("2006-01-02"), len(dailies))
	return 1, nil
}

// SealMonthly creates L3 monthly summaries from L2 weekly nodes.
func (s *Sealer) SealMonthly(monthStart time.Time) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}

	monthEnd := monthStart.AddDate(0, 1, 0)
	weeklies, err := s.store.ListByLevelAndDate(LevelWeekly, monthStart, monthEnd)
	if err != nil {
		return 0, fmt.Errorf("list weeklies for month %s: %w", monthStart.Format("2006-01"), err)
	}

	if len(weeklies) < s.config.MinWeekliesForMonthly {
		return 0, nil
	}

	summary, err := s.summarizeNodes(weeklies)
	if err != nil {
		return 0, fmt.Errorf("summarize month %s: %w", monthStart.Format("2006-01"), err)
	}

	childIDs := make([]string, len(weeklies))
	for i, w := range weeklies {
		childIDs[i] = w.ID
	}

	node := &TreeNode{
		ID:         fmt.Sprintf("L3-%s", monthStart.Format("200601")),
		Level:      LevelMonthly,
		Content:    summary,
		Source:     "mixed",
		Topic:      inferTopic(weeklies),
		Children:   childIDs,
		CreatedAt:  time.Now(),
		SealedAt:   time.Now(),
		TokenCount: estimateTokens(summary),
		Tags:       collectTags(weeklies),
	}

	if err := s.store.Save(node); err != nil {
		return 0, err
	}

	log.Printf("[tree-sealer] sealed L3 monthly node for %s (%d weeklies)", monthStart.Format("2006-01"), len(weeklies))
	return 1, nil
}

// --- Helpers ---

func (s *Sealer) summarizeNodes(nodes []*TreeNode) (string, error) {
	if s.summarizer == nil {
		// No LLM available — concatenate and truncate
		return concatenateAndTruncate(nodes, s.config.MaxSummaryTokens), nil
	}

	content := concatenateForSummary(nodes)
	summary, err := s.summarizer(content)
	if err != nil {
		// Fallback to concatenation on LLM failure
		return concatenateAndTruncate(nodes, s.config.MaxSummaryTokens), nil
	}
	return summary, nil
}

func concatenateForSummary(nodes []*TreeNode) string {
	var sb strings.Builder
	for i, n := range nodes {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		if n.Topic != "" {
			sb.WriteString("[" + n.Topic + "] ")
		}
		sb.WriteString(n.Content)
	}
	return sb.String()
}

func concatenateAndTruncate(nodes []*TreeNode, maxTokens int) string {
	combined := concatenateForSummary(nodes)
	maxRunes := maxTokens * 2 // rough: 2 runes per token for Chinese
	runes := []rune(combined)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "\n..."
	}
	return combined
}

func groupBySource(nodes []*TreeNode) map[string][]*TreeNode {
	groups := make(map[string][]*TreeNode)
	for _, n := range nodes {
		source := n.Source
		if source == "" {
			source = "unknown"
		}
		groups[source] = append(groups[source], n)
	}
	return groups
}

func inferTopic(nodes []*TreeNode) string {
	// Use the most common topic among children
	counts := make(map[string]int)
	for _, n := range nodes {
		if n.Topic != "" {
			counts[n.Topic]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	best := ""
	bestCount := 0
	for topic, count := range counts {
		if count > bestCount {
			best = topic
			bestCount = count
		}
	}
	return best
}

func collectTags(nodes []*TreeNode) []string {
	tagSet := make(map[string]bool)
	for _, n := range nodes {
		for _, tag := range n.Tags {
			tagSet[tag] = true
		}
	}
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func estimateTokens(s string) int {
	// Chinese: ~1.5 chars per token; English: ~4 chars per token
	// Use a middle ground of ~2 chars per token
	return utf8.RuneCountInString(s) / 2
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
