package memory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type EmbedStatus struct {
	TotalEntries      int    `json:"total_entries"`
	WithEmbeddings    int    `json:"with_embeddings"`
	WithoutEmbeddings int    `json:"without_embeddings"`
	EmbedderType      string `json:"embedder_type"`
	ModelPath         string `json:"model_path"`
}

type GraphNeighborSnapshot struct {
	ID       string  `json:"id"`
	Strength float64 `json:"strength"`
	Content  string  `json:"content"`
}

type StrengthSnapshot struct {
	Entry           Entry   `json:"entry"`
	CurrentStrength float64 `json:"current_strength"`
	Dormant         bool    `json:"dormant"`
}

type InferenceResult struct {
	QueryEntities []string      `json:"query_entities,omitempty"`
	Derived       []DerivedFact `json:"derived,omitempty"`
	GraphEntities int           `json:"graph_entities"`
	GraphFacts    int           `json:"graph_facts"`
	RuleCount     int           `json:"rule_count"`
	Unavailable   bool          `json:"unavailable,omitempty"`
}

func (s *Store) EmbedStatusForTool() EmbedStatus {
	status := EmbedStatus{EmbedderType: "Noop", ModelPath: "(none)"}
	if s == nil {
		return status
	}
	entries := s.AllEntries()
	status.TotalEntries = len(entries)
	for _, entry := range entries {
		if len(entry.Embedding) > 0 {
			status.WithEmbeddings++
		}
	}
	status.WithoutEmbeddings = status.TotalEntries - status.WithEmbeddings
	if embedder := s.Embedder(); embedder != nil && !embedding.IsNoop(embedder) {
		status.EmbedderType = "Gemma"
		status.ModelPath = embedding.DefaultModelPath()
	}
	return status
}

func FormatEmbedStatusForTool(status EmbedStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Embedding Status:\n")
	fmt.Fprintf(&b, "  Total entries:           %d\n", status.TotalEntries)
	fmt.Fprintf(&b, "  With embeddings:         %d\n", status.WithEmbeddings)
	fmt.Fprintf(&b, "  Without embeddings:      %d\n", status.WithoutEmbeddings)
	fmt.Fprintf(&b, "  Embedder type:           %s\n", status.EmbedderType)
	fmt.Fprintf(&b, "  Model path:              %s\n", status.ModelPath)
	return b.String()
}

func (s *Store) GraphNeighborsForTool(id string) []GraphNeighborSnapshot {
	if s == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	entries := s.AllEntries()
	entryByID := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		entryByID[entry.ID] = entry
	}
	neighbors := s.GraphNeighbors(id)
	sorted := make([]GraphNeighborSnapshot, 0, len(neighbors))
	for neighborID, strength := range neighbors {
		content := "(not found)"
		if entry, ok := entryByID[neighborID]; ok {
			content = previewForTool(entry.Content, 36)
		}
		sorted = append(sorted, GraphNeighborSnapshot{ID: neighborID, Strength: strength, Content: content})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Strength > sorted[j].Strength
	})
	return sorted
}

func FormatGraphNeighborsForTool(id string, neighbors []GraphNeighborSnapshot) string {
	if len(neighbors) == 0 {
		return fmt.Sprintf("No graph neighbors for entry %s.\n", id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Graph neighbors for %s:\n\n", id)
	fmt.Fprintf(&b, "%-26s %-10s %s\n", "NEIGHBOR", "STRENGTH", "CONTENT")
	b.WriteString(strings.Repeat("-", 76))
	b.WriteByte('\n')
	for _, neighbor := range neighbors {
		fmt.Fprintf(&b, "%-26s %-10.4f %s\n", neighbor.ID, neighbor.Strength, neighbor.Content)
	}
	return b.String()
}

func (s *Store) StrengthForTool(now time.Time) []StrengthSnapshot {
	if s == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	entries := s.AllEntries()
	items := make([]StrengthSnapshot, 0, len(entries))
	for _, entry := range entries {
		current := entry.Strength
		if current > 0 {
			hours := now.Sub(entry.UpdatedAt).Hours()
			if hours < 0 {
				hours = 0
			}
			current = entry.Strength * math.Exp(-0.003*hours)
		}
		dormant := current < 0.1 && entry.Status != StatusSuperseded && !entry.Category.IsProtected()
		items = append(items, StrengthSnapshot{Entry: entry, CurrentStrength: current, Dormant: dormant})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CurrentStrength < items[j].CurrentStrength
	})
	return items
}

func FormatStrengthForTool(items []StrengthSnapshot) string {
	if len(items) == 0 {
		return "No memories found.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-26s %-10s %-12s %-20s %s\n", "ID", "STRENGTH", "STATUS", "LAST ACCESS", "CONTENT")
	b.WriteString(strings.Repeat("-", 96))
	b.WriteByte('\n')
	for _, item := range items {
		status := string(item.Entry.Status)
		if status == "" {
			status = "active"
		}
		marker := "  "
		if item.Dormant || item.Entry.Status == StatusDormant {
			marker = "* "
		}
		fmt.Fprintf(&b, "%s%-24s %-10.4f %-12s %-20s %s\n",
			marker, item.Entry.ID, item.CurrentStrength, status, item.Entry.UpdatedAt.Format("2006-01-02 15:04"), previewForTool(item.Entry.Content, 24))
	}
	return b.String()
}

func (s *Store) InferForTool(query string, opts InferenceOptions) InferenceResult {
	result := InferenceResult{}
	if s == nil || s.InferenceEngine() == nil {
		result.Unavailable = true
		return result
	}
	expanded := ExpandQuery(query)
	result.QueryEntities = expanded.Entities
	if len(result.QueryEntities) == 0 {
		return result
	}
	if opts.MaxDerived <= 0 {
		opts.MaxDerived = 20
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.40
	}
	if opts.MaxVisitedFacts <= 0 {
		opts.MaxVisitedFacts = 200
	}
	ie := s.InferenceEngine()
	result.Derived = ie.Infer(result.QueryEntities, opts)
	result.RuleCount = len(ie.Rules())
	if sg := s.SemanticGraph(); sg != nil {
		result.GraphEntities, result.GraphFacts, _ = sg.Stats()
	}
	return result
}

func FormatInferenceResultForTool(query string, result InferenceResult) string {
	if result.Unavailable {
		return "Inference engine not available (semantic graph may be empty).\n"
	}
	if len(result.QueryEntities) == 0 {
		return fmt.Sprintf("No entities extracted from query: %q\n", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Query entities: %v\n\n", result.QueryEntities)
	if len(result.Derived) == 0 {
		b.WriteString("No derived facts found.\n")
		writeSemanticGraphStatsForTool(&b, result, false)
		return b.String()
	}
	fmt.Fprintf(&b, "Derived facts (%d):\n\n", len(result.Derived))
	fmt.Fprintf(&b, "%-20s %-15s %-20s %-10s %-30s %s\n", "SUBJECT", "PREDICATE", "OBJECT", "CONF", "RULE", "EXPLANATION")
	b.WriteString(strings.Repeat("-", 120))
	b.WriteByte('\n')
	for _, fact := range result.Derived {
		fmt.Fprintf(&b, "%-20s %-15s %-20s %-10.0f%% %-30s %s\n",
			previewForTool(fact.Subject, 18), fact.Predicate, previewForTool(fact.Object, 18), fact.Confidence*100, previewForTool(fact.RuleName, 28), fact.Explanation)
	}
	writeSemanticGraphStatsForTool(&b, result, true)
	return b.String()
}

func writeSemanticGraphStatsForTool(b *strings.Builder, result InferenceResult, includeRules bool) {
	b.WriteString("\nSemantic graph stats:\n")
	fmt.Fprintf(b, "  Entities: %d\n  Facts: %d\n", result.GraphEntities, result.GraphFacts)
	if includeRules {
		fmt.Fprintf(b, "  Rules: %d\n", result.RuleCount)
	}
}

func previewForTool(content string, maxRunes int) string {
	content = strings.ReplaceAll(content, "\n", " ")
	if maxRunes <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
