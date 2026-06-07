package memory

import (
	"sort"
	"time"
)

// RecallExhaustive returns all entries matching the query above the minimum
// relevance threshold (FusionScore > 0.10), up to hard caps on entry count
// (100) and token budget (15000). It uses the same multi-signal fusion scoring,
// OwnerID isolation, and category exclusion as RecallDynamic.
//
// Truncation order:
//  1. By entry count: keep top 100 by fusion score
//  2. By token budget: remove lowest-scored entries until total tokens <= 15000
//
// When either cap causes entries to be dropped, Truncated is set to true and
// TotalMatching reflects the total number of entries above the relevance threshold
// before truncation.
func (s *Store) RecallExhaustive(query string, category Category, projectPath string, ownerID ...string) *ExhaustiveResult {
	const minRelevanceThreshold = 0.10

	// === Phase 1: Query Understanding ===
	expanded := ExpandQuery(query)
	bm25Scores := s.multiQueryBM25(query, expanded.Entities)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))

	// Semantic graph scores (same as RecallDynamic).
	semanticScores := map[string]float64{}
	if s.semanticGraph != nil {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:             time.Now(),
			MaxHits:         30,
			MaxVisitedFacts: 500,
			OwnerID:         firstOwnerID(ownerID...),
			ProjectPath:     projectPath,
			TemporalMode:    temporalMode,
			AsOf:            asOf,
			RelationHints:   semanticRelationHintsFromQuery(query, expanded),
			SeedWeights:     semanticSeedWeightsFromEntities(expanded.Entities),
		}) {
			semanticScores[hit.EntryID] = hit.Score
		}
	}

	// Multi-hop inference boost (same as RecallDynamic).
	if s.inferenceEngine != nil && len(expanded.Entities) > 0 {
		derivedFacts := s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
			Now:             time.Now(),
			OwnerID:         firstOwnerID(ownerID...),
			ProjectPath:     projectPath,
			MaxDerived:      10,
			MinConfidence:   0.50,
			MaxVisitedFacts: 200,
		})
		for _, df := range derivedFacts {
			for _, sf := range df.SourceFacts {
				if sf.EntryID != "" {
					semanticScores[sf.EntryID] += df.Confidence * 1.5
				}
			}
		}
	}

	// === Phase 2: Candidate filtering and scoring (under RLock) ===
	s.mu.RLock()

	projectLower := semanticNormalizeProjectPath(projectPath)
	now := time.Now()
	filterOwner := firstOwnerID(ownerID...)

	type rawCandidate struct {
		entry Entry
		bm25  float64
		vec   float64
		sem   float64
	}
	var raw []rawCandidate

	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		// Apply same filtering as RecallDynamic: OwnerID isolation, category
		// matching, and project scope filtering. For exhaustive mode (user-initiated),
		// use toolRecallExcludeCategories which is less restrictive than
		// proactiveRecallExcludeCategories — user_fact and self_identity are
		// legitimate recall targets when the user explicitly requests exhaustive results.
		if !recallDynamicEntryAllowedWithExclusions(e, category, projectLower, filterOwner, toolRecallExcludeCategories) {
			continue
		}
		b := bm25Scores[e.ID]
		v := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			v = vs
		}
		raw = append(raw, rawCandidate{entry: e, bm25: b, vec: v, sem: semanticScores[e.ID]})
	}

	// === Phase 3: Three-way RRF fusion (BM25 + Vec + Tag) ===
	bm25Arr := make([]float64, len(raw))
	vecArr := make([]float64, len(raw))
	entryArr := make([]Entry, len(raw))
	for i, c := range raw {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, expanded.QueryTokens)

	// Compute final fusion score per candidate.
	type scoredCandidate struct {
		entry      Entry
		fusionScore float64
	}
	var scored []scoredCandidate

	for i, c := range raw {
		fusedRelevance := rrfScores[i]
		if c.sem > 0 {
			fusedRelevance += c.sem
		}
		sc := memoryStreamScore(c.entry, fusedRelevance, c.bm25, projectLower, now)

		// Tag exact match boost (same as RecallDynamic).
		if len(expanded.Entities) > 0 {
			sc += tagExactMatchBoost(c.entry, expanded.Entities)
		}

		// Stability boost/penalty (same as RecallDynamic).
		sc += c.entry.Stability.StabilityBoost()

		scored = append(scored, scoredCandidate{entry: c.entry, fusionScore: sc})
	}

	s.mu.RUnlock()

	// === Phase 4: Filter by minimum relevance threshold ===
	var aboveThreshold []scoredCandidate
	for _, sc := range scored {
		if sc.fusionScore > minRelevanceThreshold {
			aboveThreshold = append(aboveThreshold, sc)
		}
	}

	totalMatching := len(aboveThreshold)

	// Sort by fusion score descending.
	sort.SliceStable(aboveThreshold, func(i, j int) bool {
		return aboveThreshold[i].fusionScore > aboveThreshold[j].fusionScore
	})

	// === Phase 5: Truncation — entry count cap first ===
	truncated := false
	if len(aboveThreshold) > exhaustiveMaxEntries {
		aboveThreshold = aboveThreshold[:exhaustiveMaxEntries]
		truncated = true
	}

	// === Phase 6: Truncation — token budget (remove lowest-scored entries) ===
	totalTokens := 0
	for _, sc := range aboveThreshold {
		totalTokens += EstimateTextTokens(sc.entry.Content)
	}

	if totalTokens > exhaustiveMaxTokens {
		truncated = true
		// Remove entries from the tail (lowest-scored) until within budget.
		for len(aboveThreshold) > 0 && totalTokens > exhaustiveMaxTokens {
			last := aboveThreshold[len(aboveThreshold)-1]
			totalTokens -= EstimateTextTokens(last.entry.Content)
			aboveThreshold = aboveThreshold[:len(aboveThreshold)-1]
		}
	}

	// === Phase 7: Build result ===
	entries := make([]Entry, len(aboveThreshold))
	for i, sc := range aboveThreshold {
		entries[i] = sc.entry
	}

	return &ExhaustiveResult{
		Entries:       entries,
		Truncated:     truncated,
		TotalMatching: totalMatching,
	}
}
