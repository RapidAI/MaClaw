package memory

import (
	"log"
	"sort"
	"time"
)

const recallRLockTimeout = 10 * time.Second

// recallSignals is the index-side evidence gathered before the hot-set lock.
type recallSignals struct {
	now            time.Time
	expanded       ExpandResult
	aliasExpanded  []string
	bm25Scores     map[string]float64
	vecScores      map[string]float64
	semanticScores map[string]float64
	semanticHits   map[string]SemanticSearchHit
	derivedFacts   []DerivedFact
	nameAnchors    []string
}

// prepareRecallSignals runs query expansion and the BM25, vector, and graph
// indexes. Those indexes are safe to read without the entry-slice lock.
func (s *Store) prepareRecallSignals(query, projectPath, ownerID string) recallSignals {
	now := time.Now()
	expanded := ExpandQuery(query)
	aliasExpanded := expanded.Entities
	if s.aliasIndex != nil && len(expanded.Entities) > 0 {
		aliases := s.aliasIndex.ExpandForOwner(expanded.Entities, ownerID)
		if len(aliases) > 0 {
			aliasExpanded = append(append([]string(nil), expanded.Entities...), aliases...)
		}
	}

	bm25Scores := s.multiQueryBM25(query, aliasExpanded)
	nameAnchors := strictRecallAnchors(query)
	vecScores := map[string]float64{}
	if len(nameAnchors) == 0 {
		vecScores = s.vecIndex.score(s.queryEmbeddingCached(query))
	}
	semanticScores := map[string]float64{}
	semanticHits := map[string]SemanticSearchHit{}
	if s.semanticGraph != nil && len(nameAnchors) == 0 {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:             now,
			AsOf:            asOf,
			OwnerID:         ownerID,
			ProjectPath:     projectPath,
			RelationHints:   semanticRelationHintsFromQuery(query, expanded),
			SeedWeights:     semanticSeedWeightsFromEntities(expanded.Entities),
			MaxHits:         30,
			MaxVisitedFacts: 500,
			TemporalMode:    temporalMode,
		}) {
			semanticScores[hit.EntryID] = hit.Score
			semanticHits[hit.EntryID] = hit
		}
	}
	var derivedFacts []DerivedFact
	if s.inferenceEngine != nil && len(expanded.Entities) > 0 && len(nameAnchors) == 0 {
		derivedFacts = s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
			Now:             now,
			OwnerID:         ownerID,
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
	return recallSignals{
		now:            now,
		expanded:       expanded,
		aliasExpanded:  aliasExpanded,
		bm25Scores:     bm25Scores,
		vecScores:      vecScores,
		semanticScores: semanticScores,
		semanticHits:   semanticHits,
		derivedFacts:   derivedFacts,
		nameAnchors:    nameAnchors,
	}
}

// scoreRecallCandidatesLocked ranks the active entries the filter allows.
// Caller must hold s.mu read lock. Pagination and RecallDynamic both use it.
func (s *Store) scoreRecallCandidatesLocked(signals recallSignals, query string, category Category, projectPath, filterOwner string, opts recallFilterOptions, themeRerank bool) []recallScored {
	projectLower := semanticNormalizeProjectPath(projectPath)
	expanded := signals.expanded
	aliasExpanded := signals.aliasExpanded

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
		if opts.strictProject && projectLower != "" {
			if !recallDynamicEntryAllowedStrict(e, category, projectLower, filterOwner) {
				continue
			}
		} else if !recallDynamicEntryAllowedWithExclusions(e, category, projectLower, filterOwner, opts.excludeWhenNoCategory) {
			continue
		}
		if len(signals.nameAnchors) > 0 && !entryMentionsAnchors(e, signals.nameAnchors) {
			continue
		}
		raw = append(raw, rawCandidate{
			entry: e,
			bm25:  signals.bm25Scores[e.ID],
			vec:   signals.vecScores[e.ID],
			sem:   signals.semanticScores[e.ID],
		})
	}

	bm25Arr := make([]float64, len(raw))
	vecArr := make([]float64, len(raw))
	entryArr := make([]Entry, len(raw))
	for i, c := range raw {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, expanded.QueryTokens)

	candidates := make([]recallScored, 0, len(raw))
	for i, c := range raw {
		fusedRelevance := rrfScores[i]
		if c.sem > 0 {
			fusedRelevance += c.sem
		}
		sc := memoryStreamScore(c.entry, fusedRelevance, c.bm25, projectLower, signals.now)
		candidates = append(candidates, recallScored{entry: c.entry, score: sc})
	}
	if len(expanded.Entities) > 0 {
		for i := range candidates {
			candidates[i].score += tagExactMatchBoost(candidates[i].entry, expanded.Entities)
		}
	}
	if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
		aliasOnly := aliasExpanded[len(expanded.Entities):]
		for i := range candidates {
			boost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
			if boost > 0 {
				if boost > AliasMatchBoost {
					boost = AliasMatchBoost
				}
				candidates[i].score += boost
			}
		}
	}
	for i := range candidates {
		candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	preExpandLen := len(candidates)
	candidates = s.graphExpand(candidates, graphExpandSeeds)
	if len(candidates) > preExpandLen {
		var aliasOnly []string
		if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
			aliasOnly = aliasExpanded[len(expanded.Entities):]
		}
		for i := preExpandLen; i < len(candidates); i++ {
			if len(expanded.Entities) > 0 {
				candidates[i].score += tagExactMatchBoost(candidates[i].entry, expanded.Entities)
			}
			if len(aliasOnly) > 0 {
				aliasBoost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
				if aliasBoost > AliasMatchBoost {
					aliasBoost = AliasMatchBoost
				}
				candidates[i].score += aliasBoost
			}
			candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
		}
	}
	if opts.strictProject && projectLower != "" {
		candidates = filterRecallDynamicCandidatesStrict(candidates, category, projectLower, filterOwner)
	} else {
		candidates = filterRecallDynamicCandidatesWithExclusions(candidates, category, projectLower, filterOwner, opts.excludeWhenNoCategory)
	}
	candidates = filterRecallByAnchors(candidates, signals.nameAnchors)
	if themeRerank && ClassifyComplexity(query, expanded.Entities, nil) != ComplexitySimple && s.themeManager != nil {
		candidates = themeAwareDiversityRerank(candidates, s.themeManager.Themes(), graphExpandSeeds)
	}
	applyTemporalDemotion(candidates, signals.now)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates
}

func recallLockTimeoutTrace(query string, category Category, projectPath, ownerID string, signals recallSignals) RecallTrace {
	trace := newRecallTrace(query, category, projectPath, signals.expanded, signals.bm25Scores, signals.vecScores, signals.semanticScores, nil, nil)
	trace.LockTimedOut = true
	log.Printf("[memory_store] RecallDynamic: RLock timeout after %s — recall failed for owner %q (pipeline may be holding write lock)", recallRLockTimeout, ownerID)
	return trace
}
