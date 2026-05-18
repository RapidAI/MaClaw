package memory

import (
	"sort"

	corebm25 "github.com/RapidAI/CodeClaw/corelib/bm25"
)

// RecallTrace captures the retrieval-side signals for the most recent recall.
// It is intentionally compact and contains IDs/counts rather than entry bodies.
type RecallTrace struct {
	Query          string         `json:"query"`
	Category       Category       `json:"category,omitempty"`
	ProjectPath    string         `json:"project_path,omitempty"`
	Entities       []string       `json:"entities,omitempty"`
	QueryTokens    []string       `json:"query_tokens,omitempty"`
	BM25Tokens     []string       `json:"bm25_tokens,omitempty"`
	BM25Hits       int            `json:"bm25_hits"`
	VectorHits     int            `json:"vector_hits"`
	SemanticHits   int            `json:"semantic_hits"`
	CandidateCount int            `json:"candidate_count"`
	ResultEntryIDs []string       `json:"result_entry_ids,omitempty"`
	SourceCounts   map[string]int `json:"source_counts,omitempty"`
}

func newRecallTrace(query string, category Category, projectPath string, expanded ExpandResult, bm25Scores, vecScores, semanticScores map[string]float64, candidates []recallScored, results []Entry) RecallTrace {
	trace := RecallTrace{
		Query:          query,
		Category:       category,
		ProjectPath:    projectPath,
		Entities:       append([]string(nil), expanded.Entities...),
		QueryTokens:    append([]string(nil), expanded.QueryTokens...),
		BM25Tokens:     corebm25.Tokenize(query),
		BM25Hits:       len(bm25Scores),
		VectorHits:     len(vecScores),
		SemanticHits:   len(semanticScores),
		CandidateCount: len(candidates),
		SourceCounts:   make(map[string]int),
	}
	for _, entry := range results {
		if entry.ID != "" {
			trace.ResultEntryIDs = append(trace.ResultEntryIDs, entry.ID)
		}
		source := entry.SourceType
		if source == "" {
			source = string(entry.Category)
		}
		trace.SourceCounts[source]++
	}
	if len(trace.SourceCounts) == 0 {
		trace.SourceCounts = nil
	}
	return trace
}

// LastRecallTrace returns diagnostics for the most recent RecallDynamic call.
func (s *Store) LastRecallTrace() RecallTrace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRecallTrace(s.lastRecallTrace)
}

func cloneRecallTrace(in RecallTrace) RecallTrace {
	out := in
	out.Entities = append([]string(nil), in.Entities...)
	out.QueryTokens = append([]string(nil), in.QueryTokens...)
	out.BM25Tokens = append([]string(nil), in.BM25Tokens...)
	out.ResultEntryIDs = append([]string(nil), in.ResultEntryIDs...)
	if len(in.SourceCounts) > 0 {
		out.SourceCounts = make(map[string]int, len(in.SourceCounts))
		keys := make([]string, 0, len(in.SourceCounts))
		for key := range in.SourceCounts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out.SourceCounts[key] = in.SourceCounts[key]
		}
	}
	return out
}
